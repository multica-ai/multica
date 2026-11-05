//go:build verificationdev

// A manually invoked protocol integration harness. It never loads user profiles or model CLIs.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
	"github.com/multica-ai/multica/server/internal/localconversation/catalog"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

func main() {
	directory := flag.String("directory", "", "Private integration state directory")
	flag.Parse()
	if *directory == "" {
		panic("--directory is required")
	}
	if err := os.MkdirAll(*directory, 0o700); err != nil {
		panic(err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(secret)
	_ = os.Setenv("MULTICA_VERIFICATION_MODE", "1")
	_ = os.Setenv("MULTICA_VERIFICATION_TOKEN", token)
	if err := os.WriteFile(filepath.Join(*directory, ".desktop-user-id"), []byte("verification-user"), 0o600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(*directory, "token"), []byte(token), 0o600); err != nil {
		panic(err)
	}
	svc, err := service.Open(context.Background(), service.Options{Catalog: catalog.New(func(context.Context, string) (string, error) {
		panic("verification must not discover model CLIs, including background title generation")
	}, filepath.Join(*directory, "provider-claude"), filepath.Join(*directory, "provider-codex")), Path: filepath.Join(*directory, "conversations.sqlite"), Claude: func(context.Context, sqlitedb.Conversation) (service.ClaudeSession, service.StartClaudeAdapter, error) {
		panic("fake provider interception must happen before a real launcher")
	}})
	if err != nil {
		panic(err)
	}
	defer svc.Close(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	identity := "verification-harness"
	origin := "http://verification.invalid"
	mux := http.NewServeMux()
	v := localhost.NewV2(svc, token, *directory)
	v.SetServerOrigin(origin)
	v.Register(mux)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"pid": os.Getpid(), "instance_owner_id": identity, "instance_run_id": identity, "cli_commit": "verification", "server_url": origin, "status": "running"})
	})
	metadata := map[string]any{"baseUrl": "http://" + listener.Addr().String(), "scope": map[string]string{"apiOrigin": origin, "userId": "verification-user", "workspaceId": "verification-workspace"}, "identity": map[string]any{"pid": os.Getpid(), "ownerId": identity, "runId": identity, "commit": "verification"}, "tokenFile": filepath.Join(*directory, "token"), "databaseFile": filepath.Join(*directory, "conversations.sqlite")}
	encoded, _ := json.Marshal(metadata)
	if err := os.WriteFile(filepath.Join(*directory, "harness.json"), encoded, 0o600); err != nil {
		panic(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	closing, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(closing)
}
