package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	"k8s.io/client-go/pkg/watch"

	"github.com/CyCoreSystems/dispatchers/pkg/endpoints"
	"github.com/CyCoreSystems/dispatchers/pkg/rpcClient"
	"github.com/CyCoreSystems/dispatchers/pkg/sets"
	"github.com/pkg/errors"
)

var ds map[int]sets.DispatcherSet
var outputFilename string
var rpcPort string

func init() {
	ds = make(map[int]sets.DispatcherSet)

	flag.Var(&kubeSetArgs, "set", "Dispatcher sets of the form [namespace:]name=index, where index is a number.  May be passed multiple times for multiple sets.")
	flag.StringVar(&outputFilename, "o", "/data/kamailio/dispatcher.list", "Output file for dispatcher list")
	flag.StringVar(&rpcPort, "p", "9998", "Port for kamailio's RPC service")
}

// KubeSetArgs is an interface for accepting kubernetes-derived dispatcher
// sets from command-line arguments
type KubeSetArgs string

var kubeSetArgs KubeSetArgs

func (a *KubeSetArgs) String() string {
	var ret string
	for _, v := range ds {
		ret += v.String()
	}
	return ret
}

// Set configures a kubernetes-derived dispatcher set
func (a *KubeSetArgs) Set(raw string) error {
	// Handle multiple comma-delimited arguments
	if strings.Contains(raw, ",") {
		args := strings.Split(raw, ",")
		for _, n := range args {
			err := a.Set(n)
			if err != nil {
				return err
			}
		}
		return nil
	}

	pieces := strings.SplitN(raw, "=", 2)
	if len(pieces) < 2 {
		return fmt.Errorf("Failed to parse %s as the form [namespace:]name=index", raw)
	}

	id, err := strconv.Atoi(pieces[1])
	if err != nil {
		return errors.Wrap(err, "failed to parse index as an integer")
	}

	s := sets.KubernetesSet{
		Index:   id,
		Members: []string{},
	}

	naming := strings.SplitN(pieces[0], ":", 2)

	if len(naming) < 2 {
		s.EndpointNamespace = "default"
		s.EndpointName = naming[0]
	} else {
		s.EndpointNamespace = naming[0]
		s.EndpointName = naming[1]
	}

	// Add this set to the list of sets
	ds[s.Index] = &s

	return nil
}

func main() {
	var failureCount int
	flag.Parse()

	for _, v := range ds {
		_, err := v.Update()
		if err != nil {
			fmt.Println("Failed to update dispatcher set", v.ID(), err)
		}
		fmt.Println(v.String())
	}
	err := export()
	if err != nil {
		panic("Failed to export dispatchers: " + err.Error())
	}
	err = notify()
	if err != nil {
		panic("Failed to notify kamailio of update: " + err.Error())
	}

	for failureCount < 10 {
		err := maintain()
		if err != nil {
			fmt.Println("Error: ", err)
			failureCount++
		}
	}
}

func maintain() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes := make(chan error, 10)

	// Listen to each of the namespaces
	nsList := make(map[string]string)
	for _, v := range ds {
		ks, ok := v.(*sets.KubernetesSet)
		if !ok {
			continue
		}
		nsList[ks.EndpointNamespace] = ks.EndpointNamespace
		go watchNamespace(ctx, changes, ks.EndpointNamespace)
	}

	for {
		err := <-changes
		if err != nil {
			return errors.Wrap(err, "error maintaining sets")
		}

		// Update and check if the changes were significant
		var changed bool
		for _, v := range ds {
			diff, err := v.Update()
			if err != nil {
				return errors.Wrap(err, "error updating set")
			}
			if diff {
				changed = diff
			}
		}

		if changed {
			err = export()
			if err != nil {
				return errors.Wrap(err, "failed to export dispatcher set")
			}

			err = notify()
			if err != nil {
				return errors.Wrap(err, "failed to notify kamailio of update")
			}
		}
	}
}

func watchNamespace(ctx context.Context, changes chan error, ns string) {
	w, err := endpoints.Watch(ns)
	if err != nil {
		changes <- errors.Wrap(err, "failed to watch namespace")
		return
	}
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-w.ResultChan():
			if ev.Type == watch.Error {
				changes <- errors.Wrap(err, "watch error")
				return
			}
			changes <- nil
		}
	}
}

// export pushes the current dispatcher sets to file
func export() error {
	var outString string
	for _, v := range ds {
		outString += v.String()
	}
	out := []byte(outString)
	return ioutil.WriteFile(outputFilename, out, 0644)
}

// notify signals to kamailio to reload its dispatcher list
func notify() error {
	return rpcClient.InvokeMethod("dispatcher.reload", "127.0.0.1", rpcPort)
}
