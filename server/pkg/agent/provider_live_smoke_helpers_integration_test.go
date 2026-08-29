//go:build agentintegration

package agent

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveProviderHarnessRequiresExplicitProvider(t *testing.T) {
	_, _, err := providerSmokeSelection(func(key string) string {
		if key == providerSmokeModelEnv {
			return "fixture-model"
		}
		return ""
	})
	if err == nil {
		t.Fatal("provider selection succeeded without an explicit provider")
	}
	if err.Error() != providerSmokeProviderEnv+" is required" {
		t.Fatal("provider selection returned an unstable missing-provider diagnostic")
	}
}

func TestLiveProviderHarnessRequiresExplicitModel(t *testing.T) {
	_, _, err := providerSmokeSelection(func(key string) string {
		if key == providerSmokeProviderEnv {
			return "openrouter"
		}
		return ""
	})
	if err == nil {
		t.Fatal("provider selection succeeded without an explicit model")
	}
	if err.Error() != providerSmokeModelEnv+" is required" {
		t.Fatal("provider selection returned an unstable missing-model diagnostic")
	}
}

func TestLiveProviderHarnessTrimsExplicitSelection(t *testing.T) {
	provider, model, err := providerSmokeSelection(func(key string) string {
		switch key {
		case providerSmokeProviderEnv:
			return " openrouter "
		case providerSmokeModelEnv:
			return " fixture-model "
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("providerSmokeSelection: %v", err)
	}
	if provider != "openrouter" || model != "fixture-model" {
		t.Fatal("provider selection did not trim the explicit provider and model")
	}
}

func TestLiveProviderHarnessBuildsProviderOwnedEnvironment(t *testing.T) {
	desc, ok := ProviderByID("opencode-api")
	if !ok {
		t.Fatal("opencode-api provider descriptor is unavailable")
	}
	fixtures := map[string]string{
		desc.BaseURLEnv:          "https://fixture.invalid/v1",
		desc.APIKeyEnv:           "fixture-primary-value",
		desc.OptionalKeyEnv[0]:   "fixture-optional-value",
		"HOME":                   "/fixture/home",
		"OPENROUTER_API_KEY":     "fixture-other-provider-value",
		providerSmokeProviderEnv: "opencode-api",
		providerSmokeModelEnv:    "fixture-model",
	}
	var lookedUp []string
	env, err := providerSmokeEnv("opencode-api", func(key string) string {
		lookedUp = append(lookedUp, key)
		return fixtures[key]
	})
	if err != nil {
		t.Fatalf("providerSmokeEnv: %v", err)
	}

	wantKeys := append([]string{desc.BaseURLEnv, desc.APIKeyEnv}, desc.OptionalKeyEnv...)
	sort.Strings(wantKeys)
	sort.Strings(lookedUp)
	if !reflect.DeepEqual(lookedUp, wantKeys) {
		t.Fatalf("environment lookup keys = %v, want descriptor keys %v", lookedUp, wantKeys)
	}
	if len(env) != len(wantKeys) {
		t.Fatalf("allowlisted environment has %d keys, want %d", len(env), len(wantKeys))
	}
	for _, key := range wantKeys {
		if _, ok := env[key]; !ok {
			t.Fatalf("allowlisted environment omitted %s", key)
		}
	}
	for _, key := range []string{"HOME", "OPENROUTER_API_KEY", providerSmokeProviderEnv, providerSmokeModelEnv} {
		if _, ok := env[key]; ok {
			t.Fatalf("allowlisted environment included unrelated key %s", key)
		}
	}
}

func TestLiveProviderHarnessDrainsMessagesAndResultConcurrently(t *testing.T) {
	messagesCh := make(chan Message)
	resultCh := make(chan Result)
	producerDone := make(chan struct{})
	go func() {
		resultCh <- Result{Status: "completed", Output: "fixture-final"}
		close(resultCh)
		messagesCh <- Message{Type: MessageText, Content: "fixture-stream"}
		close(messagesCh)
		close(producerDone)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	messages, result, err := drainProviderSmokeSession(ctx, &Session{
		Messages: messagesCh,
		Result:   resultCh,
	})
	if err != nil {
		t.Fatalf("drainProviderSmokeSession: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "fixture-stream" {
		t.Fatal("session drain did not collect the complete message stream")
	}
	if result.Status != "completed" || result.Output != "fixture-final" {
		t.Fatal("session drain did not collect the final result")
	}
	select {
	case <-producerDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("session drain left a producer blocked")
	}
}

type providerSmokeTrackingTransport struct {
	closed atomic.Bool
}

func (*providerSmokeTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("tracking transport must not make requests")
}

func (t *providerSmokeTrackingTransport) CloseIdleConnections() {
	t.closed.Store(true)
}

func TestLiveProviderHarnessClosesTransport(t *testing.T) {
	transport := &providerSmokeTrackingTransport{}
	cleanup := providerSmokeTransportCleanup(&http.Client{Transport: transport})
	cleanup()
	if !transport.closed.Load() {
		t.Fatal("provider smoke transport cleanup did not close idle connections")
	}
}

func TestLiveProviderHarnessUsesDedicatedTransport(t *testing.T) {
	client := newProviderSmokeHTTPClient()
	t.Cleanup(providerSmokeTransportCleanup(client))
	if client.Transport == nil {
		t.Fatal("provider smoke client has no explicit transport")
	}
	if client.Transport == http.DefaultTransport {
		t.Fatal("provider smoke client reused the process-wide default transport")
	}
}

func TestLiveProviderHarnessSecretLeakDetectionUsesFixedMessage(t *testing.T) {
	secret := strings.Join([]string{"fixture", "provider", "credential"}, "-")
	tests := []struct {
		name     string
		messages []Message
		result   Result
		logs     string
	}{
		{name: "message", messages: []Message{{Type: MessageText, Content: "prefix " + secret}}},
		{name: "message input", messages: []Message{{Type: MessageToolUse, Input: map[string]any{"value": secret}}}},
		{name: "result output", result: Result{Output: "prefix " + secret}},
		{name: "result error", result: Result{Error: "prefix " + secret}},
		{name: "logs", logs: "prefix " + secret},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := providerSmokeLeakError(secret, test.messages, test.result, test.logs)
			if err == nil {
				t.Fatal("secret leak detector accepted a credential-bearing diagnostic")
			}
			if err.Error() != providerSmokeSecretLeakMessage {
				t.Fatal("secret leak detector did not return its fixed diagnostic")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("secret leak diagnostic echoed the configured credential")
			}
		})
	}

	if err := providerSmokeLeakError(secret,
		[]Message{{Type: MessageText, Content: "safe stream"}},
		Result{Status: "completed", Output: "safe result"},
		"safe logs",
	); err != nil {
		t.Fatal("secret leak detector rejected credential-free diagnostics")
	}
}
