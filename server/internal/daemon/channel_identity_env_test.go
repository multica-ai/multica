package daemon

import (
	"io"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestChannelIdentityEnvironment(t *testing.T) {
	t.Run("Feishu adds generic metadata and Lark alias", func(t *testing.T) {
		task := Task{
			ChatSessionID:   "chat-1",
			ChatChannelType: execenv.ChannelTypeFeishu,
			ChannelIdentity: &ChannelIdentityData{
				ChannelType:    execenv.ChannelTypeFeishu,
				InstallationID: "11111111-1111-1111-1111-111111111111",
				ChannelUserID:  "ou_sender",
			},
		}
		got, reason := channelIdentityEnvironment(task)
		if reason != "" {
			t.Fatalf("channelIdentityEnvironment reason = %q", reason)
		}
		want := map[string]string{
			channelTypeEnv:           execenv.ChannelTypeFeishu,
			channelInstallationIDEnv: "11111111-1111-1111-1111-111111111111",
			channelUserIDEnv:         "ou_sender",
			larkOpenIDEnv:            "ou_sender",
		}
		if len(got) != len(want) {
			t.Fatalf("environment = %#v, want %#v", got, want)
		}
		for key, value := range want {
			if got[key] != value {
				t.Errorf("%s = %q, want %q", key, got[key], value)
			}
		}
	})

	t.Run("Slack gets generic metadata without Lark alias", func(t *testing.T) {
		task := Task{
			ChatSessionID:   "chat-2",
			ChatChannelType: execenv.ChannelTypeSlack,
			ChannelIdentity: &ChannelIdentityData{
				ChannelType:    execenv.ChannelTypeSlack,
				InstallationID: "22222222-2222-2222-2222-222222222222",
				ChannelUserID:  "U_SENDER",
			},
		}
		got, reason := channelIdentityEnvironment(task)
		if reason != "" {
			t.Fatalf("channelIdentityEnvironment reason = %q", reason)
		}
		if got[channelTypeEnv] != execenv.ChannelTypeSlack ||
			got[channelInstallationIDEnv] != "22222222-2222-2222-2222-222222222222" ||
			got[channelUserIDEnv] != "U_SENDER" {
			t.Fatalf("environment = %#v", got)
		}
		if _, ok := got[larkOpenIDEnv]; ok {
			t.Fatalf("%s must not be set for Slack: %#v", larkOpenIDEnv, got)
		}
	})

	t.Run("missing optional payload is backward compatible", func(t *testing.T) {
		got, reason := channelIdentityEnvironment(Task{ChatSessionID: "chat-3", ChatChannelType: execenv.ChannelTypeFeishu})
		if reason != "" || got != nil {
			t.Fatalf("environment = %#v reason = %q, want nil and empty reason", got, reason)
		}
	})
}

func TestChannelIdentityEnvironmentRejectsPartialOrInconsistentPayload(t *testing.T) {
	tests := []struct {
		name   string
		task   Task
		reason string
	}{
		{
			name: "not a channel chat",
			task: Task{
				ChatChannelType: execenv.ChannelTypeFeishu,
				ChannelIdentity: &ChannelIdentityData{
					ChannelType:    execenv.ChannelTypeFeishu,
					InstallationID: "installation",
					ChannelUserID:  "ou_sender",
				},
			},
			reason: "task_not_channel_chat",
		},
		{
			name: "incomplete",
			task: Task{
				ChatSessionID:   "chat",
				ChatChannelType: execenv.ChannelTypeFeishu,
				ChannelIdentity: &ChannelIdentityData{ChannelType: execenv.ChannelTypeFeishu},
			},
			reason: "identity_incomplete",
		},
		{
			name: "channel mismatch",
			task: Task{
				ChatSessionID:   "chat",
				ChatChannelType: execenv.ChannelTypeSlack,
				ChannelIdentity: &ChannelIdentityData{
					ChannelType:    execenv.ChannelTypeFeishu,
					InstallationID: "installation",
					ChannelUserID:  "ou_sender",
				},
			},
			reason: "channel_type_mismatch",
		},
		{
			name: "control character",
			task: Task{
				ChatSessionID:   "chat",
				ChatChannelType: execenv.ChannelTypeFeishu,
				ChannelIdentity: &ChannelIdentityData{
					ChannelType:    execenv.ChannelTypeFeishu,
					InstallationID: "installation",
					ChannelUserID:  "ou_sender\ninjected",
				},
			},
			reason: "identity_invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := channelIdentityEnvironment(tc.task)
			if got != nil || reason != tc.reason {
				t.Fatalf("environment = %#v reason = %q, want nil reason %q", got, reason, tc.reason)
			}
		})
	}
}

func TestChannelIdentityEnvironmentCannotBeOverriddenByCustomEnv(t *testing.T) {
	task := Task{
		ChatSessionID:   "chat",
		ChatChannelType: execenv.ChannelTypeFeishu,
		ChannelIdentity: &ChannelIdentityData{
			ChannelType:    execenv.ChannelTypeFeishu,
			InstallationID: "installation",
			ChannelUserID:  "ou_attested",
		},
	}
	agentEnv, reason := channelIdentityEnvironment(task)
	if reason != "" {
		t.Fatalf("channelIdentityEnvironment reason = %q", reason)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	layerCustomEnvAndHermesHome(agentEnv, map[string]string{
		channelTypeEnv:           "slack",
		channelInstallationIDEnv: "forged-installation",
		channelUserIDEnv:         "U_FORGED",
		larkOpenIDEnv:            "ou_forged",
	}, "", logger)

	if agentEnv[channelTypeEnv] != execenv.ChannelTypeFeishu ||
		agentEnv[channelInstallationIDEnv] != "installation" ||
		agentEnv[channelUserIDEnv] != "ou_attested" ||
		agentEnv[larkOpenIDEnv] != "ou_attested" {
		t.Fatalf("custom env overrode attested identity: %#v", agentEnv)
	}
}

func TestChannelIdentityEnvironmentIsVisibleToCodexShellPolicy(t *testing.T) {
	explicit := map[string]string{
		channelTypeEnv:           execenv.ChannelTypeFeishu,
		channelInstallationIDEnv: "installation",
		channelUserIDEnv:         "ou_sender",
		larkOpenIDEnv:            "ou_sender",
	}
	allowed := execenv.CodexShellEnvAllowlist(nil, explicit, nil)
	seen := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		seen[key] = true
	}
	for key := range explicit {
		if !seen[key] {
			t.Errorf("%s missing from Codex shell environment allowlist: %v", key, allowed)
		}
	}
}
