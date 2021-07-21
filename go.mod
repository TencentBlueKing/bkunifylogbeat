module github.com/TencentBlueKing/bkunifylogbeat

go 1.14

require (
	github.com/TencentBlueKing/collector-go-sdk/v2 v2.0.0
	github.com/docker/docker v1.13.1
	github.com/dustin/go-humanize v1.0.0
	github.com/elastic/beats v7.1.1+incompatible
	github.com/elastic/go-ucfg v0.7.0
	github.com/gogo/protobuf v1.2.1
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.6.1
	go.opentelemetry.io/proto/otlp v0.9.0
	google.golang.org/grpc v1.37.1
)

replace (
	github.com/Sirupsen/logrus v1.6.0 => github.com/sirupsen/logrus v1.6.0
	github.com/elastic/beats v7.1.1+incompatible => github.com/TencentBlueKing/beats v7.1.5-bk+incompatible
)
