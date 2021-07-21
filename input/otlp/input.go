package otlp

import (
	"github.com/TencentBlueKing/bkunifylogbeat/inputsource/grpc"
	"github.com/elastic/beats/filebeat/channel"
	"github.com/elastic/beats/filebeat/harvester"
	"github.com/elastic/beats/filebeat/input"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	collectorTrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"sync"
)

func init() {
	err := input.Register("otlp", NewInput)
	if err != nil {
		panic(err)
	}
}

// Input for OLTP connection
type Input struct {
	sync.Mutex
	grpcServer *grpc.Server
	started    bool
	outlet     channel.Outleter
	config     *Config
	log        *logp.Logger
}

func NewInput(
	cfg *common.Config,
	outlet channel.Connector,
	ctx input.Context,
) (input.Input, error) {

	out, err := outlet(cfg, ctx.DynamicFields)
	if err != nil {
		return nil, err
	}

	forwarder := harvester.NewForwarder(out)
	traceService := NewTraceService(forwarder)

	config := defaultConfig
	err = cfg.Unpack(&config)
	if err != nil {
		return nil, err
	}

	grpcServer, err := grpc.New(&config.Config)

	if err != nil {
		return nil, err
	}

	collectorTrace.RegisterTraceServiceServer(grpcServer.GrpcServer(), traceService)
	return &Input{
		outlet:     out,
		grpcServer: grpcServer,
		config:     config,
	}, nil
}

func (oltp *Input) Run() {
	oltp.Lock()
	defer oltp.Unlock()

	if !oltp.started {
		logp.Info("Starting OLTP input")
		err := oltp.grpcServer.Start()
		if err != nil {
			logp.Err("Error running harvester: %v", err)
		}
		oltp.started = true
	}
}

func (oltp *Input) Reload() {
	return
}

func (oltp *Input) Stop() {
	defer oltp.outlet.Close()
	oltp.Lock()
	defer oltp.Unlock()

	logp.Info("Stopping OLTP input")
	oltp.grpcServer.Stop()
	oltp.started = false
}

func (oltp *Input) Wait() {
	oltp.Stop()
}
