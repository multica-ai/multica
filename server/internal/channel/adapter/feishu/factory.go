package feishu

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/provider"
)

const defaultReconnectDelay = 5 * time.Second

type Factory struct{}

func NewFactory() *Factory {
	return &Factory{}
}

func (*Factory) Provider() string {
	return channelName
}

func (*Factory) DisplayName() string {
	return "Feishu"
}

// EnvConfig returns an optional local-development bootstrap connection.
// Production wiring uses DB-backed channel_connection rows created through
// Settings -> Integrations; this path only seeds an empty non-production DB
// when CHANNEL_ENV_BOOTSTRAP permits it.
func (*Factory) EnvConfig() provider.ConnectionConfig {
	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")
	encryptKey := os.Getenv("FEISHU_ENCRYPT_KEY")
	verifyToken := os.Getenv("FEISHU_VERIFY_TOKEN")
	return provider.ConnectionConfig{
		Provider:     channelName,
		ConnectionID: channelName,
		DisplayName:  "Feishu",
		Enabled:      appID != "" && appSecret != "",
		Values: map[string]string{
			"app_id":       appID,
			"app_secret":   appSecret,
			"encrypt_key":  encryptKey,
			"verify_token": verifyToken,
		},
	}
}

func (*Factory) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{Key: "app_id", Label: "App ID", Required: true},
		{Key: "app_secret", Label: "App Secret", Required: true, Secret: true},
		{Key: "encrypt_key", Label: "Encrypt Key", Secret: true},
		{Key: "verify_token", Label: "Verify Token", Secret: true},
	}
}

func (*Factory) Build(_ context.Context, cfg provider.ConnectionConfig) (provider.Bundle, error) {
	appID := cfg.Value("app_id")
	appSecret := cfg.Value("app_secret")
	encryptKey := cfg.Value("encrypt_key")
	verifyToken := cfg.Value("verify_token")
	if appID == "" || appSecret == "" {
		return provider.Bundle{}, fmt.Errorf("feishu: app_id and app_secret are required")
	}

	client := NewRealClient(appID, appSecret, encryptKey, verifyToken)
	adapter := NewAdapter(client, Config{
		AppID:       appID,
		AppSecret:   appSecret,
		EncryptKey:  encryptKey,
		VerifyToken: verifyToken,
	})
	return provider.Bundle{
		Channel:        adapter,
		FileDownloader: NewRealFileDownloader(client.APIClient()),
	}, nil
}

func (*Factory) LeaderLockID(cfg provider.ConnectionConfig) (int64, bool) {
	connectionID := cfg.ConnectionID
	if connectionID == "" {
		connectionID = channelName
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte("channel:" + channelName + ":" + connectionID))
	return int64(h.Sum64()), true
}

func (*Factory) ReconnectDelay(provider.ConnectionConfig) time.Duration {
	return defaultReconnectDelay
}

var _ provider.Factory = (*Factory)(nil)
