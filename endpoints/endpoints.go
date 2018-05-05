package endpoints

import (
	"context"

	"github.com/ericchiang/k8s"
	corev1 "github.com/ericchiang/k8s/apis/core/v1"

	"github.com/pkg/errors"
)

// Get retrieves the IP addresses for a named endpoint in a given
// namespace.  If the namespace is empty, the `default` namespace
// will be used.
func Get(ctx context.Context, c *k8s.Client, epNamespace, epName string) ([]string, error) {

	ep := new(corev1.Endpoints)
	if err := c.Get(ctx, epNamespace, epName, ep); err != nil {
		return nil, errors.Wrap(err, "failed to list endpoints")
	}

	addrs := []string{}
	for _, s := range ep.GetSubsets() {
		for _, a := range s.GetAddresses() {
			addrs = append(addrs, a.GetIp())
		}
	}

	return addrs, nil
}

// Watch watches a namespace and returns a nil error on the provided channel
// when a change occurs.  If an error occurs, the error will be sent down the
// channel and the watch will terminate.
func Watch(ctx context.Context, c *k8s.Client, changes chan error, namespace string) error {

	epList := new(corev1.Endpoints)
	w, err := c.Watch(ctx, namespace, epList)
	if err != nil {
		changes <- errors.Wrap(err, "failed to watch namespace")
		return errors.Wrap(err, "failed to watch namespace")
	}
	defer w.Close()

	for ctx.Err() == nil {
		ep := new(corev1.Endpoints)
		_, err := w.Next(ep)
		if err != nil {
			changes <- errors.Wrap(err, "failure during watch")
			return errors.Wrap(err, "failure during watch")
		}
		changes <- nil
	}

	return ctx.Err()
}
