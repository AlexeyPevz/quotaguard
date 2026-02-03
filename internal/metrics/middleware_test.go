package metrics

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	dto "github.com/prometheus/client_model/go"
	"github.com/quotaguard/quotaguard/internal/logging"
)

func TestMiddlewareRecordsMetricsAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	m := NewMetrics("testmw")
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.WithOutput(&buf), logging.WithLevel(logging.LevelDebug))

	r := gin.New()
	r.Use(Middleware(m, logger))

	r.GET("/ok", func(c *gin.Context) {
		c.Status(200)
	})
	r.GET("/err", func(c *gin.Context) {
		_ = c.Error(errors.New("boom"))
		c.Status(500)
	})
	r.NoRoute(func(c *gin.Context) {
		c.Status(404)
	})

	requests := []string{"/ok", "/err", "/missing"}
	for _, path := range requests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		r.ServeHTTP(w, req)
	}

	if !bytes.Contains(buf.Bytes(), []byte("request error")) {
		t.Fatalf("expected error log to be recorded")
	}

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	if !metricHasLabel(families, "testmw_http_requests_total", "endpoint", "/ok") {
		t.Fatalf("expected metrics for /ok endpoint")
	}
	if !metricHasLabel(families, "testmw_http_requests_total", "endpoint", "/missing") {
		t.Fatalf("expected metrics for /missing endpoint")
	}
}

func metricHasLabel(families []*dto.MetricFamily, name, key, value string) bool {
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == key && label.GetValue() == value {
					return true
				}
			}
		}
	}
	return false
}
