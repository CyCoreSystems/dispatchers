# dispatchers
[![Go Reference](https://pkg.go.dev/badge/github.com/CyCoreSystems/dispatchers/v2.svg)](https://pkg.go.dev/github.com/CyCoreSystems/dispatchers/v2)
![master](https://github.com/github/docs/actions/workflows/main.yml/badge.svg?branch=master)

dispatcher management for kamailio running inside kubernetes

This tool can be used as a library, with the `Controller` as a common base,
plugging your own `Notifier` and/or `Exporter` into place.

It can also be used directly with the included daemon, which will keep a
`dispatchers.list` file in sync with sets of Endpoints of Kubernetes Services.
Each Service is mapped to a single dispatcher set ID for the Kamailio
`dispatcher` module.

When the `dispatchers.list` file is updated, the tool connects to kamailio over
its binrpc service and tells it to reload the file.

The following documentation describes the default usage with the reference
daemon.

## Usage

In general, `dispatchers` is meant to run as a container within the same Pod as
the kamailio container.

Here is an example kamailio Pod definition with a `disaptchers` container which
will populate dispatcher set 1 using the Endpoints from the `asterisk` service
in the same namespace as the kamailio Pod:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kamailio
spec:
  volumes:
    - name: config
  containers:
    - name: kamailio
      image: cycoresystems/astricon-2016-kamailio
      volumeMounts:
        - name: config
          mountPath: /data/kamailio
    - name: dispatchers
      image: cycoresystems/dispatchers
      env:
         - name: POD_NAMESPACE
           valueFrom:
             fieldRef:
               fieldPath: metadata.namespace
      command:
        - /app
        - "-set"
        - asterisk=1
      volumeMounts:
        - name: config
          mountPath: /data/kamailio
```

The image may also be pulled directly:

```sh
  docker pull cycoresystems/dispatchers
```

## Options

Command-line options are available to customize and configure the operation of
`dispatchers`:

- `-kubecfg <string>`: allows specification of a kubecfg, if not running inside kubernetes
- `-o <string>`: specifies the output filename for the dispatcher list.  It defaults to `/data/kamailio/dispatcher.list`.
- `-p <string>`: specifies the port on which kamailio is running its binrpc service.  It defaults to `9998`.
- `-set [namespace:]<service-name>=<index>[:port]`: Specifies a dispatcher set.  This may be passed multiple times for multiple dispatcher sets.  Namespace and port are optional.  If not specified, namespace is `default` or the value of `POD_NAMESPACE` and port is `5060`.
- `-static <index>=<host>[:port][,<host>[:port]]...`: Specifies a static dispatcher set.  This is usually used to define a dispatcher set composed on external resources, such as an external trunk.  Multiple host:port pairs may be passed for multiple contacts in the same dispatcher set.  The option may be declared any number of times for defining any number of unique dispatcher sets.  If not specified, the port will be assigned as `5060`.

For simple systems where the monitored services are in the same namespace as
`dispatchers`, you can set the `POD_NAMESPACE` environment variable to
automatically use the same namespace in which `dispatcher` runs.

## RBAC

When role-based access control (RBAC) is enabled in kubernetes, `dispatchers`
will need to run under a service account with access to the `endpointsices` resource
for the namespace(s) in which your dispatcher services exist.

Example RBAC Role for services in the `sip` namespace:

```yaml

kind: ServiceAccount
apiVersion: v1
metadata:
  name: dispatchers
  namespace: sip

--

kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: endpointslice-reader
rules:
  - apiGroups: ["discovery.k8s.io"]
    resources: ["endpointslices"]
    verbs: ["get", "watch", "list"]

--

kind: ClusterRoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: dispatchers
subjects:
  - kind: ServiceAccount
    name: dispatchers
    namespace: sip
roleRef:
  kind: ClusterRole
  name: endpointslice-reader
```

One `RoleBinding` should be added for each namespace `dispatchers` should have
access to, changing `metadata.namespace` as appropriate.

Once added, make sure that the Pod in which the `dispatchers` container is
running is assigned to the ServiceAccount you created using the
`spec.serviceAccountName` parameter.  For instance:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  serviceAccountName: dispatchers
  ...
```

You can also bind the namespace-default ServiceAccount to make things easier, if
you have a simple setup:  `system:serviceaccount:<namespace>:default`.
