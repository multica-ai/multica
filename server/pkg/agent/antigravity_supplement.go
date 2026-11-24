package agent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// AntigravityHookHelperArg selects the Antigravity hook relay mode of the
// multica binary. agy runs it before every model call of the turn; it forwards
// the hook event to the run's loopback endpoint and prints the hook output agy
// expects.
const AntigravityHookHelperArg = "__multica_antigravity_hook"

const (
	antigravityHookURLEnv   = "MULTICA_ANTIGRAVITY_HOOK_URL"
	antigravityHookTokenEnv = "MULTICA_ANTIGRAVITY_HOOK_TOKEN"
	// antigravityHookName keys Multica's entry in the workspace hooks.json.
	antigravityHookName           = "multica-steering"
	antigravityHookTimeoutSeconds = 10
	antigravityHookRequestLimit   = 64 << 10
	antigravityHookStdinLimit     = 64 << 20
	// antigravityHookGuidanceLimit bounds one message. It admits the API's
	// 64 KiB comment plus the daemon's framing, and JSON escaping grows a byte
	// to at most six (\u003c), so the hook reply stays inside the relay's
	// response limit.
	antigravityHookGuidanceLimit = 72 << 10
	antigravityHookEmptyReply    = "{}"
)

// The relay must be able to carry the largest reply the session can build.
const _ = uint(hookRelayResponseLimit - 6*antigravityHookGuidanceLimit - 1024)

// antigravityHookRequest is the subset of agy's PreInvocation payload that
// decides ownership. The relay forwards only this field.
type antigravityHookRequest struct {
	ConversationID string `json:"conversationId"`
}

// antigravitySupplementSession delivers additional guidance through an
// Antigravity workspace hook. agy print mode reads no input after the prompt,
// so a managed .agents/hooks.json runs the multica relay before each model
// call, and the relay asks this session, over a token-protected loopback
// endpoint, for guidance to return as PreInvocation injectSteps userMessage
// entries. agy appends each as a user_input step of the running turn, so the
// model reads it as a message from the user.
//
// A hook response is not delivery: the relay can fail after it, and other
// hooks (global, plugin) can inject user input of their own. Each message
// opens with its own random marker, and counts as delivered only when agy's
// conversation transcript shows a USER_INPUT record carrying that marker. The
// marker leads the text so it survives the transcript's truncated content. The
// daemon delivers one message at a time, so each hook reply carries at most
// one.
type antigravitySupplementSession struct {
	ctx       context.Context
	logger    *slog.Logger
	token     string
	url       string
	server    *http.Server
	hooksPath string
	hooksData []byte

	mu             sync.Mutex
	conversationID string
	active         bool
	ended          bool
	pending        []*antigravitySupplementInput
	inflight       []*antigravitySupplementInput
	closeOnce      sync.Once
}

type antigravitySupplementInput struct {
	ctx    context.Context
	marker string
	text   string
	done   chan error
	// claimed is set while a hook response carries the input; only the stream
	// receipt or the end of the turn settles it then.
	claimed bool
}

func startAntigravitySupplementSession(ctx context.Context, cwd string, hookCommand []string, logger *slog.Logger) (*antigravitySupplementSession, error) {
	if strings.TrimSpace(cwd) == "" {
		return nil, errors.New("antigravity hooks require a working directory")
	}
	if len(hookCommand) == 0 || strings.TrimSpace(hookCommand[0]) == "" {
		return nil, errors.New("antigravity hook relay command is not configured")
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	hooksData, err := antigravityHooksFileData(hookShellCommand(hookCommand))
	if err != nil {
		return nil, err
	}
	s := &antigravitySupplementSession{
		ctx:       ctx,
		logger:    logger,
		token:     token,
		hooksPath: filepath.Join(cwd, ".agents", "hooks.json"),
		hooksData: hooksData,
	}
	if err := s.writeHooksFile(); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.removeHooksFile()
		return nil, fmt.Errorf("listen for antigravity hooks: %w", err)
	}
	s.url = "http://" + listener.Addr().String() + "/hook"
	s.server = &http.Server{Handler: s, ReadHeaderTimeout: hookRelayTimeout}
	go func() { _ = s.server.Serve(listener) }()
	return s, nil
}

