package collector

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

type RotatingClient struct {
	client      *http.Client
	userAgents  []string
	langs       []string
	priorities  []string
	rng         *rand.Rand
	mu          sync.Mutex
	useUTLS     bool
	defaultUA   string
	defaultLang string
}

func NewRotatingClient() *RotatingClient {
	useUTLS := strings.TrimSpace(os.Getenv("QUOTAGUARD_UTLS")) == "1"
	transport := newTransport(useUTLS)
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}

	return &RotatingClient{
		client: client,
		userAgents: []string{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.3 Safari/605.1.15",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
		},
		langs:       []string{"en-US,en;q=0.9", "ru-RU,ru;q=0.9,en;q=0.8", "en-GB,en;q=0.8"},
		priorities:  []string{"u=1, i", "u=0, i", "u=1"},
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		useUTLS:     useUTLS,
		defaultUA:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		defaultLang: "en-US,en;q=0.9",
	}
}

func (rc *RotatingClient) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("nil request")
	}
	rc.applyHeaders(req)
	return rc.client.Do(req)
}

func (rc *RotatingClient) applyHeaders(req *http.Request) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	ua := rc.defaultUA
	lang := rc.defaultLang
	priority := "u=1"
	if len(rc.userAgents) > 0 {
		ua = rc.userAgents[rc.rng.Intn(len(rc.userAgents))]
	}
	if len(rc.langs) > 0 {
		lang = rc.langs[rc.rng.Intn(len(rc.langs))]
	}
	if len(rc.priorities) > 0 {
		priority = rc.priorities[rc.rng.Intn(len(rc.priorities))]
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", ua)
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", lang)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if req.Header.Get("Sec-CH-UA") == "" {
		req.Header.Set("Sec-CH-UA", `"Chromium";v="120", "Not(A:Brand";v="8", "Google Chrome";v="120"`)
	}
	if req.Header.Get("Sec-CH-UA-Platform") == "" {
		req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	}
	if req.Header.Get("Priority") == "" {
		req.Header.Set("Priority", priority)
	}
}

func newTransport(useUTLS bool) http.RoundTripper {
	if !useUTLS {
		return &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
		}
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			rawConn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			host := addr
			if strings.Contains(addr, ":") {
				host, _, _ = net.SplitHostPort(addr)
			}
			config := &utls.Config{
				ServerName: host,
				NextProtos: []string{"h2", "http/1.1"},
			}
			uconn := utls.UClient(rawConn, config, utls.HelloChrome_120)
			if err := uconn.Handshake(); err != nil {
				_ = rawConn.Close()
				return nil, err
			}
			return uconn, nil
		},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
	}
}
