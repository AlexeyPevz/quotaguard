package middleware

import (
	"bytes"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/logging"
)

// AuditMiddleware creates a Gin middleware that logs all API requests
func AuditMiddleware(auditStore logging.AuditStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Determine event type based on status code
		eventType := logging.APIAccess
		if c.Writer.Status() >= 400 {
			eventType = logging.AuthFailure
		}

		// Create audit event
		event := logging.NewAuditEvent(eventType, c.Request.Method+" "+path, logging.StatusSuccess)
		event.IPAddress = c.ClientIP()
		event.Resource = path

		// Add details
		event.Details = map[string]interface{}{
			"method":     c.Request.Method,
			"path":       path,
			"query":      query,
			"status":     c.Writer.Status(),
			"latency_ms": latency.Milliseconds(),
			"user_agent": c.Request.UserAgent(),
		}

		// Add user ID if authenticated
		if userID, exists := c.Get("user_id"); exists {
			event.UserID = userID.(string)
		}

		// Save asynchronously to not block the request
		auditStore.SaveEventAsync(event)
	}
}

// AuditEvent creates a middleware that logs a specific event type
func AuditEvent(auditStore logging.AuditStore, eventType logging.AuditEventType, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Only log if the request was successful
		if c.Writer.Status() < 400 {
			event := logging.NewAuditEvent(eventType, action, logging.StatusSuccess)
			event.IPAddress = c.ClientIP()

			// Add user ID if authenticated
			if userID, exists := c.Get("user_id"); exists {
				event.UserID = userID.(string)
			}

			// Add resource if available
			if resource, exists := c.Get("audit_resource"); exists {
				event.Resource = resource.(string)
			}

			// Save asynchronously
			auditStore.SaveEventAsync(event)
		}
	}
}

// AuditEventWithBody creates a middleware that logs a specific event type with request body details
func AuditEventWithBody(auditStore logging.AuditStore, eventType logging.AuditEventType, action string, captureBody bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read body if capturing
		var body []byte
		if captureBody {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				body = bodyBytes
				// Restore body for downstream handlers
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		// Process request
		c.Next()

		// Only log if the request was successful
		if c.Writer.Status() < 400 {
			event := logging.NewAuditEvent(eventType, action, logging.StatusSuccess)
			event.IPAddress = c.ClientIP()

			// Add user ID if authenticated
			if userID, exists := c.Get("user_id"); exists {
				event.UserID = userID.(string)
			}

			// Add details
			details := map[string]interface{}{
				"method": c.Request.Method,
				"path":   c.Request.URL.Path,
			}

			if captureBody && len(body) > 0 {
				// Limit body size to avoid huge logs
				maxBodySize := 1000
				if len(body) > maxBodySize {
					details["body"] = string(body[:maxBodySize]) + "..."
				} else {
					details["body"] = string(body)
				}
			}

			event.Details = details

			// Save asynchronously
			auditStore.SaveEventAsync(event)
		}
	}
}

// SetAuditResource is a helper to set the resource for audit logging in handlers
func SetAuditResource(c *gin.Context, resource string) {
	c.Set("audit_resource", resource)
}

// AuditAuthSuccess logs successful authentication
func AuditAuthSuccess(auditStore logging.AuditStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Only log successful auth
		if c.Writer.Status() == 200 {
			event := logging.NewAuditEvent(logging.AuthSuccess, "authentication", logging.StatusSuccess)
			event.IPAddress = c.ClientIP()
			event.UserID = c.GetString("user_id")

			event.Details = map[string]interface{}{
				"method":     c.Request.Method,
				"path":       c.Request.URL.Path,
				"user_agent": c.Request.UserAgent(),
			}

			auditStore.SaveEventAsync(event)
		}
	}
}

// AuditAuthFailure logs failed authentication
func AuditAuthFailure(auditStore logging.AuditStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Only log failed auth (status 401)
		if c.Writer.Status() == 401 {
			event := logging.NewAuditEvent(logging.AuthFailure, "authentication", logging.StatusFailure)
			event.IPAddress = c.ClientIP()
			event.Severity = logging.SeverityWarning

			event.Details = map[string]interface{}{
				"method":     c.Request.Method,
				"path":       c.Request.URL.Path,
				"user_agent": c.Request.UserAgent(),
			}

			// Add error message if available
			if errMsg, exists := c.Get("auth_error"); exists {
				event.ErrorMessage = errMsg.(string)
			}

			auditStore.SaveEventAsync(event)
		}
	}
}

// AuditQuotaChange logs quota changes
func AuditQuotaChange(auditStore logging.AuditStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Only log successful quota changes
		if c.Writer.Status() == 200 {
			event := logging.NewAuditEvent(logging.ConfigChange, "quota_update", logging.StatusSuccess)
			event.IPAddress = c.ClientIP()

			// Add user ID if authenticated
			if userID, exists := c.Get("user_id"); exists {
				event.UserID = userID.(string)
			}

			// Add quota details if available
			if accountID, exists := c.Get("account_id"); exists {
				event.Resource = accountID.(string)
				event.Details = map[string]interface{}{
					"account_id": accountID,
				}
			}

			auditStore.SaveEventAsync(event)
		}
	}
}

// AuditReservation logs reservation operations
func AuditReservation(auditStore logging.AuditStore, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		// Log based on status
		if c.Writer.Status() >= 400 {
			event := logging.NewAuditEvent(logging.AdminAction, action, logging.StatusFailure)
			event.IPAddress = c.ClientIP()
			event.Severity = logging.SeverityWarning

			if userID, exists := c.Get("user_id"); exists {
				event.UserID = userID.(string)
			}

			auditStore.SaveEventAsync(event)
		} else if c.Writer.Status() == 200 {
			event := logging.NewAuditEvent(logging.AdminAction, action, logging.StatusSuccess)
			event.IPAddress = c.ClientIP()

			if userID, exists := c.Get("user_id"); exists {
				event.UserID = userID.(string)
			}

			// Add reservation details if available
			if reservationID, exists := c.Get("reservation_id"); exists {
				event.Resource = reservationID.(string)
				event.Details = map[string]interface{}{
					"reservation_id": reservationID,
				}
			}

			auditStore.SaveEventAsync(event)
		}
	}
}

// AuditAdminAction logs admin actions
func AuditAdminAction(auditStore logging.AuditStore, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Process request
		c.Next()

		event := logging.NewAuditEvent(logging.AdminAction, action, logging.StatusSuccess)
		event.IPAddress = c.ClientIP()
		event.Severity = logging.SeverityWarning

		if userID, exists := c.Get("user_id"); exists {
			event.UserID = userID.(string)
		}

		event.Details = map[string]interface{}{
			"method": c.Request.Method,
			"path":   c.Request.URL.Path,
		}

		if c.Writer.Status() >= 400 {
			event.Status = logging.StatusFailure
			if errMsg, exists := c.Get("error_message"); exists {
				event.ErrorMessage = errMsg.(string)
			}
		}

		auditStore.SaveEventAsync(event)
	}
}

// GetAuditStore returns the audit store from Gin context
func GetAuditStore(c *gin.Context) (logging.AuditStore, bool) {
	store, exists := c.Get("audit_store")
	if !exists {
		return nil, false
	}
	return store.(logging.AuditStore), true
}
