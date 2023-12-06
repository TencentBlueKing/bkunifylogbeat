package json

import (
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/outputs/codec"
)

type Encoder struct{}

func init() {
	codec.RegisterType("sonic", func(info beat.Info, cfg *common.Config) (codec.Codec, error) {
		return New(), nil
	})

}

// New creates a new json Encoder.
func New() *Encoder {
	return &Encoder{}
}

func (e *Encoder) Encode(index string, event *beat.Event) ([]byte, error) {
	return Marshal(event.Fields)
}
