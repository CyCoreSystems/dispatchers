package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CyCoreSystems/dispatchers/kamailio"
	"github.com/CyCoreSystems/dispatchers/sets"
	"github.com/ericchiang/k8s"
	"github.com/ghodss/yaml"

	"github.com/pkg/errors"
)

var outputFilename string
var rpcPort string
var rpcHost string
var kubeCfg string

var maxShortDeaths = 10
var minRuntime = time.Minute

func init() {
	flag.Var(&setDefinitions, "set", "Dispatcher sets of the form [namespace:]name=index[:port], where index is a number and port is the port number on which SIP is to be signaled to the dispatchers.  May be passed multiple times for multiple sets.")
	flag.StringVar(&outputFilename, "o", "/data/kamailio/dispatcher.list", "Output file for dispatcher list")
	flag.StringVar(&rpcHost, "h", "128.0.0.1", "Host for kamailio's RPC service")
	flag.StringVar(&rpcPort, "p", "9998", "Port for kamailio's RPC service")
	flag.StringVar(&kubeCfg, "kubecfg", "", "Location of kubecfg file (if not running inside k8s)")
}

// SetDefinition describes a kubernetes dispatcher set's parameters
type SetDefinition struct {
	id        int
	namespace string
	name      string
	port      string
}

// SetDefinitions represents a set of kubernetes dispatcher set parameter definitions
type SetDefinitions struct {
	list []*SetDefinition
}

// String implements flag.Value
func (s *SetDefinitions) String() string {
	var list []string
	for _, d := range s.list {
		list = append(list, d.String())
	}

	return strings.Join(list, ",")
}

// Set implements flag.Value
func (s *SetDefinitions) Set(raw string) error {
	d := new(SetDefinition)

	if err := d.Set(raw); err != nil {
		return err
	}

	s.list = append(s.list, d)
	return nil
}

var setDefinitions SetDefinitions

func (s *SetDefinition) String() string {
	return fmt.Sprintf("%s:%s=%d:%s", s.namespace, s.name, s.id, s.port)
}

// Set configures a kubernetes-derived dispatcher set
func (s *SetDefinition) Set(raw string) (err error) {

	// Handle multiple comma-delimited arguments
	if strings.Contains(raw, ",") {
		args := strings.Split(raw, ",")
		for _, n := range args {
			if err = s.Set(n); err != nil {
				return err
			}
		}
		return nil
	}

	var id int
	var ns = "default"
	var name string
	var port = "5060"

	if os.Getenv("POD_NAMESPACE") != "" {
		ns = os.Getenv("POD_NAMESPACE")
	}

	pieces := strings.SplitN(raw, "=", 2)
	if len(pieces) < 2 {
		return fmt.Errorf("failed to parse %s as the form [namespace:]name=index", raw)
	}

	var naming = strings.SplitN(pieces[0], ":", 2)
	if len(naming) < 2 {
		name = naming[0]
	} else {
		ns = naming[0]
		name = naming[1]
	}

	var idString = pieces[1]
	if pieces = strings.Split(pieces[1], ":"); len(pieces) > 1 {
		idString = pieces[0]
		port = pieces[1]
	}

	id, err = strconv.Atoi(idString)
	if err != nil {
		return errors.Wrap(err, "failed to parse index as an integer")
	}

	s.id = id
	s.namespace = ns
	s.name = name
	s.port = port

	return nil
}

type dispatcherSets struct {
	kc             *k8s.Client
	outputFilename string
	rpcHost        string
	rpcPort        string

	sets map[int]sets.DispatcherSet
}

// add creates a dispatcher set from a k8s set definition
func (s *dispatcherSets) add(ctx context.Context, args *SetDefinition) error {

	ds, err := sets.NewKubernetesSet(ctx, s.kc, args.id, args.namespace, args.name, args.port)
	if err != nil {
		return errors.Wrap(err, "failed to create kubernetes-based dispatcher set")
	}

	if s.sets == nil {
		s.sets = make(map[int]sets.DispatcherSet)
	}

	// Add this set to the list of sets
	s.sets[args.id] = ds

	return nil
}

// export dumps the output from all dispatcher sets
func (s *dispatcherSets) export() error {

	f, err := os.Create(s.outputFilename)
	if err != nil {
		return errors.Wrap(err, "failed to open dispatchers file for writing")
	}
	defer f.Close() // nolint: errcheck

	for _, v := range s.sets {
		_, err = f.WriteString(v.Export())
		if err != nil {
			return errors.Wrap(err, "failed to write to dispatcher file")
		}
	}

	return nil
}

func (s *dispatcherSets) update(ctx context.Context) error {
	for _, v := range s.sets {
		_, err := v.Update(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *dispatcherSets) maintain(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	changes := make(chan error, 10)

	// Listen to each of the namespaces
	for _, v := range s.sets {
		go func(ds sets.DispatcherSet) {
			for {
				_, err := ds.Watch(ctx)
				changes <- err
			}
		}(v)
	}

	for ctx.Err() == nil {
		err := <-changes
		if err == io.EOF {
			log.Println("kubernetes API connection terminated:", err)
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "error maintaining sets")
		}

		if err = s.export(); err != nil {
			return errors.Wrap(err, "failed to export dispatcher set")
		}

		if err = s.notify(); err != nil {
			return errors.Wrap(err, "failed to notify kamailio of update")
		}
	}

	return ctx.Err()
}

// notify signals to kamailio to reload its dispatcher list
func (s *dispatcherSets) notify() error {
	return kamailio.InvokeMethod("dispatcher.reload", s.rpcHost, s.rpcPort)
}

func main() {
	flag.Parse()

	var shortDeaths int
	for shortDeaths < maxShortDeaths {
		t := time.Now()

		if err := run(); err != nil {
			log.Println("run died:", err)
		}

		if time.Since(t) < minRuntime {
			shortDeaths++
		}
	}

	log.Println("too many short-term deaths")
	os.Exit(1)
}

func run() error {
	ctx, cancel := newStopContext()
	defer cancel()

	flag.Parse()

	kc, err := connect()
	if err != nil {
		fmt.Println("failed to create k8s client:", err.Error())
		os.Exit(1)
	}

	var s = &dispatcherSets{
		kc:             kc,
		outputFilename: outputFilename,
		rpcHost:        rpcHost,
		rpcPort:        rpcPort,
	}

	for _, v := range setDefinitions.list {
		if err = s.add(ctx, v); err != nil {
			return errors.Wrap(err, "failed to add dispatcher set")
		}

	}

	if err = s.update(ctx); err != nil {
		return errors.Wrap(err, "failed to run initial dispatcher set update")
	}

	if err = s.export(); err != nil {
		return errors.Wrap(err, "failed to run initial dispatcher set export")
	}

	if err = s.maintain(ctx); err != nil {
		return errors.Wrap(err, "failed to maintain dispatcher sets")
	}

	return nil
}

func connect() (*k8s.Client, error) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return k8s.NewInClusterClient()
	}

	data, err := ioutil.ReadFile(kubeCfg) // nolint: gosec
	if err != nil {
		return nil, errors.Wrap(err, "failed to read kubecfg")
	}

	cfg := new(k8s.Config)
	if err = yaml.Unmarshal(data, cfg); err != nil {
		return nil, errors.Wrap(err, "failed to parse kubecfg")
	}

	return k8s.NewClient(cfg)
}

func newStopContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-ctx.Done():
		case <-sigs:
		}
		cancel()
	}()

	return ctx, cancel
}
