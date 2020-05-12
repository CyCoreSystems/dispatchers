package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CyCoreSystems/dispatchers/sets"
	"github.com/CyCoreSystems/go-kamailio/binrpc"
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

var apiAddr string

// KamailioStartupDebounceTimer is the amount of time to wait on startup to
// send an additional notify to kamailio.
//
// NOTE:  because we are notifying kamailio via UDP, we have no way of knowing
// if it actually received the notification.  This debounce timer is a hack to
// send a subsequent notification after kamailio should have had time to start.
// Ideally, we should instead query kamailio to validate the dispatcher list.
// However, our binrpc implementation does not yet support _reading_ from
// binrpc.
const KamailioStartupDebounceTimer = time.Minute

func init() {
	flag.Var(&setDefinitions, "set", "Dispatcher sets of the form [namespace:]name=index[:port], where index is a number and port is the port number on which SIP is to be signaled to the dispatchers.  May be passed multiple times for multiple sets.")
	flag.Var(&staticSetDefinitions, "static", "Static dispatcher sets of the form index=host[:port][,host[:port]]..., where index is the dispatcher set number/index and port is the port number on which SIP is to be signaled to the dispatchers.  Multiple hosts may be defined using a comma-separated list.")
	flag.StringVar(&outputFilename, "o", "/data/kamailio/dispatcher.list", "Output file for dispatcher list")
	flag.StringVar(&rpcHost, "h", "127.0.0.1", "Host for kamailio's RPC service")
	flag.StringVar(&rpcPort, "p", "9998", "Port for kamailio's RPC service")
	flag.StringVar(&kubeCfg, "kubecfg", "", "Location of kubecfg file (if not running inside k8s)")
	flag.StringVar(&apiAddr, "api", "", "Address on which to run web API service.  Example ':8080'. (defaults to not run)")
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

func (s *dispatcherSets) addStatic(ctx context.Context, v *StaticSetDefinition) error {
	if s.sets == nil {
		s.sets = make(map[int]sets.DispatcherSet)
	}

	s.sets[v.id] = sets.NewStaticSet(v.id, v.Members())

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

// ServeHTTP offers a web service by which clients may validate membership of an IP address within a dispatcher set or fetch a dispatcher set
func (s *dispatcherSets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle requests for /check/<setID>/<ip address> to validate membership of an IP to a dispatcher set
	if strings.HasPrefix(r.URL.Path, "/check/") {
		pieces := strings.Split(strings.TrimPrefix(r.URL.Path, "/check/"), "/")
		if len(pieces) != 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		setID, err := strconv.Atoi(pieces[0])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if s.validateSetMember(setID, pieces[1]) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	} else if strings.HasPrefix(r.URL.Path, "/dispatcher/") { // Handle requests for /dispatcher/<setID> to fetch a dispatcher set
		pieces := strings.Split(strings.TrimPrefix(r.URL.Path, "/dispatcher/"), "/")
		if len(pieces) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		setID, err := strconv.Atoi(pieces[0])
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		selectedSet := s.getDispatcherSet(setID)
		if selectedSet != nil {
			js, err := json.Marshal(selectedSet)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Write(js) //If WriteHeader has not yet been called, Write calls WriteHeader(http.StatusOK) before writing the data
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
}

func (s *dispatcherSets) validateSetMember(id int, addr string) bool {
	selectedSet, ok := s.sets[id]
	if !ok {
		return false
	}
	return selectedSet.Validate(addr)
}

func (s *dispatcherSets) getDispatcherSet(id int) sets.DispatcherSet {
	selectedSet, ok := s.sets[id]
	if !ok {
		return nil
	}
	return selectedSet
}

// notify signals to kamailio to reload its dispatcher list
func (s *dispatcherSets) notify() error {
	return binrpc.InvokeMethod("dispatcher.reload", s.rpcHost, s.rpcPort)
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

	s := &dispatcherSets{
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

	for _, vs := range staticSetDefinitions.list {
		if err = s.addStatic(ctx, vs); err != nil {
			return errors.Wrapf(err, "failed to add static dispatcher set: %s", vs.String())
		}
	}

	if err = s.update(ctx); err != nil {
		return errors.Wrap(err, "failed to run initial dispatcher set update")
	}

	if err = s.export(); err != nil {
		return errors.Wrap(err, "failed to run initial dispatcher set export")
	}

	if err = s.notify(); err != nil {
		log.Println("NOTICE: failed to notify kamailio after initial dispatcher export; kamailio may not be up yet:", err)
	}

	// FIXME: quick hack to work around race condition where kamailio is not up
	// before the notify is run.  Since binrpc is over UDP and returns no data,
	// we have no idea whether the kamailio instance is actually up and
	// receiving the notification.  Therefore, we send a notify again a little
	// later, for good measure.
	time.AfterFunc(KamailioStartupDebounceTimer, func() {
		if err = s.notify(); err != nil {
			log.Println("follow-up kamailio notification failed:", err)
		}
	})

	// Run a web service to offer IP checks for each member of the dispatcher set and fetch a dispatcher set
	if apiAddr != "" {
		var srv http.Server
		srv.Addr = apiAddr
		srv.Handler = s

		go func() {
			<-ctx.Done()
			if err := srv.Shutdown(ctx); err != nil {
				log.Fatalln("failed to shut down HTTP server:", err)
			}
		}()
		go func() {
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				log.Fatalln("failed to start HTTP server:", err)
			}
		}()
	}

	for ctx.Err() == nil {
		err = s.maintain(ctx)
		if errors.Cause(err) == io.EOF {
			continue
		}
		if err != nil {
			return errors.Wrap(err, "failed to maintain dispatcher sets")
		}
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
