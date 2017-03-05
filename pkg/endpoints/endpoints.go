package endpoints

import (
	"context"

	"github.com/ericchiang/k8s"
	"github.com/pkg/errors"
)

// Get retrieves the IP addresses for a named endpoint in a given
// namespace.  If the namespace is empty, the `default` namespace
// will be used.
func Get(ctx context.Context, epNamespace, epName string) ([]string, error) {
	c, err := k8s.NewInClusterClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get k8s client")
	}

	ret, err := c.CoreV1().GetEndpoints(ctx, epName, epNamespace)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get list of endpoints")
	}

	addrs := []string{}
	for _, ep := range ret.GetSubsets() {
		for _, addr := range ep.Addresses {
			addrs = append(addrs, *addr.Ip)
		}
	}
	return addrs, nil
}

// Watch watches a namespace and returns a nil error on the provided channel
// when a change occurs.  If an error occurs, the error will be sent down the
// channel and the watch will terminate.
func Watch(ctx context.Context, changes chan error, namespace string) {
	c, err := k8s.NewInClusterClient()
	if err != nil {
		changes <- errors.Wrap(err, "failed to connect to k8s")
		return
	}

	w, err := c.CoreV1().WatchEndpoints(ctx, namespace)
	if err != nil {
		changes <- errors.Wrap(err, "failed to watch namespace")
		return
	}
	defer w.Close()

	for {
		if ctx.Err() != nil {
			return
		}

		ev, _, err := w.Next()
		if err != nil {
			changes <- errors.Wrap(err, "watch error")
			return
		}
		if *ev.Type == k8s.EventError {
			changes <- errors.Wrap(err, "watch error received")
			return
		}
		changes <- nil
	}
}
