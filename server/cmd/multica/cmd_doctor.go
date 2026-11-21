package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{Use: "doctor", Short: "Check local setup before starting a task", Args: cobra.NoArgs, RunE: runDoctor}

func init() {
	doctorCmd.Flags().String("tool", "", "Check for a specific coding tool by runtime ID (for example, hermes)")
	doctorCmd.Flags().String("output", "table", "Output format: table or json")
}

type setupCheck struct {
	Check   string `json:"check"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	if err := requireHumanLocalCommand("doctor"); err != nil {
		return err
	}
	output := flagString(cmd, "output")
	if output != "table" && output != "json" {
		return fmt.Errorf("--output must be table or json")
	}
	profile := resolveProfile(cmd)
	checks := []setupCheck{checkDoctorServer(cmd.Context(), tryResolveHumanServerURL(cmd)), checkDoctorDaemon(cmd.Context(), profile)}
	tool := flagString(cmd, "tool")
	if daemon.HasDiscoveredTool(tool) {
		checks = append(checks, setupCheck{"tool", true, "Coding tool executable found."})
	} else {
		checks = append(checks, setupCheck{"tool", false, "Install a supported coding tool and check its executable path."})
	}
	if output == "json" {
		if err := cli.PrintJSON(cmd.OutOrStdout(), checks); err != nil {
			return err
		}
	} else {
		for _, row := range checks {
			state := "OK"
			if !row.OK {
				state = "FAIL"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", row.Check, state, row.Message); err != nil {
				return err
			}
		}
	}
	for _, row := range checks {
		if !row.OK {
			return errSilent
		}
	}
	return nil
}

func checkDoctorServer(ctx context.Context, raw string) setupCheck {
	fail := setupCheck{"server", false, "Check the configured server URL, network and TLS trust."}
	if raw == "" {
		fail.Message = "Configure the server URL with multica setup or --server-url."
		return fail
	}
	base, err := url.Parse(raw)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return fail
	}
	base.User = nil
	base.RawQuery = ""
	base.Fragment = ""
	base.Path = strings.TrimRight(base.Path, "/") + "/health"
	base.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return fail
	}
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fail
	}
	defer resp.Body.Close()
	var health struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health) != nil || health.Status != "ok" {
		return fail
	}
	return setupCheck{"server", true, "Server health endpoint responded."}
}

func checkDoctorDaemon(ctx context.Context, profile string) setupCheck {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := requireKnownProfile(profile); err != nil {
		return setupCheck{"daemon", false, "Check the selected profile with multica config."}
	}
	port := resolveHealthPort(profile)
	health := checkDaemonHealthOnPort(ctx, port)
	if daemonIdentityMismatch(health, profile, port) != nil {
		return setupCheck{"daemon", false, "Health port belongs to another profile. Check multica daemon status."}
	}
	if health["status"] == "starting" {
		return setupCheck{"daemon", false, "Daemon is starting. Retry after startup finishes."}
	}
	if health["status"] != "running" {
		return setupCheck{"daemon", false, "Start the selected profile with multica daemon start."}
	}
	return setupCheck{"daemon", true, "Daemon for the selected profile is running."}
}
