package gateway

import (
	"context"
	"fmt"
	"sync"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// RegistryGateway routes provider-neutral runtime operations to the active
// adapter registered for a concrete channel connection.
type RegistryGateway struct {
	registry *channel.Registry

	mu              sync.RWMutex
	fileDownloaders map[string]port.FileDownloader
}

func NewRegistryGateway(registry *channel.Registry) *RegistryGateway {
	return &RegistryGateway{
		registry:        registry,
		fileDownloaders: make(map[string]port.FileDownloader),
	}
}

func (g *RegistryGateway) RegisterConnection(connectionID string, fileDownloader port.FileDownloader) {
	if g == nil || connectionID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if fileDownloader != nil {
		g.fileDownloaders[connectionID] = fileDownloader
	} else {
		delete(g.fileDownloaders, connectionID)
	}
}

func (g *RegistryGateway) UnregisterConnection(connectionID string) {
	if g == nil || connectionID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fileDownloaders, connectionID)
}

func (g *RegistryGateway) SendText(ctx context.Context, connectionID string, msg port.OutboundMessage) (port.SendResult, error) {
	ch, err := g.channel(connectionID)
	if err != nil {
		return port.SendResult{Retryable: true}, err
	}
	return ch.Send(ctx, msg)
}

func (g *RegistryGateway) SendRich(ctx context.Context, connectionID string, msg port.OutboundRichMessage) (port.SendResult, error) {
	ch, err := g.channel(connectionID)
	if err != nil {
		return port.SendResult{Retryable: true}, err
	}
	return ch.SendCard(ctx, msg)
}

func (g *RegistryGateway) GetChatInfo(ctx context.Context, connectionID, chatID string) (port.ChatInfo, error) {
	ch, err := g.channel(connectionID)
	if err != nil {
		return port.ChatInfo{}, err
	}
	return ch.GetChatInfo(ctx, chatID)
}

func (g *RegistryGateway) GetUserInfo(ctx context.Context, connectionID, userID string) (port.UserInfo, error) {
	ch, err := g.channel(connectionID)
	if err != nil {
		return port.UserInfo{}, err
	}
	return ch.GetUserInfo(ctx, userID)
}

func (g *RegistryGateway) FileDownloader(connectionID string) (port.FileDownloader, bool) {
	if g == nil || connectionID == "" {
		return nil, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	downloader, ok := g.fileDownloaders[connectionID]
	return downloader, ok && downloader != nil
}

func (g *RegistryGateway) channel(connectionID string) (port.Channel, error) {
	if g == nil || g.registry == nil {
		return nil, fmt.Errorf("channel gateway is not configured")
	}
	if connectionID == "" {
		return nil, fmt.Errorf("channel connection id is empty")
	}
	ch, err := g.registry.Get(connectionID)
	if err != nil {
		return nil, fmt.Errorf("channel connection %q not in registry: %w", connectionID, err)
	}
	return ch, nil
}

var _ port.ChannelGateway = (*RegistryGateway)(nil)
