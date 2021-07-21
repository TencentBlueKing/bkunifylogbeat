package grpc

import (
	"github.com/elastic/beats/libbeat/common/cfgtype"
	"github.com/elastic/beats/libbeat/common/transport/tlscommon"
	"time"
)

type Config struct {
	Endpoint       string                  `config:"endpoint"`
	Timeout        time.Duration           `config:"timeout" validate:"nonzero,positive"`
	MaxMessageSize cfgtype.ByteSize        `config:"max_message_size" validate:"nonzero,positive"`
	TLS            *tlscommon.ServerConfig `config:"ssl"`
	Transport      string                  `config:"transport"`
}
