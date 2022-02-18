// Copyright (c) 2022, R.I. Pienaar and the Project contributors
//
// SPDX-License-Identifier: Apache-2.0

package asyncjobs

import (
	"context"
	"sync"
	"time"

	"github.com/choria-io/asyncjobs/election"
	"github.com/dustin/go-humanize"
	"github.com/robfig/cron/v3"
)

type TaskScheduler struct {
	s                  ScheduledTaskStorage
	log                Logger
	tasks              map[string]*scheduledTask
	mu                 sync.Mutex
	cron               *cron.Cron
	leader             bool
	name               string
	skipLeaderElection bool

	ctx    context.Context
	cancel context.CancelFunc
}

type scheduledTask struct {
	item   *ScheduledTask
	cronID cron.EntryID
}

// NewTaskScheduler creates a new Task Scheduler service
func NewTaskScheduler(name string, c *Client) (*TaskScheduler, error) {
	sched := &TaskScheduler{
		s:     c.ScheduledTasksStorage(),
		log:   c.log,
		tasks: make(map[string]*scheduledTask),
		cron:  cron.New(),
		name:  name,
	}

	if sched.s == nil {
		return nil, ErrStorageNotReady
	}

	c.startPrometheus()

	return sched, nil
}

func (s *TaskScheduler) onCampaign(state election.State) {
	s.log.Debugf("Campaigning for leadership while in state %s", state.String())
}

func (s *TaskScheduler) onWon() {
	s.mu.Lock()
	s.leader = true
	taskSchedulerPausedGauge.WithLabelValues().Set(0)
	s.mu.Unlock()

	s.s.PublishLeaderElectedEvent(s.ctx, s.name, "task_scheduler")

	s.log.Infof("Became leader, tasks will be scheduled")
}

func (s *TaskScheduler) onLost() {
	s.mu.Lock()
	s.leader = false
	taskSchedulerPausedGauge.WithLabelValues().Set(1)
	s.mu.Unlock()

	s.log.Infof("Leadership lost, tasks will not be scheduled")
}

func (s *TaskScheduler) startElection(ctx context.Context) error {
	bucket, err := s.s.ElectionStorage()
	if err != nil {
		return err
	}

	e, err := election.NewElection(s.name, "task_scheduler", bucket, election.OnCampaign(s.onCampaign), election.OnWon(s.onWon), election.OnLost(s.onLost))
	if err != nil {
		return err
	}

	s.log.Infof("Starting leader election as %s", s.name)
	go func() {
		err := e.Start(ctx)
		if err != nil {
			s.log.Errorf("Election failed: %v", err)
		}
	}()

	return nil
}

// Run starts the Task Scheduler, it will block until done
func (s *TaskScheduler) Run(ctx context.Context, wg *sync.WaitGroup) error {
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.tasks = make(map[string]*scheduledTask)
	s.mu.Unlock()

	if s.skipLeaderElection {
		s.leader = true
	} else {
		err := s.startElection(ctx)
		if err != nil {
			return err
		}
	}

	err := s.prepareAndWatch(ctx, wg, true)
	if err != nil {
		return err
	}

	go s.cron.Run()

	<-s.ctx.Done()
	s.Stop()

	return nil
}

// Stop stops the scheduler service
func (s *TaskScheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	cron := s.cron
	s.mu.Unlock()

	s.log.Infof("Shutting down after stop called")

	if cron != nil {
		cron.Stop()
	}

	if cancel != nil {
		cancel()
	}
}

// Count reports how many schedules are managed by this Scheduler
func (s *TaskScheduler) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.tasks)
}

func (s *TaskScheduler) deleteItem(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.tasks[name]
	if !ok {
		return nil
	}

	s.cron.Remove(item.cronID)

	return nil
}

func (s *TaskScheduler) handlerFactory(name string) func() {
	return func() {
		s.mu.Lock()
		task, ok := s.tasks[name]
		leader := s.leader
		s.mu.Unlock()

		if !ok {
			s.log.Warnf("Received a trigger for scheduled task %s but it does not exist", name)
			return
		}

		if !leader {
			s.log.Debugf("Skipping task schedule %s while not leader", name)
			return
		}

		var opts []TaskOpt
		if task.item.Deadline > 0 {
			opts = append(opts, TaskDeadline(time.Now().UTC().Add(task.item.Deadline)))
		}
		if task.item.MaxTries > 0 {
			opts = append(opts, TaskMaxTries(task.item.MaxTries))
		}

		nt, err := NewTask(task.item.TaskType, task.item.Payload, opts...)
		if err != nil {
			s.log.Warnf("Could not create new task to schedule for scheduled task %s in queue %s: %s", name, err)
			taskSchedulerScheduleErrorCount.WithLabelValues(task.item.TaskType, task.item.Queue)
			return
		}

		s.log.Infof("Creating new task %s for scheduled task %s on schedule %s", name, nt.ID, task.item.Schedule)
		err = s.s.EnqueueTask(s.ctx, &Queue{Name: task.item.Queue}, nt)
		if err != nil {
			s.log.Warnf("Enqueueing new task for scheduled task %s failed: %s", name, err)
			taskSchedulerScheduleErrorCount.WithLabelValues(task.item.TaskType, task.item.Queue)
			return
		}

		taskSchedulerScheduledCount.WithLabelValues(task.item.TaskType, task.item.Queue)
	}
}

func (s *TaskScheduler) prepareAndWatch(ctx context.Context, wg *sync.WaitGroup, keepWatch bool) error {
	wctx, cancel := context.WithCancel(ctx)
	watch, err := s.s.ScheduledTasksWatch(wctx)
	if err != nil {
		cancel()
		return err
	}

	s.log.Debugf("Loading known scheduled tasks")

	ready := make(chan struct{}, 1)

	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()

		for {
			select {
			case item := <-watch:
				if item == nil {
					ready <- struct{}{}
					if !keepWatch {
						s.log.Debugf("Stopping watch due to keepWatch settings")
						return
					}

					continue
				}

				if item.Delete {
					err = s.deleteItem(item.Name)
					if err != nil {
						s.log.Errorf("Could not remove item %s: %v", item.Name, err)
						continue
					}
					taskSchedulerSchedules.WithLabelValues().Dec()
					s.log.Infof("Removed scheduled task %s", item.Name)
					continue
				}

				if item.Task == nil {
					s.log.Warnf("Received a watch update with no task: %v", item)
					continue
				}

				id, err := s.cron.AddFunc(item.Task.Schedule, s.handlerFactory(item.Name))
				if err != nil {
					s.log.Warnf("Received an invalid schedule in a watch update: %q: %v", item.Task.Schedule, err)
					continue
				}

				s.mu.Lock()
				s.tasks[item.Name] = &scheduledTask{
					item:   item.Task,
					cronID: id,
				}
				s.mu.Unlock()
				s.log.Infof("Registered a new item %v on queue %v: %v", item.Name, item.Task.Queue, item.Task.Schedule)
				taskSchedulerSchedules.WithLabelValues().Inc()

			case <-ctx.Done():
				ready <- struct{}{}
				return
			}
		}
	}()

	<-ready

	s.log.Infof("Loaded %s scheduled task(s)", humanize.Comma(int64(s.Count())))

	return nil
}
