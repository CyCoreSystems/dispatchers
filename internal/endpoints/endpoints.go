// Package endpoints provides kubernetes Endpoint IP retrieval
package endpoints

import (
	"context"
	"log"

	"github.com/ericchiang/k8s"
	corev1 "github.com/ericchiang/k8s/apis/core/v1"

	"github.com/pkg/errors"
)

// Endpoints describes the set of Endpoints for a Service as well as the addresses of the Nodes on which those Endpoints run
type Endpoints struct {

	// Addresses is the list of IP addresses for the Endpoints
	Addresses []string

	// NodeAddresses is the list of Node addresses on which the Endpoints run
	NodeAddresses []string
}

// Get retrieves the IP addresses for a named endpoint in a given
// namespace.  If the namespace is empty, the `default` namespace
// will be used.
func Get(ctx context.Context, c *k8s.Client, epNamespace, epName string) (ret Endpoints, err error) {
	nodes := make(map[string]*corev1.Node)

	ep := new(corev1.Endpoints)
	if err := c.Get(ctx, epNamespace, epName, ep); err != nil {
		return ret, errors.Wrap(err, "failed to list endpoints")
	}

	for _, s := range ep.GetSubsets() {
		for _, a := range s.GetAddresses() {
			ret.Addresses = append(ret.Addresses, a.GetIp())

			epNode := new(corev1.Node)
			if err = c.Get(ctx, "", a.GetNodeName(), epNode); err != nil {
				log.Printf("WARNING: failed to get node %s for endpoint %s: %v", a.GetNodeName(), ep.GetMetadata().GetName(), err)
			}
		}
	}

	for _, n := range nodes {
		for _, a := range n.GetStatus().GetAddresses() {
			ret.NodeAddresses = append(ret.NodeAddresses, a.GetAddress())
		}
	}

	return
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
	defer w.Close() // nolint: errcheck

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
