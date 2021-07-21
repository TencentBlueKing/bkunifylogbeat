package http

import (
	"github.com/elastic/beats/libbeat/common/cfgtype"
	"github.com/elastic/beats/libbeat/common/transport/tlscommon"
	"time"
)

type Config struct {
	Host           string                  `config:"host"`
	Timeout        time.Duration           `config:"timeout" validate:"nonzero,positive"`
	MaxMessageSize cfgtype.ByteSize        `config:"max_message_size" validate:"nonzero,positive"`
	TLS            *tlscommon.ServerConfig `config:"ssl"`
}
