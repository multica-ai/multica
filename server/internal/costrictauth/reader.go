package costrictauth

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Credentials mirrors the auth.json written by cs-cloud / csc.
type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	BaseURL      string `json:"base_url"`
	ExpiryDate   int64  `json:"expiry_date"`
}

// LoadCredentials reads ~/.costrict/share/auth.json.
// Returns (nil, nil) when the file does not exist or the token is empty.
func LoadCredentials() (*Credentials, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	p := filepath.Join(home, ".costrict", "share", "auth.json")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cred Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	if cred.AccessToken == "" {
		return nil, nil
	}
	return &cred, nil
}
