package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/recruitinbox"
	"github.com/multica-ai/multica/server/pkg/llm"
)

type config struct {
	AppID           string
	AppSecret       string
	AllowedChatID   string
	BotOpenID       string
	HashKey         []byte
	DatabaseURL     string
	OpenAIKey       string
	OpenAIBaseURL   string
	Model           string
	ImageModel      string
	AudioModel      string
	RoleVersion     string
	HTTPAddr        string
	LarkHTTPURL     string
	LarkCallbackURL string
	LarkWSProxyURL  string
	Workers         int
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	cfg, err := loadConfig()
	if err != nil {
		log.Error("recruit inbox: invalid configuration", "error_code", "config_invalid")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ledger, err := recruitinbox.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("recruit inbox: ledger unavailable", "error_code", "ledger_unavailable")
		os.Exit(1)
	}
	defer ledger.Close()

	llmClient := llm.New(llm.Config{APIKey: cfg.OpenAIKey, BaseURL: cfg.OpenAIBaseURL, DefaultModel: cfg.Model})
	analyzer, err := recruitinbox.NewOpenAIAnalyzer(llmClient, cfg.Model, cfg.ImageModel, cfg.AudioModel)
	if err != nil {
		log.Error("recruit inbox: analyzer unavailable", "error_code", "analyzer_unavailable")
		os.Exit(1)
	}
	creds := lark.InstallationCredentials{AppID: cfg.AppID, AppSecret: cfg.AppSecret, Region: lark.RegionFeishu}
	larkClient := lark.NewHTTPAPIClient(lark.HTTPClientConfig{BaseURL: cfg.LarkHTTPURL, Logger: log})
	transport := &feishuTransport{api: larkClient, creds: creds, allowedChatID: cfg.AllowedChatID}
	processor, err := recruitinbox.NewProcessor(recruitinbox.Config{
		AllowedChatID: cfg.AllowedChatID,
		BotOpenID:     cfg.BotOpenID,
		HashKey:       cfg.HashKey,
		RoleVersion:   cfg.RoleVersion,
		Logger:        log,
	}, ledger, analyzer, transport)
	if err != nil {
		log.Error("recruit inbox: processor unavailable", "error_code", "processor_unavailable")
		os.Exit(1)
	}

	queue := make(chan recruitinbox.Inbound, 64)
	var inFlight sync.Map
	enqueue := func(in recruitinbox.Inbound) {
		if _, loaded := inFlight.LoadOrStore(in.MessageID, struct{}{}); loaded {
			return
		}
		select {
		case queue <- in:
		default:
			// The event is already durable. Release the in-memory claim; the
			// pending scanner will fetch and enqueue it after workers catch up.
			inFlight.Delete(in.MessageID)
		}
	}
	var workers sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for in := range queue {
				workCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				if err := processor.Process(workCtx, in); err != nil {
					log.Error("recruit inbox: event processing infrastructure failure", "error_code", "processing_infrastructure_failure")
				}
				cancel()
				inFlight.Delete(in.MessageID)
			}
		}()
	}

	var connected atomic.Bool
	go serveHealth(ctx, cfg.HTTPAddr, ledger, &connected, log)
	if err := recoverPending(ctx, processor, transport, enqueue, log); err != nil {
		log.Error("recruit inbox: pending recovery failed", "error_code", "pending_recovery_failed")
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		scanPending(ctx, processor, transport, enqueue, log)
	}()

	connector, err := buildConnector(cfg, creds, &connected, log)
	if err != nil {
		log.Error("recruit inbox: connector initialization failed", "error_code", "connector_initialization_failed")
		os.Exit(1)
	}
	inst := lark.Installation{AppID: cfg.AppID, BotOpenID: cfg.BotOpenID, Region: string(lark.RegionFeishu), ID: pgtype.UUID{Valid: false}}

	for ctx.Err() == nil {
		err := connector.Run(ctx, inst, func(eventCtx context.Context, msg lark.InboundMessage) (lark.DispatchResult, error) {
			in := inboundFromLark(msg)
			claimed, err := processor.Admit(eventCtx, in)
			if err != nil {
				return lark.DispatchResult{}, err
			}
			if claimed {
				enqueue(in)
			}
			return lark.DispatchResult{}, nil
		})
		connected.Store(false)
		if ctx.Err() != nil {
			break
		}
		log.Warn("recruit inbox: Feishu connection ended", "error_code", "feishu_connection_ended")
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		_ = err
	}
	close(queue)
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(95 * time.Second):
		log.Error("recruit inbox: worker shutdown timed out", "error_code", "worker_shutdown_timeout")
	}
}

