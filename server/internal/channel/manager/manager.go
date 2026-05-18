package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel"
	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	"github.com/multica-ai/multica/server/internal/channel/gateway"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/leader"
	channelmetrics "github.com/multica-ai/multica/server/internal/channel/metrics"
	"github.com/multica-ai/multica/server/internal/channel/outbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/channel/provider"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type RuntimeComponents struct {
	PrePipeline        *inbound.Pipeline
	PostPipeline       *inbound.Pipeline
	RuleResolvers      []chintent.IntentResolver
	ChannelTurn        chintent.ChannelAgentTurnClient
	DispatchStore      inbound.DispatchCompletionStore
	ConversationStore  channelconversation.Store
	ContextMaxEntities int
}

type RuntimeBuilder func() RuntimeComponents

type Config struct {
	Pool           *pgxpool.Pool
	Queries        *db.Queries
	Bus            *events.Bus
	Registry       *channel.Registry
	Gateway        *gateway.RegistryGateway
	Factories      []provider.Factory
	RuntimeBuilder RuntimeBuilder

	ConversationLimit      int
	GlobalLimit            int
	Workers                int
	ClaimBatch             int
	AgentTaskTimeout      time.Duration
	ActionTaskTimeout      time.Duration
	ProcessingLease        time.Duration
	OutboundCleanupEnabled bool
}

type Manager struct {
	cfg Config

	mu      sync.Mutex
	started bool
	cancels []context.CancelFunc
	ready   map[string]*atomic.Bool
	running map[string]*runningConnection
	runtime *inbound.Runtime
}

func New(cfg Config) *Manager {
	if cfg.Gateway == nil && cfg.Registry != nil {
		cfg.Gateway = gateway.NewRegistryGateway(cfg.Registry)
	}
	return &Manager{
		cfg:     cfg,
		ready:   make(map[string]*atomic.Bool),
		running: make(map[string]*runningConnection),
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	m.started = true

	m.startInboundRuntimeLocked(ctx)
	m.startOutbox(ctx)
	m.startReconcileLoopLocked(ctx)
	go m.reconcile(ctx)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.cancels {
		cancel()
	}
	m.cancels = nil
	for _, running := range m.running {
		running.cancel()
		if running.subscriber != nil {
			running.subscriber.Stop()
		}
	}
	m.running = make(map[string]*runningConnection)
	m.runtime = nil
	m.started = false
}

func (m *Manager) IsReady(connectionID string) bool {
	m.mu.Lock()
	ready := m.ready[connectionID]
	m.mu.Unlock()
	return ready != nil && ready.Load()
}

type envConnection struct {
	factory provider.Factory
	config  provider.ConnectionConfig
}

type runningConnection struct {
	cancel     context.CancelFunc
	ready      *atomic.Bool
	subscriber *outbound.Subscriber
	configHash string
}

func (m *Manager) connections(ctx context.Context) []envConnection {
	if m.cfg.Queries != nil {
		rows, err := m.cfg.Queries.ListChannelConnections(ctx)
		if err != nil {
			slog.Error("channel manager: list configured connections failed", "error", err)
			return nil
		}
		if len(rows) == 0 && m.envBootstrapAllowed() {
			if err := m.bootstrapEnvConnections(ctx); err != nil {
				slog.Error("channel manager: env bootstrap failed", "error", err)
				return nil
			}
			rows, err = m.cfg.Queries.ListChannelConnections(ctx)
			if err != nil {
				slog.Error("channel manager: list bootstrapped connections failed", "error", err)
				return nil
			}
		}
		if len(rows) == 0 {
			return nil
		}
		enabled := make([]db.ChannelConnection, 0, len(rows))
		for _, row := range rows {
			if row.Enabled {
				enabled = append(enabled, row)
			}
		}
		return m.dbConnections(ctx, enabled)
	}
	return m.envConnections()
}

func (m *Manager) envBootstrapAllowed() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("CHANNEL_ENV_BOOTSTRAP")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	return appEnv != "production" && appEnv != "prod"
}

