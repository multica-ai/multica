package weixin

import (
	"encoding/base64"
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

const TypeWeixin channel.Type = "weixin"

type installConfig struct {
	AppID          string `json:"app_id"`
	UserID         string `json:"user_id"`
	BaseURL        string `json:"base_url"`
	TokenEncrypted string `json:"bot_token_encrypted"`
}

type credentials struct {
	installConfig
	Token string
}

type PublicConfig struct{ BotID, UserID string }

func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{BotID: cfg.AppID, UserID: cfg.UserID}
}

func decodeCredentials(raw json.RawMessage, box *secretbox.Box) (credentials, error) {
	var cfg installConfig
	if box == nil || json.Unmarshal(raw, &cfg) != nil || cfg.AppID == "" || cfg.UserID == "" {
		return credentials{}, ErrProtocol
	}
	base, err := validateBaseURL(cfg.BaseURL)
	if err != nil {
		return credentials{}, err
	}
	sealed, err := base64.StdEncoding.DecodeString(cfg.TokenEncrypted)
	if err != nil {
		return credentials{}, ErrProtocol
	}
	plain, err := box.Open(sealed)
	if err != nil {
		return credentials{}, err
	}
	if len(plain) == 0 {
		return credentials{}, ErrTokenExpired
	}
	cfg.BaseURL = base
	return credentials{installConfig: cfg, Token: string(plain)}, nil
}
