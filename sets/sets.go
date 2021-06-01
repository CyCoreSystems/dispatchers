package sets

import (
	"fmt"
	"strconv"
	"sync"

	"inet.af/netaddr"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Endpoint describes an Address:Port pair for a dispatcher endpoint member.
type Endpoint struct {
	Address string
	Port    uint32
}

func (ep *Endpoint) String() string {
	return fmt.Sprintf("%s:%d", formatAddress(ep.Address), ep.Port)
}

func formatAddress(addr string) string {
	ip, err := netaddr.ParseIP(addr)
	if err != nil {
		return addr
	}

	if ip.Is6() {
		return fmt.Sprintf("[%s]", ip.String())
	}

	return ip.String()
}

type State struct {
	// ID is the unique identifier of the dispatcher set.
	ID int

	// Endpoints is the set of hosts within the dispatcher set.
	Endpoints []*Endpoint
}

// DispatcherSet defines an individual dispatcher set
type DispatcherSet interface {

	// Close shuts down the dispatcher set
	Close()

	// State returns the current state of the DispatcherSet
	State() *State

	// IsMember checks an address for membership in the set
	IsMember(address string, port uint32) bool

	// RegisterChangeFunc registers a callback function which will be invoked whenever the DispatcherSet contents changes.
	RegisterChangeFunc(func(*State))
}

// StaticSet represents a dispatcher set whose members are static or manually defined
type staticSet struct {
	id        int
	Endpoints []*Endpoint
}

func (s *staticSet) Close() {}

func (s *staticSet) State() *State {
	return &State{
		ID:        s.id,
		Endpoints: s.Endpoints,
	}
}

func (s *staticSet) IsMember(addr string, port uint32) bool {
	for _, m := range s.Endpoints {
		if m.Address == addr {

			if port > 0 {
				if m.Port != port {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (s *staticSet) RegisterChangeFunc(f func(*State)) {}

// NewStaticSet returns a new statically-defined dispatcher set
func NewStaticSet(id int, endpoints []*Endpoint) DispatcherSet {
	return &staticSet{
		id:        id,
		Endpoints: endpoints,
	}
}

// kubernetesSet represents a dispatcher set whose
// data should be derived from Kubernetes.
type kubernetesSet struct {
	// id is the dispatch set index for this set
	id int

	endpoints []*Endpoint

	// callbacks is the set of functions which should be called when the endpoint membership changes.
	callbacks []func(*State)

	// name is the name of the Kubernetes Endpoint List
	// from which the dispatcher endpoints should be derived.
	name string

	// namespace is the namespace in which the Endpoint
	// should be found.
	namespace string

	port string

	mu sync.Mutex
}

// NewKubernetesSet returns a new kubernetes-based dispatcher set.
//
//  * `setID` is the dispatcher set's id
//
//  * `namespace` is the namespace of the Service whose endpoints will describe this dispatcher set.
//
//  * `name` is the name of the Service whose endpoints will describe this dispatcher set.
//
//  * `port` is the port reference of the SIP endpoints this set describes.  This is optional, and if not specified, will default to "5060".
//
func NewKubernetesSet(f informers.SharedInformerFactory, setID int, namespace, name, port string) (DispatcherSet, error) {
	if port == "" {
		port = "5060"
	}

	informer := f.Discovery().V1().EndpointSlices()

	s := &kubernetesSet{
		id:        setID,
		namespace: namespace,
		name:      name,
		port:      port,
	}

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.addFunc,
		UpdateFunc: s.updateFunc,
		DeleteFunc: s.deleteFunc,
	})

	return s, nil
}

func (s *kubernetesSet) updateSet(obj interface{}) {
	epSlice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return
	}

	if epSlice.Namespace != s.namespace ||
		epSlice.Name != s.name {
		return
	}

	list, err := flattenEndpointSlice(s.port, epSlice)
	if err != nil {
		return
	}

	s.mu.Lock()
	if !isChanged(s.endpoints, list) {
		s.mu.Unlock()
		return
	}

	s.endpoints = list
	s.mu.Unlock()

	state := &State{
		ID:        s.id,
		Endpoints: list,
	}

	for _, f := range s.callbacks {
		f(state)
	}
}

func (s *kubernetesSet) addFunc(obj interface{}) {
	s.updateSet(obj)
}

func (s *kubernetesSet) updateFunc(old interface{}, obj interface{}) {
	s.updateSet(obj)
}

func (s *kubernetesSet) deleteFunc(obj interface{}) {
	s.updateSet(obj)
}

func (s *kubernetesSet) Close() {}

func (s *kubernetesSet) State() *State {
	return &State{
		ID: s.id,
		Endpoints: s.endpoints,
	}
}

func (s *kubernetesSet) RegisterChangeFunc(f func(*State)) {
	s.mu.Lock()

	s.callbacks = append(s.callbacks, f)

	defer s.mu.Unlock()
}

func (s *kubernetesSet) IsMember(addr string, port uint32) bool {
	for _, ep := range s.endpoints {
		if ep.Address == addr {

			if port > 0 {
				if ep.Port != port {
					return false
				}
			}
			return true
		}
	}
	return false
}

func flattenEndpointSlice(refPort string, epSlice *discoveryv1.EndpointSlice) (out []*Endpoint, err error) {
	portNumber, err := strconv.Atoi(refPort)
	if err != nil {
		portNumber = 0
	}

	if portNumber == 0 {
		for _, p := range epSlice.Ports {
			if p.Name == nil {
				continue
			}

			if *p.Name == refPort {
				if p.Port == nil {
					return nil, fmt.Errorf("endpoint port %s has no numerical port", *p.Name)
				}
				portNumber = int(*p.Port)
			}
		}

		if portNumber == 0 {
			return nil, fmt.Errorf("failed to find port %s in EndpointSlice %s", refPort, epSlice.Name)
		}
	}

	for _, n := range epSlice.Endpoints {
		for _, addr := range n.Addresses {
			out = append(out, &Endpoint{
				Address: addr,
				Port:    uint32(portNumber),
			})
		}
	}

	return out, nil
}

func isChanged(previous []*Endpoint, current []*Endpoint) (changed bool) {
	if len(previous) != len(current) {
		return true
	}

	for _, p := range previous {
		var found bool

		for _, c := range current {
			if c.Address == p.Address &&
				c.Port == p.Port {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}
