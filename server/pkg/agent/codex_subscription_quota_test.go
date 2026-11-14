//go:build !windows

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCodexSubscriptionQuota(t *testing.T) {
	for _, tc := range []struct {
		name, account, limits string
		wantQuota             bool
	}{
		{"ChatGPT", `{"type":"chatgpt","email":"private@example.test"}`, `{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":180},"secondary":{"usedPercent":32,"resetsAt":2000000000}}}`, true},
		{"API key", `{"type":"apiKey"}`, "", false},
		{"signed out", `null`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "fake-codex")
			t.Setenv("CODEX_HOME", dir)
			script := `#!/bin/sh
test -d "$CODEX_HOME" && test "$*" = 'app-server --listen stdio://' || exit 1
IFS= read -r msg; case "$msg" in *'"method":"initialize"'*) ;; *) exit 2;; esac
printf '%s\n' '{"id":1,"result":{}}'
IFS= read -r msg; case "$msg" in *'"method":"initialized"'*) ;; *) exit 3;; esac
IFS= read -r msg; case "$msg" in *'"method":"account/read"'*'"refreshToken":false'*) ;; *) exit 4;; esac
printf '%s\n' '{"id":2,"result":{"account":` + tc.account + `}}'
`
			if tc.wantQuota {
				script += `
IFS= read -r msg; case "$msg" in *'"method":"account/rateLimits/read"'*) ;; *) exit 5;; esac
printf '%s\n' '{"method":"account/rateLimits/updated","params":{}}' '{"id":3,"result":` + tc.limits + `}'
`
			}
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			snapshot, err := ReadCodexSubscriptionQuota(context.Background(), NewCommand(path, nil))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantQuota {
				if snapshot != nil {
					t.Fatal("unexpected subscription quota")
				}
				return
			}
			if snapshot == nil || snapshot.ObservedAt <= 0 ||
				snapshot.Primary == nil || snapshot.Primary.UsedPercent == nil || *snapshot.Primary.UsedPercent != 0 ||
				snapshot.Primary.DurationMins == nil || *snapshot.Primary.DurationMins != 180 ||
				snapshot.Secondary == nil || snapshot.Secondary.UsedPercent == nil || *snapshot.Secondary.UsedPercent != 32 ||
				snapshot.Secondary.ResetsAt == nil || *snapshot.Secondary.ResetsAt != 2000000000 {
				t.Fatalf("unexpected subscription quota: %+v", snapshot)
			}
		})
	}
}
