package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Registry hosts are operator-controlled, exact host[:port] entries, never URL prefixes.
func registryHostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, entry := range strings.Split(os.Getenv("MULTICA_SKILL_REGISTRY_HOSTS"), ",") {
		candidate := strings.ToLower(strings.TrimSpace(entry))
		if candidate != "" && candidate == host {
			return true
		}
	}
	return false
}

// Confine discovery and archive redirects to the configured registry origin.
func registryHTTPClient(client *http.Client, origin *url.URL) *http.Client {
	copy := *client
	previous := client.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != origin.Scheme || !strings.EqualFold(req.URL.Host, origin.Host) {
			return fmt.Errorf("registry redirect leaves configured origin")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many registry redirects")
		}
		return nil
	}
	return &copy
}

func registryAPIBase(ctx context.Context, client *http.Client, origin *url.URL) string {
	base := origin.Scheme + "://" + origin.Host
	fallback := base + "/api/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/.well-known/clawhub.json", nil)
	if err != nil {
		return fallback
	}
	resp, err := client.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback
	}
	var config struct {
		APIBase string `json:"apiBase"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&config); err != nil {
		return fallback
	}
	reference, err := url.Parse(config.APIBase)
	if err != nil || config.APIBase == "" || reference.IsAbs() || reference.Host != "" || strings.HasPrefix(config.APIBase, "//") {
		return fallback
	}
	root, _ := url.Parse(base + "/")
	resolved := root.ResolveReference(reference)
	if resolved.Scheme != origin.Scheme || resolved.Host != origin.Host || resolved.RawQuery != "" || resolved.Fragment != "" {
		return fallback
	}
	return strings.TrimRight(resolved.String(), "/")
}

func fetchRegistryArchive(ctx context.Context, client *http.Client, apiBase, slug string) (*importedSkill, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/download?slug="+url.QueryEscape(slug), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry download: HTTP %d", resp.StatusCode)
	}
	// The compressed transfer is bounded too; the archive parser enforces expanded limits.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImportTotalSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImportTotalSize {
		return nil, fmt.Errorf("%w: registry archive exceeds %d byte limit", errImportCapExceeded, maxImportTotalSize)
	}
	return parseSkillArchive(data, slug+".zip")
}
