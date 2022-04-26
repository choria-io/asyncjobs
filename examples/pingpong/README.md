# PingPong - an example integration project forh `Choria` CLI and Go API

## Installation
1. Run NATS server using [this](https://hub.docker.com/_/nats)
2. Install NATS CLI from [here](https://github.com/nats-io/natscli)
3. Follow Chroria [wiki](https://github.com/choria-io/asyncjobs/wiki) to set up the Nats queues
4. Install the Choria command line tool: `go install github.com/choria-io/asyncjobs/ajc`
5. Create a JetStream server: `nats context add AJC --server <nats_server>:4222`
6. Run the JetStream server: `nats server run --jetstream AJC`
7. Create a queue: `ajc queue add SDK --run-time 1h --tries 50`

For examples, check the [video](https://www.youtube.com/watch?v=yRbPCpGsgq4)


### Ping-Pong play
Ping Pong is a demo async app with 2 queues. An input Queue PING catches an event, processes it and sends it into a PONG result queue.


This implements a fully async, independent, parallel and scalable computation pattern with multiple independent queues.

First create two queues:

`ajc queue add PING --run-time 1h --tries 20`

`ajc queue add PONG --run-time 1h --tries 20`

Then run the app

1. Run PING: `cd pingpong/ping; go run -v main.v`
2. Run PONG: `cd pingpong/pong; go run -v main.v`
3. Create tasks from command line: `for i in {1..5}; do ajc task add aj:pingpong -q PING '{.....}'; done`

Notice how tasks get processed

