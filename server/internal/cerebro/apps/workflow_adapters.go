package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/workflowexec"
)

const registryResponseLimit = 8 << 20

type workflowTokenSource struct {
	issuer   tokenIssuer
	identity tokens.Identity
}

func (s workflowTokenSource) Key(ctx context.Context) (string, error) {
	if s.issuer == nil {
		return "", errors.New("workflow token issuer is not configured")
	}
	// A durable run keeps the identity envelope, never a key. Force the broker
	// to exchange again at every Registry step so workers cannot retain a
	// long-lived credential between steps.
	s.issuer.Forget(s.identity)
	token, err := s.issuer.PersonalKey(ctx, s.identity)
	if err != nil {
		return "", err
	}
	return token.Key, nil
}

type registryAdapter struct {
	baseURL string
	traceID string
	client  *http.Client
}

func newRegistryAdapter(baseURL, traceID string, client *http.Client) *registryAdapter {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if !strings.HasSuffix(base, "/api/registry/v1") {
		base += "/api/registry/v1"
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &registryAdapter{baseURL: base, traceID: traceID, client: client}
}

func (a *registryAdapter) Execute(ctx context.Context, key string, call workflowexec.RegistryCall) (any, error) {
	if a == nil || a.baseURL == "/api/registry/v1" {
		return nil, errors.New("Registry workflow adapter is not configured")
	}
	if strings.TrimSpace(call.ResourceID) == "" {
		return nil, errors.New("Registry workflow step requires resource_id")
	}
	kind := "data-sources"
	if call.Kind == "write" {
		kind = "data-destinations"
	} else if call.Kind != "read" {
		return nil, fmt.Errorf("unsupported Registry operation %q", call.Kind)
	}
	body := make(map[string]any, len(call.Config)+1)
	for name, value := range call.Config {
		if name != "resource_id" {
			body[name] = value
		}
	}
	if call.Kind == "write" {
		body["input"] = call.Input
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode Registry request: %w", err)
	}
	endpoint := a.baseURL + "/" + kind + "/" + url.PathEscape(call.ResourceID) + "/execute"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build Registry request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Trace-ID", a.traceID)
	response, err := a.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Registry request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, registryResponseLimit))
	if err != nil {
		return nil, fmt.Errorf("read Registry response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Registry returned HTTP %d", response.StatusCode)
	}
	var output any
	if err := json.Unmarshal(responseBody, &output); err != nil {
		return nil, errors.New("Registry returned invalid JSON")
	}
	return output, nil
}
