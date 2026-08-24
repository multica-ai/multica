package featureflags

import (
	"context"
	"testing"
)

func TestResourceLabelsCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[resourceLabelsCompat] {
		t.Fatal("resource labels must stay enabled for installed clients")
	}
}

// MUL-5345: hang stack capture is gone from this build, but v0.4.13–v0.4.18 are
// installed and still hold a debugger channel open on every renderer whenever
// this key arrives as `true`. Those clients are fail-closed on absence, so NOT
// publishing the key is what disarms them — re-adding it would put a flag flip
// back within reach of a fleet that can no longer produce a usable stack.
func TestDesktopHangStackCaptureIsNotPublished(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if _, published := flags["desktop_hang_stack_capture"]; published {
		t.Fatal("hang stack capture must stay unpublished so installed clients keep their debugger channels closed")
	}
}

func TestAgentBuilderCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentBuilderCompat] {
		t.Fatal("agent builder must stay enabled for installed clients")
	}
}

func TestAgentSkillTogglesCompatDecisionStaysEnabled(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if !flags[agentSkillTogglesCompat] {
		t.Fatal("agent skill toggles must stay enabled for installed v0.4.0 clients")
	}
}

// MUL-6643: the server-side rollout gate on creating a custom status is gone,
// but the key stays unpublished on purpose. v0.4.30 shipped the feature without
// the four fixes that landed in v0.4.31 — custom-status cards render in the
// wrong board column (MUL-6409), the timeline glyph loses the status identity
// (MUL-6413), built-in colors are wrong (MUL-6440), and the catalog does not
// sync over the realtime channel (MUL-6458). Those clients gate their "New
// status" button on this key and fail closed on absence, so NOT publishing it
// is what keeps a client that cannot render the result from producing one.
//
// Clients from v0.4.33 read no flag at all, so they get the button the moment
// they update — the key never has to be published again.
func TestCustomIssueStatusesIsNotPublished(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if _, published := flags["custom_issue_statuses"]; published {
		t.Fatal("custom_issue_statuses must stay unpublished so pre-v0.4.33 clients keep creation hidden")
	}
}

func TestPluginsV1DefaultsOff(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	if flags[PluginsV1] {
		t.Fatal("plugins_v1 must stay disabled unless explicitly enabled")
	}
}

func TestPluginSubFlagsAreNotPublished(t *testing.T) {
	flags := EvaluateFrontendPublicFlags(context.Background(), nil)
	for _, retired := range []string{"private_plugins_v1", "remote_mcp_plugins_v1"} {
		if _, published := flags[retired]; published {
			t.Fatalf("retired Plugin sub-flag %q must not be published", retired)
		}
	}
}
