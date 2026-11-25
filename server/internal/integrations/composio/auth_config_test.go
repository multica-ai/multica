package composio

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdk "github.com/multica-ai/multica/server/pkg/composio"
)

// catalogFakeSDK serves a toolkit catalog and records the auth configs
// ensureAuthConfig creates, which then show up in ListAuthConfigs.
type catalogFakeSDK struct {
	fakeSDK
	created []string
	custom  map[string]string
	// projectFields names, per toolkit, the scheme whose auth config needs a
	// project-level field (GET /toolkits/{slug} only).
	projectFields map[string]string
}

// GetToolkit answers like GET /toolkits/{slug}: schemes come only as
// auth_config_details, without auth_schemes or no_auth, and categories as
// {name, slug} rather than the list's {id, name}.
func (f *catalogFakeSDK) GetToolkit(_ context.Context, slug string) (*sdk.Toolkit, error) {
	for _, listed := range f.toolkits {
		if listed.Slug != slug {
			continue
		}
		var meta map[string]any
		if listedCategories, ok := listed.Meta["categories"].([]any); ok {
			categories := make([]any, 0, len(listedCategories))
			for _, c := range listedCategories {
				category, _ := c.(map[string]any)
				categories = append(categories, map[string]any{"name": category["name"], "slug": category["id"]})
			}
			meta = map[string]any{"categories": categories}
		}
		tk := sdk.Toolkit{Slug: listed.Slug, Name: listed.Name, Meta: meta, ComposioManagedAuthSchemes: listed.ComposioManagedAuthSchemes}
		for _, scheme := range listed.AuthSchemes {
			detail := sdk.ToolkitAuthConfigDetail{Mode: strings.ToUpper(scheme)}
			if strings.EqualFold(f.projectFields[slug], scheme) {
				detail.Fields.AuthConfigCreation.Required = []sdk.ToolkitAuthField{{Name: "subdomain"}}
			}
			tk.AuthConfigDetails = append(tk.AuthConfigDetails, detail)
		}
		return matchSpec(&f.fakeSDK, "GET /toolkits/{slug}", &tk), nil
	}
	return nil, &sdk.APIError{HTTPStatus: 404}
}

func (f *catalogFakeSDK) ListTools(context.Context, sdk.ListToolsRequest) (*sdk.ListToolsResponse, error) {
	return &sdk.ListToolsResponse{}, nil
}

func (f *catalogFakeSDK) GetTool(context.Context, string) (*sdk.Tool, error) {
	return nil, errors.New("no tools")
}

func (f *catalogFakeSDK) ExecuteTool(context.Context, string, sdk.ExecuteToolRequest) (*sdk.ExecuteToolResponse, error) {
	return nil, errors.New("no tools")
}

func (f *catalogFakeSDK) CreateManagedAuthConfig(_ context.Context, slug string) (string, error) {
	id := "ac_managed_" + slug
	f.created = append(f.created, id)
	f.authConfigs = append(f.authConfigs, sdk.AuthConfig{ID: id, Toolkit: sdk.Toolkit{Slug: slug}, AuthScheme: "OAUTH2", IsComposioManaged: true, Status: "ENABLED"})
	return id, nil
}

// keyCreatingSDK adds custom auth configs to catalogFakeSDK.
type keyCreatingSDK struct{ *catalogFakeSDK }

func (f keyCreatingSDK) CreateCustomAuthConfig(_ context.Context, slug, scheme, name string) (string, error) {
	id := "ac_" + scheme + "_" + slug
	f.created = append(f.created, id)
	if f.custom == nil {
		f.custom = map[string]string{}
	}
	f.custom[slug] = name
	f.authConfigs = append(f.authConfigs, sdk.AuthConfig{ID: id, Toolkit: sdk.Toolkit{Slug: slug}, AuthScheme: scheme, Status: "ENABLED"})
	return id, nil
}

func newCatalogFake() *catalogFakeSDK {
	return &catalogFakeSDK{fakeSDK: fakeSDK{
		authConfigsSet: true,
		authConfigs: []sdk.AuthConfig{
			{ID: "ac_slack", Toolkit: sdk.Toolkit{Slug: "slack"}, AuthScheme: "OAUTH2", Status: "ENABLED", IsComposioManaged: true},
			{ID: "ac_sendgrid", Toolkit: sdk.Toolkit{Slug: "sendgrid"}, AuthScheme: "API_KEY", Status: "ENABLED"},
		},
		toolkits: []sdk.Toolkit{
			{Slug: "slack", Name: "Slack", AuthSchemes: []string{"oauth2"}},
			{Slug: "sendgrid", Name: "SendGrid", AuthSchemes: []string{"api_key"}},
			{Slug: "github", Name: "GitHub", AuthSchemes: []string{"oauth2", "api_key"}, ComposioManagedAuthSchemes: []string{"oauth2"},
				Meta: map[string]any{"categories": []any{map[string]any{"id": "developer-tools", "name": "Developer Tools"}}}},
			{Slug: "tavily", Name: "Tavily", AuthSchemes: []string{"api_key"},
				Meta: map[string]any{"categories": []any{map[string]any{"id": "ai-machine-learning", "name": "AI & Machine Learning"}}}},
			{Slug: "pagerduty", Name: "PagerDuty", AuthSchemes: []string{"OAUTH2", "BEARER_TOKEN"}},
			{Slug: "salesforce", Name: "Salesforce", AuthSchemes: []string{"oauth2"}},
			{Slug: "hackernews", Name: "Hacker News", NoAuth: true, AuthSchemes: []string{"no_auth"}},
			{Slug: "zendesk", Name: "Zendesk", AuthSchemes: []string{"api_key", "basic"}},
			{Slug: "freshdesk", Name: "Freshdesk", AuthSchemes: []string{"api_key"}},
		},
	}, projectFields: map[string]string{"zendesk": "api_key", "freshdesk": "api_key"}}
}

