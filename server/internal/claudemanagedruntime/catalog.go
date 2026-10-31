package claudemanagedruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CatalogItem is a Claude agent or environment as Multica shows it: only its
// identity, never its configuration, which may contain secrets.
type CatalogItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const maxCatalogPages = 10

// ListCatalog lists the non-archived agents or environments the key can see.
func ListCatalog(ctx context.Context, baseURL, token, kind string) ([]CatalogItem, error) {
	prefix := map[string]string{"agents": "agent_", "environments": "env_"}[kind]
	if prefix == "" {
		return nil, fmt.Errorf("unknown Claude catalog %q", kind)
	}
	a, err := newAnthropicAPI(baseURL, token)
	if err != nil {
		return nil, err
	}
	items := []CatalogItem{}
	seen := map[string]bool{}
	page := ""
	for range maxCatalogPages {
		q := url.Values{"limit": {"100"}}
		if page != "" {
			q.Set("page", page)
		}
		var res struct {
			Data []struct {
				ID         string  `json:"id"`
				Name       string  `json:"name"`
				ArchivedAt *string `json:"archived_at"`
			} `json:"data"`
			NextPage string `json:"next_page"`
		}
		if err := a.call(ctx, http.MethodGet, "/"+kind+"?"+q.Encode(), nil, &res); err != nil {
			return nil, err
		}
		if res.Data == nil {
			return nil, fmt.Errorf("invalid Claude catalog response")
		}
		for _, item := range res.Data {
			if !strings.HasPrefix(item.ID, prefix) {
				return nil, fmt.Errorf("invalid Claude catalog identity")
			}
			if item.ArchivedAt != nil || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = item.ID
			}
			items = append(items, CatalogItem{ID: item.ID, Name: name})
		}
		if res.NextPage == "" {
			return items, nil
		}
		if res.NextPage == page {
			return nil, fmt.Errorf("Claude pagination cursor did not advance")
		}
		page = res.NextPage
	}
	return nil, fmt.Errorf("Claude catalog exceeds %d pages", maxCatalogPages)
}

// CheckAccess confirms the key can read the environment and every vault.
func CheckAccess(ctx context.Context, baseURL, token, environmentID string, vaultIDs []string) error {
	a, err := newAnthropicAPI(baseURL, token)
	if err != nil {
		return err
	}
	resources := map[string]string{"/environments/" + url.PathEscape(environmentID): environmentID}
	for _, id := range vaultIDs {
		if !validVaultID.MatchString(id) {
			return fmt.Errorf("vault IDs must look like vlt_...")
		}
		resources["/vaults/"+url.PathEscape(id)] = id
	}
	for path, want := range resources {
		var res struct {
			ID string `json:"id"`
		}
		if err := a.call(ctx, http.MethodGet, path, nil, &res); err != nil {
			return fmt.Errorf("Claude access check for %s: %w", want, err)
		}
		if res.ID != want {
			return fmt.Errorf("Claude access check for %s: unexpected resource identity", want)
		}
	}
	return nil
}
