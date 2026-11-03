package main

import (
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newKnowledgeService builds the optional company knowledge service from the
// environment. KNOWLEDGE_URL unset means the feature is not offered; the
// returned service still answers Status ("not_configured") so the API shape
// does not depend on the deployment. Without MULTICA_KNOWLEDGE_SECRET_KEY the
// sidecar is reachable but workspace keys could not be stored safely, so
// enabling fails closed instead of storing plaintext.
func newKnowledgeService(queries *db.Queries, pool *pgxpool.Pool) *service.KnowledgeService {
	svc := &service.KnowledgeService{Queries: queries, DB: pool}
	baseURL := strings.TrimSpace(os.Getenv("KNOWLEDGE_URL"))
	if baseURL == "" {
		slog.Info("knowledge base disabled (KNOWLEDGE_URL not set)")
		return svc
	}

	if key, err := secretbox.LoadKey("MULTICA_KNOWLEDGE_SECRET_KEY"); err == nil {
		box, err := secretbox.New(key)
		if err != nil {
			slog.Error("knowledge: secretbox.New failed; knowledge not offered", "error", err)
		} else {
			svc.Box = box
		}
	} else {
		slog.Warn("knowledge base not offered: MULTICA_KNOWLEDGE_SECRET_KEY is not set or invalid", "error", err)
	}

	client, err := knowledge.New(knowledge.Config{
		BaseURL:   baseURL,
		PublicURL: strings.TrimSpace(os.Getenv("KNOWLEDGE_PUBLIC_URL")),
		Email:     strings.TrimSpace(os.Getenv("KNOWLEDGE_SERVICE_EMAIL")),
		Password:  os.Getenv("KNOWLEDGE_SERVICE_PASSWORD"),
	})
	if err != nil {
		slog.Error("knowledge: invalid configuration; knowledge not offered", "error", err)
		return svc
	}
	svc.Client = client
	svc.UIURL = strings.TrimRight(strings.TrimSpace(os.Getenv("KNOWLEDGE_UI_URL")), "/")

	dimension := 1024
	if raw := strings.TrimSpace(os.Getenv("KNOWLEDGE_EMBEDDING_DIMENSION")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			dimension = n
		} else {
			slog.Warn("knowledge: ignoring invalid KNOWLEDGE_EMBEDDING_DIMENSION", "value", raw)
		}
	}
	svc.Embedding = knowledge.EmbeddingModelSpec{
		Name:      envOr("KNOWLEDGE_EMBEDDING_MODEL", "bge-m3"),
		Provider:  envOr("KNOWLEDGE_EMBEDDING_PROVIDER", "ollama"),
		BaseURL:   envOr("KNOWLEDGE_EMBEDDING_URL", "http://host.docker.internal:11434"),
		APIKey:    os.Getenv("KNOWLEDGE_EMBEDDING_API_KEY"),
		Dimension: dimension,
	}
	svc.LLM = newKnowledgeLLM()
	if raw := strings.TrimSpace(os.Getenv("KNOWLEDGE_UPLOAD_MAX_MB")); raw != "" {
		if mb, err := strconv.Atoi(raw); err == nil && mb > 0 {
			svc.UploadMaxBytes = int64(mb) << 20
		} else {
			slog.Warn("knowledge: ignoring invalid KNOWLEDGE_UPLOAD_MAX_MB", "value", raw)
		}
	}
	slog.Info("knowledge base configured", "model", svc.Embedding.Name, "provider", svc.Embedding.Provider, "offered", svc.Box != nil)
	return svc
}

// newKnowledgeLLM reads the OpenAI-compatible endpoint WeKnora uses for summaries,
// Wiki, reranking and vision. Nil means no model is registered. The endpoint must
// be in WeKnora's SSRF allow-list (SSRF_WHITELIST_EXTRA) when it is a private host.
func newKnowledgeLLM() *service.KnowledgeLLM {
	base := strings.TrimSpace(os.Getenv("KNOWLEDGE_LLM_BASE_URL"))
	if base == "" {
		return nil
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		slog.Error("knowledge: KNOWLEDGE_LLM_BASE_URL must be an absolute http or https URL; models not registered")
		return nil
	}
	llm := &service.KnowledgeLLM{
		BaseURL:     base,
		APIKey:      os.Getenv("KNOWLEDGE_LLM_API_KEY"),
		ChatModel:   strings.TrimSpace(os.Getenv("KNOWLEDGE_LLM_MODEL")),
		RerankModel: strings.TrimSpace(os.Getenv("KNOWLEDGE_RERANK_MODEL")),
		VisionModel: strings.TrimSpace(os.Getenv("KNOWLEDGE_VISION_MODEL")),
	}
	slog.Info("knowledge models configured", "chat", llm.ChatModel != "", "rerank", llm.RerankModel != "", "vision", llm.VisionModel != "")
	return llm
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}
