package http

import (
	"crypto/tls"
	"github.com/elastic/beats/libbeat/common/transport/tlscommon"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/outputs/transport"
	"net"
	"net/http"
	"sync"
)

type Server struct {
	config     *Config
	serverHTTP *http.Server
	log        *logp.Logger
	wg         sync.WaitGroup
	tlsConfig  *transport.TLSConfig
}

func New(config *Config, handler http.Handler) (*Server, error) {
	tlsConfig, err := tlscommon.LoadTLSServerConfig(config.TLS)
	if err != nil {
		return nil, err
	}
	return &Server{
		log:       logp.NewLogger("http").With("address", config.Host),
		config:    config,
		tlsConfig: tlsConfig,
		serverHTTP: &http.Server{
			Handler: handler,
		},
	}, nil
}

func (h *Server) Start() error {
	var err error
	listener, err := h.createServer()
	if err != nil {
		return err
	}

	h.log.Info("Started listening for TCP connection")
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.run(listener)
	}()
	return nil
}

func (h *Server) run(listener net.Listener) {
	err := h.serverHTTP.Serve(listener)
	if err != http.ErrServerClosed {
		h.log.Errorf("http server start %v", err)
	}
}

func (h *Server) Stop() {
	h.log.Info("Stoping HTTP server")
	h.serverHTTP.Close()
	h.wg.Wait()
	h.log.Info("HTTP server stop")
}

func (h *Server) createServer() (net.Listener, error) {
	if h.tlsConfig != nil {
		t := h.tlsConfig.BuildModuleConfig(h.config.Host)
		h.log.Info("Listening over TLS")
		return tls.Listen("tcp", h.config.Host, t)
	}
	return net.Listen("tcp", h.config.Host)
}
