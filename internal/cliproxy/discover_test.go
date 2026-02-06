package cliproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverAuthFiles(t *testing.T) {
	// Create temp directory with auth files
	tmpDir, err := os.MkdirTemp("", "cliproxy_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test auth files
	authFiles := []struct {
		filename string
		content  string
	}{
		{
			filename: "antigravity-test@example.com.json",
			content:  `{"access_token":"token1","email":"test@example.com","type":"antigravity","project_id":"proj1"}`,
		},
		{
			filename: "codex-test@example.com.json",
			content:  `{"access_token":"token2","email":"test2@example.com","type":"codex"}`,
		},
		{
			filename: "gemini-test@example.com.json",
			content:  `{"access_token":"token3","email":"test3@example.com","type":"gemini"}`,
		},
		{
			filename: "invalid.json",
			content:  `{"not_an_auth_file":true}`,
		},
		{
			filename: "notjson.txt",
			content:  "not json content",
		},
	}

	for _, af := range authFiles {
		err := os.WriteFile(filepath.Join(tmpDir, af.filename), []byte(af.content), 0644)
		require.NoError(t, err)
	}

	// Discover auth files
	auths, err := DiscoverAuthFiles(tmpDir)
	require.NoError(t, err)

	// Should find 3 valid auth files
	assert.Len(t, auths, 3)
	for _, auth := range auths {
		assert.NotEmpty(t, auth.Path)
	}
}

func TestDiscoverAuthFiles_EmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cliproxy_empty_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auths, err := DiscoverAuthFiles(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, auths)
}

func TestDiscoverAuthFiles_NonExistent(t *testing.T) {
	auths, err := DiscoverAuthFiles("/non/existent/path")
	require.NoError(t, err)
	assert.Empty(t, auths)
}

func TestConvertToAccount(t *testing.T) {
	tests := []struct {
		name     string
		auth     AuthFile
		expected *models.Account
	}{
		{
			name: "antigravity to anthropic",
			auth: AuthFile{
				Email: "test@example.com",
				Type:  "antigravity",
			},
			expected: &models.Account{
				ID:       "antigravity_test_at_example_com",
				Provider: models.ProviderAnthropic,
				Enabled:  true,
				Priority: 90,
			},
		},
		{
			name: "codex to openai",
			auth: AuthFile{
				Email: "user@openai.com",
				Type:  "codex",
			},
			expected: &models.Account{
				ID:       "codex_user_at_openai_com",
				Provider: models.ProviderOpenAI,
				Enabled:  true,
				Priority: 80,
			},
		},
		{
			name: "gemini to gemini",
			auth: AuthFile{
				Email: "googler@gmail.com",
				Type:  "gemini",
			},
			expected: &models.Account{
				ID:       "gemini_googler_at_gmail_com",
				Provider: models.ProviderGemini,
				Enabled:  true,
				Priority: 70,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := ConvertToAccount(tt.auth)
			assert.Equal(t, tt.expected.ID, account.ID)
			assert.Equal(t, tt.expected.Provider, account.Provider)
			assert.Equal(t, tt.expected.Enabled, account.Enabled)
			assert.Equal(t, tt.expected.Priority, account.Priority)
		})
	}
}

func TestConvertToCredentialsExpiry(t *testing.T) {
	auth := AuthFile{
		Email:      "test@example.com",
		Type:       "antigravity",
		Timestamp:  1700000000000,
		ExpiresIn:  3600,
		AccessToken: "token",
		Path:       "/tmp/auth.json",
	}
	creds := ConvertToCredentials(auth)
	require.Equal(t, auth.Timestamp+auth.ExpiresIn*1000, creds.ExpiryDateMs)
	require.Equal(t, auth.Path, creds.SourcePath)

	auth.Expired = "2026-02-05T12:42:29+03:00"
	creds = ConvertToCredentials(auth)
	require.NotZero(t, creds.ExpiryDateMs)
}

func TestSanitizeAccountID(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"test@example.com"},
		{"user.name+tag@gmail.com"},
		{"UPPERCASE@EXAMPLE.COM"},
		{"a@" + strings.Repeat("b", 100) + ".com"},
	}

	for _, tt := range tests {
		t.Run("input", func(t *testing.T) {
			result := sanitizeAccountID(tt.input)
			// Max length check
			assert.LessOrEqual(t, len(result), 63)
			// Must contain @ replaced with _at_
			assert.Contains(t, result, "_at_")
			// Must be lowercase
			assert.Equal(t, strings.ToLower(result), result)
		})
	}
}

func TestDefaultAuthPaths(t *testing.T) {
	paths := DefaultAuthPaths()
	assert.NotEmpty(t, paths)

	// Should contain common paths
	found := false
	for _, p := range paths {
		if strings.Contains(p, "cliproxy") {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestAccountManager_ScanAndSync(t *testing.T) {
	// Create temp directory with auth files
	tmpDir, err := os.MkdirTemp("", "cliproxy_manager_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test auth file
	authContent := `{"access_token":"token","email":"manager_test@example.com","type":"antigravity"}`
	err = os.WriteFile(filepath.Join(tmpDir, "test.json"), []byte(authContent), 0644)
	require.NoError(t, err)

	// Create memory store
	memStore := store.NewMemoryStore()
	manager := NewAccountManager(memStore, tmpDir, time.Minute)

	// First scan - should create account
	newCount, updatedCount, err := manager.ScanAndSync()
	require.NoError(t, err)
	assert.Equal(t, 1, newCount)
	assert.Equal(t, 0, updatedCount)

	// Verify account was created
	account, ok := memStore.GetAccount("antigravity_manager_test_at_example_com")
	assert.True(t, ok)
	assert.Equal(t, models.ProviderAnthropic, account.Provider)

	// Second scan - should update (no change)
	newCount, updatedCount, err = manager.ScanAndSync()
	require.NoError(t, err)
	assert.Equal(t, 0, newCount)
	assert.Equal(t, 1, updatedCount) // Account is updated even if unchanged
}

func TestAccountManager_ScanAndSync_EmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cliproxy_empty_manager_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	memStore := store.NewMemoryStore()
	manager := NewAccountManager(memStore, tmpDir, time.Minute)

	newCount, updatedCount, err := manager.ScanAndSync()
	require.NoError(t, err)
	assert.Equal(t, 0, newCount)
	assert.Equal(t, 0, updatedCount)
}

func TestAccountManager_GetAuthPath(t *testing.T) {
	memStore := store.NewMemoryStore()
	manager := NewAccountManager(memStore, "/test/path", time.Minute)

	assert.Equal(t, "/test/path", manager.GetAuthPath())
}

func TestAccountManager_GetLastScan(t *testing.T) {
	// Create temp dir for proper test
	tmpDir, err := os.MkdirTemp("", "cliproxy_time_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	memStore := store.NewMemoryStore()
	manager := NewAccountManager(memStore, tmpDir, time.Minute)

	// Before scan - zero time
	assert.True(t, manager.GetLastScan().IsZero())

	// After scan - should have a time
	_, _, err = manager.ScanAndSync()
	require.NoError(t, err)
	assert.False(t, manager.GetLastScan().IsZero())
}
