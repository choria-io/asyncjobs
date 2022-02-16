// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	prometheusNamespace = "choria_asyncjobs"

	enqueueCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "enqueue_count"),
		Help: "The number of jobs that were enqueued",
	}, []string{"queue"})

	enqueueErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "enqueue_error_count"),
		Help: "The number of jobs that failed to enqueued",
	}, []string{"queue"})

	workQueueEntryCorruptCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "item_corrupt_error_count"),
		Help: "The number of work queue process items that were corrupt",
	}, []string{"queue"})

	workQueueEntryForUnknownTaskErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "task_not_found_error_count"),
		Help: "The number of work queue process items that referenced tasks that could not be found",
	}, []string{"queue"})

	workQueueEntryPastDeadlineCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "task_past_deadline_count"),
		Help: "The number of work queue process items that referenced tasks past their deadline",
	}, []string{"queue"})

	workQueueEntryPastMaxTriesCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "task_past_max_tries_count"),
		Help: "The number of work queue process items that referenced tasks past their maximum try limit",
	}, []string{"queue"})

	workQueuePollCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "poll_total"),
		Help: "The number of times a specific queue was polled",
	}, []string{"queue"})

	workQueuePollErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "queues", "poll_error_total"),
		Help: "The number of times a specific queue poll failed",
	}, []string{"queue"})

	taskUpdateCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "tasks", "task_update_total"),
		Help: "The number of task updates that succeeded",
	}, []string{"state"})

	taskUpdateErrorCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "tasks", "task_update_error_total"),
		Help: "The number of task updates that failed",
	}, []string{})

	handlersBusyGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "handler", "busy_count"),
		Help: "The number busy handlers",
	}, []string{})

	handlersErroredCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "handler", "error_total"),
		Help: "The number of times a task handler returned an error",
	}, []string{"queue", "type"})

	handlerRunTimeSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "handler", "runtime"),
		Help: "Time taken to handle a task",
	}, []string{"queue", "type"})

	taskSchedulerPausedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "task_scheduler", "paused"),
		Help: "Indicates if the scheduler is paused",
	}, []string{})

	taskSchedulerSchedules = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "task_scheduler", "schedules_total"),
		Help: "Indicates how many schedules are known",
	}, []string{})

	taskSchedulerScheduledCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "task_scheduler", "scheduled_count"),
		Help: "Indicates how many times a task was created by the scheduler",
	}, []string{"type"})

	taskSchedulerScheduleErrorCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(prometheusNamespace, "task_scheduler", "schedule_error_count"),
		Help: "Indicates how many times a task failed to create",
	}, []string{"type"})
)

func init() {
	prometheus.MustRegister(enqueueCounter)
	prometheus.MustRegister(enqueueErrorCounter)

	prometheus.MustRegister(workQueueEntryCorruptCounter)
	prometheus.MustRegister(workQueueEntryForUnknownTaskErrorCounter)
	prometheus.MustRegister(workQueueEntryPastDeadlineCounter)
	prometheus.MustRegister(workQueueEntryPastMaxTriesCounter)
	prometheus.MustRegister(workQueuePollCounter)
	prometheus.MustRegister(workQueuePollErrorCounter)

	prometheus.MustRegister(taskUpdateCounter)
	prometheus.MustRegister(taskUpdateErrorCounter)

	prometheus.MustRegister(handlersBusyGauge)
	prometheus.MustRegister(handlersErroredCounter)
	prometheus.MustRegister(handlerRunTimeSummary)

	prometheus.MustRegister(taskSchedulerPausedGauge)
	prometheus.MustRegister(taskSchedulerSchedules)
	prometheus.MustRegister(taskSchedulerScheduledCount)
	prometheus.MustRegister(taskSchedulerScheduleErrorCount)
}
