package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var userIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

//go:embed oauth-ui.html
var oauthUIFS embed.FS

type Config struct {
	Port           string
	ScriptsDir     string
	MappingFile    string
	UserHomesDir   string
	AllowedOrigins []string
	Version        string
}

type MappingEntry struct {
	MulticaUserID  string    `json:"multica_user_id"`
	LarkUserOpenID string    `json:"lark_user_open_id"`
	LarkUserName   string    `json:"lark_user_name"`
	Home           string    `json:"home"`
	ProvisionedAt  time.Time `json:"provisioned_at"`
}

type Server struct {
	cfg         Config
	runner      ScriptRunner
	logger      *slog.Logger
	rateLimiter *IPRateLimiter
	corsAllow   map[string]struct{}
	allowAll    bool
}

type IPRateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	limit  int
	hits   map[string][]time.Time
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

type jsonResponse map[string]any

type TokenStatus struct {
	TokenStatus string   `json:"tokenStatus"`
	ExpiresAt   string   `json:"expiresAt"`
	Scopes      []string `json:"scopes"`
}

func NewIPRateLimiter(limit int, window time.Duration) *IPRateLimiter {
	return &IPRateLimiter{limit: limit, window: window, hits: make(map[string][]time.Time)}
}

func (l *IPRateLimiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	start := now.Add(-l.window)
	times := l.hits[ip]
	k := 0
	for _, t := range times {
		if t.After(start) {
			times[k] = t
			k++
		}
	}
	times = times[:k]
	if len(times) >= l.limit {
		l.hits[ip] = times
		return false
	}
	times = append(times, now)
	l.hits[ip] = times
	return true
}

func NewServer(cfg Config, runner ScriptRunner, logger *slog.Logger) *Server {
	allowAll := len(cfg.AllowedOrigins) == 0
	allowSet := map[string]struct{}{}
	for _, origin := range cfg.AllowedOrigins {
		o := strings.TrimSpace(origin)
		if o == "" {
			continue
		}
		if o == "*" {
			allowAll = true
		}
		allowSet[o] = struct{}{}
	}
	if len(allowSet) == 0 {
		allowAll = true
	}

	return &Server{
		cfg:         cfg,
		runner:      runner,
		logger:      logger,
		rateLimiter: NewIPRateLimiter(60, time.Minute),
		corsAllow:   allowSet,
		allowAll:    allowAll,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.corsMiddleware)
	r.Use(s.rateLimitMiddleware)
	r.Use(s.requestLogMiddleware)

	r.Get("/healthz", s.healthzHandler)
	r.Get("/oauth-ui", s.serveOAuthUI)
	r.Get("/settings", s.serveOAuthUI) // 别名: multica web 入口可链 /settings, UI 自适应展示
	r.Post("/api/feishu/device/start", s.startHandler)
	r.Post("/api/feishu/device/complete", s.completeHandler)
	r.Get("/api/feishu/device/status", s.statusHandler)
	r.Post("/api/feishu/device/force-restart", s.forceRestartHandler) // V4: 清旧 mapping/home 强制重授权
	return r
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}

