// maintenance drives the container-local API; it never connects to the database.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/maintenance"
)

var errIncomplete = errors.New("incomplete: run budget exhausted; inspect the persisted job before continuing")

type response struct {
	Job       *maintenance.Job `json:"job,omitempty"`
	Error     string           `json:"error,omitempty"`
	Retryable bool             `json:"retryable,omitempty"`
}

type client struct {
	base string
	http *http.Client
	out  io.Writer
}

func (c client) call(ctx context.Context, path string, body any) (int, response, error) {
	method := http.MethodGet
	var data []byte
	if body != nil {
		method = http.MethodPost
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return 0, response{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(data))
	if err != nil {
		return 0, response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return 0, response{}, err
	}
	defer res.Body.Close()
	var payload response
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&payload); err != nil {
		return res.StatusCode, payload, err
	}
	if res.StatusCode == 200 && (payload.Job == nil || payload.Job.ID == "") {
		return res.StatusCode, payload, errors.New("missing job in API response")
	}
	return res.StatusCode, payload, nil
}

func (c client) once(ctx context.Context, path string, body any) (*maintenance.Job, error) {
	code, p, err := c.call(ctx, path, body)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", code, p.Error)
	}
	return p.Job, nil
}

func sleep(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(max(0, delay))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// advance retries the original revision on ambiguous transport failures. A
// stale revision acknowledges a committed batch without doing another one.
func (c client) advance(ctx context.Context, path string, job *maintenance.Job, retries int) (*maintenance.Job, error) {
	revision := job.Revision
	for attempt := 0; ; attempt++ {
		code, p, err := c.call(ctx, path+"/advance", map[string]any{"revision": revision})
		if err == nil {
			if code == 200 || (code == 409 && p.Job != nil && p.Job.Revision != revision) {
				return p.Job, nil
			}
			if !p.Retryable {
				return nil, fmt.Errorf("HTTP %d: %s", code, p.Error)
			}
			err = fmt.Errorf("HTTP %d: %s", code, p.Error)
		}
		if attempt >= retries {
			return nil, err
		}
		if err := sleep(ctx, min(250*time.Millisecond*time.Duration(1<<attempt), 5*time.Second)); err != nil {
			return nil, err
		}
		if p.Retryable && p.Job != nil && p.Job.Status == "paused" {
			// Only resume a retryable failure returned by this invocation, never
			// a pre-existing/operator pause. A lost resume response stops safely.
			resumed, err := c.once(ctx, path+"/resume", map[string]any{"revision": p.Job.Revision})
			if err != nil {
				return nil, err
			}
			revision = resumed.Revision
		}
	}
}

func (c client) drive(ctx context.Context, path string, batches, retries int, resume bool) error {
	job, err := c.once(ctx, path, nil)
	if err != nil {
		return err
	}
	if resume && job.Status == "paused" {
		job, err = c.once(ctx, path+"/resume", map[string]any{"revision": job.Revision})
		if err != nil {
			return err
		}
	}
	for batch := 0; ; batch++ {
		if err := json.NewEncoder(c.out).Encode(job); err != nil {
			return err
		}
		if job.Status == "completed" {
			return nil
		}
		if job.Status != "ready" {
			return fmt.Errorf("job is %s: %s; inspect before resuming", job.Status, job.LastError)
		}
		if batch == batches {
			return errIncomplete
		}
		if err := sleep(ctx, time.Until(job.NextAllowedAt)); err != nil {
			return err
		}
		job, err = c.advance(ctx, path, job, retries)
		if err != nil {
			return err
		}
	}
}

func run(ctx context.Context, args []string, envPort string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: maintenance {create|status|run|pause|resume|cancel|configure} [flags]; use COMMAND --help")
	}
	action := args[0]
	f := flag.NewFlagSet("maintenance "+action, flag.ContinueOnError)
	f.SetOutput(stderr)
	port := f.String("port", envPort, "loopback API port (default MAINTENANCE_PORT)")
	jobID := f.String("job", "", "persisted job UUID")
	revision := f.Int64("revision", -1, "expected revision for pause/resume/cancel/configure")
	maxBatches := f.Int("max-batches", 0, "required run batch budget; unfinished exits 2")
	maxSeconds := f.Int("max-seconds", 0, "required run wall-clock budget including requests/retries")
	retries := f.Int("max-retries", 3, "retries per advance (0..10)")
	resume := f.Bool("resume", false, "explicitly resume an initially paused job before running")
	jobType := f.String("type", "issue_status_category", "registered processor type")
	version := f.Int("version", 1, "processor version")
	scope := f.String("scope", "database", "processor scope")
	key := f.String("idempotency-key", "", "stable unique creation key; reuse on an ambiguous create response")
	apply := f.Bool("apply", false, "write data (default is dry-run)")
	params := f.String("parameters", "{}", "processor parameters as a JSON object; apply category jobs require writers_upgraded=true")
	var opts maintenance.Options
	f.IntVar(&opts.BatchSize, "batch-size", 0, "required for create/configure: rows per transaction (1..5000)")
	f.IntVar(&opts.DelayMS, "delay-ms", 0, "required for create/configure: delay between batches (1..60000)")
	f.IntVar(&opts.LockTimeoutMS, "lock-timeout-ms", 0, "required for create/configure: lock timeout")
	f.IntVar(&opts.StatementTimeoutMS, "statement-timeout-ms", 0, "required for create/configure: statement timeout (<=5000)")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	// Reject tuning/creation flags on run rather than silently using the job's
	// persisted limits when the operator expected a smaller batch or a dry-run.
	allowed := map[string]string{
		"create": "port type version scope idempotency-key apply parameters batch-size delay-ms lock-timeout-ms statement-timeout-ms",
		"status": "port job",
		"run":    "port job max-batches max-seconds max-retries resume",
		"pause":  "port job revision", "resume": "port job revision", "cancel": "port job revision",
		"configure": "port job revision batch-size delay-ms lock-timeout-ms statement-timeout-ms",
	}
	flags, ok := allowed[action]
	if !ok {
		return fmt.Errorf("unknown command %q", action)
	}
	var invalidFlag string
	f.Visit(func(v *flag.Flag) {
		if !strings.Contains(" "+flags+" ", " "+v.Name+" ") {
			invalidFlag = v.Name
		}
	})
	if invalidFlag != "" {
		return fmt.Errorf("--%s is not supported by %s", invalidFlag, action)
	}
	p, err := strconv.Atoi(*port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("set --port or MAINTENANCE_PORT to 1..65535")
	}
	if action != "create" {
		if _, err := uuid.Parse(*jobID); err != nil {
			return errors.New("--job must be a UUID")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Loopback never uses an operator's proxy.
	defer transport.CloseIdleConnections()
	c := client{base: "http://127.0.0.1:" + strconv.Itoa(p), http: &http.Client{Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, out: out}
	path := "/maintenance/jobs/" + *jobID
	if action == "run" {
		if *maxBatches < 1 || *maxSeconds < 1 || *maxSeconds > 86400 || *retries < 0 || *retries > 10 {
			return errors.New("run requires --max-batches >= 1 and --max-seconds 1..86400; --max-retries 0..10")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(*maxSeconds)*time.Second)
		defer cancel()
		err := c.drive(ctx, path, *maxBatches, *retries, *resume)
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errIncomplete
		}
		return err
	}
	var body any
	if action == "create" || action == "configure" {
		if opts.BatchSize < 1 || opts.BatchSize > 5000 || opts.DelayMS < 1 || opts.DelayMS > 60000 || opts.LockTimeoutMS < 1 || opts.StatementTimeoutMS < opts.LockTimeoutMS || opts.StatementTimeoutMS > 5000 {
			return errors.New("create/configure require explicit --batch-size, --delay-ms, --lock-timeout-ms, --statement-timeout-ms within documented limits; choose values using a monitored canary")
		}
	}
	switch action {
	case "create":
		if *key == "" {
			return errors.New("create requires --idempotency-key")
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(*params), &object); err != nil || object == nil {
			return errors.New("--parameters must be a JSON object")
		}
		dry := !*apply
		body = maintenance.CreateRequest{Type: *jobType, Version: *version, Scope: *scope, IdempotencyKey: *key, DryRun: &dry, Options: opts, Parameters: json.RawMessage(*params)}
		path = "/maintenance/jobs"
	case "status":
	case "pause", "resume", "cancel", "configure":
		if *revision < 0 {
			return errors.New("mutation requires the current --revision; obtain it with status")
		}
		path += "/" + action
		body = map[string]any{"revision": *revision}
		if action == "configure" {
			body = map[string]any{"revision": *revision, "options": opts}
		}
	default:
		return fmt.Errorf("unknown command %q", action)
	}
	job, err := c.once(ctx, path, body)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(job)
}

func exitCode(err error) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if errors.Is(err, errIncomplete) {
		return 2
	}
	return 1
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv("MAINTENANCE_PORT"), os.Stdout, os.Stderr)
	cancel()
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(exitCode(err))
}
