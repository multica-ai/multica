package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func rewriteFakeMuseServe(t *testing.T, path, old, replacement string) {
	t.Helper()
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), old) {
		t.Fatalf("fake server does not contain %q", old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(script), old, replacement, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestMuseHostExitAfterTurnStart(t *testing.T) {
	t.Parallel()
	bin, _, _ := writeFakeMuseServe(t, false)
	rewriteFakeMuseServe(t, bin, "      sleep 0.02", "      exit 17")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := newFakeMuseBackend(t, bin).Execute(ctx, "work", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "exited") {
		t.Fatalf("unexpected host exit: status=%q error=%q", result.Status, result.Error)
	}
}

func TestMuseTerminalSurvivesSessionCloseAndEOF(t *testing.T) {
	t.Parallel()
	bin, _, _ := writeFakeMuseServe(t, false)
	rewriteFakeMuseServe(t, bin, `"durationMs":12}}\n'`, `"durationMs":12}}\n'
      printf '{"jsonrpc":"2.0","method":"session/closed","params":{}}\n'
      exit 0
      #`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := newFakeMuseBackend(t, bin).Execute(ctx, "work", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "Muse finished" {
		t.Fatalf("terminal result was lost: %+v", result)
	}
	if session.TerminalObserved == nil || !session.TerminalObserved() {
		t.Fatal("the daemon must observe the terminal before cleanup")
	}
}

func TestMuseResumeAppliesSelectedModel(t *testing.T) {
	t.Parallel()
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "rejected"}[reject], func(t *testing.T) {
			t.Parallel()
			bin, requestsPath, _ := writeFakeMuseServe(t, false)
			if reject {
				rewriteFakeMuseServe(t, bin, `"result":{"sessionId":"`+fakeMuseSessionID+`","modelId":"new-model"}`, `"error":{"code":-32602,"message":"model unavailable"}`)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			session, err := newFakeMuseBackend(t, bin).Execute(ctx, "continue", ExecOptions{
				Cwd: t.TempDir(), ResumeSessionID: fakeMuseSessionID, Model: "new-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			result := <-session.Result
			requests := readMuseRequests(t, requestsPath)
			setModel := museRequestByMethod(requests, "session/setModel")
			if setModel == nil {
				t.Fatal("resumed session never received session/setModel")
			}
			params := setModel["params"].(map[string]any)
			selection, _ := params["model"].(map[string]any)
			if selection["modelId"] != "new-model" || params["sessionId"] != fakeMuseSessionID {
				t.Fatalf("setModel params = %v", params)
			}
			if reject {
				if result.Status != "failed" || !strings.Contains(result.Error, "model unavailable") || result.SessionID != fakeMuseSessionID {
					t.Fatalf("model rejection lost status or session: %+v", result)
				}
				if museRequestByMethod(requests, "turn/start") != nil {
					t.Fatal("turn started after model selection failed")
				}
			} else if result.Status != "completed" {
				t.Fatalf("resume failed: %+v", result)
			}
		})
	}
}
