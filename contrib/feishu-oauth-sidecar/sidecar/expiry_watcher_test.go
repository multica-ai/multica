package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// frozenRunner: 按 callKey 派发 stdout/err,记录所有调用方便断言.
type frozenRunner struct {
	mu    sync.Mutex
	resp  map[string]mockResp
	calls []string
}

func (r *frozenRunner) Run(ctx context.Context, script string, args ...string) (RunResult, error) {
	key := callKey(script, args...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, key)
	if got, ok := r.resp[key]; ok {
		return RunResult{Stdout: got.stdout, Stderr: got.stderr}, got.err
	}
	// 未配置 → 当成 lark-cli 调用成功但空输出,避免 panic; 显式声明 unknown
	return RunResult{Stdout: `{"ok":true}`}, nil
}

func (r *frozenRunner) callsCopy() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

func newTestWatcher(t *testing.T, runner ScriptRunner, mappingJSON string, now time.Time) (*ExpiryWatcher, string) {
	t.Helper()
	dir := t.TempDir()
	mapping := filepath.Join(dir, "user_mapping.json")
	if err := os.WriteFile(mapping, []byte(mappingJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	stateFile := filepath.Join(dir, "notified.json")
	w := NewExpiryWatcher(runner, slog.New(slog.NewTextHandler(io.Discard, nil)), mapping, stateFile)
	w.now = func() time.Time { return now }
	w.scriptTimeout = 2 * time.Second
	return w, stateFile
}

func TestExpiryWatcher_NotifyAboutToExpire(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	expireIn10h := now.Add(10 * time.Hour).Format(time.RFC3339)
	mapping := `[{"multica_user_id":"u1","lark_user_open_id":"ou_1","lark_user_name":"alice","home":"/h/u1","provisioned_at":"2026-05-01T00:00:00Z"}]`
	runner := &frozenRunner{resp: map[string]mockResp{
		callKey("05_agent_spawn.sh", "u1", "auth", "status"): {
			stdout: `{"tokenStatus":"valid","refreshExpiresAt":"` + expireIn10h + `","expiresAt":"` + expireIn10h + `"}`,
		},
		callKey("05_agent_spawn.sh", "u1", "im", "+messages-send",
			"--user-id", "ou_1", "--text",
			"⚠️ 飞书授权将在约 10 小时 后过期,请打开 https://multica.example.com/oauth-ui 重新扫码授权,以免 multica 任务被中断。"): {
			stdout: `{"ok":true}`,
		},
	}}
	w, stateFile := newTestWatcher(t, runner, mapping, now)

	scanned, notified, errs := w.RunOnce(context.Background())
	if scanned != 1 || notified != 1 || errs != 0 {
		t.Fatalf("want 1/1/0 got %d/%d/%d, calls=%v", scanned, notified, errs, runner.callsCopy())
	}

	// 状态文件必须含 u1 的 notified_at
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st notifyState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if _, ok := st.NotifiedAt["u1"]; !ok {
		t.Fatalf("state missing u1: %s", raw)
	}
}

func TestExpiryWatcher_NoNotifyWhenFresh(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	expireIn5d := now.Add(5 * 24 * time.Hour).Format(time.RFC3339)
	mapping := `[{"multica_user_id":"u1","lark_user_open_id":"ou_1","home":"/h/u1","provisioned_at":"2026-05-01T00:00:00Z"}]`
	runner := &frozenRunner{resp: map[string]mockResp{
		callKey("05_agent_spawn.sh", "u1", "auth", "status"): {
			stdout: `{"tokenStatus":"valid","refreshExpiresAt":"` + expireIn5d + `"}`,
		},
	}}
	w, stateFile := newTestWatcher(t, runner, mapping, now)
	_, notified, errs := w.RunOnce(context.Background())
	if notified != 0 || errs != 0 {
		t.Fatalf("want notified=0 errs=0, got %d/%d, calls=%v", notified, errs, runner.callsCopy())
	}
	// 没通知 → 状态文件不该被创建
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("state file should not exist, err=%v", err)
	}
	// 也不该有 im +messages-send 调用
	for _, c := range runner.callsCopy() {
		if strings.Contains(c, "messages-send") {
			t.Fatalf("unexpected messages-send call: %s", c)
		}
	}
}

func TestExpiryWatcher_CooldownPreventsDoubleNotify(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	expireIn10h := now.Add(10 * time.Hour).Format(time.RFC3339)
	mapping := `[{"multica_user_id":"u1","lark_user_open_id":"ou_1","home":"/h/u1","provisioned_at":"2026-05-01T00:00:00Z"}]`
	runner := &frozenRunner{resp: map[string]mockResp{
		callKey("05_agent_spawn.sh", "u1", "auth", "status"): {
			stdout: `{"tokenStatus":"valid","refreshExpiresAt":"` + expireIn10h + `"}`,
		},
		// 任意 messages-send 调用 — 这里 stdout 写空给默认 success
	}}
	w, stateFile := newTestWatcher(t, runner, mapping, now)

	// 第一次: 应通知
	_, notified1, _ := w.RunOnce(context.Background())
	if notified1 != 1 {
		t.Fatalf("first run want notified=1 got %d, calls=%v", notified1, runner.callsCopy())
	}
	// 第二次同一时刻: cooldown 24h 应抑制
	_, notified2, _ := w.RunOnce(context.Background())
	if notified2 != 0 {
		t.Fatalf("second run want notified=0 (cooldown) got %d", notified2)
	}
	// 推进时间 25h: cooldown 过, 又应通知
	w.now = func() time.Time { return now.Add(25 * time.Hour) }
	// 更新 expiry 到 now+25h+10h, 让它仍在 24h threshold 内
	expireLater := now.Add(25 * time.Hour).Add(10 * time.Hour).Format(time.RFC3339)
	runner.mu.Lock()
	runner.resp[callKey("05_agent_spawn.sh", "u1", "auth", "status")] = mockResp{
		stdout: `{"tokenStatus":"valid","refreshExpiresAt":"` + expireLater + `"}`,
	}
	runner.mu.Unlock()
	_, notified3, _ := w.RunOnce(context.Background())
	if notified3 != 1 {
		t.Fatalf("third run want notified=1 (cooldown elapsed) got %d", notified3)
	}
	// 状态文件应该被 update 过
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file missing after notify: %v", err)
	}
}

