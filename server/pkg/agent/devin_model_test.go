package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestDevinModelNamesAndAliases(t *testing.T) {
	const catalog = `{"families":[{"slug":"claude-opus-5","family_label":"Claude Opus 5","aliases":["opus"],"variants":[{"model_uid":"opus-high","label":"Opus High"},{"model_uid":"opus-low","label":"Opus Low"}]},{"slug":"swe-2","aliases":["swe"],"variants":[{"model_uid":"swe-2"}]}]}`
	for _, tc := range []struct {
		name, requested, current, options, catalog, want, set string
		wantError                                             bool
	}{
		{name: "canonical", requested: "opus-high", current: "opus-high", want: "opus-high"},
		{name: "default", current: "swe-2", want: "swe-2"},
		{name: "alias-resolved", requested: "opus", current: "opus-high", catalog: catalog, want: "opus-high"},
		{name: "slug-resolved", requested: "claude-opus-5", current: "opus-low", catalog: catalog, want: "opus-low"},
		{name: "family-name", requested: "Claude Opus 5", current: "opus-high", catalog: catalog, want: "opus-high"},
		{name: "session-name", requested: "Opus High", current: "opus-high", options: `[{"value":"opus-high","name":"Opus High"}]`, want: "opus-high"},
		{name: "session-name-normalized", requested: "OPUS-HIGH", current: "opus-high", options: `[{"value":"opus-high","name":"Opus High"}]`, want: "opus-high"},
		{name: "catalog-variant-name", requested: "Opus High", current: "opus-high", catalog: catalog, want: "opus-high"},
		{name: "resume-alias", requested: "opus", current: "swe-2", options: `[{"value":"opus-high","name":"Opus High"}]`, catalog: catalog, want: "opus-high", set: "opus-high"},
		{name: "unavailable-family-fallback", requested: "opus", current: "swe-2", catalog: catalog, wantError: true},
		{name: "ambiguous-variant", requested: "opus", current: "swe-2", options: `[{"value":"opus-high"},{"value":"opus-low"}]`, catalog: catalog, wantError: true},
		{name: "ambiguous-session-name", requested: "Opus", current: "opus-high", options: `[{"value":"opus-high","name":"Opus"},{"value":"opus-low","name":"Opus"}]`, wantError: true},
		{name: "ambiguous-family", requested: "opus", current: "opus-high", catalog: `{"families":[{"aliases":["opus"],"variants":[{"model_uid":"opus-high"}]},{"aliases":["opus"],"variants":[{"model_uid":"old-opus"}]}]}`, wantError: true},
		{name: "unknown-partial", requested: "op", current: "swe-2", catalog: catalog, wantError: true},
		{name: "punctuation-not-an-alias", requested: ".", current: "swe-2", catalog: catalog, wantError: true},
		{name: "opaque-id-not-guessed", requested: "MODEL_PRIVATE_2", current: "swe-2", catalog: catalog, wantError: true},
		{name: "bad-catalog", requested: "opus", current: "swe-2", catalog: `invalid`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := tc.options
			if options == "" {
				options = "[]"
			}
			raw := json.RawMessage(fmt.Sprintf(`{"configOptions":[{"id":"model","currentValue":%q,"options":%s}]}`, tc.current, options))
			set := ""
			actual, err := confirmDevinModel(context.Background(), func(_ context.Context, method string, params any) (json.RawMessage, error) {
				if method != "session/set_config_option" {
					t.Fatalf("unexpected RPC %s", method)
				}
				set = params.(map[string]any)["value"].(string)
				return json.RawMessage(fmt.Sprintf(`{"configOptions":[{"id":"model","currentValue":%q}]}`, set)), nil
			}, "session", tc.requested, raw, func(context.Context) ([]byte, error) {
				if tc.catalog == "" {
					t.Fatal("exact session choice unexpectedly required a catalog process")
				}
				return []byte(tc.catalog), nil
			})
			if (err != nil) != tc.wantError || actual != tc.want || set != tc.set {
				t.Fatalf("actual=%q set=%q err=%v; want model=%q set=%q error=%v", actual, set, err, tc.want, tc.set, tc.wantError)
			}
		})
	}
}
