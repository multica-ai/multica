package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	defaultNanoClawListen = "127.0.0.1:8099"
	maxNanoClawBodyBytes  = 64 << 10
)

var nanoclawCmd = &cobra.Command{
	Use:   "nanoclaw",
	Short: "Expose limited Multica issue tools to NanoClaw",
}

var nanoclawServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the authenticated NanoClaw issue bridge",
	Long: "Run a local HTTP bridge that lets NanoClaw create issues and read issue status without receiving the Multica PAT. " +
		"Set MULTICA_NANOCLAW_BRIDGE_TOKEN to a random value of at least 32 characters.",
	RunE: runNanoClawServe,
}

func init() {
	nanoclawCmd.AddCommand(nanoclawServeCmd)
	nanoclawServeCmd.Flags().String("listen", defaultNanoClawListen, "Listen address; use 0.0.0.0:8099 only when a container must reach the host")
}

type nanoclawBridge struct {
	client *cli.APIClient
	token  string
}

type nanoclawCreateIssueRequest struct {
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Assignee     string `json:"assignee"`
	AssigneeKind string `json:"assignee_kind,omitempty"`
	Status       string `json:"status,omitempty"`
}

func runNanoClawServe(cmd *cobra.Command, _ []string) error {
	token := strings.TrimSpace(os.Getenv("MULTICA_NANOCLAW_BRIDGE_TOKEN"))
	if len(token) < 32 {
		return fmt.Errorf("MULTICA_NANOCLAW_BRIDGE_TOKEN must contain at least 32 characters")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace ID not set: use --workspace-id, MULTICA_WORKSPACE_ID, or 'multica config set workspace_id <id>'")
	}

	listen, _ := cmd.Flags().GetString("listen")
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}

	bridge := &nanoclawBridge{client: client, token: token}
	server := &http.Server{
		Handler:           bridge.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cli.APITimeout() + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	fmt.Fprintf(cmd.ErrOrStderr(), "NanoClaw bridge listening on http://%s\n", listener.Addr())

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down NanoClaw bridge: %w", err)
		}
		return nil
	}
}

func (b *nanoclawBridge) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeNanoClawJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/issues", b.createIssue)
	mux.HandleFunc("GET /v1/issues/{id}", b.getIssue)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validNanoClawBearer(r.Header.Get("Authorization"), b.token) {
			writeNanoClawError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func validNanoClawBearer(header, token string) bool {
	expected := "Bearer " + token
	if len(header) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(expected)) == 1
}

func (b *nanoclawBridge) createIssue(w http.ResponseWriter, r *http.Request) {
	var input nanoclawCreateIssueRequest
	body := http.MaxBytesReader(w, r.Body, maxNanoClawBodyBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeNanoClawError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	if err := ensureNanoClawJSONEOF(decoder); err != nil {
		writeNanoClawError(w, http.StatusBadRequest, "request must contain one JSON object")
		return
	}

	input.Title = strings.TrimSpace(input.Title)
	input.Assignee = strings.TrimSpace(input.Assignee)
	input.AssigneeKind = strings.ToLower(strings.TrimSpace(input.AssigneeKind))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Title == "" {
		writeNanoClawError(w, http.StatusBadRequest, "title is required")
		return
	}
	if input.Assignee == "" {
		writeNanoClawError(w, http.StatusBadRequest, "assignee is required")
		return
	}
	if input.Status == "" {
		input.Status = "todo"
	}
	if input.Status != "todo" && input.Status != "backlog" {
		writeNanoClawError(w, http.StatusBadRequest, "status must be todo or backlog")
		return
	}

	kinds := assigneeKinds{agent: true, squad: true}
	switch input.AssigneeKind {
	case "":
	case "agent":
		kinds.squad = false
	case "squad", "team":
		kinds.agent = false
	default:
		writeNanoClawError(w, http.StatusBadRequest, "assignee_kind must be agent, squad, or omitted")
		return
	}

	ctx, cancel := cli.APIContext(r.Context())
	defer cancel()
	assigneeType, assigneeID, err := resolveAssignee(ctx, b.client, input.Assignee, kinds)
	if err != nil {
		writeNanoClawError(w, http.StatusBadRequest, err.Error())
		return
	}

	request := map[string]any{
		"title":         input.Title,
		"status":        input.Status,
		"assignee_type": assigneeType,
		"assignee_id":   assigneeID,
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		request["description"] = description
	}

	var issue map[string]any
	if err := b.client.PostJSON(ctx, "/api/issues", request, &issue); err != nil {
		writeNanoClawAPIError(w, err)
		return
	}
	writeNanoClawJSON(w, http.StatusCreated, issue)
}

func (b *nanoclawBridge) getIssue(w http.ResponseWriter, r *http.Request) {
	ref, err := url.PathUnescape(r.PathValue("id"))
	if err != nil || strings.TrimSpace(ref) == "" {
		writeNanoClawError(w, http.StatusBadRequest, "issue id is required")
		return
	}

	ctx, cancel := cli.APIContext(r.Context())
	defer cancel()
	resolved, err := resolveIssueRef(ctx, b.client, ref)
	if err != nil {
		writeNanoClawAPIError(w, err)
		return
	}

	var issue map[string]any
	if err := b.client.GetJSON(ctx, "/api/issues/"+url.PathEscape(resolved.ID), &issue); err != nil {
		writeNanoClawAPIError(w, err)
		return
	}
	writeNanoClawJSON(w, http.StatusOK, issue)
}

func ensureNanoClawJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("extra JSON value")
		}
		return err
	}
	return nil
}

func writeNanoClawAPIError(w http.ResponseWriter, err error) {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) {
		status := httpErr.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		writeNanoClawError(w, status, cli.FormatError(err, false))
		return
	}
	writeNanoClawError(w, http.StatusBadGateway, cli.FormatError(err, false))
}

func writeNanoClawError(w http.ResponseWriter, status int, message string) {
	writeNanoClawJSON(w, status, map[string]string{"error": message})
}

func writeNanoClawJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
