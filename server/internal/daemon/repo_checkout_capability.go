package daemon

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

const repoCheckoutCapabilityHeader = "X-Multica-Repo-Checkout-Capability"

type repoCheckoutCapabilityParams struct {
	WorkspaceID         string
	TaskID              string
	WorkDir             string
	AgentName           string
	Repos               []RepoData
	CoAuthoredByEnabled bool
}

type repoCheckoutCapability struct {
	workspaceID         string
	taskID              string
	workDir             string
	agentName           string
	repoRefs            map[string]string
	claimedRepos        map[string]struct{}
	coAuthoredByEnabled bool
	activeRequests      int
	revoked             bool
	drained             chan struct{}
	claimRepo           func(string) bool
}

func (d *Daemon) registerRepoCheckoutCapability(params repoCheckoutCapabilityParams) (string, error) {
	params.WorkspaceID = strings.TrimSpace(params.WorkspaceID)
	params.TaskID = strings.TrimSpace(params.TaskID)
	params.WorkDir = strings.TrimSpace(params.WorkDir)
	params.AgentName = strings.TrimSpace(params.AgentName)
	if params.WorkspaceID == "" || params.TaskID == "" || params.WorkDir == "" || params.AgentName == "" {
		return "", fmt.Errorf("repo checkout capability requires workspace, task, workdir, and agent identity")
	}
	if !filepath.IsAbs(params.WorkDir) {
		return "", fmt.Errorf("repo checkout capability workdir must be absolute")
	}

	repoRefs := make(map[string]string, len(params.Repos))
	for _, repo := range params.Repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			return "", fmt.Errorf("repo checkout capability contains an empty repo URL")
		}
		ref := strings.TrimSpace(repo.Ref)
		if existing, ok := repoRefs[url]; ok && existing != ref {
			return "", fmt.Errorf("repo checkout capability contains conflicting refs for %s", url)
		}
		repoRefs[url] = ref
	}
	if len(repoRefs) == 0 {
		return "", fmt.Errorf("repo checkout capability requires at least one repo")
	}

	binding := &repoCheckoutCapability{
		workspaceID:         params.WorkspaceID,
		taskID:              params.TaskID,
		workDir:             filepath.Clean(params.WorkDir),
		agentName:           params.AgentName,
		repoRefs:            repoRefs,
		claimedRepos:        make(map[string]struct{}, len(repoRefs)),
		coAuthoredByEnabled: params.CoAuthoredByEnabled,
		drained:             make(chan struct{}),
	}
	for {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate repo checkout capability: %w", err)
		}
		token := base64.RawURLEncoding.EncodeToString(raw)

		d.repoCheckoutCapabilityMu.Lock()
		if d.repoCheckoutCapabilities == nil {
			d.repoCheckoutCapabilities = make(map[string]*repoCheckoutCapability)
		}
		if _, exists := d.repoCheckoutCapabilities[token]; !exists {
			d.repoCheckoutCapabilities[token] = binding
			d.repoCheckoutCapabilityMu.Unlock()
			return token, nil
		}
		d.repoCheckoutCapabilityMu.Unlock()
	}
}

func (d *Daemon) acquireRepoCheckoutCapability(token string) (repoCheckoutCapability, func(), bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return repoCheckoutCapability{}, nil, false
	}
	d.repoCheckoutCapabilityMu.Lock()
	entry, ok := d.repoCheckoutCapabilities[token]
	if !ok || entry.revoked {
		d.repoCheckoutCapabilityMu.Unlock()
		return repoCheckoutCapability{}, nil, false
	}
	entry.activeRequests++
	binding := *entry
	binding.claimRepo = func(repoURL string) bool {
		d.repoCheckoutCapabilityMu.Lock()
		defer d.repoCheckoutCapabilityMu.Unlock()
		if _, claimed := entry.claimedRepos[repoURL]; claimed {
			return false
		}
		entry.claimedRepos[repoURL] = struct{}{}
		return true
	}
	d.repoCheckoutCapabilityMu.Unlock()

	var released bool
	release := func() {
		d.repoCheckoutCapabilityMu.Lock()
		defer d.repoCheckoutCapabilityMu.Unlock()
		if released {
			return
		}
		released = true
		entry.activeRequests--
		if entry.revoked && entry.activeRequests == 0 {
			close(entry.drained)
		}
	}
	return binding, release, true
}

func (d *Daemon) revokeRepoCheckoutCapability(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	d.repoCheckoutCapabilityMu.Lock()
	entry, ok := d.repoCheckoutCapabilities[token]
	if !ok {
		d.repoCheckoutCapabilityMu.Unlock()
		return
	}
	delete(d.repoCheckoutCapabilities, token)
	entry.revoked = true
	if entry.activeRequests == 0 {
		close(entry.drained)
	}
	d.repoCheckoutCapabilityMu.Unlock()
	<-entry.drained
}