func TestExpiryWatcher_SkipEntryWithMissingOpenID(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	mapping := `[{"multica_user_id":"u1","lark_user_open_id":"","home":"/h/u1"}]`
	runner := &frozenRunner{resp: map[string]mockResp{}}
	w, _ := newTestWatcher(t, runner, mapping, now)
	scanned, notified, errs := w.RunOnce(context.Background())
	if scanned != 1 || notified != 0 || errs != 0 {
		t.Fatalf("want 1/0/0 got %d/%d/%d", scanned, notified, errs)
	}
	// 不应该调任何 lark-cli
	if len(runner.callsCopy()) != 0 {
		t.Fatalf("should skip without lark-cli calls, got %v", runner.callsCopy())
	}
}

func TestExpiryWatcher_FetchExpiryParsesNestedIdentities(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	expireIn8h := now.Add(8 * time.Hour).Format(time.RFC3339)
	mapping := `[{"multica_user_id":"u1","lark_user_open_id":"ou_1","home":"/h/u1"}]`
	// 用嵌套 identities.user.refreshExpiresAt 测 fallback 解析路径
	nestedJSON := `{"identities":{"user":{"tokenStatus":"valid","refreshExpiresAt":"` + expireIn8h + `"}}}`
	runner := &frozenRunner{resp: map[string]mockResp{
		callKey("05_agent_spawn.sh", "u1", "auth", "status"): {stdout: nestedJSON},
	}}
	w, _ := newTestWatcher(t, runner, mapping, now)
	_, notified, errs := w.RunOnce(context.Background())
	if notified != 1 || errs != 0 {
		t.Fatalf("want notified=1 errs=0 got %d/%d, calls=%v", notified, errs, runner.callsCopy())
	}
}

func TestExpiryWatcher_StripsWarnLinesFromAuthOutput(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	expireIn5h := now.Add(5 * time.Hour).Format(time.RFC3339)
	noisyOut := "[WARN] proxy detected\n[lark-cli] using HOME=/h/u1\n" +
		`{"tokenStatus":"valid","refreshExpiresAt":"` + expireIn5h + `"}` + "\n"
	mapping := `[{"multica_user_id":"u1","lark_user_open_id":"ou_1","home":"/h/u1"}]`
	runner := &frozenRunner{resp: map[string]mockResp{
		callKey("05_agent_spawn.sh", "u1", "auth", "status"): {stdout: noisyOut},
	}}
	w, _ := newTestWatcher(t, runner, mapping, now)
	_, notified, errs := w.RunOnce(context.Background())
	if notified != 1 || errs != 0 {
		t.Fatalf("want notified=1 errs=0 got %d/%d", notified, errs)
	}
}

func TestExpiryWatcher_FormatRemaining(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "0 小时 (已过期)"},
		{30 * time.Minute, "30 分钟"},
		{90 * time.Minute, "1 小时"},
		{23 * time.Hour, "23 小时"},
		{72 * time.Hour, "3 天"},
	}
	for _, c := range cases {
		if got := formatRemaining(c.d); got != c.want {
			t.Errorf("formatRemaining(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestExpiryWatcher_ParseFlexibleTime(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"2026-05-30T20:46:09+08:00", false},
		{"2026-05-30T12:46:09Z", false},
		{"2026-05-30 20:46:09", false},
		{"1748520369", false}, // unix sec ~2025
		{"", true},
		{"not-a-time", true},
	}
	for _, c := range cases {
		_, err := parseFlexibleTime(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseFlexibleTime(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}
