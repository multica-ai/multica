package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// BackendServiceURL is the local single-instance boundary. URL paths remain
// significant: two deployments mounted on one host must not share a service.
func BackendServiceURL(raw string) (string, error) {
	input, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	input.Scheme = strings.ToLower(input.Scheme)
	base, err := NormalizeServerBaseURL(input.String())
	if err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid backend URL")
	}
	u = (&url.URL{Scheme: u.Scheme, Host: u.Host}).ResolveReference(u)
	if u.Path == "/ws" {
		u.Path = ""
		u.RawPath = ""
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func BackendServiceKey(raw string) (string, error) {
	normalized, err := BackendServiceURL(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:]), nil
}

// ExecutionStateProfile survives a change of launcher or authentication
// profile. Existing callers without a binding retain the profile layout.
func (c Config) ExecutionStateProfile() string {
	if c.StateProfile != nil {
		return *c.StateProfile
	}
	return c.Profile
}

// An absent directory inside this state may be ordinary GC. A directory from
// another state is a profile/backend mismatch and must be visible to the user.
func validateBoundPriorState(root, prior string) error {
	canonicalRoot, err := util.ResolveSymlinksBestEffort(root)
	if err != nil {
		return err
	}
	canonicalPrior, err := util.ResolveSymlinksBestEffort(prior)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(canonicalRoot, canonicalPrior)
	if err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("previous workdir belongs to another execution state; restart the original state-owning profile or explicitly migrate it before continuing")
	}
	return nil
}
