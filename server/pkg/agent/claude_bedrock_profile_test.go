package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

const (
	testOpusProfileARN   = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc123opus"
	testSonnetProfileARN = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/def456sonnet"
	testHaikuProfileARN  = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/ghi789haiku"
)

func TestIsBedrockApplicationInferenceProfileARN(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		testOpusProfileARN: true,
		"arn:aws-us-gov:bedrock:us-gov-west-1:123456789012:application-inference-profile/x1": true,
		"ARN:AWS:BEDROCK:US-EAST-1:123456789012:APPLICATION-INFERENCE-PROFILE/ABC":           true,
		// System-defined profiles and foundation models name the model already,
		// so the pricing table resolves them without help.
		"arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-opus-5": false,
		"arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-opus-5":                 false,
		"us.anthropic.claude-sonnet-5": false,
		"claude-opus-5":                false,
		"":                             false,
	}
	for model, want := range cases {
		if got := isBedrockApplicationInferenceProfileARN(model); got != want {
			t.Errorf("isBedrockApplicationInferenceProfileARN(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestResolveBedrockProfileUsage(t *testing.T) {
	t.Parallel()
	opus := TokenUsage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 40}
	sonnet := TokenUsage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}
	haiku := TokenUsage{InputTokens: 2, OutputTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 5}
	served := func(models ...string) map[string]struct{} {
		set := make(map[string]struct{}, len(models))
		for _, m := range models {
			set[m] = struct{}{}
		}
		return set
	}
	cases := []struct {
		name   string
		usage  map[string]TokenUsage
		served map[string]struct{}
		want   map[string]TokenUsage
	}{
		{
			name:   "single_profile_single_served_model",
			usage:  map[string]TokenUsage{testOpusProfileARN: opus},
			served: served("claude-opus-5"),
			want:   map[string]TokenUsage{"claude-opus-5": opus},
		},
		{
			name:   "synthetic_and_profile_ids_are_not_served_models",
			usage:  map[string]TokenUsage{testOpusProfileARN: opus},
			served: served("claude-opus-5", "<synthetic>", testOpusProfileARN),
			want:   map[string]TokenUsage{"claude-opus-5": opus},
		},
		{
			name:   "existing_served_key_makes_profile_ambiguous",
			usage:  map[string]TokenUsage{testOpusProfileARN: opus, "claude-opus-5": sonnet},
			served: served("claude-opus-5"),
			want:   map[string]TokenUsage{testOpusProfileARN: opus, "claude-opus-5": sonnet},
		},
		{
			name:   "direct_bedrock_opus_with_haiku_background_profile",
			usage:  map[string]TokenUsage{"us.anthropic.claude-opus-5": opus, testHaikuProfileARN: haiku},
			served: served("claude-opus-5"),
			want:   map[string]TokenUsage{"us.anthropic.claude-opus-5": opus, testHaikuProfileARN: haiku},
		},
		{
			name:   "versioned_bedrock_id_already_accounts_for_served_model",
			usage:  map[string]TokenUsage{"us.anthropic.claude-sonnet-4-5-20250929-v1:0": sonnet, testHaikuProfileARN: haiku},
			served: served("claude-sonnet-4-5-20250929"),
			want:   map[string]TokenUsage{"us.anthropic.claude-sonnet-4-5-20250929-v1:0": sonnet, testHaikuProfileARN: haiku},
		},
		{
			name:   "different_named_usage_key_does_not_account_for_served_model",
			usage:  map[string]TokenUsage{"us.anthropic.claude-sonnet-5": sonnet, testOpusProfileARN: opus},
			served: served("claude-opus-5"),
			want:   map[string]TokenUsage{"us.anthropic.claude-sonnet-5": sonnet, "claude-opus-5": opus},
		},
		{
			name:   "two_served_models_are_ambiguous",
			usage:  map[string]TokenUsage{testOpusProfileARN: opus},
			served: served("claude-opus-5", "claude-sonnet-5"),
			want:   map[string]TokenUsage{testOpusProfileARN: opus},
		},
		{
			name:   "two_profiles_are_ambiguous",
			usage:  map[string]TokenUsage{testOpusProfileARN: opus, testSonnetProfileARN: sonnet},
			served: served("claude-opus-5"),
			want:   map[string]TokenUsage{testOpusProfileARN: opus, testSonnetProfileARN: sonnet},
		},
		{
			name:   "no_served_model_keeps_profile",
			usage:  map[string]TokenUsage{testOpusProfileARN: opus},
			served: served(),
			want:   map[string]TokenUsage{testOpusProfileARN: opus},
		},
		{
			name:   "plain_model_ids_are_untouched",
			usage:  map[string]TokenUsage{"claude-sonnet-5": sonnet},
			served: served("claude-opus-5"),
			want:   map[string]TokenUsage{"claude-sonnet-5": sonnet},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			before := make(map[string]TokenUsage, len(tt.usage))
			for k, v := range tt.usage {
				before[k] = v
			}
			got := resolveBedrockProfileUsage(tt.usage, tt.served)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("usage = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.usage, before) {
				t.Fatalf("input map was mutated: %#v, want %#v", tt.usage, before)
			}
		})
	}
}

