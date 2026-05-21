package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

// GitHubApp encapsulates GitHub App auth. Use NewGitHubApp at startup, then
// MintToken on demand. ghinstallation handles per-installation token caching
// and refresh internally.
type GitHubApp struct {
	appsTransport *ghinstallation.AppsTransport
}

func NewGitHubApp(appID int64, pem []byte) (*GitHubApp, error) {
	tr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, pem)
	if err != nil {
		return nil, fmt.Errorf("init github app transport: %w", err)
	}
	return &GitHubApp{appsTransport: tr}, nil
}

// MintToken returns a short-lived installation token (≈1h) for the given
// installation. Safe for concurrent use.
func (g *GitHubApp) MintToken(ctx context.Context, installationID int64) (string, error) {
	itr := ghinstallation.NewFromAppsTransport(g.appsTransport, installationID)
	token, err := itr.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("mint installation token: %w", err)
	}
	return token, nil
}
