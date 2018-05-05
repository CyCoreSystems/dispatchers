package sets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CyCoreSystems/dispatchers/endpoints"
	"github.com/ericchiang/k8s"
	"github.com/pkg/errors"
)

var maxUpdateInterval = 30 * time.Second

// DispatcherSet defines an individual dispatcher set
type DispatcherSet interface {

	// Close shuts down the dispatcher set
	Close()

	// ID returns the dispatcher set ID
	ID() int

	// Export dumps the dispatcher set
	Export() string

	// Update causes the dispatcher set to be updated
	Update(context.Context) (changed bool, err error)

	// Watch waits for the dispatcher set to change, returning the new value when that change occurs.
	Watch(context.Context) (string, error)
}

// StaticSet represents a dispatcher set whose members are static or manually defined
type staticSet struct {
	id      int
	Members []string
}

func (s *staticSet) Close() {
	return
}

func (s *staticSet) ID() int {
	return s.id
}

func (s *staticSet) Export() string {
	var ret = fmt.Sprintf("# Dispatcher set %d\n", s.id)

	for _, m := range s.Members {
		ret += fmt.Sprintf("%d sip:%s", s.id, m)
		if !strings.Contains(m, ":") {
			ret += ":5060"
		}
		ret += "\n"
	}

	return ret
}

func (s *staticSet) Update(ctx context.Context) (bool, error) {
	return false, nil
}

func (s *staticSet) Watch(ctx context.Context) (string, error) {
	<-ctx.Done()
	return s.Export(), ctx.Err()
}

// NewStaticSet returns a new statically-defined dispatcher set
func NewStaticSet(id int, members []string) DispatcherSet {
	return &staticSet{
		id:      id,
		Members: members,
	}
}

// kubernetesSet represents a dispatcher set whose
// data should be derived from Kubernetes.
type kubernetesSet struct {
	kc *k8s.Client

	cancel context.CancelFunc

	changes chan error

	// id is the dispatch set index for this set
	id int

	// members is the list of members of this set
	members []string

	// endpointName is the name of the Kubernetes Endpoint List
	// from which the dispatcher endpoints should be derived.
	endpointName string

	// endpointNamespace is the namespace in which the Endpoint
	// should be found.
	endpointNamespace string

	port string
}

// NewKubernetesSet returns a new kubernetes-based dispatcher set.
//
//  * `id` is the dispatcher set's id
//
//  * `namespace` is the namespace of the Service whose endpoints will describe this dispatcher set.
//
//  * `name` is the name of the Service whose endpoints will describe this dispatcher set.
//
//  * `port` is the port number of the SIP endpoints this set describes.  This is optional, and if not specified, will default to "5060".
//
func NewKubernetesSet(ctx context.Context, kc *k8s.Client, id int, namespace, name, port string) (DispatcherSet, error) {
	if port == "" {
		port = "5060"
	}

	ctx, cancel := context.WithCancel(ctx)

	s := &kubernetesSet{
		cancel:            cancel,
		changes:           make(chan error),
		id:                id,
		kc:                kc,
		endpointNamespace: namespace,
		endpointName:      name,
		port:              port,
	}

	go s.maintainWatch(ctx)

	return s, nil
}

func (s *kubernetesSet) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// ID returns the index of the dispatcher set
func (s *kubernetesSet) ID() int {
	return s.id
}

func (s *kubernetesSet) Export() string {
	var ret = fmt.Sprintf("# Dispatcher set %d\n", s.id)

	for _, m := range s.members {
		ret += fmt.Sprintf("%d sip:%s:%s\n", s.id, m, s.port)
	}

	return ret
}

// Update updates the list of proxies
func (s *kubernetesSet) Update(ctx context.Context) (changed bool, err error) {
	list, err := s.getEndpoints(ctx)
	if err != nil {
		return
	}

	if differ(s.members, list) {
		changed = true
	}
	s.members = list

	return
}

func (s *kubernetesSet) Watch(ctx context.Context) (string, error) {

	for ctx.Err() == nil {
		select {
		case err := <-s.changes:
			if err != nil {
				return s.Export(), err
			}
		case <-time.After(maxUpdateInterval):
		}

		changed, err := s.Update(ctx)
		if err != nil {
			return s.Export(), errors.Wrap(err, "failed to get updated data")
		}
		if changed {
			return s.Export(), nil
		}
	}

	return s.Export(), ctx.Err()
}

func (s *kubernetesSet) getEndpoints(ctx context.Context) ([]string, error) {
	return endpoints.Get(ctx, s.kc, s.endpointNamespace, s.endpointName)
}

func (s *kubernetesSet) maintainWatch(ctx context.Context) {
	for ctx.Err() == nil {

		err := endpoints.Watch(ctx, s.kc, s.changes, s.endpointNamespace)
		if err != nil {
			s.changes <- err
		}
		time.Sleep(time.Second)
	}
}