func (m *Manager) bootstrapEnvConnections(ctx context.Context) error {
	if m.cfg.Queries == nil {
		return nil
	}
	for _, entry := range m.envConnections() {
		config, secrets, err := splitConnectionValues(entry.factory.ConfigSchema(), entry.config.Values)
		if err != nil {
			return err
		}
		if err := m.cfg.Queries.BootstrapChannelConnection(ctx, db.BootstrapChannelConnectionParams{
			ID:           entry.config.ConnectionID,
			Provider:     entry.config.Provider,
			DisplayName:  entry.config.DisplayName,
			IsDefault:    true,
			Config:       mustMarshalConfig(config),
			SecretConfig: mustMarshalConfig(secrets),
		}); err != nil {
			return fmt.Errorf("bootstrap %s: %w", entry.config.ConnectionID, err)
		}
		slog.Info("channel manager: bootstrapped env channel connection", "provider", entry.config.Provider, "connection_id", entry.config.ConnectionID)
	}
	return nil
}

func splitConnectionValues(fields []provider.ConfigField, values map[string]string) (map[string]string, map[string]string, error) {
	config := map[string]string{}
	secrets := map[string]string{}
	secretFields := make(map[string]bool, len(fields))
	for _, field := range fields {
		secretFields[field.Key] = field.Secret
	}
	for key, value := range values {
		if secretFields[key] {
			secrets[key] = value
			continue
		}
		config[key] = value
	}
	return config, secrets, nil
}

func mustMarshalConfig(values map[string]string) []byte {
	if values == nil {
		return []byte(`{}`)
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func (m *Manager) dbConnections(ctx context.Context, rows []db.ChannelConnection) []envConnection {
	factories := make(map[string]provider.Factory, len(m.cfg.Factories))
	for _, factory := range m.cfg.Factories {
		factories[factory.Provider()] = factory
	}
	out := make([]envConnection, 0, len(rows))
	for _, row := range rows {
		factory := factories[row.Provider]
		if factory == nil {
			slog.Warn("channel manager: configured provider has no factory", "provider", row.Provider, "connection_id", row.ID)
			continue
		}
		values := map[string]string{}
		valid := true
		for _, rawConfig := range [][]byte{row.Config, row.SecretConfig} {
			if len(rawConfig) == 0 {
				continue
			}
			configured := map[string]string{}
			if err := json.Unmarshal(rawConfig, &configured); err != nil {
				slog.Error("channel manager: invalid connection config", "provider", row.Provider, "connection_id", row.ID, "error", err)
				m.updateConnectionStatus(ctx, row.ID, "error", fmt.Errorf("invalid connection config: %w", err))
				valid = false
				break
			}
			for key, value := range configured {
				values[key] = value
			}
		}
		if !valid {
			continue
		}
		if err := validateRequiredConfig(factory.ConfigSchema(), values); err != nil {
			slog.Error("channel manager: connection config missing required fields", "provider", row.Provider, "connection_id", row.ID, "error", err)
			m.updateConnectionStatus(ctx, row.ID, "error", err)
			continue
		}
		out = append(out, envConnection{
			factory: factory,
			config: provider.ConnectionConfig{
				Provider:     row.Provider,
				ConnectionID: row.ID,
				DisplayName:  row.DisplayName,
				Enabled:      row.Enabled,
				Values:       values,
			},
		})
	}
	return out
}

func validateRequiredConfig(fields []provider.ConfigField, values map[string]string) error {
	for _, field := range fields {
		if !field.Required {
			continue
		}
		if strings.TrimSpace(values[field.Key]) == "" {
			return fmt.Errorf("missing required config field: %s", field.Key)
		}
	}
	return nil
}

func (m *Manager) envConnections() []envConnection {
	out := make([]envConnection, 0, len(m.cfg.Factories))
	for _, factory := range m.cfg.Factories {
		cfg := factory.EnvConfig()
		if cfg.Provider == "" {
			cfg.Provider = factory.Provider()
		}
		if cfg.DisplayName == "" {
			cfg.DisplayName = factory.DisplayName()
		}
		if cfg.ConnectionID == "" {
			cfg.ConnectionID = cfg.Provider
		}
		if !cfg.Enabled {
			slog.Info("channel manager: provider disabled", "provider", cfg.Provider, "display_name", cfg.DisplayName)
			continue
		}
		out = append(out, envConnection{factory: factory, config: cfg})
	}
	return out
}

func (m *Manager) startInboundRuntimeLocked(ctx context.Context) {
	if m.cfg.RuntimeBuilder == nil || m.runtime != nil {
		return
	}
	components := m.cfg.RuntimeBuilder()
	inboundRuntime := inbound.NewRuntime(inbound.RuntimeConfig{
		Store:              inbound.NewDBInboundEventStore(m.cfg.Pool),
		PrePipeline:        components.PrePipeline,
		PostPipeline:       components.PostPipeline,
		RuleResolvers:      components.RuleResolvers,
		ChannelTurn:        components.ChannelTurn,
		DispatchStore:      components.DispatchStore,
		ConversationStore:  components.ConversationStore,
		ContextMaxEntities: components.ContextMaxEntities,
		ReplySink:          inbound.NewGatewayReplySink(m.cfg.Gateway, inbound.WithGatewayReplyConversationStore(channelconversation.NewDBStore(m.cfg.Pool))),
		Workers:            m.cfg.Workers,
		ClaimBatch:         m.cfg.ClaimBatch,
		AgentTaskTimeout:  m.cfg.AgentTaskTimeout,
		ActionTaskTimeout:  m.cfg.ActionTaskTimeout,
		ProcessingLease:    m.cfg.ProcessingLease,
	})
	runtimeCtx, cancel := context.WithCancel(ctx)
	m.cancels = append(m.cancels, cancel)
	m.runtime = inboundRuntime
	go inboundRuntime.Run(runtimeCtx)
	slog.Info("channel manager: inbound runtime started", "workers", m.cfg.Workers, "claim_batch", m.cfg.ClaimBatch)
}

func (m *Manager) startReconcileLoopLocked(ctx context.Context) {
	reconcileCtx, cancel := context.WithCancel(ctx)
	m.cancels = append(m.cancels, cancel)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reconcileCtx.Done():
				return
			case <-ticker.C:
				m.reconcile(reconcileCtx)
			}
		}
	}()
}

