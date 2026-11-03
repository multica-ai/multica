package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/codexappserver"
)

// IsolationArgs keep native plugins and apps out of local conversations,
// matching the TypeScript host.
var IsolationArgs = []string{"--disable", "plugins", "--disable", "apps"}

const (
	maxFrameBytes    = 64 << 20
	handshakeTimeout = 10 * time.Second
	closeWait        = 5 * time.Second
)

// Command is how to start one app-server process.
type Command struct {
	Path string
	Dir  string
	Env  []string
}

// Handler receives notifications and server requests on the single stdout
// reader. It must not block; responses to this side's requests never pass
// through it.
type Handler func(codexappserver.Frame)

// Server is one running `codex app-server --listen stdio://`.
type Server struct {
	*codexappserver.Conn
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	exited chan struct{}
	// spawnTook and handshakeTook are set before Start returns.
	spawnTook, handshakeTook time.Duration
}

// StartDurations is how long launching the process and its handshake took.
func (s *Server) StartDurations() (spawn, handshake time.Duration) {
	return s.spawnTook, s.handshakeTook
}

// Start launches app-server and completes the initialize handshake.
func Start(ctx context.Context, command Command, handler Handler, logger *slog.Logger) (*Server, error) {
	return startWithArgs(ctx, command, nil, handler, logger)
}

// startWithArgs places prefix before the app-server arguments; tests use it
// to run a fake server.
func startWithArgs(ctx context.Context, command Command, prefix []string, handler Handler, logger *slog.Logger) (*Server, error) {
	args := append(append(append([]string{}, prefix...), IsolationArgs...), "app-server", "--listen", "stdio://")
	cmd := exec.Command(command.Path, args...)
	cmd.Dir, cmd.Env = command.Dir, command.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	began := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	s := &Server{Conn: codexappserver.NewConn(stdin), cmd: cmd, stdin: stdin, exited: make(chan struct{}), spawnTook: time.Since(began)}
	readDone, stderrDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stderrDone)
		drain(stderr, logger)
	}()
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64<<10), maxFrameBytes)
		for scanner.Scan() {
			frame, ok := codexappserver.ParseFrame(scanner.Bytes())
			if !ok {
				continue
			}
			if frame.Kind == codexappserver.FrameResponse {
				s.Resolve(frame)
				continue
			}
			handler(frame)
		}
		if err := scanner.Err(); err != nil {
			s.Close(fmt.Errorf("%w: %w", codexappserver.ErrProcessExited, err))
			_ = cmd.Process.Kill()
			return
		}
		s.Conn.Close(codexappserver.ErrProcessExited)
	}()
	go func() {
		// Wait closes the pipes, so every read must finish first.
		<-readDone
		<-stderrDone
		_ = cmd.Wait()
		close(s.exited)
	}()

	hsCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	handshaking := time.Now()
	if _, err := s.Request(hsCtx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "multica_dialogue", "title": "Multica", "version": "1.0.0"}}); err != nil {
		s.Stop()
		return nil, fmt.Errorf("codex app-server initialize: %w", err)
	}
	s.Notify("initialized")
	s.handshakeTook = time.Since(handshaking)
	return s, nil
}

// Stop closes stdin and kills the process if it does not exit promptly.
func (s *Server) Stop() {
	_ = s.stdin.Close()
	select {
	case <-s.exited:
	case <-time.After(closeWait):
		_ = s.cmd.Process.Kill()
		<-s.exited
	}
}

// Exited is closed once the process has exited.
func (s *Server) Exited() <-chan struct{} { return s.exited }

// IsClosed reports whether err means the process is gone.
func IsClosed(err error) bool { return errors.Is(err, codexappserver.ErrProcessExited) }

func drain(r io.Reader, logger *slog.Logger) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && logger != nil {
			logger.Debug("codex app-server stderr", "text", strings.TrimSpace(string(buf[:n])))
		}
		if err != nil {
			return
		}
	}
}
