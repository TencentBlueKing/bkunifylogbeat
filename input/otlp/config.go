package otlp

import (
	"github.com/TencentBlueKing/bkunifylogbeat/inputsource/grpc"
	"github.com/elastic/beats/filebeat/harvester"
)

var defaultConfig = &Config{
	Config: grpc.Config{
		Endpoint:       "0.0.0.0:4317",
		Transport:      "tcp",
		MaxMessageSize: 1024 * 64,
	},
}

type Config struct {
	harvester.ForwarderConfig `config:",inline"`
	grpc.Config
}