func (m *Manager) reconcile(ctx context.Context) {
	desiredEntries := m.connections(ctx)
	desired := make(map[string]envConnection, len(desiredEntries))
	hashes := make(map[string]string, len(desiredEntries))
	for _, entry := range desiredEntries {
		if entry.config.ConnectionID == "" {
			continue
		}
		desired[entry.config.ConnectionID] = entry
		hashes[entry.config.ConnectionID] = connectionConfigHash(entry.config)
	}

	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	for connectionID, running := range m.running {
		if _, ok := desired[connectionID]; !ok || running.configHash != hashes[connectionID] {
			slog.Info("channel manager: stopping channel connection", "connection_id", connectionID)
			running.cancel()
			if running.subscriber != nil {
				running.subscriber.Stop()
			}
			delete(m.running, connectionID)
			delete(m.ready, connectionID)
		}
	}
	for connectionID, entry := range desired {
		if _, ok := m.running[connectionID]; ok {
			continue
		}
		ready := &atomic.Bool{}
		connCtx, cancel := context.WithCancel(ctx)
		sub := m.startOutbound(entry.config, ready)
		m.ready[connectionID] = ready
		m.running[connectionID] = &runningConnection{
			cancel:     cancel,
			ready:      ready,
			subscriber: sub,
			configHash: hashes[connectionID],
		}
		slog.Info("channel manager: starting channel connection",
			"provider", entry.config.Provider,
			"connection_id", connectionID,
		)
		m.startConnection(connCtx, entry.factory, entry.config, ready)
	}
	if len(desired) == 0 {
		slog.Info("channel manager: no enabled channel providers")
	}
	m.mu.Unlock()
}

func (m *Manager) startConnection(ctx context.Context, factory provider.Factory, cfg provider.ConnectionConfig, ready *atomic.Bool) {
	lockID, needsLeader := factory.LeaderLockID(cfg)
	if needsLeader {
		elector := leader.NewElector(m.cfg.Pool, lockID, 5*time.Second)
		var adapterCancel context.CancelFunc
		elector.OnAcquire(func(acquireCtx context.Context) error {
			channelmetrics.M.SetLeaderState(cfg.Provider, true)
			channelmetrics.M.SetAdapterConnected(cfg.Provider, false)
			slog.Info("channel manager: leader acquired", "provider", cfg.Provider, "lock_id", lockID)

			adapterCtx, cancel := context.WithCancel(context.Background())
			adapterCancel = cancel
			go m.runAdapterLoop(adapterCtx, factory, cfg, ready)
			return nil
		})
		elector.OnRelease(func(releaseCtx context.Context) error {
			slog.Info("channel manager: leader released", "provider", cfg.Provider, "lock_id", lockID)
			ready.Store(false)
			channelmetrics.M.SetAdapterConnected(cfg.Provider, false)
			channelmetrics.M.SetLeaderState(cfg.Provider, false)
			if adapterCancel != nil {
				adapterCancel()
			}
			m.unregisterConnection(releaseCtx, cfg)
			return nil
		})
		leaderCtx, cancel := context.WithCancel(ctx)
		go func() {
			defer cancel()
			if err := elector.Run(leaderCtx); err != nil {
				slog.Error("channel manager: leader terminated", "provider", cfg.Provider, "error", err)
			}
		}()
		return
	}

	adapterCtx, cancel := context.WithCancel(ctx)
	go m.runAdapterLoop(adapterCtx, factory, cfg, ready)
	go func() {
		<-ctx.Done()
		cancel()
	}()
}

