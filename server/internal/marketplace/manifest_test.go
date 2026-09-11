package marketplace

import "testing"

func TestParseMarketplaceAcceptsClaudeManifest(t *testing.T) {
	manifest, err := ParseMarketplace([]byte(`{"name":"servinder-tools","owner":{"name":"Servinder Artes"},"plugins":[{"name":"quality-review","source":{"source":"github","repo":"example/quality-review","ref":"v1.2.3"},"description":"Quality tools"}]}`))
	if err != nil {
		t.Fatalf("ParseMarketplace() error = %v", err)
	}
	if manifest.Name != "servinder-tools" || len(manifest.Plugins) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Plugins[0].Source.Kind != SourceGitHub || manifest.Plugins[0].Source.Repository != "example/quality-review" {
		t.Fatalf("unexpected source: %+v", manifest.Plugins[0].Source)
	}
}

func TestParseMarketplaceRejectsUnsafeSourceAndMissingFields(t *testing.T) {
	cases := []string{`{"name":"x","owner":{"name":"x"},"plugins":[{"name":"x","source":"../outside"}]}`, `{"name":"x","owner":{"name":"x"},"plugins":[{"name":"x","source":{"source":"unknown","package":"x"}}]}`, `{"owner":{"name":"x"},"plugins":[]}`}
	for _, input := range cases {
		if _, err := ParseMarketplace([]byte(input)); err == nil {
			t.Fatalf("accepted invalid input: %s", input)
		}
	}
}

func TestParseMarketplaceRejectsAnthropicReservedName(t *testing.T) {
	_, err := ParseMarketplace([]byte(`{"name":"claude-plugins-official","owner":{"name":"x"},"plugins":[{"name":"x","source":"./x"}]}`))
	if err == nil {
		t.Fatal("reserved marketplace name was accepted")
	}
}
