package jsaj

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	PrometheusNamespace = "jsasj"

	enqueueCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "enqueue_count"),
		Help: "The number of jobs that were enqueued",
	}, []string{"queue"})

	enqueueErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "enqueue_error_count"),
		Help: "The number of jobs that failed to enqueued",
	}, []string{"queue"})

	workQueueEntryCorruptCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "item_corrupt_error_count"),
		Help: "The number of work queue process items that were corrupt",
	}, []string{"queue"})

	workQueueEntryForUnknownTaskErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "task_not_found_error_count"),
		Help: "The number of work queue process items that referenced tasks that could not be found",
	}, []string{"queue"})

	workQueueEntryPastDeadlineCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "task_past_deadline_count"),
		Help: "The number of work queue process items that referenced tasks past their deadline",
	}, []string{"queue"})

	workQueuePollCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "poll_total"),
		Help: "The number of times a specific queue was polled - influenced by priority",
	}, []string{"queue"})

	workQueuePollErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "queues", "poll_error_total"),
		Help: "The number of times a specific queue poll failed",
	}, []string{"queue"})

	taskUpdateCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "tasks", "task_update_total"),
		Help: "The number of task updates that succeeded",
	}, []string{})

	taskUpdateErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "tasks", "task_update_error_total"),
		Help: "The number of task updates that failed",
	}, []string{})

	handlersBusyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "handler", "busy_count"),
		Help: "The number busy handlers",
	}, []string{})

	handlersErroredCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "handler", "error_total"),
		Help: "The number of times a task handler returned an error",
	}, []string{"queue", "type"})

	handlerRunTimeSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: prometheus.BuildFQName(PrometheusNamespace, "handler", "runtime"),
		Help: "Time taken to handle a task",
	}, []string{"queue", "type"})
)

func init() {
	prometheus.MustRegister(enqueueCounter)
	prometheus.MustRegister(enqueueErrorCounter)

	prometheus.MustRegister(workQueueEntryCorruptCounter)
	prometheus.MustRegister(workQueueEntryForUnknownTaskErrorCounter)
	prometheus.MustRegister(workQueueEntryPastDeadlineCounter)
	prometheus.MustRegister(workQueuePollCounter)
	prometheus.MustRegister(workQueuePollErrorCounter)

	prometheus.MustRegister(taskUpdateCounter)
	prometheus.MustRegister(taskUpdateErrorCounter)

	prometheus.MustRegister(handlersBusyGauge)
	prometheus.MustRegister(handlersErroredCounter)
	prometheus.MustRegister(handlerRunTimeSummary)
}
