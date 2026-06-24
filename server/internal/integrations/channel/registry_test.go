package channel

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// fakeChannel is a minimal Channel used to assert which Factory the
// Registry returned. id distinguishes instances built by different
// factories.
type fakeChannel struct {
	typ Type
	id  string
}

func (f fakeChannel) Type() Type                       { return f.typ }
func (f fakeChannel) Connect(context.Context) error    { return nil }
func (f fakeChannel) Disconnect(context.Context) error { return nil }
func (f fakeChannel) Send(context.Context, OutboundMessage) (SendResult, error) {
	return SendResult{}, nil
}
func (f fakeChannel) Capabilities() Capability { return CapText }

func factoryReturning(typ Type, id string) Factory {
	return func(Config) (Channel, error) { return fakeChannel{typ: typ, id: id}, nil }
}

func TestRegistry_LookupAndBuild(t *testing.T) {
	r := NewRegistry()
	r.Register(TypeFeishu, factoryReturning(TypeFeishu, "feishu-a"))

	if _, ok := r.Lookup(TypeFeishu); !ok {
		t.Fatalf("Lookup(%q) ok = false, want true", TypeFeishu)
	}

	ch, err := r.Build(Config{Type: TypeFeishu})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	got, ok := ch.(fakeChannel)
	if !ok {
		t.Fatalf("Build returned %T, want fakeChannel", ch)
	}
	if got.id != "feishu-a" {
		t.Fatalf("Build used factory id %q, want %q", got.id, "feishu-a")
	}
	if got.Type() != TypeFeishu {
		t.Fatalf("built channel Type() = %q, want %q", got.Type(), TypeFeishu)
	}
}

// TestRegistry_LastWriterWins is the explicit acceptance item: a second
// Register for the same Type silently replaces the first, and Build/Lookup
// resolve to the LATEST factory.
func TestRegistry_LastWriterWins(t *testing.T) {
	r := NewRegistry()
	r.Register(TypeFeishu, factoryReturning(TypeFeishu, "first"))
	r.Register(TypeFeishu, factoryReturning(TypeFeishu, "second"))

	ch, err := r.Build(Config{Type: TypeFeishu})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got := ch.(fakeChannel).id; got != "second" {
		t.Fatalf("last-writer-wins failed: Build used %q, want %q", got, "second")
	}

	// Registering a type must not create duplicate Types() entries.
	if types := r.Types(); !reflect.DeepEqual(types, []Type{TypeFeishu}) {
		t.Fatalf("Types() = %v, want exactly [%q]", types, TypeFeishu)
	}
}

func TestRegistry_BuildUnknownType(t *testing.T) {
	r := NewRegistry()
	_, err := r.Build(Config{Type: "nope"})
	if !errors.Is(err, ErrUnknownType) {
		t.Fatalf("Build unknown type err = %v, want errors.Is ErrUnknownType", err)
	}
}

func TestRegistry_RegisterIgnoresInvalid(t *testing.T) {
	r := NewRegistry()
	r.Register("", factoryReturning(TypeFeishu, "x")) // empty type ignored
	r.Register(TypeFeishu, nil)                       // nil factory ignored

	if _, ok := r.Lookup(""); ok {
		t.Fatalf("empty Type was registered, want ignored")
	}
	if _, ok := r.Lookup(TypeFeishu); ok {
		t.Fatalf("nil factory was registered, want ignored")
	}
	if len(r.Types()) != 0 {
		t.Fatalf("Types() = %v, want empty", r.Types())
	}
}

func TestRegistry_TypesSorted(t *testing.T) {
	r := NewRegistry()
	r.Register("wecom", factoryReturning("wecom", "w"))
	r.Register("feishu", factoryReturning("feishu", "f"))
	r.Register("slack", factoryReturning("slack", "s"))

	got := r.Types()
	want := []Type{"feishu", "slack", "wecom"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Types() = %v, want sorted %v", got, want)
	}
}

// TestRegistry_ConcurrentAccess exercises the RWMutex under the race
// detector: concurrent Register/Lookup/Build/Types must not data-race.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Register(TypeFeishu, factoryReturning(TypeFeishu, "x"))
			_, _ = r.Lookup(TypeFeishu)
			_, _ = r.Build(Config{Type: TypeFeishu})
			_ = r.Types()
		}()
	}
	wg.Wait()
}
