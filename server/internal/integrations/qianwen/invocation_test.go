package qianwen

import (
	"errors"
	"testing"
	"time"
)

const (
	invocationTestToken     = "qws_MDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDA"
	invocationTestTimestamp = "1786723200000"
	invocationTestSignature = "4a6f71e9f58f9a470114ed1b182f7cb5f059c5ce4847a99bf77008c12c7bbfc5"
)

func invocationTestMetadata() InvocationMetadata {
	return InvocationMetadata{
		OpenUserID: "enc-user-Aa+/=",
		OpenUUID:   "enc-device-Zz+/=",
		Timestamp:  invocationTestTimestamp,
		Nonce:      "0123456789abcdef0123456789abcdef",
		Signature:  invocationTestSignature,
	}
}

func TestVerifyPairingRedeemSignatureGoldenVector(t *testing.T) {
	now := time.UnixMilli(1786723200000)
	metadata := invocationTestMetadata()

	if err := VerifyPairingRedeemSignature(
		invocationTestToken,
		"01234567",
		metadata,
		now,
	); err != nil {
		t.Fatalf("VerifyInvocationSignature() error = %v", err)
	}

	canonical, err := CanonicalPairingRedeemInvocation("01234567", metadata)
	if err != nil {
		t.Fatalf("CanonicalInvocation() error = %v", err)
	}
	want := "QIANWEN-HMAC-SHA256-V1\n" +
		"binding_redeem\n" +
		invocationTestTimestamp + "\n" +
		"0123456789abcdef0123456789abcdef\n" +
		"enc-user-Aa+/=\n" +
		"enc-device-Zz+/=\n" +
		"01234567"
	if canonical != want {
		t.Fatalf("canonical invocation = %q, want %q", canonical, want)
	}
}

