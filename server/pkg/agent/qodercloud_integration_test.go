//go:build agentintegration

package agent

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestQoderCloudRealSmoke creates a real hosted session and sends one turn.
// It is opt-in twice: the agentintegration build tag plus
// MULTICA_RUN_REAL_AGENT_SMOKE=1. Credentials are read only from the process
// environment and are never logged.
func TestQoderCloudRealSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real Qoder Cloud smoke in short mode")
	}
	pat := strings.TrimSpace(os.Getenv("MULTICA_QODERCLOUD_PAT"))
	agentID := strings.TrimSpace(os.Getenv("MULTICA_QODERCLOUD_AGENT_ID"))
	environmentID := strings.TrimSpace(os.Getenv("MULTICA_QODERCLOUD_ENVIRONMENT_ID"))
	if pat == "" || agentID == "" || environmentID == "" {
		t.Skip("set MULTICA_QODERCLOUD_PAT, MULTICA_QODERCLOUD_AGENT_ID, and MULTICA_QODERCLOUD_ENVIRONMENT_ID")
	}
	version, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("MULTICA_QODERCLOUD_AGENT_VERSION")))
	cloudCfg := QoderCloudConfig{
		BaseURL:       strings.TrimSpace(os.Getenv("MULTICA_QODERCLOUD_BASE_URL")),
		PAT:           pat,
		AgentID:       agentID,
		EnvironmentID: environmentID,
		AgentVersion:  version,
	}
	backend, err := New("qodercloud", Config{QoderCloud: cloudCfg})
	if err != nil {
		t.Fatalf("new Qoder Cloud backend: %v", err)
	}
	cleanupClient, _, cleanupClientErr := newQoderCloudClient(cloudCfg)
	if cleanupClientErr != nil {
		t.Fatalf("prepare Qoder Cloud cleanup client: %v", cleanupClientErr)
	}
	var cleanupOnce sync.Once
	registerCleanup := func(sessionID string) {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return
		}
		cleanupOnce.Do(func() {
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), qoderCloudCancelTimeout)
				defer cleanupCancel()
				if cleanupErr := cleanupClient.deleteSession(cleanupCtx, sessionID); cleanupErr != nil {
					t.Errorf("delete real Qoder Cloud smoke session: %v", cleanupErr)
				}
			})
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx, "Remember this token and reply with exactly it: multica-qodercloud-ok", ExecOptions{
		SystemPrompt: "Follow the user's exact-token response format.",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	result, awaitErr := awaitQoderCloudSmokeResult(session, 2*time.Minute+10*time.Second, registerCleanup)
	if awaitErr != nil {
		t.Fatalf("wait for real Qoder Cloud result: %v", awaitErr)
	}
	if result.Status != "completed" {
		t.Fatalf("real Qoder Cloud run failed: status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(strings.ToLower(result.Output), "multica-qodercloud-ok") {
		t.Fatalf("unexpected real Qoder Cloud output: %q", result.Output)
	}
	if result.SessionID == "" {
		t.Fatal("real Qoder Cloud run omitted session ID")
	}

	resumed, resumeErr := backend.Execute(ctx, "Reply with exactly the token from your previous response and nothing else.", ExecOptions{
		ResumeSessionID: result.SessionID,
		SystemPrompt:    "Follow the user's exact-token response format.",
	})
	if resumeErr != nil {
		t.Fatalf("resume real Qoder Cloud session: %v", resumeErr)
	}
	resumeResult, resumeAwaitErr := awaitQoderCloudSmokeResult(resumed, 2*time.Minute+10*time.Second, registerCleanup)
	if resumeAwaitErr != nil {
		t.Fatalf("wait for real Qoder Cloud resume: %v", resumeAwaitErr)
	}
	if resumeResult.Status != "completed" || resumeResult.SessionID != result.SessionID {
		t.Fatalf("real Qoder Cloud resume failed: status=%q error=%q session=%q", resumeResult.Status, resumeResult.Error, resumeResult.SessionID)
	}
	if !strings.Contains(strings.ToLower(resumeResult.Output), "multica-qodercloud-ok") {
		t.Fatalf("real Qoder Cloud resume lost context: %q", resumeResult.Output)
	}
	t.Log("real Qoder Cloud create + resume smoke completed successfully")
}

func awaitQoderCloudSmokeResult(session *Session, timeout time.Duration, onSessionID func(string)) (Result, error) {
	messages := session.Messages
	results := session.Result
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var (
		result     Result
		haveResult bool
	)
	for messages != nil || results != nil {
		if haveResult && messages == nil {
			return result, nil
		}
		select {
		case message, ok := <-messages:
			if !ok {
				messages = nil
				continue
			}
			if message.SessionID != "" {
				onSessionID(message.SessionID)
			}
		case received, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			result = received
			haveResult = true
			if received.SessionID != "" {
				onSessionID(received.SessionID)
			}
		case <-timer.C:
			drainQoderCloudSmokeSessionIDs(messages, results, onSessionID)
			return Result{}, errors.New("timed out waiting for Qoder Cloud smoke result")
		}
	}
	if !haveResult {
		return Result{}, errors.New("Qoder Cloud smoke result channel closed without a result")
	}
	return result, nil
}

func drainQoderCloudSmokeSessionIDs(messages <-chan Message, results <-chan Result, onSessionID func(string)) {
	for messages != nil || results != nil {
		select {
		case message, ok := <-messages:
			if !ok {
				messages = nil
				continue
			}
			if message.SessionID != "" {
				onSessionID(message.SessionID)
			}
		case result, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			if result.SessionID != "" {
				onSessionID(result.SessionID)
			}
		default:
			return
		}
	}
}

func TestAwaitQoderCloudSmokeResultCapturesEarlySessionID(t *testing.T) {
	t.Run("failed result", func(t *testing.T) {
		messages := make(chan Message, 1)
		results := make(chan Result, 1)
		messages <- Message{Type: MessageStatus, Status: "session_started", SessionID: "cleanup-failed"}
		close(messages)
		results <- Result{Status: "failed", Error: "synthetic failure"}
		close(results)
		var captured string
		result, err := awaitQoderCloudSmokeResult(&Session{Messages: messages, Result: results}, time.Second, func(id string) {
			captured = id
		})
		if err != nil || result.Status != "failed" || captured != "cleanup-failed" {
			t.Fatalf("early session id was not retained on failure: id=%q result=%+v err=%v", captured, result, err)
		}
	})

	t.Run("outer timeout", func(t *testing.T) {
		messages := make(chan Message, 1)
		results := make(chan Result)
		messages <- Message{Type: MessageStatus, Status: "session_started", SessionID: "cleanup-timeout"}
		var captured string
		_, err := awaitQoderCloudSmokeResult(&Session{Messages: messages, Result: results}, time.Millisecond, func(id string) {
			captured = id
		})
		if err == nil || captured != "cleanup-timeout" {
			t.Fatalf("early session id was not retained on timeout: id=%q err=%v", captured, err)
		}
	})
}
