package agent

import (
	"context"
	"reflect"
	"testing"
)

// TestRuntimeAdapterHasSevenMethods is a compile-time contract check: the
// RuntimeAdapter interface must expose exactly the seven frozen methods
// (Plan v1.4 section 13.2; the "five-method interface" is void).
func TestRuntimeAdapterHasSevenMethods(t *testing.T) {
	// Compile-time assertion: adapterSpy must satisfy RuntimeAdapter.
	var _ RuntimeAdapter = adapterSpy{}

	// Runtime assertion: exactly seven methods with the frozen names.
	iface := reflect.TypeOf((*RuntimeAdapter)(nil)).Elem()
	if iface.NumMethod() != 7 {
		t.Fatalf("RuntimeAdapter method count = %d, want exactly 7", iface.NumMethod())
	}
	want := map[string]bool{
		"Dispatch": true, "Poll": true, "Collect": true,
		"Cancel": true, "Health": true, "Bind": true, "Budget": true,
	}
	for i := 0; i < iface.NumMethod(); i++ {
		if !want[iface.Method(i).Name] {
			t.Fatalf("unexpected RuntimeAdapter method %s", iface.Method(i).Name)
		}
	}
}

// adapterSpy implements RuntimeAdapter so the compile-time assertion above
// holds. It is deliberately inert; provider implementations are ALL-18's
// responsibility.
type adapterSpy struct{}

func (adapterSpy) Dispatch(context.Context, DispatchRequest) (RunHandle, error) { return RunHandle{}, nil }
func (adapterSpy) Poll(context.Context, RunHandle) (PollStatus, error)          { return PollStatus{}, nil }
func (adapterSpy) Collect(context.Context, RunHandle) (CollectResult, error)    { return CollectResult{}, nil }
func (adapterSpy) Cancel(context.Context, RunHandle) error                      { return nil }
func (adapterSpy) Health(context.Context) (HealthSample, error)                 { return HealthSample{}, nil }
func (adapterSpy) Bind(context.Context, BindRequest) (BindResult, error)        { return BindResult{}, nil }
func (adapterSpy) Budget(context.Context, RunHandle) (BudgetStatus, error)      { return BudgetStatus{}, nil }

// TestMemoryHubCredentialNoSerialization checks the V4-3.2 safety invariant by
// contract: the type must not expose String() or MarshalJSON(), so a stray
// fmt/JSON call cannot leak the value.
func TestMemoryHubCredentialNoSerialization(t *testing.T) {
	c := MemoryHubCredential{Value: "sensitive", Placement: "mcp_authorization_env", BaseURL: "https://proxy.example"}
	if !c.Valid() {
		t.Fatal("credential should be valid")
	}
	if _, ok := any(c).(interface{ String() string }); ok {
		t.Fatal("MemoryHubCredential must not expose String()")
	}
	if _, ok := any(c).(interface{ MarshalJSON() ([]byte, error) }); ok {
		t.Fatal("MemoryHubCredential must not expose MarshalJSON()")
	}
	if _, ok := any(c).(interface{ LogValue() any }); ok {
		t.Fatal("MemoryHubCredential must not expose LogValue()")
	}
	// user-supplied placements are rejected
	if (MemoryHubCredential{Value: "x", Placement: "user_defined"}).Valid() {
		t.Fatal("user-supplied placement must be rejected")
	}
	// empty value is rejected
	if (MemoryHubCredential{Placement: "anthropic_auth_token"}).Valid() {
		t.Fatal("empty value must be rejected")
	}
}
