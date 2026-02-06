package cliproxy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

// AuthFile represents a CLIProxyAPI auth file
type AuthFile struct {
	AccessToken  string `json:"access_token"`
	Email        string `json:"email"`
	Type         string `json:"type"` // antigravity, codex, gemini
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	TokenURI     string `json:"token_uri,omitempty"`
	Expiry       string `json:"expiry,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	Expired      string `json:"expired,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Timestamp    int64  `json:"timestamp"`
	Path         string `json:"-"`
	Token        struct {
		AccessToken  string   `json:"access_token,omitempty"`
		RefreshToken string   `json:"refresh_token,omitempty"`
		ClientID     string   `json:"client_id,omitempty"`
		ClientSecret string   `json:"client_secret,omitempty"`
		TokenURI     string   `json:"token_uri,omitempty"`
		Expiry       string   `json:"expiry,omitempty"`
		ExpiresIn    int64    `json:"expires_in,omitempty"`
		TokenType    string   `json:"token_type,omitempty"`
		Scopes       []string `json:"scopes,omitempty"`
	} `json:"token,omitempty"`
}

// ProviderMapping maps CLIProxyAPI auth types to QuotaGuard providers
var ProviderMapping = map[string]string{
	"antigravity": string(models.ProviderAnthropic),
	"codex":       string(models.ProviderOpenAI),
	"gemini":      string(models.ProviderGemini),
	"openai":      string(models.ProviderOpenAI),
	"anthropic":   string(models.ProviderAnthropic),
}

// DefaultAuthPaths returns default CLIProxyAPI auth paths
func DefaultAuthPaths() []string {
	return []string{
		"/opt/cliproxyplus/auths",                                         // Linux server
		os.Getenv("HOME") + "/.config/cliproxy/auths",                     // Linux user
		os.Getenv("HOME") + "/Library/Application Support/cliproxy/auths", // macOS
		os.Getenv("APPDATA") + "\\cliproxy\\auths",                        // Windows
	}
}

// ResolveAuthPath resolves the auth path from preferred path, env var, or defaults.
func ResolveAuthPath(preferred string) string {
	if preferred != "" {
		return preferred
	}
	if envPath := os.Getenv("QUOTAGUARD_CLIPROXY_AUTH_PATH"); envPath != "" {
		return envPath
	}
	for _, path := range DefaultAuthPaths() {
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	paths := DefaultAuthPaths()
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

// HasAuthFiles returns true if the auth path contains CLIProxyAPI auth files.
func HasAuthFiles(authsPath string) bool {
	auths, err := DiscoverAuthFiles(authsPath)
	return err == nil && len(auths) > 0
}

// DiscoverAuthFiles scans a directory for CLIProxyAPI auth files
func DiscoverAuthFiles(authsPath string) ([]AuthFile, error) {
	var auths []AuthFile

	entries, err := os.ReadDir(authsPath)
	if err != nil {
		// Return empty slice for non-existent or inaccessible paths
		if os.IsNotExist(err) || os.IsPermission(err) {
			return []AuthFile{}, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(authsPath, entry.Name()))
		if err != nil {
			continue
		}

		var auth AuthFile
		if json.Unmarshal(data, &auth) != nil {
			continue
		}

		// Validate that this is a CLIProxyAPI auth file
		if auth.Type == "" || auth.Email == "" {
			continue
		}

		// Normalize nested token fields (CLIProxy often stores OAuth under "token")
		if auth.AccessToken == "" && auth.Token.AccessToken != "" {
			auth.AccessToken = auth.Token.AccessToken
		}
		if auth.RefreshToken == "" && auth.Token.RefreshToken != "" {
			auth.RefreshToken = auth.Token.RefreshToken
		}
		if auth.ClientID == "" && auth.Token.ClientID != "" {
			auth.ClientID = auth.Token.ClientID
		}
		if auth.ClientSecret == "" && auth.Token.ClientSecret != "" {
			auth.ClientSecret = auth.Token.ClientSecret
		}
		if auth.TokenURI == "" && auth.Token.TokenURI != "" {
			auth.TokenURI = auth.Token.TokenURI
		}
		if auth.Expiry == "" && auth.Token.Expiry != "" {
			auth.Expiry = auth.Token.Expiry
		}
		if auth.ExpiresIn == 0 && auth.Token.ExpiresIn > 0 {
			auth.ExpiresIn = auth.Token.ExpiresIn
		}

		// Check if provider is supported
		if _, ok := ProviderMapping[auth.Type]; !ok {
			continue
		}

		auth.Path = filepath.Join(authsPath, entry.Name())
		auths = append(auths, auth)
	}

	return auths, nil
}

// ConvertToAccount converts an AuthFile to an Account model
func ConvertToAccount(auth AuthFile) *models.Account {
	provider := models.Provider(ProviderMapping[auth.Type])
	accountID := sanitizeAccountID(auth.Type + "_" + auth.Email)

	return &models.Account{
		ID:             accountID,
		Provider:       provider,
		ProviderType:   auth.Type,
		Enabled:        true,
		Priority:       getDefaultPriority(provider),
		CredentialsRef: auth.Path,
	}
}

func ConvertToCredentials(auth AuthFile) *models.AccountCredentials {
	rawAuth := auth
	rawAuth.Path = ""
	rawJSON, _ := json.Marshal(rawAuth)
	var expiryMs int64
	if auth.Expired != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, auth.Expired); err == nil {
			expiryMs = parsed.UnixMilli()
		}
	}
	if expiryMs == 0 && auth.Expiry != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, auth.Expiry); err == nil {
			expiryMs = parsed.UnixMilli()
		}
	}
	if expiryMs == 0 && auth.Timestamp > 0 && auth.ExpiresIn > 0 {
		expiryMs = auth.Timestamp + auth.ExpiresIn*1000
	}
	if expiryMs == 0 && auth.ExpiresIn > 0 {
		expiryMs = time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second).UnixMilli()
	}
	return &models.AccountCredentials{
		Type:              auth.Type,
		Email:             auth.Email,
		AccessToken:       auth.AccessToken,
		RefreshToken:      auth.RefreshToken,
		SessionToken:      auth.SessionToken,
		ProviderAccountID: auth.AccountID,
		ProjectID:         auth.ProjectID,
		ClientID:          auth.ClientID,
		ClientSecret:      auth.ClientSecret,
		TokenURI:          auth.TokenURI,
		ExpiryDateMs:      expiryMs,
		SourcePath:        auth.Path,
		Raw:               string(rawJSON),
	}
}

