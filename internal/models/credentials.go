package models

import "time"

// AccountCredentials stores auth material for an account.
// This is persisted in SQLite and used by active collectors.
type AccountCredentials struct {
	AccountID         string    `json:"account_id"`
	Type              string    `json:"type"`
	Email             string    `json:"email,omitempty"`
	AccessToken       string    `json:"access_token,omitempty"`
	RefreshToken      string    `json:"refresh_token,omitempty"`
	SessionToken      string    `json:"session_token,omitempty"`
	ProviderAccountID string    `json:"provider_account_id,omitempty"`
	APIKey            string    `json:"api_key,omitempty"`
	ProjectID         string    `json:"project_id,omitempty"`
	ClientID          string    `json:"client_id,omitempty"`
	ClientSecret      string    `json:"client_secret,omitempty"`
	TokenURI          string    `json:"token_uri,omitempty"`
	ExpiryDateMs      int64     `json:"expiry_date,omitempty"`
	ResourceURL       string    `json:"resource_url,omitempty"`
	SourcePath        string    `json:"source_path,omitempty"`
	Raw               string    `json:"raw,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}