func (s *Server) requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		duration := time.Since(start)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"user_id", requestUserID(r),
			"duration_ms", duration.Milliseconds(),
			"status", rec.status,
		)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" {
			if _, ok := s.corsAllow[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !s.rateLimiter.Allow(ip, time.Now()) {
			writeJSON(w, http.StatusTooManyRequests, jsonResponse{"ok": false, "error": "rate limit exceeded"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) healthzHandler(w http.ResponseWriter, r *http.Request) {
	defer s.recoverPanic(w, r)
	writeJSON(w, http.StatusOK, jsonResponse{"ok": true, "version": s.cfg.Version})
}

func (s *Server) serveOAuthUI(w http.ResponseWriter, r *http.Request) {
	defer s.recoverPanic(w, r)
	body, err := oauthUIFS.ReadFile("oauth-ui.html")
	if err != nil {
		s.logger.Error("read oauth-ui failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"ok": false, "error": "oauth ui unavailable"})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) startHandler(w http.ResponseWriter, r *http.Request) {
	defer s.recoverPanic(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req struct {
		MulticaUserID string `json:"multica_user_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": err.Error()})
		return
	}
	if err := validateUserID(req.MulticaUserID); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": err.Error()})
		return
	}

	if _, err := s.runScriptJSON(ctx, "02_user_provision.sh", req.MulticaUserID); err != nil {
		s.writeExecError(w, "provision failed", err, http.StatusInternalServerError)
		return
	}
	data, err := s.runScriptJSON(ctx, "03_user_oauth_start.sh", req.MulticaUserID)
	if err != nil {
		s.writeExecError(w, "oauth start failed", err, http.StatusInternalServerError)
		return
	}

	deviceCode := stringVal(data, "device_code", "deviceCode")

	// fire-and-forget 后台 polling: oauth-ui 前端只轮询 /status, 不主动调 /complete
	// 04 脚本完成后会写 mapping + token 落到 user HOME, /status 自然检测到 bound=true
	if deviceCode != "" {
		uid := req.MulticaUserID
		go func() {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 200*time.Second)
			defer bgCancel()
			if _, err := s.runScriptJSON(bgCtx, "04_user_oauth_complete.sh", uid, deviceCode); err != nil {
				s.logger.Warn("background oauth polling failed", "user_id", uid, "error", err.Error())
			} else {
				s.logger.Info("background oauth polling succeeded", "user_id", uid)
			}
		}()
	}

	writeJSON(w, http.StatusOK, jsonResponse{
		"ok":               true,
		"multica_user_id":  req.MulticaUserID,
		"device_code":      deviceCode,
		"verification_url": stringVal(data, "verification_url", "verificationUrl"),
		"user_code":        stringVal(data, "user_code", "userCode"),
		"expires_in":       intVal(data, "expires_in", "expiresIn"),
		"qr_ascii":         stringVal(data, "qr_ascii", "qrAscii"),
	})
}

func (s *Server) completeHandler(w http.ResponseWriter, r *http.Request) {
	defer s.recoverPanic(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 200*time.Second)
	defer cancel()

	var req struct {
		MulticaUserID string `json:"multica_user_id"`
		DeviceCode    string `json:"device_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": err.Error()})
		return
	}
	if strings.TrimSpace(req.MulticaUserID) == "" || strings.TrimSpace(req.DeviceCode) == "" {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": "multica_user_id and device_code are required"})
		return
	}

	type result struct {
		data map[string]any
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		data, err := s.runScriptJSON(ctx, "04_user_oauth_complete.sh", req.MulticaUserID, req.DeviceCode)
		ch <- result{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": "oauth complete timeout"})
		return
	case r := <-ch:
		if r.err != nil {
			s.writeExecError(w, "oauth complete failed", r.err, http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, jsonResponse{
			"ok":                true,
			"multica_user_id":   stringVal(r.data, "multica_user_id", "multicaUserId"),
			"lark_user_open_id": stringVal(r.data, "lark_user_open_id", "larkUserOpenId", "userOpenId"),
			"lark_user_name":    stringVal(r.data, "lark_user_name", "larkUserName", "userName"),
			"scopes":            sliceStringVal(r.data, "scopes"),
		})
	}
}

func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	defer s.recoverPanic(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	userID := strings.TrimSpace(r.URL.Query().Get("multica_user_id"))
	if err := validateUserID(userID); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": err.Error()})
		return
	}

	entries, err := loadMappings(s.cfg.MappingFile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"ok": false, "error": fmt.Sprintf("load mapping failed: %v", err)})
		return
	}

	entry, ok := findMapping(entries, userID)
	if !ok {
		// self-heal: mapping 缺但 token enc 已落地 (例如 04 polling 成功但脚本最后 exit 失败)
		// → 查 lark-cli auth status 拿 openId+userName 补写 mapping,避免用户重新扫码
		userHome := filepath.Join(s.cfg.UserHomesDir, "mc-user-"+userID)
		if st, err := os.Stat(userHome); err == nil && st.IsDir() {
			rec, healErr := s.tryHealMapping(ctx, userID, userHome)
			if healErr != nil {
				s.logger.Warn("self-heal try failed", "user_id", userID, "error", healErr.Error())
			}
			if healErr == nil && rec != nil {
				entries = append(entries, *rec)
				if saveErr := saveMappings(s.cfg.MappingFile, entries); saveErr != nil {
					s.logger.Warn("self-heal save mapping failed", "user_id", userID, "error", saveErr.Error())
				} else {
					s.logger.Info("self-heal mapping written", "user_id", userID, "lark_user_open_id", rec.LarkUserOpenID)
					entry = *rec
					ok = true
				}
			}
		}
		if !ok {
			homeFound := false
			if st, err := os.Stat(userHome); err == nil && st.IsDir() {
				homeFound = true
			}
			status := "no_token"
			if homeFound {
				status = "not_authorized"
			}
			writeJSON(w, http.StatusOK, jsonResponse{
				"ok":              true,
				"multica_user_id": userID,
				"bound":           false,
				"token_status":    status,
			})
			return
		}
	}

	result, err := s.runner.Run(ctx, "05_agent_spawn.sh", userID, "auth", "status")
	if err != nil {
		s.writeExecError(w, "auth status failed", err, http.StatusInternalServerError)
		return
	}
	tokenInfo, err := parseTokenStatus(result.Stdout)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"ok": false, "error": fmt.Sprintf("parse auth status failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, jsonResponse{
		"ok":                true,
		"multica_user_id":   userID,
		"bound":             true,
		"lark_user_open_id": entry.LarkUserOpenID,
		"lark_user_name":    entry.LarkUserName,
		"token_status":      tokenInfo.TokenStatus,
		"expires_at":        tokenInfo.ExpiresAt,
		"scopes":            tokenInfo.Scopes,
	})
}

// forceRestartHandler V4: 用户在 oauth-ui 触发"强制重新授权" 时调用,
// 清理本机的 (mapping entry + user home dir),然后 oauth-ui 应跳回 device start 重走流程。
//
// 适用场景:
// - 用户在另一台机器扫码授权了,本机 mapping 缺
// - 本机 token enc 损坏 / refresh 失败 / auth.json 格式漂移
// - 用户想换飞书账号绑定 (旧 mapping 必须先清)
//
// 安全考虑:
// - 仅接受 POST + multica_user_id (cors + rate limit 自动覆盖)
// - 不能跨 multica_user_id 操作 (user 只能清自己的, 通过 URL 参数自证)
// - 不真的删 home 目录里 user 数据,只删 .hermes/.lark-cli 等 token 凭据 (避免误删)
func (s *Server) forceRestartHandler(w http.ResponseWriter, r *http.Request) {
	defer s.recoverPanic(w, r)
	var req struct {
		MulticaUserID string `json:"multica_user_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": err.Error()})
		return
	}
	userID := strings.TrimSpace(req.MulticaUserID)
	if err := validateUserID(userID); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"ok": false, "error": err.Error()})
		return
	}

	// 1. 删 mapping entry (如果存在)
	entries, err := loadMappings(s.cfg.MappingFile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"ok": false, "error": fmt.Sprintf("load mapping: %v", err)})
		return
	}
	cleaned := make([]MappingEntry, 0, len(entries))
	removed := 0
	for _, e := range entries {
		if e.MulticaUserID == userID {
			removed++
			continue
		}
		cleaned = append(cleaned, e)
	}
	if removed > 0 {
		if saveErr := saveMappings(s.cfg.MappingFile, cleaned); saveErr != nil {
			s.logger.Warn("force-restart save mapping failed", "user_id", userID, "error", saveErr.Error())
		}
	}

	// 2. 删 user home 里的 token 凭据 (但保留 home 目录本身, 避免误删用户其他数据)
	userHome := filepath.Join(s.cfg.UserHomesDir, "mc-user-"+userID)
	cleanedPaths := []string{}
	if _, err := os.Stat(userHome); err == nil {
		// 安全清单: 只清这些已知 token 文件 / 目录
		tokenPaths := []string{
			filepath.Join(userHome, ".lark-cli"),
			filepath.Join(userHome, ".hermes", "auth.json"),
			filepath.Join(userHome, ".config", "lark"),
		}
		for _, p := range tokenPaths {
			if _, err := os.Stat(p); err == nil {
				if err := os.RemoveAll(p); err == nil {
					cleanedPaths = append(cleanedPaths, p)
				} else {
					s.logger.Warn("force-restart remove failed", "path", p, "error", err.Error())
				}
			}
		}
	}

	s.logger.Info("force-restart done", "user_id", userID, "mapping_removed", removed, "paths_cleaned", len(cleanedPaths))
	writeJSON(w, http.StatusOK, jsonResponse{
		"ok":              true,
		"multica_user_id": userID,
		"mapping_removed": removed,
		"paths_cleaned":   cleanedPaths,
		"next_step":       "请重新点击「开始飞书授权」走完整 device flow",
	})
}

func (s *Server) runScriptJSON(ctx context.Context, script string, args ...string) (map[string]any, error) {
	res, err := s.runner.Run(ctx, script, args...)
	if err != nil {
		return nil, err
	}
	payload, err := parseJSONObject(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("parse json failed: %w", err)
	}
	if ok, exists := payload["ok"]; exists {
		if bv, isBool := ok.(bool); isBool && !bv {
			return nil, errors.New(stringVal(payload, "error", "message"))
		}
	}
	return payload, nil
}

func (s *Server) recoverPanic(w http.ResponseWriter, _ *http.Request) {
	if rec := recover(); rec != nil {
		s.logger.Error("panic", "error", rec)
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"ok": false, "error": "internal server error"})
	}
}

func (s *Server) writeExecError(w http.ResponseWriter, msg string, err error, code int) {
	var execErr *ExecError
	if errors.As(err, &execErr) {
		s.logger.Error(msg,
			"script", execErr.Script,
			"exit_code", execErr.ExitCode,
			"timeout", execErr.Timeout,
			"stderr", strings.TrimSpace(execErr.Stderr),
		)
		if execErr.Timeout {
			writeJSON(w, code, jsonResponse{"ok": false, "error": msg + ": timeout"})
			return
		}
		writeJSON(w, code, jsonResponse{"ok": false, "error": fmt.Sprintf("%s: exit=%d", msg, execErr.ExitCode)})
		return
	}
	s.logger.Error(msg, "error", err)
	writeJSON(w, code, jsonResponse{"ok": false, "error": msg})
}

func decodeJSON(r *http.Request, out any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func validateUserID(userID string) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("multica_user_id is required")
	}
	if !userIDPattern.MatchString(userID) {
		return errors.New("multica_user_id contains invalid characters")
	}
	return nil
}

func loadMappings(path string) ([]MappingEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trim := strings.TrimSpace(string(b))
	if trim == "" {
		return []MappingEntry{}, nil
	}
	var entries []MappingEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func findMapping(items []MappingEntry, userID string) (MappingEntry, bool) {
	for _, item := range items {
		if item.MulticaUserID == userID {
			return item, true
		}
	}
	return MappingEntry{}, false
}

func saveMappings(path string, entries []MappingEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700)
	}
	tmp, err := os.CreateTemp(dir, "user_mapping.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// tryHealMapping: token enc 已落地但 mapping 缺时,直接 exec lark-cli auth status (绕过 05_spawn 的 mapping 检查) 补出 MappingEntry
func (s *Server) tryHealMapping(ctx context.Context, userID, userHome string) (*MappingEntry, error) {
	larkBin := os.Getenv("LARK_BIN")
	if larkBin == "" {
		// fallback to common nvm path; same default as wrapper
		larkBin = filepath.Join(os.Getenv("HOME"), ".nvm/versions/node/v22.12.0/bin/lark-cli")
	}
	cmd := exec.CommandContext(ctx, larkBin, "auth", "status")
	cmd.Env = append(os.Environ(), "HOME="+userHome)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec lark-cli (bin=%s home=%s): %w stderr=%s", larkBin, userHome, err, strings.TrimSpace(stderr.String()))
	}
	obj, err := parseJSONObject(string(stdout))
	if err != nil {
		return nil, fmt.Errorf("parse lark-cli stdout (head=%q): %w", truncate(string(stdout), 200), err)
	}
	// lark-cli auth status 输出 .identities.user.{openId,userName,tokenStatus}
	openID := ""
	userName := ""
	if ids, ok := obj["identities"].(map[string]any); ok {
		if u, ok := ids["user"].(map[string]any); ok {
			openID = stringVal(u, "openId", "open_id")
			userName = stringVal(u, "userName", "user_name")
		}
	}
	if openID == "" {
		// fallback: 顶层有 userOpenId
		openID = stringVal(obj, "userOpenId", "user_open_id")
		userName = stringVal(obj, "userName", "user_name")
	}
	if openID == "" {
		return nil, errors.New("auth status missing user openId")
	}
	return &MappingEntry{
		MulticaUserID:  userID,
		LarkUserOpenID: openID,
		LarkUserName:   userName,
		Home:           userHome,
		ProvisionedAt:  time.Now().UTC(),
	}, nil
}

func parseTokenStatus(out string) (TokenStatus, error) {
	obj, err := parseJSONObject(out)
	if err != nil {
		return TokenStatus{}, err
	}
	return TokenStatus{
		TokenStatus: stringVal(obj, "tokenStatus", "token_status"),
		ExpiresAt:   stringVal(obj, "expiresAt", "expires_at"),
		Scopes:      sliceStringVal(obj, "scopes"),
	}, nil
}

func parseJSONObject(raw string) (map[string]any, error) {
	clean := stripWarnLines(raw)
	first := strings.Index(clean, "{")
	last := strings.LastIndex(clean, "}")
	if first < 0 || last < 0 || last <= first {
		return nil, errors.New("json object not found")
	}
	clean = clean[first : last+1]
	var obj map[string]any
	if err := json.Unmarshal([]byte(clean), &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func stripWarnLines(raw string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "[WARN]") || strings.HasPrefix(trim, "[lark-cli]") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

func stringVal(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func intVal(m map[string]any, keys ...string) int {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			}
		}
	}
	return 0
}

func sliceStringVal(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body jsonResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
