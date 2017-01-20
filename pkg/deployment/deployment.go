package deployment

import (
	"github.com/pkg/errors"
	"k8s.io/client-go/1.5/kubernetes"
	"k8s.io/client-go/1.5/rest"
)

// Scale changes the number of app instances
func Scale(app string, n *int32) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.Wrap(err, "failed to get cluster configuration")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errors.Wrap(err, "failed to construct k8s clientset")
	}

	d, err := clientset.Extensions().Deployments("default").Get(app)
	if err != nil {
		return errors.Wrap(err, "failed to get deployment")
	}

	d.Spec.Replicas = n

	_, err = clientset.Extensions().Deployments("default").Update(d)
	return errors.Wrap(err, "failed to scale deployment")

}
