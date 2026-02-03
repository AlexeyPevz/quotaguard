package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"testing"
	"time"

	"crypto/tls"
)

type mockComponent struct {
	called bool
	err    error
}

func (m *mockComponent) Shutdown(ctx context.Context) error {
	m.called = true
	return m.err
}

func TestHTTPServerAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	srv := NewHTTPServer(ln.Addr().String(), http.NewServeMux())
	go func(s *http.Server, l net.Listener) { _ = s.Serve(l) }(srv, ln)

	if err := GracefulShutdown(srv, time.Second); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
}

func TestShutdownWithComponents(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	srv := NewHTTPServer(ln.Addr().String(), http.NewServeMux())
	go func() { _ = srv.Serve(ln) }()

	comp := &mockComponent{}
	if err := ShutdownWithComponents(srv, time.Second, []Shutdownable{comp}); err != nil {
		t.Fatalf("expected shutdown success: %v", err)
	}
	if !comp.called {
		t.Fatalf("expected component shutdown to be called")
	}

	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	srv = NewHTTPServer(ln.Addr().String(), http.NewServeMux())
	go func(s *http.Server, l net.Listener) { _ = s.Serve(l) }(srv, ln)

	compErr := &mockComponent{err: os.ErrInvalid}
	if err := ShutdownWithComponents(srv, time.Second, []Shutdownable{compErr}); err == nil {
		t.Fatalf("expected shutdown error")
	}
}

func TestHTTPSServerCreation(t *testing.T) {
	certFile, keyFile := writeTempCert(t)

	srv, err := NewHTTPSServer("127.0.0.1:0", certFile, keyFile, http.NewServeMux())
	if err != nil {
		t.Fatalf("failed to create https server: %v", err)
	}
	if srv.TLSConfig == nil {
		t.Fatalf("expected TLS config")
	}

	srv, err = NewHTTPSServerWithConfig("127.0.0.1:0", certFile, keyFile, "1.2", http.NewServeMux())
	if err != nil {
		t.Fatalf("failed to create https server with config: %v", err)
	}
	if srv.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected TLS 1.2 min version")
	}

	srv, err = NewHTTPSServerWithConfig("127.0.0.1:0", certFile, keyFile, "1.3", http.NewServeMux())
	if err != nil {
		t.Fatalf("failed to create https server with config: %v", err)
	}
	if srv.TLSConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3 min version")
	}

	if _, err := NewHTTPSServer("127.0.0.1:0", "missing", "missing", http.NewServeMux()); err == nil {
		t.Fatalf("expected error for missing cert files")
	}
}

func TestSignalHandling(t *testing.T) {
	ch := SetupSignalHandler()
	defer signal.Stop(ch)

	go func() {
		ch <- os.Interrupt
	}()

	sig := WaitForSignal(ch)
	if sig != os.Interrupt {
		t.Fatalf("unexpected signal: %v", sig)
	}
}

func writeTempCert(t *testing.T) (string, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	return certFile, keyFile
}
