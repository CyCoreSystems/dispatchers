package sets

import (
	"context"
	"fmt"

	"github.com/CyCoreSystems/dispatchers/pkg/endpoints"
)

// DispatcherSet defines an individual dispatcher set
type DispatcherSet interface {
	ID() int
	String() string
	Update(ctx context.Context) (bool, error)
}

// KubernetesSet represents a dispatcher set whose
// data should be derived from Kubernetes.
type KubernetesSet struct {
	// Index is the dispatch set index for this set
	Index int

	// Members is the list of members of this set
	Members []string

	// EndpointName is the name of the Kubernetes Endpoint List
	// from which the dispatcher endpoints should be derived.
	EndpointName string

	// EndpointNamespace is the namespace in which the Endpoint
	// should be found.
	EndpointNamespace string
}

// ID returns the index of the dispatcher set
func (s *KubernetesSet) ID() int {
	return s.Index
}

func (s *KubernetesSet) String() (ret string) {
	ret += fmt.Sprintf("# Dispatcher set %d\n", s.Index)
	for _, m := range s.Members {
		ret += fmt.Sprintf("%d sip:%s:5060\n", s.Index, m)
	}
	return
}

// Update updates the list of proxies
func (s *KubernetesSet) Update(ctx context.Context) (changed bool, err error) {
	list, err := endpoints.Get(ctx, s.EndpointNamespace, s.EndpointName)
	if err != nil {
		return
	}

	if differ(s.Members, list) {
		changed = true
	}
	s.Members = list

	return
}
