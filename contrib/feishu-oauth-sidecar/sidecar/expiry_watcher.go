package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExpiryWatcher 周期扫所有 mapping,token 距 refresh expiry < threshold 时发飞书通知.
//
// 防止重复打扰: notify 状态文件 notified.json 记录每个 user 最近通知时间,
// 同一 user 24h 内不重复发通知.
//
// 复用 05_agent_spawn.sh 作为 lark-cli wrapper (HOME 隔离已在 wrapper 内做了),
// 不直接 exec lark-cli,避免重复实现 HOME 切换/proxy 注入逻辑.
type ExpiryWatcher struct {
	runner          ScriptRunner
	logger          *slog.Logger
	mappingFile     string
	stateFile       string
	scanInterval    time.Duration // 多久跑一次 scan, 默认 1h
	warnThreshold   time.Duration // token 距 expiry 多少时间触发通知, 默认 24h
	notifyCooldown  time.Duration // 同 user 多久内不重复通知, 默认 24h
	scriptTimeout   time.Duration // 单次 lark-cli 调用超时, 默认 30s
	notifyTextTpl   string        // 通知文案模板, %s = 剩余时间
	now             func() time.Time
	mu              sync.Mutex // 守护 notify 状态文件
}

// notifyState 是 stateFile 序列化结构.
type notifyState struct {
	NotifiedAt map[string]time.Time `json:"notified_at"` // key = multica_user_id, val = last notify time
}

// authStatusOutput 解析 lark-cli auth status 输出. 字段名都是 wrapper / lark-cli 已稳定的.
type authStatusOutput struct {
	TokenStatus      string `json:"tokenStatus"`
	ExpiresAt        string `json:"expiresAt"`
	RefreshExpiresAt string `json:"refreshExpiresAt"`
	// 嵌套结构 fallback: lark-cli 部分版本把字段塞 identities.user 下
	Identities struct {
		User struct {
			ExpiresAt        string `json:"expiresAt"`
			RefreshExpiresAt string `json:"refreshExpiresAt"`
			TokenStatus      string `json:"tokenStatus"`
		} `json:"user"`
	} `json:"identities"`
}

// NewExpiryWatcher 构造 watcher. 配置项走显式参数避免 surprise.
func NewExpiryWatcher(runner ScriptRunner, logger *slog.Logger, mappingFile, stateFile string) *ExpiryWatcher {
	return &ExpiryWatcher{
		runner:         runner,
		logger:         logger,
		mappingFile:    mappingFile,
		stateFile:      stateFile,
		scanInterval:   time.Hour,
		warnThreshold:  24 * time.Hour,
		notifyCooldown: 24 * time.Hour,
		scriptTimeout:  30 * time.Second,
		notifyTextTpl:  "⚠️ 飞书授权将在约 %s 后过期,请打开 https://multica.example.com/oauth-ui 重新扫码授权,以免 multica 任务被中断。",
		now:            time.Now,
	}
}

