package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/logging"
)

// Middleware records HTTP metrics for each request.
func Middleware(m *Metrics, logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		m.IncHTTPRequestsInFlight()
		c.Next()
		m.DecHTTPRequestsInFlight()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = c.Request.URL.Path
		}

		m.RecordRequestLatency(endpoint, c.Request.Method, status, duration)
		m.RecordHTTPRequest(endpoint, c.Request.Method, status)

		if len(c.Errors) > 0 {
			logger.ErrorWithContext(c.Request.Context(), "request error", "error", c.Errors.String())
		}
	}
}
