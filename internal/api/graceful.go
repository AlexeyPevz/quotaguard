package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// NewHTTPServer creates a configured HTTP server
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// NewHTTPSServer creates a configured HTTPS server with TLS
func NewHTTPSServer(addr string, certFile, keyFile string, handler http.Handler) (*http.Server, error) {
	// Load TLS certificate and key
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Upgrade to TLS 1.3 if specified
	tlsConfig.MaxVersion = tls.VersionTLS13

	return &http.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}, nil
}

// NewHTTPSServerWithConfig creates an HTTPS server with custom TLS configuration
func NewHTTPSServerWithConfig(addr string, certFile, keyFile, minVersion string, handler http.Handler) (*http.Server, error) {
	// Load TLS certificate and key
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Set minimum TLS version
	switch minVersion {
	case "1.2":
		tlsConfig.MinVersion = tls.VersionTLS12
	case "1.3":
		tlsConfig.MinVersion = tls.VersionTLS13
		fallthrough
	default:
		tlsConfig.MinVersion = tls.VersionTLS13
	}

	return &http.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}, nil
}

// GracefulShutdown performs graceful shutdown of the HTTP server
func GracefulShutdown(srv *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return err
	}

	return nil
}

// SetupSignalHandler sets up OS signal handling for SIGINT and SIGTERM
func SetupSignalHandler() chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	return ch
}

// WaitForSignal waits for termination signals and returns the received signal
func WaitForSignal(ch chan os.Signal) os.Signal {
	return <-ch
}

// ShutdownComponents performs graceful shutdown of all dependent components
type Shutdownable interface {
	Shutdown(ctx context.Context) error
}

// ShutdownWithComponents performs graceful shutdown of server and all components
func ShutdownWithComponents(srv *http.Server, timeout time.Duration, components []Shutdownable) error {
	// First shutdown the HTTP server
	if err := GracefulShutdown(srv, timeout); err != nil {
		return err
	}

	// Then shutdown all components
	for _, comp := range components {
		ctx, cancel := context.WithTimeout(context.Background(), timeout/time.Duration(len(components)+1))
		if err := comp.Shutdown(ctx); err != nil {
			cancel()
			return err
		}
		cancel()
	}

	return nil
}
