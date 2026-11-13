package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// ErrOpenCodeClockSkew identifies a transcript whose timestamps can outrank
// the next user input in OpenCode's timestamp-based anchor selection.
var ErrOpenCodeClockSkew = errors.New("opencode clock skew: session ordering is unreliable")

var errOpenCodeClockExport = errors.New("OpenCode session clock check could not verify the export")

const openCodeClockExportMaxBytes = 32 << 20

// CheckOpenCodeResumeClock attempts a clock check on every built-in OpenCode
// version. The export command and timestamp schema establish compatibility;
// version detection only selects the CLI syntax. It never reads the runtime DB.
func CheckOpenCodeResumeClock(ctx context.Context, cfg Config, opts ExecOptions) error {
	if !cfg.BuiltinRuntime || len(cfg.LaunchPrefix) != 0 {
		return errors.New("OpenCode session clock check does not support this runtime contract")
	}
	for _, arg := range opts.CustomArgs {
		flag, _, _ := strings.Cut(arg, "=")
		if strings.HasPrefix(flag, "-s") || strings.HasPrefix(flag, "-c") {
			return errors.New("OpenCode session clock check cannot verify custom connection or session arguments")
		}
		switch flag {
		case "--server", "--attach", "--session", "-s", "--continue", "-c", "--fork":
			return errors.New("OpenCode session clock check cannot verify custom connection or session arguments")
		case "--standalone":
			if arg != "--standalone" {
				return errors.New("OpenCode session clock check cannot verify custom connection or session arguments")
			}
		}
	}
	if opts.ResumeSessionID == "" || strings.HasPrefix(opts.ResumeSessionID, "-") {
		return errOpenCodeClockExport
	}
	execPath, err := resolveOpenCodeExecutable(cfg)
	if err != nil {
		return err
	}
	// Export needs the same storage selection and project root, but does not
	// need the task's managed MCP servers or their credentials.
	env, err := openCodeEnvironment(cfg, ExecOptions{Cwd: opts.Cwd})
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	checkedAt := time.Now().UnixMilli()
	args := []string{"export", opts.ResumeSessionID}
	if opencodeUsesV2Contract(cfg) {
		args = []string{"session", "export", opts.ResumeSessionID}
		if _, standalone := opencodeConnectionFromArgs(opts.CustomArgs); standalone {
			args = append(args, "--standalone")
		}
	}
	args = append(args, "--sanitize")
	check := func(args []string) (error, bool) {
		attemptCtx, stop := context.WithCancel(probeCtx)
		defer stop()
		cmd := cfg.commandAt(execPath).exec(attemptCtx, args...)
		hideAgentWindow(cmd)
		cmd.Dir, cmd.Env = opts.Cwd, env
		// Neither transcript data nor private error details enter daemon logs.
		// Keep only a bounded stderr tail to recognize an unsupported flag.
		stderr := &tailBuffer{max: 4096}
		cmd.Stderr = stderr
		reader, writer := io.Pipe()
		cmd.Stdout = writer
		finished := make(chan error, 1)
		go func() {
			err := runOwned(cmd, cfg.Logger)
			_ = writer.CloseWithError(err)
			finished <- err
		}()
		limited := &io.LimitedReader{R: reader, N: openCodeClockExportMaxBytes + 1}
		checkErr := checkOpenCodeClockExport(limited, opts.ResumeSessionID, checkedAt)
		if checkErr != nil || limited.N == 0 {
			stop()
		}
		_ = reader.Close()
		waitErr := <-finished
		if waitErr != nil || limited.N == 0 {
			text := strings.ToLower(string(stderr.Bytes()))
			unsupported := strings.Contains(text, "unknown argument: sanitize") ||
				strings.Contains(text, "unknown arguments: sanitize") ||
				strings.Contains(text, "unexpected flag: --sanitize") ||
				strings.Contains(text, "unrecognized option '--sanitize'")
			return errOpenCodeClockExport, waitErr != nil && unsupported && probeCtx.Err() == nil
		}
		return checkErr, false
	}
	err, unsupportedSanitize := check(args)
	if unsupportedSanitize {
		// Older CLIs can export the same timestamp schema without --sanitize.
		// Parse only metadata in memory; never store or log the raw transcript.
		err, _ = check(args[:len(args)-1])
	}
	return err
}

// Walk tokens instead of decoding the transcript into structs or RawMessages:
// parts are discarded as they arrive. The process deadline and byte/depth caps
// bound malformed or unexpectedly large exports as unverified.
func checkOpenCodeClockExport(reader io.Reader, sessionID string, checkedAt int64) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	var matched, updated, messages, skew bool
	created := 0
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if depth > 64 {
			return errOpenCodeClockExport
		}
		token, err := decoder.Token()
		if err != nil {
			return errOpenCodeClockExport
		}
		switch path {
		case "", ".info", ".info.time", ".messages[]", ".messages[].info", ".messages[].info.time", ".messages[].time":
			if token != json.Delim('{') {
				return errOpenCodeClockExport
			}
		case ".messages":
			if token != json.Delim('[') || messages {
				return errOpenCodeClockExport
			}
			messages = true
		case ".info.id":
			if token != sessionID {
				return errOpenCodeClockExport
			}
			matched = true
		case ".info.time.updated", ".messages[].info.time.created", ".messages[].time.created":
			number, ok := token.(json.Number)
			if !ok {
				return errOpenCodeClockExport
			}
			millis, err := number.Int64()
			if err != nil || millis <= 0 {
				return errOpenCodeClockExport
			}
			if millis >= checkedAt {
				skew = true
			}
			if path == ".info.time.updated" {
				updated = true
			} else {
				created++
			}
		}
		switch token {
		case json.Delim('{'):
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return errOpenCodeClockExport
				}
				name, ok := key.(string)
				if !ok {
					return errOpenCodeClockExport
				}
				if err := walk(path+"."+name, depth+1); err != nil {
					return err
				}
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return errOpenCodeClockExport
			}
		case json.Delim('['):
			for decoder.More() {
				before := created
				if err := walk(path+"[]", depth+1); err != nil {
					return err
				}
				if path == ".messages" && created != before+1 {
					return errOpenCodeClockExport
				}
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return errOpenCodeClockExport
			}
		}
		return nil
	}
	if err := walk("", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF || !matched || !updated || !messages {
		return errOpenCodeClockExport
	}
	if skew {
		return ErrOpenCodeClockSkew
	}
	return nil
}