// sanitizeAccountID creates a safe account ID from email
func sanitizeAccountID(email string) string {
	// Remove special characters and lowercase
	result := strings.ToLower(email)
	result = strings.ReplaceAll(result, "@", "_at_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ReplaceAll(result, "+", "_plus_")

	// Limit length
	if len(result) > 63 {
		result = result[:63]
	}

	return result
}

// getDefaultPriority returns default priority based on provider
func getDefaultPriority(provider models.Provider) int {
	switch provider {
	case models.ProviderAnthropic:
		return 90 // Antigravity is preferred
	case models.ProviderOpenAI:
		return 80
	case models.ProviderGemini:
		return 70
	default:
		return 50
	}
}

// AccountManager manages auto-discovery of CLIProxyAPI accounts
type AccountManager struct {
	store     store.Store
	authsPath string
	lastScan  time.Time
	interval  time.Duration
}

// NewAccountManager creates a new account manager
func NewAccountManager(s store.Store, authsPath string, scanInterval time.Duration) *AccountManager {
	if scanInterval == 0 {
		scanInterval = 5 * time.Minute
	}

	return &AccountManager{
		store:     s,
		authsPath: authsPath,
		interval:  scanInterval,
	}
}

// ScanAndSync scans auth files and syncs to database
func (am *AccountManager) ScanAndSync() (newCount, updatedCount int, err error) {
	auths, err := DiscoverAuthFiles(am.authsPath)
	if err != nil {
		return 0, 0, err
	}

	// Get existing accounts
	existingAccounts := am.store.ListAccounts()
	existingMap := make(map[string]*models.Account)
	for _, acc := range existingAccounts {
		existingMap[acc.ID] = acc
	}

	// Track which accounts we've seen
	seen := make(map[string]bool)

	for _, auth := range auths {
		accountID := sanitizeAccountID(auth.Type + "_" + auth.Email)
		legacyID := sanitizeAccountID(auth.Email)
		seen[accountID] = true

		account := ConvertToAccount(auth)
		creds := ConvertToCredentials(auth)

		if existing, ok := existingMap[accountID]; ok {
			// Update existing account
			account.Enabled = existing.Enabled
			account.Priority = existing.Priority
			am.store.SetAccount(account)
			_ = am.store.SetAccountCredentials(account.ID, creds)
			updatedCount++
		} else if legacy, ok := existingMap[legacyID]; ok && legacy.CredentialsRef == auth.Path {
			// Migrate legacy account ID to provider-specific ID
			account.Enabled = legacy.Enabled
			account.Priority = legacy.Priority
			am.store.SetAccount(account)
			_ = am.store.SetAccountCredentials(account.ID, creds)
			am.store.DeleteAccount(legacy.ID)
			updatedCount++
		} else {
			// Create new account
			am.store.SetAccount(account)
			_ = am.store.SetAccountCredentials(account.ID, creds)
			newCount++
		}
	}

	am.lastScan = time.Now()
	return newCount, updatedCount, nil
}

// WatchAuths starts a file watcher for auth directory changes.
func (am *AccountManager) WatchAuths(ctx context.Context) error {
	if am.authsPath == "" {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := watcher.Add(am.authsPath); err != nil {
		watcher.Close()
		return err
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
					_, _, _ = am.ScanAndSync()
				}
			case <-watcher.Errors:
				// Ignore watcher errors; periodic scan can still handle updates.
			}
		}
	}()

	return nil
}

// StartAutoSync performs an initial scan and starts periodic and watcher-based sync.
func (am *AccountManager) StartAutoSync(ctx context.Context) error {
	if _, _, err := am.ScanAndSync(); err != nil {
		return err
	}
	if err := am.WatchAuths(ctx); err != nil {
		return err
	}
	if am.interval <= 0 {
		return nil
	}

	go func() {
		ticker := time.NewTicker(am.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _, _ = am.ScanAndSync()
			}
		}
	}()

	return nil
}

// GetAuthPath returns the current auth path
func (am *AccountManager) GetAuthPath() string {
	return am.authsPath
}

// GetLastScan returns the last scan time
func (am *AccountManager) GetLastScan() time.Time {
	return am.lastScan
}
