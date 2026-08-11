// Package service: adapter bridging the MemoryHub HTTP client to the
// RemoteClient boundary used by the binding lifecycle. Owner: ALL-16.
//
// The integrations/memoryhub.Client speaks in typed FindOrCreateRequest /
// RemoteRef payloads; the service layer consumes a string-returning
// RemoteClient. This adapter is the ONLY bridge; it lives in the service
// package so cmd/server can wire it without reaching into client internals.
package service

import (
	"context"
	"errors"

	"github.com/multica-ai/multica/server/internal/integrations/memoryhub"
)

// ErrMemoryHubUnconfigured is returned by the adapter when no MemoryHub client
// was configured. Callers that can proceed without remote side effects (e.g.
// the review-repair flow) are unaffected; binding operations surface it as a
// transient/fatal classification.
var ErrMemoryHubUnconfigured = errors.New("memoryhub: remote client not configured")

// memoryHubRemoteAdapter adapts *memoryhub.Client to the RemoteClient
// interface used by MemoryHubService.
type memoryHubRemoteAdapter struct {
	client *memoryhub.Client
}

var _ RemoteClient = (*memoryHubRemoteAdapter)(nil)

// NewMemoryHubRemoteClient wraps a configured client as a RemoteClient.
func NewMemoryHubRemoteClient(c *memoryhub.Client) RemoteClient {
	if c == nil {
		return unconfiguredRemoteClient{}
	}
	return &memoryHubRemoteAdapter{client: c}
}

func (a *memoryHubRemoteAdapter) FindOrCreateTeam(ctx context.Context, kind, remoteID string) (string, error) {
	ref, err := a.client.FindOrCreateTeam(ctx, memoryhub.FindOrCreateRequest{Kind: kind, RemoteID: remoteID})
	if err != nil {
		return "", err
	}
	return ref.TeamID, nil
}

func (a *memoryHubRemoteAdapter) FindOrCreateAgent(ctx context.Context, kind, remoteID string) (string, error) {
	ref, err := a.client.FindOrCreateAgent(ctx, memoryhub.FindOrCreateRequest{Kind: kind, RemoteID: remoteID})
	if err != nil {
		return "", err
	}
	return ref.AgentID, nil
}

func (a *memoryHubRemoteAdapter) FindOrCreateTask(ctx context.Context, kind, remoteID string) (string, error) {
	ref, err := a.client.FindOrCreateTask(ctx, memoryhub.FindOrCreateRequest{Kind: kind, RemoteID: remoteID})
	if err != nil {
		return "", err
	}
	return ref.TaskID, nil
}

func (a *memoryHubRemoteAdapter) FindRemote(ctx context.Context, kind, remoteID string) (string, error) {
	ref, err := a.client.FindRemote(ctx, kind, remoteID)
	if err != nil {
		return "", err
	}
	if ref.TeamID != "" {
		return ref.TeamID, nil
	}
	if ref.AgentID != "" {
		return ref.AgentID, nil
	}
	return ref.TaskID, nil
}

func (a *memoryHubRemoteAdapter) DeleteRemote(ctx context.Context, remoteID string) error {
	return a.client.DeleteRemote(ctx, remoteID)
}

// unconfiguredRemoteClient is the nil-safe RemoteClient used when no MemoryHub
// client was configured. Every remote call fails closed; the DB-backed
// review-repair flow never touches it.
type unconfiguredRemoteClient struct{}

func (unconfiguredRemoteClient) FindOrCreateTeam(context.Context, string, string) (string, error) {
	return "", ErrMemoryHubUnconfigured
}
func (unconfiguredRemoteClient) FindOrCreateAgent(context.Context, string, string) (string, error) {
	return "", ErrMemoryHubUnconfigured
}
func (unconfiguredRemoteClient) FindOrCreateTask(context.Context, string, string) (string, error) {
	return "", ErrMemoryHubUnconfigured
}
func (unconfiguredRemoteClient) FindRemote(context.Context, string, string) (string, error) {
	return "", ErrMemoryHubUnconfigured
}
func (unconfiguredRemoteClient) DeleteRemote(context.Context, string) error {
	return ErrMemoryHubUnconfigured
}
