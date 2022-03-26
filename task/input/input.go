package input

import (
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/filebeat/input/file"
	"github.com/elastic/beats/libbeat/common"
)

type Input struct {
	*input.Runner
}

func New(
	conf *common.Config,
	outlet channel.Connector,
	beatDone chan struct{},
	states []file.State,
	dynFields *common.MapStrPointer,
) (*Input, error) {
	r, err := input.New(conf, outlet, beatDone, states, dynFields)
	if err != nil {
		return nil, err
	}
	return &Input{r}, err
}