func TestListToolkits_SurfacesKeyToolkits(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, keyCreatingSDK{newCatalogFake()}, newFakeStore())
	tks, err := svc.ListToolkits(context.Background())
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	got := map[string]ToolkitView{}
	order := []string{}
	for _, tk := range tks {
		got[tk.Slug] = tk
		order = append(order, tk.Slug)
	}
	// salesforce needs our own OAuth app and hackernews needs no account:
	// neither can be connected, so neither is listed.
	want := map[string]string{
		"slack":     ConnectOAuth,
		"sendgrid":  ConnectKey,
		"github":    ConnectOAuth,
		"tavily":    ConnectKey,
		"pagerduty": ConnectKey,
		"zendesk":   ConnectKey,
		"freshdesk": ConnectKey,
	}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %d toolkits", order, len(want))
	}
	for slug, method := range want {
		if got[slug].ConnectMethod != method {
			t.Errorf("%s connect method = %q, want %q", slug, got[slug].ConnectMethod, method)
		}
	}
	if tv := got["tavily"]; tv.Category != "AI & Machine Learning" || tv.CategoryID != "ai-machine-learning" {
		t.Errorf("tavily category = %q / %q", tv.Category, tv.CategoryID)
	}
}

func TestListToolkits_KeyToolkitsNeedCustomAuthSupport(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newCatalogFake(), newFakeStore())
	tks, err := svc.ListToolkits(context.Background())
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	for _, tk := range tks {
		if tk.Slug == "tavily" || tk.Slug == "pagerduty" {
			t.Errorf("%s listed although no key config can be created for it", tk.Slug)
		}
	}
}

func TestEnsureAuthConfig_CreatesPerToolkitKind(t *testing.T) {
	t.Parallel()
	fake := newCatalogFake()
	svc := newTestService(t, keyCreatingSDK{fake}, newFakeStore())
	ctx := context.Background()

	cases := []struct {
		slug string
		want string
	}{
		{"sendgrid", "ac_sendgrid"},
		{"github", "ac_managed_github"},
		{"tavily", "ac_API_KEY_tavily"},
		{"pagerduty", "ac_BEARER_TOKEN_pagerduty"},
		// Its API_KEY config would need a project-level field.
		{"zendesk", "ac_BASIC_zendesk"},
	}
	for _, tc := range cases {
		id, err := svc.ensureAuthConfig(ctx, tc.slug)
		if err != nil || id != tc.want {
			t.Errorf("ensureAuthConfig(%s) = %q, %v; want %q", tc.slug, id, err, tc.want)
		}
	}
	for _, slug := range []string{"salesforce", "hackernews", "freshdesk", "unknown"} {
		if _, err := svc.ensureAuthConfig(ctx, slug); !errors.Is(err, ErrToolkitNotSupported) {
			t.Errorf("ensureAuthConfig(%s) err = %v, want ErrToolkitNotSupported", slug, err)
		}
	}
	if fake.custom["tavily"] != "Tavily" {
		t.Errorf("custom config name = %q, want the toolkit name", fake.custom["tavily"])
	}

	// A second connect reuses the config the first one created.
	fake.created = nil
	if id, err := svc.ensureAuthConfig(ctx, "tavily"); err != nil || id != "ac_API_KEY_tavily" || len(fake.created) != 0 {
		t.Errorf("second ensureAuthConfig = %q, %v; created %v", id, err, fake.created)
	}
	tks, err := svc.ListToolkits(ctx)
	if err != nil {
		t.Fatalf("ListToolkits: %v", err)
	}
	for _, tk := range tks {
		if tk.Slug == "tavily" && tk.ConnectMethod != ConnectKey {
			t.Errorf("tavily connect method after its config exists = %q", tk.ConnectMethod)
		}
	}
}

func TestEnsureAuthConfig_KeyToolkitWithoutCustomAuthSupport(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newCatalogFake(), newFakeStore())
	if _, err := svc.ensureAuthConfig(context.Background(), "tavily"); !errors.Is(err, ErrToolkitNotSupported) {
		t.Fatalf("err = %v, want ErrToolkitNotSupported", err)
	}
}
