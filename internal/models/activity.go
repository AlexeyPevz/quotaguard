package models

import "time"

// AccountActivity stores exact routing usage timestamps for an account.
type AccountActivity struct {
	AccountID      string
	AccountLastUse *time.Time
	GroupLastUse   map[string]time.Time
}
