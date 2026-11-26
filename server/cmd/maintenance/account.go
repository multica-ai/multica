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
	"strconv"
	"time"
)

// runAccount drives the account suspension actions (MUL-10296):
//
//	maintenance account suspend           --email E --actor A --reason R
//	maintenance account incident-response --email E --actor A --reason R [--apply]
//	maintenance account unsuspend         --email E --actor A --reason R
//	maintenance account status            --email E
//
// incident-response is a dry run unless --apply is given, like job creation.
// It is safe to repeat: a run that stopped part-way resumes where it stopped.
func runAccount(ctx context.Context, args []string, envPort string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: maintenance account {suspend|incident-response|unsuspend|status} [flags]; use account COMMAND --help")
	}
	action := args[0]
	switch action {
	case "suspend", "incident-response", "unsuspend", "status":
	default:
		return fmt.Errorf("unknown account command %q", action)
	}
	f := flag.NewFlagSet("maintenance account "+action, flag.ContinueOnError)
	f.SetOutput(stderr)
	port := f.String("port", envPort, "loopback API port (default MAINTENANCE_PORT)")
	userID := f.String("user-id", "", "account user id (or --email)")
	email := f.String("email", "", "account email (or --user-id)")
	actor := f.String("actor", "", "who is acting, recorded on the account (required except status)")
	reason := f.String("reason", "", "why, recorded on the account (required except status)")
	apply := f.Bool("apply", false, "incident-response only: make the changes (default is a dry run)")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	var invalidFlag string
	f.Visit(func(v *flag.Flag) {
		switch {
		case v.Name == "apply" && action != "incident-response",
			(v.Name == "actor" || v.Name == "reason") && action == "status":
			invalidFlag = v.Name
		}
	})
	if invalidFlag != "" {
		return fmt.Errorf("--%s is not supported by account %s", invalidFlag, action)
	}
	if (*userID == "") == (*email == "") {
		return errors.New("give exactly one of --user-id or --email")
	}
	if action != "status" && (*actor == "" || *reason == "") {
		return errors.New("--actor and --reason are required")
	}
	p, err := strconv.Atoi(*port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("set --port or MAINTENANCE_PORT to 1..65535")
	}
	body := map[string]any{"user_id": *userID, "email": *email}
	if action != "status" {
		body["actor"] = *actor
		body["reason"] = *reason
	}
	if action == "incident-response" {
		body["dry_run"] = !*apply
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Loopback never uses an operator's proxy.
	defer transport.CloseIdleConnections()
	hc := &http.Client{Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(p)+"/maintenance/accounts/"+action, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		if action == "incident-response" && *apply {
			return fmt.Errorf("%w; run the same command again to finish it", err)
		}
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, raw, "", "  ") == nil {
		raw = append(pretty.Bytes(), '\n')
	}
	if res.StatusCode != http.StatusOK {
		if action == "incident-response" && *apply && res.StatusCode >= 500 {
			return fmt.Errorf("HTTP %d: %s; run the same command again to finish it", res.StatusCode, bytes.TrimSpace(raw))
		}
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, bytes.TrimSpace(raw))
	}
	_, err = out.Write(raw)
	return err
}