func TestVerifyPairingRedeemSignatureRejectsTamperingAndStaleRequests(t *testing.T) {
	baseNow := time.UnixMilli(1786723200000)
	tests := []struct {
		name     string
		token    string
		code     string
		metadata InvocationMetadata
		now      time.Time
		want     error
	}{
		{name: "code tampered", token: invocationTestToken, code: "76543210", metadata: invocationTestMetadata(), now: baseNow, want: ErrInvalidInvocation},
		{name: "token tampered", token: invocationTestToken + "x", code: "01234567", metadata: invocationTestMetadata(), now: baseNow, want: ErrInvalidInvocation},
		{name: "stale", token: invocationTestToken, code: "01234567", metadata: invocationTestMetadata(), now: baseNow.Add(121 * time.Second), want: ErrStaleInvocation},
		{name: "too far in future", token: invocationTestToken, code: "01234567", metadata: invocationTestMetadata(), now: baseNow.Add(-121 * time.Second), want: ErrStaleInvocation},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyPairingRedeemSignature(tc.token, tc.code, tc.metadata, tc.now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("VerifyInvocationSignature() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifyPairingRedeemSignatureRejectsUnavailableIdentityAndMalformedMetadata(t *testing.T) {
	baseNow := time.UnixMilli(1786723200000)
	tests := []struct {
		name   string
		mutate func(*InvocationMetadata)
		want   error
	}{
		{name: "missing openUserId", mutate: func(m *InvocationMetadata) { m.OpenUserID = "" }, want: ErrIdentityUnavailable},
		{name: "missing openUuid", mutate: func(m *InvocationMetadata) { m.OpenUUID = "" }, want: ErrIdentityUnavailable},
		{name: "identity newline", mutate: func(m *InvocationMetadata) { m.OpenUserID += "\nforged" }, want: ErrInvalidInvocation},
		{name: "short nonce", mutate: func(m *InvocationMetadata) { m.Nonce = "short" }, want: ErrInvalidInvocation},
		{name: "invalid timestamp", mutate: func(m *InvocationMetadata) { m.Timestamp = "yesterday" }, want: ErrInvalidInvocation},
		{name: "invalid signature", mutate: func(m *InvocationMetadata) { m.Signature = "not-hex" }, want: ErrInvalidInvocation},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metadata := invocationTestMetadata()
			tc.mutate(&metadata)
			err := VerifyPairingRedeemSignature(invocationTestToken, "01234567", metadata, baseNow)
			if !errors.Is(err, tc.want) {
				t.Fatalf("VerifyInvocationSignature() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifySubmitInvocationSignatureGoldenVector(t *testing.T) {
	invocation := SubmitInvocation{
		Request: SubmitRequest{
			RequestID: "550E8400-E29B-41D4-A716-446655440000",
			Query:     "run tests",
		},
		Identity: invocationTestMetadata(),
	}
	invocation.Identity.Signature = "2b3342238b5d5c8db34bed0ba6d7e4eda2d38be4fb8075feae3808ef664c9138"

	if err := VerifySubmitInvocationSignature(invocationTestToken, invocation, time.UnixMilli(1786723200000)); err != nil {
		t.Fatalf("VerifySubmitInvocationSignature() error = %v", err)
	}
	canonical, err := CanonicalSubmitInvocation(invocation)
	if err != nil {
		t.Fatalf("CanonicalSubmitInvocation() error = %v", err)
	}
	want := "QIANWEN-HMAC-SHA256-V1\n" +
		"request_submit\n" +
		invocationTestTimestamp + "\n" +
		"0123456789abcdef0123456789abcdef\n" +
		"enc-user-Aa+/=\n" +
		"enc-device-Zz+/=\n" +
		"550e8400-e29b-41d4-a716-446655440000\n" +
		"c7b8e61142837b8ee5c2846f5c05c420dcbf72fff1b8d30dc20afcc518e8b4f5"
	if canonical != want {
		t.Fatalf("canonical submit invocation = %q, want %q", canonical, want)
	}
}

func TestVerifyStatusInvocationSignatureGoldenVector(t *testing.T) {
	invocation := StatusInvocation{
		RequestID: "550E8400-E29B-41D4-A716-446655440000",
		Identity:  invocationTestMetadata(),
	}
	invocation.Identity.Signature = "832d79ddc5d183f98431d10cab799d517fb3995be1dd4b80b6f0e602f22d54fc"

	if err := VerifyStatusInvocationSignature(invocationTestToken, invocation, time.UnixMilli(1786723200000)); err != nil {
		t.Fatalf("VerifyStatusInvocationSignature() error = %v", err)
	}
	canonical, err := CanonicalStatusInvocation(invocation)
	if err != nil {
		t.Fatalf("CanonicalStatusInvocation() error = %v", err)
	}
	want := "QIANWEN-HMAC-SHA256-V1\n" +
		"request_status\n" +
		invocationTestTimestamp + "\n" +
		"0123456789abcdef0123456789abcdef\n" +
		"enc-user-Aa+/=\n" +
		"enc-device-Zz+/=\n" +
		"550e8400-e29b-41d4-a716-446655440000"
	if canonical != want {
		t.Fatalf("canonical status invocation = %q, want %q", canonical, want)
	}
}

func TestVerifyTaskListInvocationSignatureGoldenVector(t *testing.T) {
	invocation := TaskListInvocation{
		Request:  TaskListRequest{Limit: 10},
		Identity: invocationTestMetadata(),
	}
	invocation.Identity.Signature = "5afa94d22dd516dc71bf96ef4090951d15bd8fb3dac532680b95d8bf102a4e4a"

	if err := VerifyTaskListInvocationSignature(invocationTestToken, invocation, time.UnixMilli(1786723200000)); err != nil {
		t.Fatalf("VerifyTaskListInvocationSignature() error = %v", err)
	}
	canonical, err := CanonicalTaskListInvocation(invocation)
	if err != nil {
		t.Fatalf("CanonicalTaskListInvocation() error = %v", err)
	}
	want := "QIANWEN-HMAC-SHA256-V1\n" +
		"task_list\n" +
		invocationTestTimestamp + "\n" +
		"0123456789abcdef0123456789abcdef\n" +
		"enc-user-Aa+/=\n" +
		"enc-device-Zz+/=\n" +
		"10\n" +
		"-"
	if canonical != want {
		t.Fatalf("canonical task-list invocation = %q, want %q", canonical, want)
	}
}
