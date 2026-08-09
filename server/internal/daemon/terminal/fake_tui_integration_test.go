//go:build !windows

package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFakeTUIEndToEnd(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-tui")
	build := exec.Command("go", "build", "-o", binary, "../../../cmd/fake-tui")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake TUI: %v\n%s", err, output)
	}
	events := make(chan Event, 256)
	manager := NewManager(Options{OnEvent: func(event Event) { events <- event }})
	session, err := manager.Start(context.Background(), StartRequest{
		SessionID: uuid.New(), TaskID: "task", WorkspaceID: "workspace", RuntimeID: "runtime",
		Provider: "fake", Generation: 1,
		Command: Command{Path: binary, Dir: t.TempDir(), Env: os.Environ()},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, events, "\x1b[?1049h")
	if err := session.WriteInput(1, []byte("中文输入\n")); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, events, "echo: 中文输入")
	if err := session.Resize(80, 24); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, events, "size:80x24")
	if err := session.CtrlC(2); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, events, "interrupt:^C")
	if err := session.WriteInput(3, []byte("fail\n")); err != nil {
		t.Fatal(err)
	}
	exit := session.Wait()
	if exit.Code != 23 {
		t.Fatalf("exit code = %d, want 23", exit.Code)
	}
	select {
	case event := <-events:
		_ = event
	case <-time.After(time.Second):
	}
}