// AntigravityHooksInstallable reports whether a run in cwd can carry Multica's
// managed .agents/hooks.json. Multica never replaces hooks it did not write: a
// workdir with its own keeps them and goes without steering, so the daemon
// must check this before it offers steering for the run.
func AntigravityHooksInstallable(cwd string) error {
	if strings.TrimSpace(cwd) == "" {
		return errors.New("antigravity hooks require a working directory")
	}
	dir := filepath.Join(cwd, ".agents")
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect .agents dir: %w", err)
	}
	path := filepath.Join(dir, "hooks.json")
	existing, err := os.ReadFile(path)
	switch {
	case err == nil && !antigravityManagedHooks(existing):
		return fmt.Errorf("workdir already has an unmanaged %s", path)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("read antigravity hooks: %w", err)
	}
	return nil
}

// antigravityManagedHooks reports whether data is exactly a hooks file Multica
// generated, possibly by an earlier daemon binary. Anything a user added to it
// makes the file theirs.
func antigravityManagedHooks(data []byte) bool {
	var file map[string]antigravityHookConfig
	if json.Unmarshal(data, &file) != nil || len(file) != 1 {
		return false
	}
	hooks := file[antigravityHookName].PreInvocation
	if len(hooks) != 1 || !strings.Contains(hooks[0].Command, AntigravityHookHelperArg) {
		return false
	}
	generated, err := antigravityHooksFileData(hooks[0].Command)
	return err == nil && bytes.Equal(data, generated)
}

func (s *antigravitySupplementSession) writeHooksFile() error {
	if err := AntigravityHooksInstallable(filepath.Dir(filepath.Dir(s.hooksPath))); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.hooksPath), 0o755); err != nil {
		return fmt.Errorf("create .agents dir: %w", err)
	}
	if err := os.WriteFile(s.hooksPath, s.hooksData, 0o600); err != nil {
		return fmt.Errorf("write antigravity hooks: %w", err)
	}
	return nil
}

func (s *antigravitySupplementSession) removeHooksFile() {
	current, err := os.ReadFile(s.hooksPath)
	if err != nil || !bytes.Equal(current, s.hooksData) {
		return
	}
	if err := os.Remove(s.hooksPath); err != nil && s.logger != nil {
		s.logger.Warn("antigravity hooks cleanup failed", "path", s.hooksPath, "error", err)
	}
}

// env returns the variables agy passes on to the hook relay.
func (s *antigravitySupplementSession) env() []string {
	return []string{antigravityHookURLEnv + "=" + s.url, antigravityHookTokenEnv + "=" + s.token}
}

// activate opens delivery once the CLI reports the conversation it is running.
func (s *antigravitySupplementSession) activate(conversationID string) {
	if s == nil || conversationID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended || s.conversationID != "" {
		return
	}
	s.conversationID = conversationID
	s.active = true
}

func (s *antigravitySupplementSession) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active && !s.ended && s.ctx.Err() == nil
}

func (s *antigravitySupplementSession) supplement(ctx context.Context, text string) error {
	nonce, err := randomHex(8)
	if err != nil {
		return err
	}
	marker := cursorGuidanceTagPrefix + nonce + "]"
	text = marker + "\n" + text
	if len(text) > antigravityHookGuidanceLimit {
		return fmt.Errorf("antigravity guidance is %d bytes, limit %d", len(text), antigravityHookGuidanceLimit)
	}
	input := &antigravitySupplementInput{ctx: ctx, marker: marker, text: text, done: make(chan error, 1)}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.ended || s.ctx.Err() != nil {
		s.mu.Unlock()
		return context.Canceled
	}
	if !s.active {
		s.mu.Unlock()
		return errors.New("antigravity turn has not started")
	}
	s.pending = append(s.pending, input)
	s.mu.Unlock()
	select {
	case err := <-input.done:
		return err
	case <-ctx.Done():
	case <-s.ctx.Done():
	}
	// Serialize cancellation with hook delivery: once a hook carries the input
	// it may already be in agy's trajectory, so only the receipt or the end of
	// the turn decides the outcome; otherwise withdraw it so a later hook
	// cannot deliver it after this call has reported failure.
	s.mu.Lock()
	if input.claimed {
		s.mu.Unlock()
		return <-input.done
	}
	defer s.mu.Unlock()
	select {
	case err := <-input.done:
		return err
	default:
	}
	if i := slices.Index(s.pending, input); i >= 0 {
		s.pending = slices.Delete(s.pending, i, i+1)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.ctx.Err()
}

// prepareHook claims the oldest pending guidance for one hook response. Only
// the run's own conversation may consume it: subagents run their own
// conversations and their model calls fire the same hook.
func (s *antigravitySupplementSession) prepareHook(req antigravityHookRequest) (*antigravitySupplementInput, map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.ended || s.ctx.Err() != nil || req.ConversationID != s.conversationID {
		return nil, map[string]any{}
	}
	for len(s.pending) > 0 {
		input := s.pending[0]
		s.pending[0] = nil
		s.pending = s.pending[1:]
		if err := input.ctx.Err(); err != nil {
			input.done <- err
			continue
		}
		input.claimed = true
		s.inflight = append(s.inflight, input)
		return input, map[string]any{"injectSteps": []map[string]string{{"userMessage": input.text}}}
	}
	return nil, map[string]any{}
}

// unclaim returns guidance whose hook response never left this process to the
// head of the queue for the next model call.
func (s *antigravitySupplementSession) unclaim(input *antigravitySupplementInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.Index(s.inflight, input)
	if input == nil || i < 0 {
		return
	}
	s.inflight = slices.Delete(s.inflight, i, i+1)
	input.claimed = false
	switch {
	case input.ctx.Err() != nil:
		input.done <- input.ctx.Err()
	case s.ended:
		input.done <- context.Canceled
	default:
		s.pending = append([]*antigravitySupplementInput{input}, s.pending...)
	}
}

// awaitingReceipt reports whether injected guidance is waiting for the
// user_input step it should become.
func (s *antigravitySupplementSession) awaitingReceipt() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inflight) > 0
}

