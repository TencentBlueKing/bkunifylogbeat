package grpc

import (
	"github.com/elastic/beats/libbeat/common/transport/tlscommon"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/outputs/transport"
	"google.golang.org/grpc"
	"net"
	"sync"
)

type Server struct {
	config     *Config
	serverGRPC *grpc.Server
	log        *logp.Logger
	tlsConfig  *transport.TLSConfig
	wg         sync.WaitGroup
}

func New(config *Config) (*Server, error) {
	tlsConfig, err := tlscommon.LoadTLSServerConfig(config.TLS)
	if err != nil {
		return nil, err
	}
	return &Server{
		log:        logp.NewLogger("grpc").With("address", config.Endpoint),
		config:     config,
		tlsConfig:  tlsConfig,
		serverGRPC: grpc.NewServer(),
	}, nil
}

func (g *Server) GrpcServer() *grpc.Server {
	return g.serverGRPC
}

func (g *Server) RegisterService(sd *grpc.ServiceDesc, svr interface{}) {
	g.serverGRPC.RegisterService(sd, svr)
}

func (g *Server) Start() error {
	var listener net.Listener
	var err error
	//if g.config.Transport == "udp" {
	//	listener, err = net.ListenPacket(g.config.Transport, g.config.Endpoint)
	//}
	listener, err = net.Listen(g.config.Transport, g.config.Endpoint)
	if err != nil {
		return err
	}
	g.log.Info("Started listening for GRPC connection")
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		g.run(listener)
	}()
	return nil
}

func (g *Server) run(listener net.Listener) {
	err := g.serverGRPC.Serve(listener)
	if err != nil && err != grpc.ErrServerStopped {
		g.log.Errorf("Grpc server error %v", err)
	}
}

func (g *Server) Stop() {
	g.log.Info("Stoping GRPC server")
	g.serverGRPC.GracefulStop()
	g.wg.Wait()
	g.log.Info("GRPC server stoped")
}
