package sandbox

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// MockProvider is a test double for SandboxProvider.
type MockProvider struct {
	Sandboxes map[string]*Sandbox
	Files     map[string]map[string][]byte // sandboxID -> path -> content
	ExecFunc  func(ctx context.Context, sb *Sandbox, cmd []string) (string, error)
}

func NewMockProvider() *MockProvider {
	return &MockProvider{
		Sandboxes: make(map[string]*Sandbox),
		Files:     make(map[string]map[string][]byte),
	}
}

func (m *MockProvider) CreateOrConnect(_ context.Context, sandboxID string, opts CreateOpts) (*Sandbox, error) {
	if sandboxID != "" {
		if sb, ok := m.Sandboxes[sandboxID]; ok {
			return sb, nil
		}
	}

	id := sandboxID
	if id == "" {
		id = fmt.Sprintf("mock-sandbox-%d", len(m.Sandboxes)+1)
	}

	sb := &Sandbox{
		ID:       id,
		Status:   "running",
		Provider: "mock",
		metadata: make(map[string]string),
	}
	m.Sandboxes[id] = sb
	m.Files[id] = make(map[string][]byte)
	return sb, nil
}

func (m *MockProvider) Exec(ctx context.Context, sb *Sandbox, cmd []string) (string, error) {
	if m.ExecFunc != nil {
		return m.ExecFunc(ctx, sb, cmd)
	}
	return fmt.Sprintf("mock exec: %s", strings.Join(cmd, " ")), nil
}

func (m *MockProvider) ReadFile(_ context.Context, sb *Sandbox, path string) ([]byte, error) {
	files, ok := m.Files[sb.ID]
	if !ok {
		return nil, fmt.Errorf("sandbox %s not found", sb.ID)
	}
	content, ok := files[path]
	if !ok {
		return nil, fmt.Errorf("file %s not found in sandbox %s", path, sb.ID)
	}
	return content, nil
}

func (m *MockProvider) WriteFile(_ context.Context, sb *Sandbox, path string, content []byte) error {
	files, ok := m.Files[sb.ID]
	if !ok {
		return fmt.Errorf("sandbox %s not found", sb.ID)
	}
	files[path] = content
	return nil
}

func (m *MockProvider) Destroy(_ context.Context, sandboxID string) error {
	delete(m.Sandboxes, sandboxID)
	delete(m.Files, sandboxID)
	return nil
}

// Verify MockProvider satisfies the interface at compile time.
var _ SandboxProvider = (*MockProvider)(nil)
var _ SandboxProvider = (*E2BProvider)(nil)

func TestMockProvider_CreateOrConnect(t *testing.T) {
	p := NewMockProvider()

	t.Run("create new sandbox", func(t *testing.T) {
		sb, err := p.CreateOrConnect(context.Background(), "", CreateOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if sb.ID == "" {
			t.Fatal("expected non-empty sandbox ID")
		}
		if sb.Status != "running" {
			t.Fatalf("expected status running, got %s", sb.Status)
		}
	})

	t.Run("reconnect to existing", func(t *testing.T) {
		sb1, _ := p.CreateOrConnect(context.Background(), "", CreateOpts{})
		sb2, err := p.CreateOrConnect(context.Background(), sb1.ID, CreateOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if sb1.ID != sb2.ID {
			t.Fatalf("expected same ID %s, got %s", sb1.ID, sb2.ID)
		}
	})
}

func TestMockProvider_Exec(t *testing.T) {
	p := NewMockProvider()
	sb, _ := p.CreateOrConnect(context.Background(), "", CreateOpts{})

	t.Run("default returns mock output", func(t *testing.T) {
		out, err := p.Exec(context.Background(), sb, []string{"echo", "hello"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "echo hello") {
			t.Fatalf("unexpected output: %s", out)
		}
	})

	t.Run("custom ExecFunc", func(t *testing.T) {
		p.ExecFunc = func(_ context.Context, _ *Sandbox, cmd []string) (string, error) {
			return "custom: " + cmd[0], nil
		}
		out, err := p.Exec(context.Background(), sb, []string{"test"})
		if err != nil {
			t.Fatal(err)
		}
		if out != "custom: test" {
			t.Fatalf("expected 'custom: test', got %s", out)
		}
		p.ExecFunc = nil
	})
}

func TestMockProvider_FileOps(t *testing.T) {
	p := NewMockProvider()
	sb, _ := p.CreateOrConnect(context.Background(), "", CreateOpts{})

	t.Run("write and read file", func(t *testing.T) {
		err := p.WriteFile(context.Background(), sb, "/tmp/test.txt", []byte("hello world"))
		if err != nil {
			t.Fatal(err)
		}
		content, err := p.ReadFile(context.Background(), sb, "/tmp/test.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "hello world" {
			t.Fatalf("expected 'hello world', got %s", string(content))
		}
	})

	t.Run("read non-existent file fails", func(t *testing.T) {
		_, err := p.ReadFile(context.Background(), sb, "/tmp/nope.txt")
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})
}

func TestMockProvider_Destroy(t *testing.T) {
	p := NewMockProvider()
	sb, _ := p.CreateOrConnect(context.Background(), "", CreateOpts{})
	id := sb.ID

	err := p.Destroy(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	// After destroy, reconnect should create a new sandbox
	sb2, _ := p.CreateOrConnect(context.Background(), id, CreateOpts{})
	if sb2.ID != id {
		t.Fatalf("expected reconnect to create with same ID")
	}
}