// inflightMarkers lists the markers of guidance a hook has injected.
func (s *antigravitySupplementSession) inflightMarkers() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	markers := make([]string, len(s.inflight))
	for i, input := range s.inflight {
		markers[i] = input.marker
	}
	return markers
}

// receipt settles the injected guidance a transcript USER_INPUT record
// carries the marker of.
func (s *antigravitySupplementSession) receipt(marker string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.inflight, func(input *antigravitySupplementInput) bool { return input.marker == marker })
	if i < 0 {
		return
	}
	s.inflight[i].done <- nil
	s.inflight = slices.Delete(s.inflight, i, i+1)
}

func (s *antigravitySupplementSession) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/hook" {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req antigravityHookRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, antigravityHookRequestLimit)).Decode(&req); err != nil {
		http.Error(w, "invalid hook request", http.StatusBadRequest)
		return
	}
	claimed, reply := s.prepareHook(req)
	body, err := json.Marshal(reply)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(body)
	}
	if err != nil {
		s.unclaim(claimed)
		if s.logger != nil {
			s.logger.Warn("antigravity hook response failed", "error", err)
		}
	}
}

// end closes delivery when the turn reaches its terminal result. Guidance
// still in flight never became a user_input step of this turn.
func (s *antigravitySupplementSession) end() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended, s.active = true, false
	for _, input := range slices.Concat(s.pending, s.inflight) {
		input.done <- context.Canceled
	}
	s.pending, s.inflight = nil, nil
}

func (s *antigravitySupplementSession) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.end()
		_ = s.server.Close()
		s.removeHooksFile()
	})
}

type antigravityHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type antigravityHookConfig struct {
	PreInvocation []antigravityHookEntry `json:"PreInvocation"`
}

func antigravityHooksFileData(command string) ([]byte, error) {
	file := map[string]antigravityHookConfig{
		antigravityHookName: {PreInvocation: []antigravityHookEntry{{
			Type: "command", Command: command, Timeout: antigravityHookTimeoutSeconds,
		}}},
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal antigravity hooks: %w", err)
	}
	return append(data, '\n'), nil
}

// RunAntigravityHookRelay is the body of the Antigravity hook command. It
// always prints one JSON object and never fails the hook: a run without
// steering, a stale hooks.json or an unreachable endpoint all answer "{}" so
// the model call proceeds untouched.
func RunAntigravityHookRelay(stdin io.Reader, stdout io.Writer, getenv func(string) string) {
	_, _ = io.WriteString(stdout, antigravityHookRelay(stdin, getenv)+"\n")
}

func antigravityHookRelay(stdin io.Reader, getenv func(string) string) string {
	var req antigravityHookRequest
	if err := json.NewDecoder(io.LimitReader(stdin, antigravityHookStdinLimit)).Decode(&req); err != nil {
		return antigravityHookEmptyReply
	}
	return forwardHookRequest(getenv(antigravityHookURLEnv), getenv(antigravityHookTokenEnv), req, antigravityHookEmptyReply)
}
