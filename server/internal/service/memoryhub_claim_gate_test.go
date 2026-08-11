package service

import "testing"

func TestClaimGateReady(t *testing.T) {
	out := EvaluateMemoryHubClaimGate(ClaimGateInput{
		MemoryPolicy:          PolicyRequired,
		BindingsResolved:      true,
		CredentialResolved:    true,
		DocketResolved:        true,
		AttachmentResolved:    true,
		ControlPlaneHealthy:   true,
		ProviderNeedsCredential: true,
		DaemonSupportsClaimV1: true,
	})
	if out.State != GateReady {
		t.Fatalf("state = %s, want ready", out.State)
	}
}

func TestClaimGateRequiredFailures(t *testing.T) {
	cases := []struct {
		name string
		in   ClaimGateInput
		want string
	}{
		{
			name: "binding",
			in: ClaimGateInput{
				MemoryPolicy: PolicyRequired, BindingsResolved: false,
				DaemonSupportsClaimV1: true,
			},
			want: ErrCodeBindingRequiredUnavailable,
		},
		{
			name: "credential",
			in: ClaimGateInput{
				MemoryPolicy: PolicyRequired, BindingsResolved: true,
				ProviderNeedsCredential: true, CredentialResolved: false,
				DaemonSupportsClaimV1: true,
			},
			want: ErrCodeCredentialRequiredUnavailable,
		},
		{
			name: "docket required",
			in: ClaimGateInput{
				MemoryPolicy: PolicyRequired, BindingsResolved: true,
				CredentialResolved: true, DocketResolved: false,
				DaemonSupportsClaimV1: true,
			},
			want: ErrCodeDocketRequiredUnavailable,
		},
		{
			name: "attachment required",
			in: ClaimGateInput{
				MemoryPolicy: PolicyRequired, BindingsResolved: true,
				CredentialResolved: true, DocketResolved: true,
				AttachmentResolved: false, DaemonSupportsClaimV1: true,
			},
			want: ErrCodeAttachmentRequiredUnavailable,
		},
		{
			name: "daemon capability",
			in: ClaimGateInput{
				MemoryPolicy: PolicyRequired, BindingsResolved: true,
				DaemonSupportsClaimV1: false,
			},
			want: "memoryhub_daemon_capability_required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := EvaluateMemoryHubClaimGate(tc.in)
			if out.State != GateBlockedRequired {
				t.Fatalf("state = %s, want blocked_required", out.State)
			}
			if out.Failure == nil || out.Failure.ErrorCode != tc.want {
				t.Fatalf("error code = %v, want %s", out.Failure, tc.want)
			}
		})
	}
}

func TestClaimGateOptionalDegraded(t *testing.T) {
	out := EvaluateMemoryHubClaimGate(ClaimGateInput{
		MemoryPolicy:          PolicyOptional,
		BindingsResolved:      true,
		CredentialResolved:    true,
		DocketResolved:        false, // optional docket may degrade
		AttachmentResolved:    false,
		ControlPlaneHealthy:   true,
		DaemonSupportsClaimV1: true,
	})
	if out.State != GateDegraded {
		t.Fatalf("state = %s, want degraded", out.State)
	}
	if out.Failure == nil || out.Failure.ErrorCode != ErrCodeOptionalDegraded {
		t.Fatalf("failure = %v, want optional_degraded", out.Failure)
	}
}

func TestClaimGateOptionalDoesNotMaskBinding(t *testing.T) {
	// Optional policy does NOT make a missing binding degradable.
	out := EvaluateMemoryHubClaimGate(ClaimGateInput{
		MemoryPolicy:          PolicyOptional,
		BindingsResolved:      false,
		DaemonSupportsClaimV1: true,
	})
	if out.State != GateBlockedRequired || out.Failure.ErrorCode != ErrCodeBindingRequiredUnavailable {
		t.Fatalf("optional must not mask required binding: %s %v", out.State, out.Failure)
	}
}

func TestClaimGateControlPlane(t *testing.T) {
	out := EvaluateMemoryHubClaimGate(ClaimGateInput{
		MemoryPolicy:          PolicyRequired,
		BindingsResolved:      true,
		CredentialResolved:    true,
		DocketResolved:        true,
		AttachmentResolved:    true,
		ControlPlaneHealthy:   false,
		DaemonSupportsClaimV1: true,
	})
	if out.State != GateBlockedControlPlane {
		t.Fatalf("state = %s, want blocked_control_plane", out.State)
	}
}
