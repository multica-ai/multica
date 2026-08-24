package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Device authorization login (`multica login --device`): the CLI asks the
// server for a short user code, the user types it into the web app on any
// already-signed-in device (/activate), and this process polls until the
// server hands back the login JWT — no browser on this machine, no localhost
// callback, no SSH tunnel. See issue #7463 for the motivation.

const deviceAuthErrPending = "authorization_pending"
const deviceAuthErrSlowDown = "slow_down"
const deviceAuthErrExpired = "expired_token"
const deviceAuthErrDenied = "access_denied"

type deviceStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type deviceTokenResponse struct {
	Token string `json:"token"`
	Error string `json:"error"`
}

func runAuthLoginDevice(cmd *cobra.Command) error {
	serverURL := resolveHumanServerURL(cmd)
	appURL := resolveAppURL(cmd)

	client := cli.NewAPIClient(serverURL, "", "")
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var start deviceStartResponse
	if err := client.PostJSON(ctx, "/auth/device/start", map[string]any{}, &start); err != nil {
		return cli.WithUserMessage("The server does not support device login (is it older than this CLI?) or could not be reached.", err)
	}

	verificationURL := start.VerificationURL
	if verificationURL == "" && appURL != "" {
		verificationURL = strings.TrimRight(appURL, "/") + "/activate"
	}
	if verificationURL == "" {
		verificationURL = "<your Multica app URL>/activate"
	}

	fmt.Fprintf(os.Stderr, "Device login started.\n\n")
	fmt.Fprintf(os.Stderr, "  1. Open %s in a browser where you are signed in to Multica\n", verificationURL)
	fmt.Fprintf(os.Stderr, "  2. Enter this code: %s\n\n", start.UserCode)
	fmt.Fprintf(os.Stderr, "Waiting for approval")

	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	if start.ExpiresIn <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}

	var jwtToken string
	for {
		// A fresh context per request: cli.APIContext carries a ~35s
		// deadline (per-request HTTP timeout + slack), which a single
		// shared context would impose on the whole wait loop — approval
		// after ~35s would then always fail with "lost contact".
		pollCtx, pollCancel := cli.APIContext(context.Background())
		var poll deviceTokenResponse
		err := client.PostJSON(pollCtx, "/auth/device/token", map[string]string{"device_code": start.DeviceCode}, &poll)
		pollCancel()
		if err == nil {
			jwtToken = poll.Token
			break
		}
		var httpErr *cli.HTTPError
		if !errors.As(err, &httpErr) {
			return cli.WithUserMessage("Lost contact with the server while waiting for approval.", err)
		}
		// 429 from the rate limiter behaves like slow_down: back off and
		// keep waiting (e.g. two device logins behind one NAT).
		pollErr := ""
		if httpErr.StatusCode == http.StatusTooManyRequests {
			pollErr = deviceAuthErrSlowDown
		} else if httpErr.StatusCode == 400 {
			var body deviceTokenResponse
			_ = json.Unmarshal([]byte(httpErr.Body), &body)
			pollErr = body.Error
		}
		switch pollErr {
		case deviceAuthErrPending:
			fmt.Fprint(os.Stderr, ".")
		case deviceAuthErrSlowDown:
			// Server says we are polling too fast; back off one interval.
			interval *= 2
			fmt.Fprint(os.Stderr, ".")
		case deviceAuthErrExpired:
			fmt.Fprintln(os.Stderr)
			return errors.New("the code expired before it was approved — run `multica login --device` again")
		case deviceAuthErrDenied:
			fmt.Fprintln(os.Stderr)
			return errors.New("the approval was denied on the web side")
		default:
			fmt.Fprintln(os.Stderr)
			return cli.WithUserMessage("The server rejected this device login attempt.", err)
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr)
			return errors.New("timed out waiting for approval — run `multica login --device` again")
		}
		time.Sleep(interval)
	}

	fmt.Fprintln(os.Stderr, " approved.")
	return completeLoginWithJWT(cmd, serverURL, appURL, jwtToken)
}
