package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quotaguard/quotaguard/internal/config"
	"github.com/quotaguard/quotaguard/internal/logging"
)

// Constants for header names
const (
	// DefaultAPIKeyHeader is the default header name for API key authentication
	DefaultAPIKeyHeader = "X-API-Key"
)

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// APIKeyAuth creates a middleware that validates API keys from the request header.
// If no API keys are configured, authentication is bypassed.
// If authentication is enabled but no keys are provided, requests are rejected.
func APIKeyAuth(apiKeys []string, headerName string, logger *logging.Logger) gin.HandlerFunc {
	// Default header name
	if headerName == "" {
		headerName = DefaultAPIKeyHeader
	}

	// If no API keys configured, skip authentication
	if len(apiKeys) == 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		apiKey := c.GetHeader(headerName)

		// Log missing API key attempt
		if apiKey == "" {
			clientIP := c.ClientIP()
			logger.WarnWithContext(c.Request.Context(), "API authentication failed: missing API key",
				"header_name", headerName,
				"client_ip", clientIP,
				"path", c.Request.URL.Path,
				"method", c.Request.Method,
			)

			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
				Error:   "unauthorized",
				Message: "API key is required. Provide it in the '" + headerName + "' header",
				Code:    http.StatusUnauthorized,
			})
			return
		}

		// Validate API key (case-sensitive comparison for security)
		for _, key := range apiKeys {
			if apiKey == key {
				// Log successful authentication
				clientIP := c.ClientIP()
				logger.InfoWithContext(c.Request.Context(), "API authentication successful",
					"header_name", headerName,
					"client_ip", clientIP,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
				)

				// Store authenticated key in context for potential logging/auditing
				c.Set("api_key", apiKey)
				c.Set("authenticated", true)
				c.Next()
				return
			}
		}

		// Log invalid API key attempt
		clientIP := c.ClientIP()
		logger.WarnWithContext(c.Request.Context(), "API authentication failed: invalid API key",
			"header_name", headerName,
			"client_ip", clientIP,
			"path", c.Request.URL.Path,
			"method", c.Request.Method,
		)

		c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
			Error:   "unauthorized",
			Message: "Invalid API key",
			Code:    http.StatusUnauthorized,
		})
	}
}

// OptionalAuth creates a middleware that performs optional API key authentication.
// If a key is provided and valid, the request is authenticated.
// If no key is provided or the key is invalid, the request continues as anonymous.
func OptionalAuth(apiKeys []string, headerName string, logger *logging.Logger) gin.HandlerFunc {
	// Default header name
	if headerName == "" {
		headerName = DefaultAPIKeyHeader
	}

	// If no API keys configured, skip authentication entirely
	if len(apiKeys) == 0 {
		return func(c *gin.Context) {
			c.Set("authenticated", false)
			c.Set("api_key", "")
			c.Next()
		}
	}

	return func(c *gin.Context) {
		apiKey := c.GetHeader(headerName)

		if apiKey == "" {
			// No key provided, continue as anonymous
			c.Set("authenticated", false)
			c.Set("api_key", "")
		} else {
			// Validate the provided key
			authenticated := false
			for _, key := range apiKeys {
				if apiKey == key {
					authenticated = true
					break
				}
			}

			if authenticated {
				c.Set("authenticated", true)
				c.Set("api_key", apiKey)
			} else {
				// Invalid key provided, but we don't reject - just don't authenticate
				c.Set("authenticated", false)
				c.Set("api_key", "")

				// Log invalid key attempt in optional auth mode
				clientIP := c.ClientIP()
				logger.WarnWithContext(c.Request.Context(), "Optional auth: invalid API key provided",
					"header_name", headerName,
					"client_ip", clientIP,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
				)
			}
		}

		c.Next()
	}
}

// RequireAuth creates a middleware that rejects requests without valid authentication.
// This is useful for endpoints that must have a valid API key.
func RequireAuth(apiKeys []string, headerName string, logger *logging.Logger) gin.HandlerFunc {
	return APIKeyAuth(apiKeys, headerName, logger)
}

// Authenticated returns a middleware that checks if the request was authenticated.
// Use this after OptionalAuth to require authentication for specific operations.
func Authenticated() gin.HandlerFunc {
	return func(c *gin.Context) {
		authenticated, exists := c.Get("authenticated")
		if !exists || !authenticated.(bool) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, ErrorResponse{
				Error:   "unauthorized",
				Message: "Authentication required",
				Code:    http.StatusUnauthorized,
			})
			return
		}
		c.Next()
	}
}

// IsAuthenticated checks if the current request is authenticated and returns the API key.
// Returns the API key and true if authenticated, empty string and false otherwise.
func IsAuthenticated(c *gin.Context) (string, bool) {
	apiKey, exists := c.Get("api_key")
	if !exists {
		return "", false
	}
	return apiKey.(string), true
}

// AuthConfigHelper provides helper methods for working with config.AuthConfig
type AuthConfigHelper struct {
	cfg config.AuthConfig
}

// NewAuthConfigHelper creates a new AuthConfigHelper
func NewAuthConfigHelper(cfg config.AuthConfig) *AuthConfigHelper {
	return &AuthConfigHelper{cfg: cfg}
}

// GetAPIKeys returns the configured API keys
func (a *AuthConfigHelper) GetAPIKeys() []string {
	return a.cfg.APIKeys
}

// GetHeaderName returns the configured header name or the default
func (a *AuthConfigHelper) GetHeaderName() string {
	if a.cfg.HeaderName == "" {
		return DefaultAPIKeyHeader
	}
	return a.cfg.HeaderName
}

// IsEnabled returns whether authentication is enabled
func (a *AuthConfigHelper) IsEnabled() bool {
	return a.cfg.Enabled
}

// ValidateAPIKey validates an API key against the configured keys
func (a *AuthConfigHelper) ValidateAPIKey(apiKey string) bool {
	if len(a.cfg.APIKeys) == 0 {
		return true // No keys configured, always valid
	}

	for _, key := range a.cfg.APIKeys {
		if apiKey == key {
			return true
		}
	}
	return false
}

// HasAPIKey checks if the configuration has any API keys set
func (a *AuthConfigHelper) HasAPIKey() bool {
	return len(a.cfg.APIKeys) > 0
}

// MaskAPIKeys masks API keys for logging (shows only first 4 characters)
func MaskAPIKeys(keys []string) []string {
	masked := make([]string, len(keys))
	for i, key := range keys {
		if len(key) <= 4 {
			masked[i] = strings.Repeat("*", len(key))
		} else {
			masked[i] = key[:4] + strings.Repeat("*", len(key)-4)
		}
	}
	return masked
}
