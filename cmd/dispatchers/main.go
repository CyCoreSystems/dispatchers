package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CyCoreSystems/dispatchers/v2"
	"github.com/CyCoreSystems/dispatchers/v2/exporter"
	"github.com/CyCoreSystems/dispatchers/v2/notifier"
	"github.com/CyCoreSystems/dispatchers/v2/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var outputFilename string
var rpcPort string
var rpcHost string
var kubeCfg string

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

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Println("run died:", err)
	}
	os.Exit(1)
}

func run() (err error) {
	ctx, cancel := newStopContext()
	defer cancel()

	var kCfg *rest.Config

	if kubeCfg != "" {
		kCfg, err = clientcmd.BuildConfigFromFlags("", kubeCfg)
		if err != nil {
			return fmt.Errorf("failed to read kubeconfig %q: %w", kubeCfg, err)
		}
	} else {
		kCfg, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to create in-cluster kubeconfig: %w", err)
		}
	}

	kc, err := kubernetes.NewForConfig(kCfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	exp, err := exporter.NewFileExporter(outputFilename, "")
	if err != nil {
		return fmt.Errorf("failed to construct file exporter: %w", err)
	}

	controller := &dispatchers.Controller{
		Exporter: exp,
		Notifier: &notifier.BinRPCNotifier{
			Host: rpcHost,
			Port: rpcPort,
		},
		Logger: log.Default(),
	}

	informerFactory := informers.NewSharedInformerFactory(kc, time.Minute)

	for _, v := range setDefinitions.list {
		ds, err := sets.NewKubernetesSet(ctx, informerFactory, v.id, v.namespace, v.name, v.port)
		if err != nil {
			return fmt.Errorf("failed to create dispatcher set %s: %w", v.String(), err)
		}

		controller.AddSet(ds)
	}

	for _, vs := range staticSetDefinitions.list {
		controller.AddSet(sets.NewStaticSet(vs.id, vs.members))
	}

	// NB: Since binrpc is over UDP and returns no data,
	// we have no idea whether the kamailio instance is actually up and
	// receiving the notification.  Therefore, we send a notify again a little
	// later, for good measure.
	time.AfterFunc(KamailioStartupDebounceTimer, func() {
		if err = controller.Notify(); err != nil {
			log.Println("follow-up kamailio notification failed:", err)
		}
	})

	// Run HTTP API service
	if apiAddr != "" {
		svc := &httpService{controller}

		go svc.Run(ctx, apiAddr)
	}

	for ctx.Err() == nil {
		<-time.After(time.Minute)

		log.Println("current sets:")
		for _, set := range controller.CurrentState() {
			log.Printf("  set %d: %v", set.ID, set.Endpoints)
		}
	}

	<-ctx.Done()

	return nil
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
