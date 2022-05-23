+++
title = "Handlers in K8s"
toc = true
weight = 40
+++

We publish Helm charts to deploy the system to Kubernetes.

## Requirements

### NATS Server with JetStream

You need a NATS JetStream server, if you are a Choria User you can enable [Choria Streams](https://choria.io/docs/streams/) otherwise the NATS Community has their own [NATS Helm Charts](https://choria.io/docs/streams/).

### Connection Context

We use NATS Contexts to configure the connection between asyncjobs and NATS. If you already have a context configured using the [NATS CLI](https://github.com/nats-io/natscli) then use `nats context show CONTEXTNAME --json` to get the keys and values to configure.

For me I needed some TLS Certificates to authenticate to NATS along with the context, so we made a secret called `task-scheduler-tls` holding that, you can put NATS credential files and more in the same manner:

```
$ find asyncjobs/task-scheduler
asyncjobs/task-scheduler/secret
asyncjobs/task-scheduler/secret/tls.crt
asyncjobs/task-scheduler/secret/tls.key
asyncjobs/task-scheduler/secret/ca.crt
$ kubectl -n asyncjobs create secret generic task-scheduler-tls --from-file asyncjobs/task-scheduler/secret
```

### Choria Helm Repository

Choria has it's own Helm repository that you need to import:

```
$ helm repo add choria https://choria-io.github.io/helm
$ helm repo update
```

### Kubernetes Namespace

We suggest running the asyncjobs components in a namespace:

```
$ kubectl create namespace asyncjobs
namespace/asyncjobs created
```

## Task Scheduler

Here I show a basic values file for the Task Scheduler, it will run 2 replicas with one being active:

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

We reference the secret added earlier.

```
$ helm install --namespace asyncjobs --values asyncjobs-task-scheduler-values.yaml task-scheduler choria/asyncjobs-task-scheduler
```
