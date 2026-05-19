package channel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// stubChannel is a no-op port.Channel used purely for registry tests. Lives in
// the channel_test package so it never escapes into production code.
type stubChannel struct{ name string }

func (s stubChannel) Name() string                                              { return s.name }
func (s stubChannel) Connect(ctx context.Context) error                         { return nil }
func (s stubChannel) Disconnect(ctx context.Context) error                      { return nil }
func (s stubChannel) Events() <-chan port.InboundEvent                          { return nil }
func (s stubChannel) Send(ctx context.Context, _ port.OutboundMessage) (port.SendResult, error) {
	return port.SendResult{}, nil
}
func (s stubChannel) SendCard(ctx context.Context, _ port.OutboundCardMessage) (port.SendResult, error) {
	return port.SendResult{}, nil
}
func (s stubChannel) GetChatInfo(ctx context.Context, _ string) (port.ChatInfo, error) {
	return port.ChatInfo{}, nil
}
func (s stubChannel) GetUserInfo(ctx context.Context, _ string) (port.UserInfo, error) {
	return port.UserInfo{}, nil
}

// TC-port-1 · MockChannel 注册成功
//
// Per TestCase §1.TC-port-1: empty Registry → Register(mock) → Get returns it,
// List has length 1, duplicate Register returns ErrDuplicateChannel (typed
// error, asserted via errors.Is — not by string match).
func TestRegistry_Register_Success(t *testing.T) {
	t.Parallel()

	reg := channel.NewRegistry()

	if err := reg.Register(stubChannel{name: "mock"}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	got, err := reg.Get("mock")
	if err != nil {
		t.Fatalf("Get(\"mock\") returned error: %v", err)
	}
	if got == nil {
		t.Fatal("Get(\"mock\") returned nil channel")
	}
	if got.Name() != "mock" {
		t.Fatalf("Get(\"mock\").Name() = %q, want %q", got.Name(), "mock")
	}

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("List() length = %d, want 1", len(list))
	}
	if list[0].Name() != "mock" {
		t.Fatalf("List()[0].Name() = %q, want %q", list[0].Name(), "mock")
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	t.Parallel()

	reg := channel.NewRegistry()
	if err := reg.Register(stubChannel{name: "mock"}); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	err := reg.Register(stubChannel{name: "mock"})
	if err == nil {
		t.Fatal("duplicate Register returned nil error, want ErrDuplicateChannel")
	}
	if !errors.Is(err, channel.ErrDuplicateChannel) {
		t.Fatalf("duplicate Register error = %v, want errors.Is(err, ErrDuplicateChannel)", err)
	}
}

// TC-port-2 · MockChannel 反注册
//
// Per TestCase §1.TC-port-2: after Unregister the channel must disappear from
// Get/List, and re-Unregister must return ErrChannelNotFound (idempotent
// error — caller can decide to ignore it). Both cases asserted via errors.Is.
func TestRegistry_Unregister(t *testing.T) {
	t.Parallel()

	reg := channel.NewRegistry()
	if err := reg.Register(stubChannel{name: "mock"}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := reg.Unregister("mock"); err != nil {
		t.Fatalf("first Unregister returned error: %v", err)
	}

	if _, err := reg.Get("mock"); !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("Get after Unregister error = %v, want errors.Is(err, ErrChannelNotFound)", err)
	}

	if list := reg.List(); len(list) != 0 {
		t.Fatalf("List() after Unregister length = %d, want 0", len(list))
	}

	err := reg.Unregister("mock")
	if !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("second Unregister error = %v, want errors.Is(err, ErrChannelNotFound)", err)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	t.Parallel()

	reg := channel.NewRegistry()
	_, err := reg.Get("nonexistent")
	if !errors.Is(err, channel.ErrChannelNotFound) {
		t.Fatalf("Get on empty registry error = %v, want errors.Is(err, ErrChannelNotFound)", err)
	}
}
