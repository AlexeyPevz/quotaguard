package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/logging"
)

func TestAPIKeyAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logging.NewLogger(logging.WithOutput(&bytes.Buffer{}))

	r := gin.New()
	r.Use(APIKeyAuth([]string{"key1"}, "", logger))
	r.GET("/", func(c *gin.Context) {
		auth, _ := c.Get("authenticated")
		if auth == true {
			c.Status(200)
		} else {
			c.Status(500)
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for missing key")
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set(DefaultAPIKeyHeader, "bad")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for invalid key")
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set(DefaultAPIKeyHeader, "key1")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected ok for valid key")
	}

	r = gin.New()
	r.Use(APIKeyAuth(nil, "", logger))
	r.GET("/", func(c *gin.Context) {
		c.Status(200)
	})

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected ok when auth disabled")
	}
}

func TestOptionalAuthAndAuthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logging.NewLogger(logging.WithOutput(&bytes.Buffer{}))

	r := gin.New()
	r.Use(OptionalAuth([]string{"key1"}, DefaultAPIKeyHeader, logger))
	r.GET("/", func(c *gin.Context) {
		if key, ok := IsAuthenticated(c); ok && key != "" {
			c.String(200, key)
			return
		}
		c.Status(204)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected no content for anonymous")
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set(DefaultAPIKeyHeader, "bad")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected no content for invalid key")
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set(DefaultAPIKeyHeader, "key1")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected ok for valid key")
	}

	r = gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authenticated", true)
		c.Next()
	})
	r.Use(Authenticated())
	r.GET("/secure", func(c *gin.Context) {
		c.Status(200)
	})

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/secure", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected ok for authenticated request")
	}
}

func TestAuthConfigHelper(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, HeaderName: "X-KEY", APIKeys: []string{"k1"}}
	helper := NewAuthConfigHelper(cfg)

	if !helper.IsEnabled() {
		t.Fatalf("expected auth enabled")
	}
	if helper.GetHeaderName() != "X-KEY" {
		t.Fatalf("unexpected header name")
	}
	if !helper.ValidateAPIKey("k1") {
		t.Fatalf("expected key to validate")
	}
	if helper.ValidateAPIKey("bad") {
		t.Fatalf("did not expect invalid key to validate")
	}
	if !helper.HasAPIKey() {
		t.Fatalf("expected api key to be present")
	}

	cfg = config.AuthConfig{}
	helper = NewAuthConfigHelper(cfg)
	if helper.GetHeaderName() != DefaultAPIKeyHeader {
		t.Fatalf("expected default header name")
	}
	if !helper.ValidateAPIKey("any") {
		t.Fatalf("expected validate to allow when no keys configured")
	}
	if helper.HasAPIKey() {
		t.Fatalf("expected no api keys configured")
	}
}

func TestMaskAPIKeys(t *testing.T) {
	masked := MaskAPIKeys([]string{"abcd", "abcdef"})
	if masked[0] != "****" {
		t.Fatalf("unexpected mask for short key: %s", masked[0])
	}
	if masked[1] != "abcd**" {
		t.Fatalf("unexpected mask for long key: %s", masked[1])
	}
}