// Start 启动后台 ticker, ctx 取消即退出. 不阻塞调用方.
func (w *ExpiryWatcher) Start(ctx context.Context) {
	go func() {
		// 启动后等 30s 让 daemon/runner 完全 ready, 再做第一次扫描
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
			return
		}
		w.RunOnce(ctx)
		ticker := time.NewTicker(w.scanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce 同步跑一次完整扫描. 测试 / 手动触发都走这.
// 返回 (扫描人数, 通知人数, 错误数).
func (w *ExpiryWatcher) RunOnce(ctx context.Context) (scanned, notified, errs int) {
	entries, err := loadMappings(w.mappingFile)
	if err != nil {
		w.logger.Warn("expiry-watcher: load mapping failed", "error", err.Error())
		return 0, 0, 1
	}
	state, err := w.loadState()
	if err != nil {
		w.logger.Warn("expiry-watcher: load state failed (continue with empty)", "error", err.Error())
		state = notifyState{NotifiedAt: map[string]time.Time{}}
	}

	for _, entry := range entries {
		scanned++
		if entry.MulticaUserID == "" || entry.LarkUserOpenID == "" {
			continue
		}
		expireAt, status, err := w.fetchExpiry(ctx, entry.MulticaUserID)
		if err != nil {
			errs++
			w.logger.Warn("expiry-watcher: fetch expiry failed", "user_id", entry.MulticaUserID, "error", err.Error())
			continue
		}
		if expireAt.IsZero() {
			// 无 refreshExpiresAt 也无 expiresAt — lark-cli 输出格式异常, 跳过避免误报
			w.logger.Debug("expiry-watcher: no expiry field", "user_id", entry.MulticaUserID, "token_status", status)
			continue
		}
		remaining := expireAt.Sub(w.now())
		if remaining > w.warnThreshold {
			continue
		}
		// 已通知过且未到冷却期 → skip
		if last, ok := state.NotifiedAt[entry.MulticaUserID]; ok {
			if w.now().Sub(last) < w.notifyCooldown {
				continue
			}
		}
		if err := w.notify(ctx, entry, remaining); err != nil {
			errs++
			w.logger.Warn("expiry-watcher: notify failed", "user_id", entry.MulticaUserID, "error", err.Error())
			continue
		}
		state.NotifiedAt[entry.MulticaUserID] = w.now()
		notified++
		w.logger.Info("expiry-watcher: notify sent",
			"user_id", entry.MulticaUserID,
			"lark_user_open_id", entry.LarkUserOpenID,
			"remaining", remaining.String(),
		)
	}
	if notified > 0 {
		if err := w.saveState(state); err != nil {
			w.logger.Warn("expiry-watcher: save state failed", "error", err.Error())
			errs++
		}
	}
	w.logger.Info("expiry-watcher: scan done", "scanned", scanned, "notified", notified, "errors", errs)
	return scanned, notified, errs
}

// fetchExpiry 通过 05_agent_spawn.sh 调 lark-cli auth status 拿 refresh expiry.
// 优先用 refreshExpiresAt (~30 天有效); 没有就 fallback expiresAt (~2 小时, lark-cli 自动 refresh).
func (w *ExpiryWatcher) fetchExpiry(ctx context.Context, userID string) (time.Time, string, error) {
	ctx2, cancel := context.WithTimeout(ctx, w.scriptTimeout)
	defer cancel()
	res, err := w.runner.Run(ctx2, "05_agent_spawn.sh", userID, "auth", "status")
	if err != nil {
		return time.Time{}, "", err
	}
	clean := stripWarnLines(res.Stdout)
	// 去掉头尾非 JSON
	first := strings.Index(clean, "{")
	last := strings.LastIndex(clean, "}")
	if first < 0 || last <= first {
		return time.Time{}, "", fmt.Errorf("auth status: no JSON object (head=%q)", truncate(res.Stdout, 200))
	}
	var out authStatusOutput
	if err := json.Unmarshal([]byte(clean[first:last+1]), &out); err != nil {
		return time.Time{}, "", fmt.Errorf("auth status unmarshal: %w", err)
	}
	status := out.TokenStatus
	if status == "" {
		status = out.Identities.User.TokenStatus
	}
	pick := func(vals ...string) string {
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				return v
			}
		}
		return ""
	}
	expStr := pick(out.RefreshExpiresAt, out.Identities.User.RefreshExpiresAt, out.ExpiresAt, out.Identities.User.ExpiresAt)
	if expStr == "" {
		return time.Time{}, status, nil
	}
	t, err := parseFlexibleTime(expStr)
	if err != nil {
		return time.Time{}, status, fmt.Errorf("parse expiry %q: %w", expStr, err)
	}
	return t, status, nil
}

// parseFlexibleTime 接受 RFC3339 / RFC3339 含偏移 / 秒级 unix 数字字符串.
func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	// fallback: 数字 unix 秒
	if n := parseUnixSeconds(s); n > 0 {
		return time.Unix(n, 0).UTC(), nil
	}
	return time.Time{}, errors.New("unrecognized time format")
}

func parseUnixSeconds(s string) int64 {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int64(ch-'0')
	}
	// 1e9 (2001 年) ~ 1e10 (2286 年) 之间才像合理 unix 秒
	if n < 1_000_000_000 || n > 99_999_999_999 {
		return 0
	}
	return n
}

// notify 调 lark-cli im messages-send 给 user 自己发消息.
// 文本里写剩余时间 (向下取整到小时), 避免显示 "0h 12m" 这种太精确造成假紧迫感.
func (w *ExpiryWatcher) notify(ctx context.Context, entry MappingEntry, remaining time.Duration) error {
	ctx2, cancel := context.WithTimeout(ctx, w.scriptTimeout)
	defer cancel()
	humanRemain := formatRemaining(remaining)
	text := fmt.Sprintf(w.notifyTextTpl, humanRemain)
	_, err := w.runner.Run(ctx2, "05_agent_spawn.sh", entry.MulticaUserID, "im", "+messages-send",
		"--user-id", entry.LarkUserOpenID, "--text", text)
	return err
}

func formatRemaining(d time.Duration) string {
	if d <= 0 {
		return "0 小时 (已过期)"
	}
	hours := int(d.Hours())
	if hours >= 24 {
		days := hours / 24
		return fmt.Sprintf("%d 天", days)
	}
	if hours >= 1 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 分钟", int(d.Minutes()))
}

// ---- state file (notified.json) ----

func (w *ExpiryWatcher) loadState() (notifyState, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	empty := notifyState{NotifiedAt: map[string]time.Time{}}
	b, err := os.ReadFile(w.stateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, nil
		}
		return empty, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return empty, nil
	}
	var s notifyState
	if err := json.Unmarshal(b, &s); err != nil {
		return empty, err
	}
	if s.NotifiedAt == nil {
		s.NotifiedAt = map[string]time.Time{}
	}
	return s, nil
}

func (w *ExpiryWatcher) saveState(s notifyState) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(w.stateFile)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700)
	}
	tmp, err := os.CreateTemp(dir, "notified.*.tmp")
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
	return os.Rename(tmpName, w.stateFile)
}
