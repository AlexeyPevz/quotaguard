package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHandleOAuthCallback_NotConfigured(t *testing.T) {
	SetOAuthCallbackHandler(nil)
	t.Cleanup(func() {
		SetOAuthCallbackHandler(nil)
	})

	server, _ := setupTestServer()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/oauth/callback/claude?sid=test", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_configured")
	assert.Contains(t, w.Body.String(), "\"provider\":\"claude\"")
}

func TestHandleOAuthCallback_DispatchesToConfiguredHandler(t *testing.T) {
	SetOAuthCallbackHandler(func(c *gin.Context) {
		c.String(http.StatusOK, "ok:"+c.Param("provider"))
	})
	t.Cleanup(func() {
		SetOAuthCallbackHandler(nil)
	})

	server, _ := setupTestServer()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/oauth/callback/gemini?sid=test", nil)
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok:gemini", w.Body.String())
}
