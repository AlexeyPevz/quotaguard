package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/quotaguard/quotaguard/internal/store"
)

type geminiOAuthFile struct {
	AccessToken  string   `json:"access_token"`
	TokenType    string   `json:"token_type"`
	RefreshToken string   `json:"refresh_token"`
	TokenURI     string   `json:"token_uri"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes"`
	ExpiryDate   int64    `json:"expiry_date"`
	Type         string   `json:"type"`
	QuotaProject string   `json:"quota_project_id"`
}

type qwenOAuthFile struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
	ExpiryDate   int64  `json:"expiry_date"`
}

func importOAuthCredentials(s store.Store) (int, int, error) {
	var newCount, updatedCount int

	geminiPaths := resolveOAuthPaths("GEMINI_OAUTH_PATH", []string{
		"~/.gemini/oauth_creds.json",
		"~/.config/google-gemini/token.json",
		"~/.config/gcloud/application_default_credentials.json",
	})
	qwenPaths := resolveOAuthPaths("QWEN_OAUTH_PATH", []string{
		"~/.qwen/oauth_creds.json",
	})

	for _, path := range geminiPaths {
		created, updated, err := upsertGeminiOAuth(s, path)
		if err != nil {
			continue
		}
		if created {
			newCount++
		}
		if updated {
			updatedCount++
		}
	}

	for _, path := range qwenPaths {
		created, updated, err := upsertQwenOAuth(s, path)
		if err != nil {
			continue
		}
		if created {
			newCount++
		}
		if updated {
			updatedCount++
		}
	}

	if newCount == 0 && updatedCount == 0 {
		return 0, 0, nil
	}
	return newCount, updatedCount, nil
}

func resolveOAuthPaths(envKey string, defaults []string) []string {
	if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
		parts := strings.Split(raw, ",")
		var paths []string
		for _, p := range parts {
			path := strings.TrimSpace(p)
			if path == "" {
				continue
			}
			paths = append(paths, expandHome(path))
		}
		return paths
	}

	var paths []string
	for _, p := range defaults {
		paths = append(paths, expandHome(p))
	}
	return paths
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func upsertGeminiOAuth(s store.Store, path string) (bool, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false, err
	}

	var file geminiOAuthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return false, false, err
	}

	if file.TokenURI == "" && file.Type == "authorized_user" {
		file.TokenURI = "https://oauth2.googleapis.com/token"
	}

	accountID := oauthAccountID("gemini", path)
	account := &models.Account{
		ID:             accountID,
		Provider:       models.ProviderGemini,
		ProviderType:   "gemini_oauth",
		Enabled:        true,
		Priority:       70,
		OAuthCredsPath: path,
	}

	_, exists := s.GetAccount(accountID)
	s.SetAccount(account)
	creds := &models.AccountCredentials{
		Type:         "gemini",
		AccessToken:  file.AccessToken,
		RefreshToken: file.RefreshToken,
		ClientID:     file.ClientID,
		ClientSecret: file.ClientSecret,
		TokenURI:     file.TokenURI,
		ExpiryDateMs: file.ExpiryDate,
		SourcePath:   path,
		Raw:          string(data),
		UpdatedAt:    time.Now(),
	}
	_ = s.SetAccountCredentials(accountID, creds)

	if exists {
		return false, true, nil
	}
	return true, false, nil
}

func upsertQwenOAuth(s store.Store, path string) (bool, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false, err
	}

	var file qwenOAuthFile
	if err := json.Unmarshal(data, &file); err != nil {
		return false, false, err
	}

	accountID := oauthAccountID("qwen", path)
	account := &models.Account{
		ID:             accountID,
		Provider:       models.ProviderQwen,
		ProviderType:   "qwen_oauth",
		Enabled:        true,
		Priority:       60,
		OAuthCredsPath: path,
	}

	_, exists := s.GetAccount(accountID)
	s.SetAccount(account)
	creds := &models.AccountCredentials{
		Type:         "qwen",
		AccessToken:  file.AccessToken,
		RefreshToken: file.RefreshToken,
		ResourceURL:  file.ResourceURL,
		ExpiryDateMs: file.ExpiryDate,
		SourcePath:   path,
		Raw:          string(data),
		UpdatedAt:    time.Now(),
	}
	_ = s.SetAccountCredentials(accountID, creds)

	if exists {
		return false, true, nil
	}
	return true, false, nil
}

func oauthAccountID(prefix, path string) string {
	base := "default"
	if path != "" {
		base = filepath.Base(path)
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return sanitizeOAuthAccountID(fmt.Sprintf("%s_%s", prefix, base))
}

func sanitizeOAuthAccountID(input string) string {
	result := strings.ToLower(input)
	result = strings.ReplaceAll(result, "@", "_at_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ReplaceAll(result, "+", "_plus_")
	if len(result) > 63 {
		result = result[:63]
	}
	return result
}
