package provider

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

type ConnectionConfig struct {
	Provider     string
	ConnectionID string
	DisplayName  string
	Enabled      bool
	Values       map[string]string
}

func (c ConnectionConfig) Value(key string) string {
	if c.Values == nil {
		return ""
	}
	return c.Values[key]
}

type ConfigField struct {
	Key      string
	Label    string
	Required bool
	Secret   bool
}

type Bundle struct {
	Channel        port.Channel
	FileDownloader port.FileDownloader
}

type Factory interface {
	Provider() string
	DisplayName() string
	EnvConfig() ConnectionConfig
	ConfigSchema() []ConfigField
	Build(ctx context.Context, cfg ConnectionConfig) (Bundle, error)
	LeaderLockID(cfg ConnectionConfig) (int64, bool)
	ReconnectDelay(cfg ConnectionConfig) time.Duration
}