// End to end through the Claude backend, using the stream shape Claude Code
// emits when ANTHROPIC_MODEL is a Bedrock application inference profile ARN:
// the init event and the result's modelUsage key carry the ARN, while each
// assistant message carries the model Bedrock actually served.
func TestClaudeExecuteResolvesBedrockApplicationInferenceProfile(t *testing.T) {
	t.Parallel()
	assistant := func(id, model, parent string) json.RawMessage {
		return mustMarshal(t, map[string]any{
			"type": "assistant", "parent_tool_use_id": parent,
			"message": map[string]any{"id": id, "role": "assistant", "model": model,
				"usage": &claudeUsage{InputTokens: 10, OutputTokens: 1},
				"content": []any{
					map[string]any{"type": "text", "text": "ok"},
					map[string]any{"type": "tool_use", "id": "tool_" + id, "name": "Read", "input": map[string]string{"file_path": "fixture.txt"}},
				}},
		})
	}
	init := mustMarshal(t, map[string]any{"type": "system", "subtype": "init", "session_id": "sess-profile", "model": testOpusProfileARN})
	result := mustMarshal(t, map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "result": "ok", "session_id": "sess-profile",
		"modelUsage": map[string]claudeResultModelUsage{
			testOpusProfileARN: {InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 100, CacheCreationInputTokens: 50},
		},
	})
	events := []json.RawMessage{init, assistant("msg_main", "claude-opus-5", ""), assistant("msg_sub", "claude-opus-5", "tool_1")}
	backend := claudeUsageFixtureBackend(t, events, result, false)
	// The harness counts forwarded text and tool blocks: one of each per assistant event.
	got := executeClaudeUsageFixture(t, backend, len(events)-1)
	if got.Status != "completed" {
		t.Fatalf("status = %q, want completed: %s", got.Status, got.Error)
	}
	want := map[string]TokenUsage{"claude-opus-5": {InputTokens: 10, OutputTokens: 5, CacheReadTokens: 100, CacheWriteTokens: 50}}
	if !reflect.DeepEqual(got.Usage, want) {
		t.Fatalf("usage = %#v, want %#v", got.Usage, want)
	}
}

func TestClaudeBedrockProfileIgnoresReplayedCommandModel(t *testing.T) {
	t.Parallel()
	messages := make(chan Message, 8)
	turn := &claudeTurn{
		backend:      &claudeBackend{},
		commandUUID:  "current-prompt",
		msgCh:        messages,
		coalescer:    newClaudeStreamCoalescer(messages, nil),
		servedModels: make(map[string]struct{}),
	}
	turn.handleMessage(claudeSDKMessage{Type: "assistant", Message: json.RawMessage(`{"model":"claude-opus-5"}`)})
	turn.handleMessage(claudeSDKMessage{Type: "command_lifecycle", CommandUUID: "replayed-prompt", State: "started"})
	turn.handleMessage(claudeSDKMessage{Type: "assistant", Message: json.RawMessage(`{"model":"claude-opus-5"}`)})
	turn.handleMessage(claudeSDKMessage{Type: "command_lifecycle", CommandUUID: turn.commandUUID, State: "started"})

	usage := map[string]TokenUsage{testSonnetProfileARN: {InputTokens: 12}}
	if got := resolveBedrockProfileUsage(usage, turn.servedModels); !reflect.DeepEqual(got, usage) {
		t.Fatalf("usage with only a replayed model = %#v, want %#v", got, usage)
	}

	turn.handleMessage(claudeSDKMessage{Type: "assistant", Message: json.RawMessage(`{"model":"claude-sonnet-5"}`)})
	want := map[string]TokenUsage{"claude-sonnet-5": {InputTokens: 12}}
	if got := resolveBedrockProfileUsage(usage, turn.servedModels); !reflect.DeepEqual(got, want) {
		t.Fatalf("usage with the current command's model = %#v, want %#v", got, want)
	}
}
