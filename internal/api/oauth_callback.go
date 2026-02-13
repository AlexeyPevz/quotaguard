package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

var (
	oauthCallbackMu      sync.RWMutex
	oauthCallbackHandler func(*gin.Context)
)

// SetOAuthCallbackHandler registers a runtime OAuth callback handler.
func SetOAuthCallbackHandler(handler func(*gin.Context)) {
	oauthCallbackMu.Lock()
	defer oauthCallbackMu.Unlock()
	oauthCallbackHandler = handler
}

func getOAuthCallbackHandler() func(*gin.Context) {
	oauthCallbackMu.RLock()
	defer oauthCallbackMu.RUnlock()
	return oauthCallbackHandler
}

func handleOAuthCallback(c *gin.Context) {
	if handler := getOAuthCallbackHandler(); handler != nil {
		handler(c)
		return
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error":    "oauth callback handler is not configured",
		"status":   "not_configured",
		"provider": c.Param("provider"),
	})
}