func loadConfig() (config, error) {
	hashKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_HASH_KEY")))
	if err != nil {
		return config{}, errors.New("invalid RECRUITMENT_INBOX_HASH_KEY")
	}
	workers, _ := strconv.Atoi(os.Getenv("RECRUITMENT_INBOX_WORKERS"))
	if workers <= 0 || workers > 8 {
		workers = 2
	}
	cfg := config{
		AppID:           strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_FEISHU_APP_ID")),
		AppSecret:       strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_FEISHU_APP_SECRET")),
		AllowedChatID:   strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_ALLOWED_CHAT_ID")),
		BotOpenID:       strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_BOT_OPEN_ID")),
		HashKey:         hashKey,
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		OpenAIKey:       strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_OPENAI_API_KEY")),
		OpenAIBaseURL:   strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_OPENAI_BASE_URL")),
		Model:           strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_MODEL")),
		ImageModel:      strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_IMAGE_MODEL")),
		AudioModel:      strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_AUDIO_MODEL")),
		RoleVersion:     strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_ROLE_VERSION")),
		HTTPAddr:        strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_HTTP_ADDR")),
		LarkHTTPURL:     strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_FEISHU_HTTP_BASE_URL")),
		LarkCallbackURL: strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_FEISHU_CALLBACK_BASE_URL")),
		LarkWSProxyURL:  strings.TrimSpace(os.Getenv("RECRUITMENT_INBOX_FEISHU_WS_PROXY_URL")),
		Workers:         workers,
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.6-luna"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}
	if cfg.RoleVersion == "" {
		cfg.RoleVersion = "v1"
	}
	if cfg.AppID == "" || cfg.AppSecret == "" || cfg.AllowedChatID == "" || cfg.DatabaseURL == "" || len(cfg.HashKey) < 32 || (cfg.OpenAIKey == "" && cfg.OpenAIBaseURL == "") {
		return config{}, errors.New("required secret/config missing")
	}
	return cfg, nil
}

func buildConnector(cfg config, creds lark.InstallationCredentials, connected *atomic.Bool, log *slog.Logger) (*lark.WSLongConnConnector, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	if cfg.LarkWSProxyURL != "" {
		proxyURL, err := url.Parse(cfg.LarkWSProxyURL)
		if err != nil {
			return nil, err
		}
		dialer.Proxy = http.ProxyURL(proxyURL)
	}
	fetcher, err := lark.NewHTTPConnectionTokenFetcher(lark.HTTPConnectionTokenConfig{BaseURL: cfg.LarkCallbackURL, Logger: log})
	if err != nil {
		return nil, err
	}
	return lark.NewWSLongConnConnector(lark.WSConnectorConfig{
		Dialer:          wsDialer{dialer: &dialer},
		EndpointFetcher: fetcher,
		FrameDecoder:    lark.NewLarkJSONFrameDecoder(),
		CredentialsProvider: lark.CredentialsProviderFunc(func(context.Context, lark.Installation) (lark.InstallationCredentials, error) {
			return creds, nil
		}),
		Logger:         log,
		OnConnected:    func() { connected.Store(true) },
		OnDisconnected: func() { connected.Store(false) },
	})
}

type wsDialer struct{ dialer *websocket.Dialer }

func (d wsDialer) DialContext(ctx context.Context, urlStr string, header http.Header) (lark.WSConn, *http.Response, error) {
	return d.dialer.DialContext(ctx, urlStr, header)
}

type feishuTransport struct {
	api           lark.APIClient
	creds         lark.InstallationCredentials
	allowedChatID string
}

func (t *feishuTransport) Get(ctx context.Context, messageID string) (recruitinbox.Inbound, error) {
	items, err := t.api.GetMessage(ctx, t.creds, messageID)
	if err != nil {
		return recruitinbox.Inbound{}, err
	}
	if len(items) == 0 {
		return recruitinbox.Inbound{}, errors.New("Feishu message not found")
	}
	it := items[0]
	return recruitinbox.Inbound{MessageID: it.MessageID, ChatID: t.allowedChatID, SenderType: it.SenderType, MessageType: it.MessageType, Content: it.Content, CreatedAt: parseMillis(it.CreateTime)}, nil
}

func (t *feishuTransport) Download(ctx context.Context, messageID string, ref recruitinbox.ResourceRef) (recruitinbox.Resource, error) {
	streamer, ok := t.api.(interface {
		DownloadMessageResourceStream(context.Context, lark.InstallationCredentials, lark.DownloadResourceParams) (lark.DownloadedResourceStream, error)
	})
	if !ok {
		return recruitinbox.Resource{}, errors.New("streaming resource API unavailable")
	}
	got, err := streamer.DownloadMessageResourceStream(ctx, t.creds, lark.DownloadResourceParams{MessageID: messageID, FileKey: ref.Key, Type: ref.Kind})
	if err != nil {
		return recruitinbox.Resource{}, err
	}
	return recruitinbox.Resource{Body: got.Body, Filename: got.Filename, ContentType: got.ContentType}, nil
}

func (t *feishuTransport) Reply(ctx context.Context, chatID, text, key string) (string, error) {
	return t.api.SendTextMessage(ctx, lark.SendTextParams{InstallationID: t.creds, ChatID: lark.ChatID(chatID), Text: text, IdempotencyKey: key})
}

func inboundFromLark(msg lark.InboundMessage) recruitinbox.Inbound {
	return recruitinbox.Inbound{MessageID: msg.MessageID, ChatID: string(msg.ChatID), SenderID: string(msg.SenderOpenID), SenderType: msg.SenderType, MessageType: msg.MessageType, Content: msg.Content, CreatedAt: parseMillis(msg.CreateTime)}
}

func parseMillis(raw string) time.Time {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

func recoverPending(ctx context.Context, p *recruitinbox.Processor, transport *feishuTransport, enqueue func(recruitinbox.Inbound), log *slog.Logger) error {
	records, err := p.Pending(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		in, err := transport.Get(ctx, record.SourceMessageID)
		if err != nil {
			log.Warn("recruit inbox: could not recover pending event", "message_key", record.MessageKey, "error_code", "pending_source_unavailable")
			continue
		}
		enqueue(in)
	}
	return nil
}

func scanPending(ctx context.Context, p *recruitinbox.Processor, transport *feishuTransport, enqueue func(recruitinbox.Inbound), log *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := recoverPending(ctx, p, transport, enqueue, log); err != nil {
				log.Error("recruit inbox: pending scan failed", "error_code", "pending_scan_failed")
			}
		}
	}
}

func serveHealth(ctx context.Context, addr string, ledger recruitinbox.Ledger, connected *atomic.Bool, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if !connected.Load() || ledger.Health(checkCtx) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("recruit inbox: health server stopped", "error_code", "health_server_stopped")
	}
}
