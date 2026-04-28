package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	connectapi "github.com/multica-ai/multica-connect/api"
	"github.com/multica-ai/multica-connect/channel"
	"github.com/multica-ai/multica-connect/channel/telegram"
	"github.com/multica-ai/multica-connect/handler"
	"github.com/multica-ai/multica-connect/multica"
	"github.com/multica-ai/multica-connect/store"
	"github.com/multica-ai/multica-connect/triage"
)

func main() {
	// Required env vars.
	telegramToken := requireEnv("TELEGRAM_BOT_TOKEN")
	anthropicKey := requireEnv("ANTHROPIC_API_KEY")
	multicaPAT := requireEnv("MULTICA_PAT")
	multicaURL := envOrDefault("MULTICA_URL", "http://localhost:8180")
	multicaWebURL := envOrDefault("MULTICA_WEB_URL", "http://localhost:4200")
	workspaceID := requireEnv("MULTICA_WORKSPACE_ID")
	obsidianVault := envOrDefault("OBSIDIAN_VAULT", "Brain")
	listenAddr := envOrDefault("LISTEN_ADDR", ":8190")
	remindersPath := envOrDefault("REMINDERS_PATH", "reminders.json")
	dbPath := envOrDefault("CONNECT_DB_PATH", "connect.db")

	// Initialize note store.
	noteStore, err := store.NewNoteStore(dbPath)
	if err != nil {
		slog.Error("failed to open note store", "error", err)
		os.Exit(1)
	}
	defer noteStore.Close()

	// Initialize components.
	multicaClient := multica.NewClient(multicaURL, multicaPAT, workspaceID)
	classifier := triage.NewClassifier(anthropicKey)
	claudeClient := anthropic.NewClient(option.WithAPIKey(anthropicKey))

	tg := telegram.New(telegramToken)

	// Reminder store with fire callback that sends back to Telegram.
	reminderStore := handler.NewReminderStore(remindersPath, func(r handler.Reminder) {
		slog.Info("reminder fired", "title", r.Title, "thread_id", r.ThreadID)
		tg.Reply(context.Background(), channel.Reply{
			Text:      fmt.Sprintf("⏰ Påmindelse: *%s*\n%s", r.Title, r.Body),
			ThreadID:  r.ThreadID,
			ParseMode: "Markdown",
		})
	})

	router := &handler.Router{
		Issue: &handler.IssueHandler{
			Client:  multicaClient,
			BaseURL: multicaWebURL,
		},
		Note: &handler.NoteHandler{
			VaultName: obsidianVault,
			Store:     noteStore,
		},
		Reminder: &handler.ReminderHandler{
			Store: reminderStore,
		},
		Answer: &handler.AnswerHandler{
			Claude:       claudeClient,
			MulticClient: multicaClient,
		},
	}

	// Message handler: classify → route → reply.
	onMessage := func(ctx context.Context, msg channel.Message) {
		slog.Info("incoming message",
			"source", msg.Source,
			"user", msg.UserName,
			"text", truncate(msg.Text, 100),
			"is_reply", msg.IsReply,
		)

		// If this is a reply to a previous thread, reuse that context.
		if msg.IsReply && msg.ReplyToThreadID != "" {
			msg.ThreadID = msg.ReplyToThreadID
		}

		result, err := classifier.Classify(ctx, msg.Text)
		if err != nil {
			slog.Error("classification failed", "error", err)
			tg.Reply(ctx, channel.Reply{
				Text:     "❌ Kunne ikke forstå beskeden. Prøv igen.",
				ThreadID: msg.ThreadID,
			})
			return
		}

		resp, err := router.Route(ctx, msg, result)
		if err != nil {
			slog.Error("handler failed", "category", result.Category, "error", err)
			tg.Reply(ctx, channel.Reply{
				Text:     fmt.Sprintf("❌ Fejl ved %s: %s", result.Category, err.Error()),
				ThreadID: msg.ThreadID,
			})
			return
		}

		if err := tg.Reply(ctx, channel.Reply{
			Text:      resp.Text,
			ThreadID:  msg.ThreadID,
			ParseMode: resp.ParseMode,
		}); err != nil {
			slog.Error("telegram reply failed", "error", err)
		}
	}

	// Start reminder scheduler.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go reminderStore.StartScheduler(ctx)

	// HTTP server for webhook + notes API.
	mux := http.NewServeMux()
	mux.Handle("/webhook/telegram", tg.WebhookHandler(onMessage))
	notesAPI := &connectapi.NotesAPI{Store: noteStore}
	notesAPI.RegisterRoutes(mux)
	ingestAPI := &connectapi.IngestAPI{Classifier: classifier, Router: router}
	ingestAPI.RegisterRoutes(mux)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	slog.Info("multica-connect starting", "addr", listenAddr)

	srv := &http.Server{Addr: listenAddr, Handler: mux}

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down")
		cancel()
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "required env var %s is not set\n", key)
		os.Exit(1)
	}
	return v
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
