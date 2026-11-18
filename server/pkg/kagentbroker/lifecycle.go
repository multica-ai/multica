package kagentbroker

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
)

func requestDigest(m map[string]any) string { b, _ := json.Marshal(m); return fingerprintBytes(b) }
func (b *Broker) lifecycle(w http.ResponseWriter, ctx context.Context, p Policy, request map[string]any, token string, create bool) {
	j := b.ownership
	if !j.acquire(ctx) {
		deny(w, 504)
		return
	}
	defer func() { <-j.gate }()
	rec, ok := j.snapshot()
	if !ok {
		deny(w, 503)
		return
	}
	if rec.State == bound {
		p.ExpectedUID = rec.UID
	}
	if !create && (rec.State != bound || !p.agent(request, "patch")) {
		deny(w, 403)
		return
	}
	digest := requestDigest(request)
	if create && (rec.State == createIntent || (rec.State == bound && rec.Digest != digest)) {
		deny(w, 409)
		return
	}
	current, err := b.kube.result(ctx, "GET", p.resource(), "", nil)
	if err != nil {
		deny(w, 502)
		return
	}
	if create && rec.State == uninitialized {
		if current.Status != http.StatusNotFound {
			deny(w, 409)
			return
		}
		rec.State = createIntent
		rec.Digest = digest
		if j.save(rec) != nil {
			deny(w, 503)
			return
		}
		if ctx.Err() != nil {
			deny(w, 504)
			return
		}
		// Intent is permanent even if dispatch never occurs or its response is lost.
		result, e := b.kube.result(ctx, "POST", p.collection(), "application/json", request)
		if e == nil && result.Status == http.StatusConflict {
			deny(w, 409)
			return
		}
		if e != nil || result.Status != http.StatusCreated {
			deny(w, 502)
			return
		}
		uid := str(object(result.Object["metadata"])["uid"])
		p.ExpectedUID = uid
		if !tokenSafe(uid) || len(uid) > 256 || !p.agent(result.Object, "observed") || !matchesWrite(result.Object, request, false) || !b.safeAgentResponse(result.Object, p, token) {
			deny(w, 502)
			return
		}
		rec.State = bound
		rec.UID = uid
		if j.save(rec) != nil {
			deny(w, 503)
			return
		}
		b.agentResponse(w, result.Object, p, token, http.StatusCreated)
		return
	}
	if current.Status != http.StatusOK || !p.agent(current.Object, "observed") {
		deny(w, 502)
		return
	}
	if create {
		// Exact committed replay performs no POST. Never adopt an intent via GET.
		b.agentResponse(w, current.Object, p, token, http.StatusCreated)
		return
	}
	if object(current.Object["metadata"])["resourceVersion"] != object(request["metadata"])["resourceVersion"] {
		deny(w, 409)
		return
	}
	if ctx.Err() != nil {
		deny(w, 504)
		return
	}
	result, err := b.kube.result(ctx, "PATCH", p.resource(), "application/merge-patch+json", request)
	if err == nil && result.Status == http.StatusConflict {
		deny(w, 409)
		return
	}
	if err != nil || result.Status != http.StatusOK || !p.agent(result.Object, "observed") || !matchesWrite(result.Object, request, true) {
		deny(w, 502)
		return
	}
	b.agentResponse(w, result.Object, p, token, http.StatusOK)
}

// Compare owned leaves, treating merge-patch null as removal, not a literal
// field value. Unknown response capabilities are independently rejected by policy.
func matchesWrite(observed, request map[string]any, patch bool) bool {
	var matches func(map[string]any, map[string]any) bool
	matches = func(got, want map[string]any) bool {
		for k, v := range want {
			if k == "status" || (patch && k == "resourceVersion") {
				continue
			}
			if v == nil {
				if gv, exists := got[k]; exists && gv != nil {
					return false
				}
				continue
			}
			if m, ok := v.(map[string]any); ok {
				if !matches(object(got[k]), m) {
					return false
				}
			} else if !reflect.DeepEqual(got[k], v) {
				return false
			}
		}
		return true
	}
	return matches(observed, request)
}
func (b *Broker) projectedAgent(observed map[string]any, p Policy) (map[string]any, bool) {
	meta := object(observed["metadata"])
	out := map[string]any{"apiVersion": "api.kagent.dev/v1alpha3", "kind": "Agent", "metadata": map[string]any{"name": p.name(), "namespace": p.Namespace, "uid": p.ExpectedUID, "resourceVersion": meta["resourceVersion"], "generation": meta["generation"], "labels": meta["labels"], "annotations": meta["annotations"]}, "spec": observed["spec"]}
	return out, readiness(observed, out)
}
func (b *Broker) safeAgentResponse(observed map[string]any, p Policy, token string) bool {
	out, ok := b.projectedAgent(observed, p)
	secrets := []string{token, b.kube.credential, b.a2a.credential}
	if reviewer, yes := b.reviewer.(*TokenReviewer); yes {
		secrets = append(secrets, reviewer.endpoint.credential)
	}
	data, err := json.Marshal(out)
	return ok && err == nil && len(data) <= responseLimit && !reflects(out, secrets)
}
func (b *Broker) agentResponse(w http.ResponseWriter, observed map[string]any, p Policy, token string, status int) {
	if !b.safeAgentResponse(observed, p, token) {
		deny(w, 502)
		return
	}
	out, _ := b.projectedAgent(observed, p)
	data, _ := json.Marshal(out)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