func (m *Manager) runAdapterLoop(ctx context.Context, factory provider.Factory, cfg provider.ConnectionConfig, ready *atomic.Bool) {
	delay := factory.ReconnectDelay(cfg)
	if delay <= 0 {
		delay = 5 * time.Second
	}
	for {
		if ctx.Err() != nil {
			return
		}
		ready.Store(false)
		channelmetrics.M.SetAdapterConnected(cfg.Provider, false)

		bundle, err := factory.Build(ctx, cfg)
		if err != nil {
			slog.Error("channel manager: build adapter failed", "provider", cfg.Provider, "error", err)
			m.updateConnectionStatus(ctx, cfg.ConnectionID, "error", err)
			waitReconnect(ctx, delay)
			continue
		}
		baseAdapter := bundle.Channel
		if baseAdapter == nil {
			slog.Error("channel manager: provider returned nil adapter", "provider", cfg.Provider)
			m.updateConnectionStatus(ctx, cfg.ConnectionID, "error", errors.New("provider returned nil adapter"))
			waitReconnect(ctx, delay)
			continue
		}
		adapter := newConnectionChannel(cfg.ConnectionID, baseAdapter)

		_ = m.cfg.Registry.Unregister(cfg.ConnectionID)
		if err := m.cfg.Registry.Register(adapter); err != nil {
			slog.Error("channel manager: register adapter failed", "provider", cfg.Provider, "error", err)
			m.updateConnectionStatus(ctx, cfg.ConnectionID, "error", err)
			waitReconnect(ctx, delay)
			continue
		}
		if m.cfg.Gateway != nil {
			m.cfg.Gateway.RegisterConnection(cfg.ConnectionID, bundle.FileDownloader)
		}
		if err := adapter.Connect(ctx); err != nil {
			slog.Error("channel manager: connect adapter failed; will retry", "provider", cfg.Provider, "error", err)
			m.updateConnectionStatus(ctx, cfg.ConnectionID, "error", err)
			if m.cfg.Gateway != nil {
				m.cfg.Gateway.UnregisterConnection(cfg.ConnectionID)
			}
			_ = m.cfg.Registry.Unregister(cfg.ConnectionID)
			waitReconnect(ctx, delay)
			continue
		}

		ready.Store(true)
		channelmetrics.M.SetAdapterConnected(cfg.Provider, true)
		m.updateConnectionStatus(ctx, cfg.ConnectionID, "connected", nil)
		slog.Info("channel manager: adapter connected", "provider", cfg.Provider, "connection_id", cfg.ConnectionID)

		reconnect := m.drainAdapterEvents(ctx, cfg, adapter)
		ready.Store(false)
		channelmetrics.M.SetAdapterConnected(cfg.Provider, false)
		m.updateConnectionStatus(ctx, cfg.ConnectionID, "configured", nil)
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := adapter.Disconnect(disconnectCtx); err != nil {
			slog.Warn("channel manager: adapter disconnect before reconnect failed", "provider", cfg.Provider, "error", err)
		}
		cancel()
		if m.cfg.Gateway != nil {
			m.cfg.Gateway.UnregisterConnection(cfg.ConnectionID)
		}
		_ = m.cfg.Registry.Unregister(cfg.ConnectionID)
		if !reconnect {
			return
		}
		waitReconnect(ctx, delay)
	}
}

func (m *Manager) drainAdapterEvents(ctx context.Context, cfg provider.ConnectionConfig, adapter port.Channel) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case evt, ok := <-adapter.Events():
			if !ok {
				slog.Warn("channel manager: adapter event stream closed; reconnecting", "provider", cfg.Provider)
				return true
			}
			if evt.ChannelConnectionID == "" {
				evt.ChannelConnectionID = cfg.ConnectionID
			}
			m.mu.Lock()
			inboundRuntime := m.runtime
			m.mu.Unlock()
			if inboundRuntime == nil {
				slog.Error("channel manager: inbound runtime is not running", "provider", cfg.Provider, "connection_id", cfg.ConnectionID)
				continue
			}
			result, err := inboundRuntime.Accept(ctx, evt, inbound.AcceptOptions{
				ConversationLimit: m.cfg.ConversationLimit,
				GlobalLimit:       m.cfg.GlobalLimit,
			})
			if err != nil {
				slog.Error("channel manager: inbound accept failed",
					"provider", evt.ChannelName,
					"chat_id", evt.ChatID,
					"event_id", evt.EventID,
					"error", err,
				)
				continue
			}
			slog.Debug("channel manager: inbound event accepted",
				"provider", evt.ChannelName,
				"chat_id", evt.ChatID,
				"event_id", evt.EventID,
				"row_id", result.EventID,
				"duplicate", result.Duplicate,
				"rejected_backpressure", result.RejectedBackpressure,
			)
		}
	}
}

