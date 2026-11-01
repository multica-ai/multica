package knowledge

// Opt-in integration test against a live WeKnora. It never runs by default:
// it needs MULTICA_KNOWLEDGE_INTEGRATION_URL, and it creates and removes its
// own throwaway tenant. Run it with, for example:
//
//	MULTICA_KNOWLEDGE_INTEGRATION_URL=http://localhost:8090 \
//	MULTICA_KNOWLEDGE_INTEGRATION_EMAIL=<service user> \
//	MULTICA_KNOWLEDGE_INTEGRATION_PASSWORD=<password> \
//	go test ./internal/knowledge -run TestLiveWeKnora -count=1 -v
//
// Optional: MULTICA_KNOWLEDGE_INTEGRATION_EMBEDDING_URL (the embedding
// endpoint as WeKnora reaches it; default http://172.17.0.1:11434),
// ..._EMBEDDING_MODEL (bge-m3) and ..._EMBEDDING_DIMENSION (1024).

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestLiveWeKnora(t *testing.T) {
	base := os.Getenv("MULTICA_KNOWLEDGE_INTEGRATION_URL")
	if base == "" {
		t.Skip("MULTICA_KNOWLEDGE_INTEGRATION_URL is not set")
	}
	email := os.Getenv("MULTICA_KNOWLEDGE_INTEGRATION_EMAIL")
	password := os.Getenv("MULTICA_KNOWLEDGE_INTEGRATION_PASSWORD")
	if email == "" || password == "" {
		t.Fatal("MULTICA_KNOWLEDGE_INTEGRATION_EMAIL and _PASSWORD are required")
	}
	getenv := func(name, def string) string {
		if v := os.Getenv("MULTICA_KNOWLEDGE_INTEGRATION_" + name); v != "" {
			return v
		}
		return def
	}
	dim, err := strconv.Atoi(getenv("EMBEDDING_DIMENSION", "1024"))
	if err != nil {
		t.Fatalf("EMBEDDING_DIMENSION: %v", err)
	}

	c, err := New(Config{BaseURL: base, Email: email, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	run := hex.EncodeToString(suffix)

	conformance{
		TenantName: "multica-it-" + run,
		Model: EmbeddingModelSpec{
			Name:      getenv("EMBEDDING_MODEL", "bge-m3"),
			Provider:  "ollama",
			BaseURL:   getenv("EMBEDDING_URL", "http://172.17.0.1:11434"),
			Dimension: dim,
		},
		SearchWait: 90 * time.Second,
		Token:      "zq" + run + "quartz",
	}.run(t, c)
}
