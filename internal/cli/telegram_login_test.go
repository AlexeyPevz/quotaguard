package cli

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/cliproxy"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/require"
)

func TestFallbackGoogleOAuthClientCreds_FromAuthDir(t *testing.T) {
	tmpDir := t.TempDir()
	authFile := filepath.Join(tmpDir, "gemini-test.json")
	payload := map[string]interface{}{
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"type":          "gemini",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(authFile, raw, 0o600))

	old := os.Getenv("QUOTAGUARD_CLIPROXY_AUTH_PATH")
	require.NoError(t, os.Setenv("QUOTAGUARD_CLIPROXY_AUTH_PATH", tmpDir))
	t.Cleanup(func() {
		if old == "" {
			_ = os.Unsetenv("QUOTAGUARD_CLIPROXY_AUTH_PATH")
		} else {
			_ = os.Setenv("QUOTAGUARD_CLIPROXY_AUTH_PATH", old)
		}
	})

	clientID, clientSecret := fallbackGoogleOAuthClientCreds("gemini")
	require.Equal(t, "test-client-id", clientID)
	require.Equal(t, "test-client-secret", clientSecret)
}

func TestCompleteCodexDeviceAuthLogin(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))

	emailPayload, err := json.Marshal(map[string]interface{}{"email": "device@example.com"})
	require.NoError(t, err)
	idToken := "x." + base64.RawURLEncoding.EncodeToString(emailPayload) + ".y"

	authJSON := map[string]interface{}{
		"tokens": map[string]interface{}{
			"access_token":  "acc-token",
			"refresh_token": "ref-token",
			"account_id":    "acc-123",
			"id_token":      idToken,
		},
	}
	raw, err := json.Marshal(authJSON)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "auth.json"), raw, 0o600))

	oldHome := os.Getenv("HOME")
	require.NoError(t, os.Setenv("HOME", home))
	t.Cleanup(func() {
		if oldHome == "" {
			_ = os.Unsetenv("HOME")
		} else {
			_ = os.Setenv("HOME", oldHome)
		}
	})

	memStore := store.NewMemoryStore()
	manager := cliproxy.NewAccountManager(memStore, filepath.Join(tmpDir, "auths"), time.Minute)

	session := &deviceAuthSession{Provider: "codex", LogPath: filepath.Join(tmpDir, "codex.log")}
	session.setResult(nil)

	result, err := completeCodexDeviceAuthLogin(memStore, manager, session, "done")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "codex", result.Provider)
	require.Equal(t, "device@example.com", result.Email)

	acc, ok := memStore.GetAccount(result.AccountID)
	require.True(t, ok)
	require.Equal(t, models.ProviderOpenAI, acc.Provider)
	require.Equal(t, "codex", acc.ProviderType)

	creds, ok := memStore.GetAccountCredentials(result.AccountID)
	require.True(t, ok)
	require.Equal(t, "codex", creds.Type)
	require.Equal(t, "acc-token", creds.AccessToken)
	require.Equal(t, "acc-123", creds.ProviderAccountID)
}

func TestRewriteProviderAuthURLForRelay(t *testing.T) {
	original := os.Getenv("QUOTAGUARD_PUBLIC_BASE_URL")
	require.NoError(t, os.Setenv("QUOTAGUARD_PUBLIC_BASE_URL", "https://relay.example.com/base"))
	t.Cleanup(func() {
		if original == "" {
			_ = os.Unsetenv("QUOTAGUARD_PUBLIC_BASE_URL")
		} else {
			_ = os.Setenv("QUOTAGUARD_PUBLIC_BASE_URL", original)
		}
	})

	authURL := "https://accounts.google.com/o/oauth2/auth?client_id=test&redirect_uri=http%3A%2F%2Flocalhost%3A1456%2Foauth-callback&response_type=code"
	rewritten, localCallback, relayEnabled, err := rewriteProviderAuthURLForRelay(authURL, "gemini", "sid-123")
	require.NoError(t, err)
	require.True(t, relayEnabled)
	require.Equal(t, "http://localhost:1456/oauth-callback", localCallback)

	parsed, err := url.Parse(rewritten)
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	require.Equal(t, "https://relay.example.com/base/oauth/callback/gemini?sid=sid-123", redirectURI)
}

func TestBuildLocalOAuthCallbackForwardURL(t *testing.T) {
	query := url.Values{}
	query.Set("code", "abc")
	query.Set("state", "xyz")
	query.Set("sid", "session-1")
	query.Set("scope", "email")

	forwardURL, err := buildLocalOAuthCallbackForwardURL("http://127.0.0.1:1456/oauth-callback", query)
	require.NoError(t, err)

	parsed, err := url.Parse(forwardURL)
	require.NoError(t, err)
	require.Equal(t, "abc", parsed.Query().Get("code"))
	require.Equal(t, "xyz", parsed.Query().Get("state"))
	require.Equal(t, "email", parsed.Query().Get("scope"))
	require.Equal(t, "", parsed.Query().Get("sid"))
}