func (m *Manager) unregisterConnection(ctx context.Context, cfg provider.ConnectionConfig) {
	if m.cfg.Gateway != nil {
		m.cfg.Gateway.UnregisterConnection(cfg.ConnectionID)
	}
	if ch, err := m.cfg.Registry.Get(cfg.ConnectionID); err == nil {
		if discErr := ch.Disconnect(ctx); discErr != nil {
			slog.Error("channel manager: adapter disconnect failed", "provider", cfg.Provider, "connection_id", cfg.ConnectionID, "error", discErr)
		}
		_ = m.cfg.Registry.Unregister(cfg.ConnectionID)
	}
}

func connectionConfigHash(cfg provider.ConnectionConfig) string {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Sprintf("%s:%s:%s", cfg.Provider, cfg.ConnectionID, cfg.DisplayName)
	}
	return string(raw)
}

func (m *Manager) updateConnectionStatus(ctx context.Context, connectionID, status string, runErr error) {
	if m.cfg.Queries == nil || connectionID == "" {
		return
	}
	lastError := pgtype.Text{}
	if runErr != nil {
		lastError = pgtype.Text{String: truncateStatusError(runErr), Valid: true}
	}
	if err := m.cfg.Queries.UpdateChannelConnectionStatus(ctx, db.UpdateChannelConnectionStatusParams{
		ID:        connectionID,
		Status:    status,
		LastError: lastError,
	}); err != nil {
		slog.Warn("channel manager: update connection status failed", "connection_id", connectionID, "status", status, "error", err)
	}
}

func truncateStatusError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	return msg
}

func (m *Manager) startOutbound(cfg provider.ConnectionConfig, _ *atomic.Bool) *outbound.Subscriber {
	if m.cfg.Bus == nil || m.cfg.Queries == nil || m.cfg.Pool == nil || m.cfg.Registry == nil {
		return nil
	}
	notificationStore := outbound.NewDBNotificationStore(m.cfg.Pool)
	sub := outbound.NewSubscriber(
		m.cfg.Bus,
		newRegistryChannel(m.cfg.Registry, cfg.Provider, cfg.ConnectionID),
		outbound.NewDBBindingStore(m.cfg.Pool),
		outbound.NewDBPrefStore(m.cfg.Queries),
		"",
	)
	sub.SetNotificationEnqueuer(notificationStore)
	sub.Start()
	slog.Info("channel manager: outbound subscriber started", "provider", cfg.Provider, "connection_id", cfg.ConnectionID)
	return sub
}

func (m *Manager) startOutbox(ctx context.Context) {
	if m.cfg.Pool == nil || m.cfg.Queries == nil || m.cfg.Registry == nil {
		return
	}
	outboxCtx, cancel := context.WithCancel(ctx)
	m.cancels = append(m.cancels, cancel)
	worker := outbound.NewOutboxWorker(outbound.NewDBNotificationStore(m.cfg.Pool), newRegistryRetrySender(m.cfg.Registry))
	worker.SetMessageRecorder(outbound.NewConversationMessageRecorder(channelconversation.NewDBStore(m.cfg.Pool)))
	worker.SetReadyConnectionsFunc(m.readyConnectionIDs)
	go worker.Run(outboxCtx)
	if m.cfg.OutboundCleanupEnabled {
		cleanupCtx, cancel := context.WithCancel(ctx)
		m.cancels = append(m.cancels, cancel)
		worker := outbound.NewCleanupWorker(m.cfg.Queries)
		go worker.Run(cleanupCtx)
	}
}

func (m *Manager) readyConnectionIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.ready))
	for id, ready := range m.ready {
		if ready != nil && ready.Load() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (m *Manager) anyProviderReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ready := range m.ready {
		if ready != nil && ready.Load() {
			return true
		}
	}
	return false
}

