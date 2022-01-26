# JS Async Jobs

## Subjects and storage

### Job Definitions and state

A Stream called `JSAJ_TASKS` with subjects `JSAJ.T.<id>` holding individual task state and config, KV like with no tombstones or history

```json
  {
    "name": "JSAJ_TASKS",
    "subjects": [
      "JSAJ.T.*"
    ],
    "retention": "limits",
    "max_consumers": -1,
    "max_msgs_per_subject": 1,
    "max_msgs": -1,
    "max_bytes": -1,
    "max_age": 0,
    "max_msg_size": -1,
    "storage": "file",
    "discard": "old",
    "num_replicas": 3,
    "duplicate_window": 120000000000,
    "sealed": false,
    "deny_delete": false,
    "deny_purge": false,
    "allow_rollup_hdrs": false
  }
```

### Schedule Definitions and state

A Stream called `JSAJ_SCHEDULES` with subjects `jsaj.S.<id or name>` holding scheduled job definitions

### Work Queues

WQ Streams per queue with unique limits `JSAJ_QUEUE_<NAME>` with subjects `jsaj.Q.<queue name>`. When a task is made it gets an entry in `JSAJ_TASKS` and once created a WQ entry here in the appropriate queue. Workers will use the queue to find pointers to tasks to execute and JS retry mechanisms etc will be used to handle actual executions.  But once a execution happens the task is loaded KV like from `JSAJ_TASKS` and updates done to its state there using update/create style constraints.

### Lifecycles

A Stream called `JSAJ_EVENTS` with subjects `jsaj.E.<event type>` holding various runtime events per job/schedule/etc. We could optionally use this stream to maintain the `JSASJ_TASKS` rather than doing it at job execution stage, this would be CQRS like meaning it would be the only one writing to tasks. There would then have to be a server side process to manage that, so only do that when/if the other option fails
