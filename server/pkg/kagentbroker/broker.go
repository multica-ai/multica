package kagentbroker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const InnerTokenHeader = "X-Kagent-Token"

type Config struct {
	Policy          Policy
	Attempts        *AttemptJournal
	Ownership       *OwnershipJournal
	Reviewer        Reviewer
	Kubernetes, A2A Endpoint
}
type Broker struct {
	policy      Policy
	ownership   *OwnershipJournal
	reviewer    Reviewer
	kube, a2a   endpoint
	mu          sync.Mutex
	tasks       map[string]string
	contexts    map[string]bool
	attempts    map[string]bool
	busy        map[string]bool
	sendBlocked bool
	journal     *AttemptJournal
}

func New(c Config) (*Broker, error) {
	if c.Ownership == nil || !c.Policy.valid() || c.Reviewer == nil || c.Attempts == nil || c.Attempts.file == nil || c.Attempts.gate == nil || c.Attempts.ids == nil {
		return nil, errDenied
	}
	record, healthy := c.Ownership.snapshot()
	if !healthy || record.Policy != fingerprint(c.Policy) {
		return nil, errDenied
	}
	k, err := newEndpoint(c.Kubernetes)
	if err != nil {
		return nil, err
	}
	a, err := newEndpoint(c.A2A)
	if err != nil {
		return nil, err
	}
	return &Broker{ownership: c.Ownership, journal: c.Attempts, policy: c.Policy, reviewer: c.Reviewer, kube: k, a2a: a, tasks: map[string]string{}, contexts: map[string]bool{}, attempts: map[string]bool{}, busy: map[string]bool{}}, nil
}
func deny(w http.ResponseWriter, status int) { http.Error(w, "broker: request denied", status) }
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Exact RequestURI comparison also rejects escaped separators, dot segments,
	// absolute-form targets, trailing ?, query selectors, and alternative encodings.
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" || r.URL.Fragment != "" || r.RequestURI != r.URL.Path || strings.ContainsAny(r.URL.Path, "%\\") {
		deny(w, 403)
		return
	}
	p := b.policy
	record, healthy := b.ownership.snapshot()
	if !healthy {
		deny(w, 503)
		return
	}
	if record.State == bound {
		p.ExpectedUID = record.UID
	}
	path := r.URL.Path
	read := r.Method == "GET" && path == p.resource()
	a2a := r.Method == "POST" && path == p.a2a()
	create := r.Method == "POST" && path == p.collection()
	patch := r.Method == "PATCH" && path == p.resource()
	del := r.Method == "DELETE" && path == p.resource()
	if !read && !a2a && !create && !patch && !del {
		deny(w, 403)
		return
	}
	// Count case-insensitively even for synthetic requests with noncanonical maps.
	var tokens []string
	for name, values := range r.Header {
		if strings.EqualFold(name, InnerTokenHeader) {
			tokens = append(tokens, values...)
		}
	}
	if len(tokens) != 1 || !tokenSafe(tokens[0]) {
		deny(w, 401)
		return
	}
	token := tokens[0]
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	id, err := b.reviewer.Review(ctx, token, p.Audience)
	if err != nil || id.Username != p.ServiceAccountUsername || id.UID != p.ServiceAccountUID {
		deny(w, 401)
		return
	}
	if r.Header.Get("Content-Encoding") != "" {
		deny(w, 400)
		return
	}
	var raw []byte
	if r.Body != nil {
		raw, err = boundedRead(r.Body, bodyLimit)
		if err != nil {
			deny(w, 400)
			return
		}
	}
	var request map[string]any
	if read {
		if len(raw) != 0 {
			deny(w, 400)
			return
		}
	} else {
		ct := "application/json"
		if patch {
			ct = "application/merge-patch+json"
		}
		if values := r.Header.Values("Content-Type"); len(values) != 1 || values[0] != ct {
			deny(w, 400)
			return
		}
		request, err = parseObject(raw)
		if err != nil {
			deny(w, 400)
			return
		}
	}
	if create || patch || del {
		valid := false
		if create {
			valid = p.agent(request, "create")
		}
		if patch {
			valid = p.agent(request, "patch")
		}
		if del {
			valid = p.deletion(request)
		}
		if !valid {
			deny(w, 400)
			return
		}
		if del {
			deny(w, 403)
			return
		}
		b.lifecycle(w, ctx, p, request, token, create)
		return
	}
	if record.State != bound && !(read && record.State == uninitialized) {
		deny(w, 403)
		return
	}

	if a2a {
		request, err = rpc(request)
		if err != nil {
			deny(w, 400)
			return
		}
		if status := b.authorizeRPC(request); status != 0 {
			deny(w, status)
			return
		}
	}

	upstream, err := b.kube.result(ctx, "GET", p.resource(), "", nil)
	if read && err == nil && upstream.Status == http.StatusNotFound {
		deny(w, 404)
		return
	}
	observed := upstream.Object
	if err == nil && upstream.Status != http.StatusOK {
		err = errDenied
	}
	if record.State != bound {
		deny(w, 403)
		return
	}
	recovery := a2a && request["method"] != "SendMessage"
	if err != nil || (!recovery && !p.agent(observed, "observed")) || (recovery && !p.recoveryAgent(observed)) {
		deny(w, 502)
		return
	}
	var response map[string]any
	if read {
		meta := object(observed["metadata"])
		response = map[string]any{"apiVersion": "api.kagent.dev/v1alpha3", "kind": "Agent", "metadata": map[string]any{
			"name": p.name(), "namespace": p.Namespace, "uid": p.ExpectedUID, "resourceVersion": str(meta["resourceVersion"]), "generation": meta["generation"], "labels": meta["labels"], "annotations": meta["annotations"],
		}, "spec": observed["spec"]}
		if !readiness(observed, response) {
			deny(w, 502)
			return
		}
	} else {
		if ctx.Err() != nil {
			deny(w, 504)
			return
		}
		if request["method"] == "SendMessage" {
			if status := b.beginSend(request); status != 0 {
				deny(w, status)
				return
			}
		}
		outbound := request
		if request["method"] == "SendMessage" {
			message := object(object(request["params"])["message"])
			if status := b.journal.reserve(ctx, str(message["messageId"])); status != 0 {
				b.abortSend(request)
				deny(w, status)
				return
			}
			if ctx.Err() != nil {
				b.abortSend(request)
				deny(w, 504)
				return
			}
			if message["contextId"] == nil {
				// alpha7 deduplicates session creation by message ID. A caller
				// must not select an old accessible session via that second key.
				msg := map[string]any{"messageId": uuid.NewString(), "role": message["role"], "parts": message["parts"]}
				outbound = map[string]any{"jsonrpc": request["jsonrpc"], "id": request["id"], "method": request["method"], "params": map[string]any{"message": msg, "configuration": object(request["params"])["configuration"]}}
			}
		}
		response, err = b.a2a.call(ctx, "POST", p.a2a(), "application/json", outbound)
		// Record the correlated identity before optional output validation or
		// credential filtering can fail. A failed send is never dispatched again.
		if request["method"] == "SendMessage" {
			if !b.finishSend(request, response) {
				deny(w, 502)
				return
			}
		}
		if err != nil || !validRPCResponse(response, request) || !b.boundResponse(request, response) {
			deny(w, 502)
			return
		}
	}
	// Decode before inspecting so JSON escapes cannot conceal credential bytes.
	secrets := []string{token, b.kube.credential, b.a2a.credential}
	if t, ok := b.reviewer.(*TokenReviewer); ok {
		secrets = append(secrets, t.endpoint.credential)
	}
	if reflects(response, secrets) {
		deny(w, 502)
		return
	}

	data, err := json.Marshal(response)
	if err != nil || len(data) > responseLimit {
		deny(w, 502)
		return
	}
	if a2a {
		b.releaseSend(request, response)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func reflects(v any, secrets []string) bool {
	switch x := v.(type) {
	case string:
		for _, s := range secrets {
			if s != "" && strings.Contains(x, s) {
				return true
			}
		}
	case map[string]any:
		for k, v := range x {
			if reflects(k, secrets) || reflects(v, secrets) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if reflects(v, secrets) {
				return true
			}
		}
	}
	return false
}
