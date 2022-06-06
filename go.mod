module github.com/choria-io/asyncjobs

go 1.17

// this should not be needed, but something somewhere is putting jwt v1 in go.sum
// triggering github security alerts, the dependency is unused
replace github.com/nats-io/jwt v1.2.2 => github.com/nats-io/jwt/v2 v2.2.0

require (
	github.com/AlecAivazis/survey/v2 v2.3.4
	github.com/choria-io/fisk v0.0.3-0.20220605065054-0cf82636ff4e
	github.com/dustin/go-humanize v1.0.0
	github.com/nats-io/jsm.go v0.0.31
	github.com/nats-io/nats-server/v2 v2.8.3-0.20220504192053-f20fe2c2d8b8
	github.com/nats-io/nats.go v1.15.0
	github.com/onsi/ginkgo/v2 v2.1.4
	github.com/onsi/gomega v1.19.0
	github.com/prometheus/client_golang v1.12.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/segmentio/ksuid v1.0.4
	github.com/sirupsen/logrus v1.8.1
	github.com/xlab/tablewriter v0.0.0-20160610135559-80b567a11ad5
	golang.org/x/term v0.0.0-20220411215600-e5f449aeb171
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/klauspost/compress v1.15.1 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/minio/highwayhash v1.0.2 // indirect
	github.com/nats-io/jwt/v2 v2.2.1-0.20220330180145-442af02fd36a // indirect
	github.com/nats-io/nkeys v0.3.1-0.20220214171627-79ae42e4d898 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	golang.org/x/crypto v0.0.0-20220315160706-3147a52a75dd // indirect
	golang.org/x/net v0.0.0-20220225172249-27dd8689420f // indirect
	golang.org/x/sys v0.0.0-20220319134239-a9b59b0215f8 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