func waitReconnect(ctx context.Context, delay time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

type connectionChannel struct {
	provider     string
	connectionID string
	base         port.Channel
}

func newConnectionChannel(connectionID string, base port.Channel) *connectionChannel {
	return &connectionChannel{provider: base.Name(), connectionID: connectionID, base: base}
}

func (c *connectionChannel) Name() string { return c.connectionID }

func (c *connectionChannel) ProviderName() string { return c.provider }

func (c *connectionChannel) ConnectionID() string { return c.connectionID }

func (c *connectionChannel) Connect(ctx context.Context) error { return c.base.Connect(ctx) }

func (c *connectionChannel) Disconnect(ctx context.Context) error { return c.base.Disconnect(ctx) }

func (c *connectionChannel) Send(ctx context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	return c.base.Send(ctx, msg)
}

func (c *connectionChannel) SendCard(ctx context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	return c.base.SendCard(ctx, msg)
}

func (c *connectionChannel) Events() <-chan port.InboundEvent { return c.base.Events() }

func (c *connectionChannel) GetChatInfo(ctx context.Context, chatID string) (port.ChatInfo, error) {
	return c.base.GetChatInfo(ctx, chatID)
}

func (c *connectionChannel) GetUserInfo(ctx context.Context, userID string) (port.UserInfo, error) {
	return c.base.GetUserInfo(ctx, userID)
}

type registryChannel struct {
	registry     *channel.Registry
	provider     string
	connectionID string
}

func newRegistryChannel(registry *channel.Registry, providerName, connectionID string) *registryChannel {
	return &registryChannel{registry: registry, provider: providerName, connectionID: connectionID}
}

func (c *registryChannel) Name() string { return c.connectionID }

func (c *registryChannel) ProviderName() string { return c.provider }

func (c *registryChannel) ConnectionID() string { return c.connectionID }

func (c *registryChannel) Connect(context.Context) error { return nil }

func (c *registryChannel) Disconnect(context.Context) error { return nil }

func (c *registryChannel) Events() <-chan port.InboundEvent { return nil }

func (c *registryChannel) Send(ctx context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	ch, err := c.registry.Get(c.connectionID)
	if err != nil {
		return port.SendResult{Retryable: true}, fmt.Errorf("registry channel: get %s: %w", c.connectionID, err)
	}
	return ch.Send(ctx, msg)
}

func (c *registryChannel) SendCard(ctx context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	ch, err := c.registry.Get(c.connectionID)
	if err != nil {
		return port.SendResult{Retryable: true}, fmt.Errorf("registry channel: get %s: %w", c.connectionID, err)
	}
	return ch.SendCard(ctx, msg)
}

func (c *registryChannel) GetChatInfo(ctx context.Context, chatID string) (port.ChatInfo, error) {
	ch, err := c.registry.Get(c.connectionID)
	if err != nil {
		return port.ChatInfo{}, fmt.Errorf("registry channel: get %s: %w", c.connectionID, err)
	}
	return ch.GetChatInfo(ctx, chatID)
}

func (c *registryChannel) GetUserInfo(ctx context.Context, userID string) (port.UserInfo, error) {
	ch, err := c.registry.Get(c.connectionID)
	if err != nil {
		return port.UserInfo{}, fmt.Errorf("registry channel: get %s: %w", c.connectionID, err)
	}
	return ch.GetUserInfo(ctx, userID)
}

type registryRetrySender struct {
	registry *channel.Registry
}

func newRegistryRetrySender(registry *channel.Registry) *registryRetrySender {
	return &registryRetrySender{registry: registry}
}

func (s *registryRetrySender) SendCard(ctx context.Context, connectionID string, target port.OutboundTarget, payload outbound.RetryPayload) (port.SendResult, error) {
	ch, err := s.registry.Get(connectionID)
	if err != nil {
		return port.SendResult{Retryable: true}, outbound.WrapRetryable(fmt.Errorf("retry sender: get %s: %w", connectionID, err))
	}
	result, err := ch.SendCard(ctx, port.OutboundCardMessage{
		Target:   target,
		Title:    payload.Title,
		Body:     payload.Body,
		Mentions: payload.Mentions,
	})
	if err != nil && result.Retryable {
		return result, outbound.WrapRetryable(err)
	}
	return result, err
}

var (
	_ port.Channel         = (*registryChannel)(nil)
	_ port.Channel         = (*connectionChannel)(nil)
	_ outbound.RetrySender = (*registryRetrySender)(nil)
)
