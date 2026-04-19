# Handlers in K8s

Helm charts are published to deploy the system to Kubernetes.

## Requirements

### NATS Server with JetStream

A NATS JetStream server is required. Choria users can enable [Choria Streams](https://choria.io/docs/streams/). The NATS community publishes its own [NATS Helm Charts](https://choria.io/docs/streams/).

### Connection context

NATS Contexts configure the connection between asyncjobs and NATS. For an existing context configured through the [NATS CLI](https://github.com/nats-io/natscli), run `nats context show CONTEXTNAME --json` to retrieve the keys and values.

TLS certificates for NATS authentication can be stored in a secret called `task-scheduler-tls`. NATS credential files and similar data fit the same pattern:

```
$ find asyncjobs/task-scheduler
asyncjobs/task-scheduler/secret
asyncjobs/task-scheduler/secret/tls.crt
asyncjobs/task-scheduler/secret/tls.key
asyncjobs/task-scheduler/secret/ca.crt
$ kubectl -n asyncjobs create secret generic task-scheduler-tls --from-file asyncjobs/task-scheduler/secret
```

### Choria Helm repository

Import the Choria Helm repository:

```
$ helm repo add choria https://choria-io.github.io/helm
$ helm repo update
```

### Kubernetes namespace

Run the asyncjobs components in a dedicated namespace:

```
$ kubectl create namespace asyncjobs
namespace/asyncjobs created
```

## Task scheduler

A basic values file for the Task Scheduler runs two replicas, with one active:

```yaml
# asyncjobs-task-scheduler-values.yaml
image:
  tag: 0.0.6

taskScheduler:
  contextSecret: task-scheduler-tls
  context:
    url: nats://broker-broker-ss:4222
    ca: /etc/asyncjobs/secret/ca.crt
    key: /etc/asyncjobs/secret/tls.key
    cert: /etc/asyncjobs/secret/tls.crt
```

The values file references the secret added earlier.

```
$ helm install --namespace asyncjobs --values asyncjobs-task-scheduler-values.yaml task-scheduler choria/asyncjobs-task-scheduler
```
