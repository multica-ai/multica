package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestExtractWebhookIdentity_GitHubPullRequest(t *testing.T) {
	payload := []byte(`{
		"action":"synchronize",
		"comment":{"body":"do not store this untrusted text"},
		"pull_request":{"number":42,"html_url":"https://github.com/o/r/pull/42","head":{"sha":"abc123"}},
		"repository":{"full_name":"o/r"},
		"sender":{"login":"bot","type":"Bot"}
	}`)
	headers := http.Header{
		"X-GitHub-Delivery": {"delivery-1"},
		"X-GitHub-Event":    {"pull_request"},
	}
	id := extractWebhookIdentity("github", headers, "", json.RawMessage(payload))
	if id.DeliveryID != "delivery-1" {
		t.Fatalf("delivery: %q", id.DeliveryID)
	}
	if id.Repository != "o/r" || id.PrURL != "https://github.com/o/r/pull/42" || id.PrNumber != 42 {
		t.Fatalf("pr identity: %#v", id)
	}
	if id.HeadSHA != "abc123" || id.SenderLogin != "bot" {
		t.Fatalf("head/sender: %#v", id)
	}
}

func TestBuildStoredWebhookEnvelope_OmitsRawComment(t *testing.T) {
	env := WebhookEnvelope{
		Event:        "github.issue_comment.created",
		EventPayload: json.RawMessage(`{"comment":{"body":"secret"},"issue":{"pull_request":{"html_url":"https://github.com/o/r/pull/1","number":1,"head":{"sha":"sha1"}}},"repository":{"full_name":"o/r"}}`),
		Request:      WebhookRequest{ReceivedAt: "2026-08-05T00:00:00Z"},
	}
	stored := buildStoredWebhookEnvelope("github", env, "coderabbit-pr-fix-monitor:https://github.com/o/r/pull/1", "d1", http.Header{"X-GitHub-Event": {"issue_comment"}}, env.EventPayload)
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("stored envelope leaked comment text: %s", string(raw))
	}
	if stored.ScopeClaim == "" || stored.Identity.PrURL == "" {
		t.Fatalf("identity envelope incomplete: %#v", stored)
	}
}

func TestExtractScopeClaimRejectsOverlongClaim(t *testing.T) {
	_, err := extractScopeClaim(http.Header{
		scopeClaimHeader: {strings.Repeat("x", maxScopeClaimLength+1)},
	})
	if err == nil {
		t.Fatal("expected overlong scope claim to be rejected")
	}
}

func TestMergeStoredEnvelopeHeadSHA_PreservesOriginalDelivery(t *testing.T) {
	oldPayload, err := json.Marshal(StoredWebhookEnvelope{
		Event: "github.pull_request.opened",
		Identity: WebhookEventIdentity{
			DeliveryID: "delivery-old",
			HeadSHA:    "sha-old",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	newPayload, err := json.Marshal(StoredWebhookEnvelope{
		Event: "github.pull_request.synchronize",
		Identity: WebhookEventIdentity{
			DeliveryID: "delivery-new",
			HeadSHA:    "sha-new",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	merged, changed, err := mergeStoredEnvelopeHeadSHA(oldPayload, newPayload)
	if err != nil || !changed {
		t.Fatalf("merge: changed=%v err=%v", changed, err)
	}
	var got StoredWebhookEnvelope
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatal(err)
	}
	if got.Identity.DeliveryID != "delivery-old" || got.Identity.HeadSHA != "sha-new" {
		t.Fatalf("unexpected merged identity: %#v", got.Identity)
	}
}
