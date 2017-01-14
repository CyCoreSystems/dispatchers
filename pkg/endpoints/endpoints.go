package endpoints

import (
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/rest"
)

// Get retrieves the IP addresses for a named endpoint in a given
// namespace.  If the namespace is empty, the `default` namespace
// will be used.
func Get(epNamespace, epName string) ([]string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get cluster configuration")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to construct k8s clientset")
	}

	addrs := []string{}
	res, err := clientset.Core().Endpoints(epNamespace).Get(epName, meta_v1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to retrieve endpoints")
	}
	for _, ep := range res.Subsets {
		for _, addr := range ep.Addresses {
			addrs = append(addrs, addr.IP)
		}
	}
	return addrs, nil
}

// Watch returns a watch interface to listen for changes of endpoints
// in a namespace
func Watch(epNamespace string) (watch.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get cluster configuration")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to construct k8s clientset")
	}

	w, err := clientset.Core().Endpoints(epNamespace).Watch(v1.ListOptions{Watch: true})
	return w, errors.Wrap(err, "failed to watch endpoints")
}
