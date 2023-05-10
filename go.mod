module github.com/TencentBlueKing/bkunifylogbeat

go 1.19

require (
	github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse v0.0.0-00010101000000-000000000000
	github.com/TencentBlueKing/bkmonitor-datalink/pkg/utils v0.0.0-00010101000000-000000000000
	github.com/TencentBlueKing/collector-go-sdk/v2 v2.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/elastic/beats v7.1.1+incompatible
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil v3.21.8+incompatible
	github.com/stretchr/testify v1.8.0
	github.com/tklauser/go-sysconf v0.3.9
)

require (
	github.com/Shopify/sarama v1.27.1 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/andrewkroh/sys v0.0.0-20151128191922-287798fe3e43 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/containerd/cgroups v1.0.1 // indirect
	github.com/coreos/go-systemd/v22 v22.1.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/eapache/go-resiliency v1.2.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20180814174437-776d5712da21 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/elastic/ecs v1.6.0 // indirect
	github.com/elastic/go-lumber v0.1.0 // indirect
	github.com/elastic/go-seccomp-bpf v1.1.0 // indirect
	github.com/elastic/go-structform v0.0.7 // indirect
	github.com/elastic/go-sysinfo v1.4.0 // indirect
	github.com/elastic/go-ucfg v0.7.0 // indirect
	github.com/elastic/go-windows v1.0.0 // indirect
	github.com/elastic/gosigar v0.11.0 // indirect
	github.com/fatih/color v1.13.0 // indirect
	github.com/garyburd/redigo v1.6.2 // indirect
	github.com/go-ole/go-ole v1.2.5 // indirect
	github.com/godbus/dbus/v5 v5.0.3 // indirect
	github.com/gofrs/uuid v4.3.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/grafana/regexp v0.0.0-20220304095617-2e8d9baf4ac2 // indirect
	github.com/hashicorp/go-uuid v1.0.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jcmturner/gofork v1.0.0 // indirect
	github.com/joeshaw/multierror v0.0.0-20140124173710-69b34d4ec901 // indirect
	github.com/klauspost/compress v1.13.6 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/mitchellh/hashstructure v1.0.0 // indirect
	github.com/natefinch/npipe v0.0.0-20160621034901-c1b8fa8bdcce // indirect
	github.com/nightlyone/lockfile v1.0.0 // indirect
	github.com/opencontainers/runtime-spec v1.0.2 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pierrec/lz4 v2.5.2+incompatible // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.12.2 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/prometheus/prometheus v0.37.0 // indirect
	github.com/rapidloop/skv v0.0.0-20180909015525-9def2caac4cc // indirect
	github.com/rcrowley/go-metrics v0.0.0-20200313005456-10cdbea86bc0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tklauser/numcpus v0.3.0 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.17.0 // indirect
	golang.org/x/crypto v0.0.0-20220511200225-c6db032c6c88 // indirect
	golang.org/x/net v0.0.0-20220624214902-1bab6f366d9e // indirect
	golang.org/x/sys v0.0.0-20220627191245-f75cf1eec38b // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/jcmturner/aescts.v1 v1.0.1 // indirect
	gopkg.in/jcmturner/dnsutils.v1 v1.0.1 // indirect
	gopkg.in/jcmturner/gokrb5.v7 v7.5.0 // indirect
	gopkg.in/jcmturner/rpc.v1 v1.1.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v0.0.0-20181124034731-591f970eefbb // indirect
)

replace (
	github.com/Sirupsen/logrus v1.6.0 => github.com/sirupsen/logrus v1.6.0
	github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse => ../bkmonitor-datalink/pkg/libgse
	github.com/TencentBlueKing/bkmonitor-datalink/pkg/utils => ../bkmonitor-datalink/pkg/utils
	github.com/elastic/beats v7.1.1+incompatible => github.com/TencentBlueKing/beats v7.1.20-bk+incompatible
)
