package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/engineering"
	"github.com/spf13/cobra"
)

func newLabrastroProjectBaselineCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "baseline", Short: "Read, propose, compare, adopt and export project baselines"}
	for _, action := range []string{"get", "propose", "diff", "adopt", "export"} {
		sub := &cobra.Command{Use: action + " <project-id>", Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return baselineFailure(2, "exactly one project ID is required")
			}
			return nil
		}, RunE: func(cmd *cobra.Command, args []string) error {
			err := runLabrastroProjectBaseline(cmd, args)
			var failure *baselineCLIError
			if err != nil && !errors.As(err, &failure) {
				return baselineFailure(1, "cannot write baseline output")
			}
			return err
		}}
		switch action {
		case "get":
			sub.Short = "Read adopted chapters or candidates, including a specific historical revision"
			sub.Flags().Bool("candidate", false, "Read candidates instead of adopted revisions")
			sub.Flags().String("revision", "", "Historical revision as chapter:number")
		case "propose":
			sub.Short = "Submit a candidate and show its server-computed Diff"
			sub.Flags().String("content-file", "", "UTF-8 Markdown file inside the current directory (required)")
			sub.Flags().String("summary", "", "Change summary (required)")
			sub.Flags().String("base", "", "Adopted revision number on which this proposal is based")
			sub.Flags().Bool("base-current", false, "Use the adopted version read immediately before proposing")
			sub.Flags().String("replaces", "", "Explicit candidate revision number to replace")
			sub.Flags().String("source-issue", "", "Source issue identifier or UUID")
		case "diff":
			sub.Short = "Show candidate Diffs or compare two historical chapter revisions"
			sub.Flags().String("from", "", "Historical source revision number (requires --chapter and --to)")
			sub.Flags().String("to", "", "Historical target revision number")
		case "adopt":
			sub.Short = "Decide explicitly named chapter versions using the current credentials"
			sub.Long = "Adopt chapter:number versions atomically. The server checks decider permissions,\n" +
				"human decision credentials and concurrent changes."
			sub.Flags().StringArray("revision", nil, "Chapter:number to adopt (required, repeatable for different chapters)")
			sub.Flags().Bool("reject", false, "Reject the specified candidates instead of adopting")
		case "export":
			sub.Short = "Export adopted chapters as read-only Markdown snapshots"
			sub.Flags().String("dir", "./docs/baseline", "Directory for the four numbered chapter files")
			sub.Flags().String("single-file", "", "Write all chapters to one Markdown file instead of --dir")
		}
		if action == "get" || action == "propose" || action == "diff" {
			sub.Flags().String("chapter", "", "Chapter: overview, domain_rules, architecture, exclusions_and_risks")
		}
		sub.Flags().String("output", "text", "Output format: text or json (get also supports markdown)")
		cmd.AddCommand(sub)
	}
	return cmd
}

type baselineCLIError struct {
	code    int
	message string
}

func (e *baselineCLIError) Error() string { return e.message }
func baselineFailure(code int, format string, args ...any) error {
	return &baselineCLIError{code, fmt.Sprintf(format, args...)}
}

// Scope the contract's 0/1/2 exit convention to this command family, including
// Cobra's flag/argument errors. Other commands retain the global CLI convention.
func labrastroBaselineExitCode(cmd *cobra.Command, err error) int {
	if err == nil {
		return 0
	}
	for c := cmd; c != nil; c = c.Parent() {
		if (c.Name() == "baseline" || c.Name() == "engineering") && c.Parent() != nil && c.Parent().Name() == "project" {
			var e *baselineCLIError
			if errors.As(err, &e) {
				return e.code
			}
			return 2
		}
	}
	return cli.ExitCodeFor(err)
}

type baselineCLIRevision struct {
	ID        string           `json:"id"`
	Chapter   string           `json:"chapter"`
	Number    int32            `json:"revision_number"`
	Status    string           `json:"status"`
	Content   string           `json:"content_markdown"`
	Summary   string           `json:"change_summary"`
	Base      *string          `json:"base_revision_id"`
	DecidedAt *string          `json:"decided_at"`
	Diff      engineering.Diff `json:"anchors_diff"`
}
type baselineCLIChapter struct {
	Chapter   string          `json:"chapter"`
	Adopted   json.RawMessage `json:"adopted"`
	Candidate json.RawMessage `json:"candidate"`
}
type baselineCLIState struct {
	Engineering json.RawMessage      `json:"engineering"`
	Chapters    []baselineCLIChapter `json:"chapters"`
}
type baselineCLISelection struct {
	Chapter  string          `json:"chapter"`
	Revision json.RawMessage `json:"revision"`
}
type baselineCLIRef struct {
	chapter string
	number  int32
}

func baselineNumber(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 1 {
		return 0, baselineFailure(2, "revision number must be a positive 32-bit integer")
	}
	return int32(n), nil
}
func baselineRef(s string) (baselineCLIRef, error) {
	chapter, number, ok := strings.Cut(s, ":")
	if !ok || !engineering.ValidChapter(chapter) {
		return baselineCLIRef{}, baselineFailure(2, "--revision must be chapter:number using a supported chapter")
	}
	n, err := baselineNumber(number)
	return baselineCLIRef{chapter, n}, err
}
func baselineDecodeRevision(raw json.RawMessage) (*baselineCLIRevision, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var r baselineCLIRevision
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &r) != nil || json.Unmarshal(raw, &fields) != nil ||
		!uuidRegexp.MatchString(r.ID) || !engineering.ValidChapter(r.Chapter) || r.Number < 1 ||
		len(fields["content_markdown"]) == 0 || string(fields["content_markdown"]) == "null" ||
		r.Diff.Chapter != r.Chapter {
		return nil, baselineFailure(1, "invalid baseline revision response")
	}
	return &r, nil
}
func baselineChapter(state baselineCLIState, chapter string) baselineCLIChapter {
	for _, c := range state.Chapters {
		if c.Chapter == chapter {
			return c
		}
	}
	return baselineCLIChapter{Chapter: chapter}
}
func baselineValidate(cmd *cobra.Command) error {
	action, output := cmd.Name(), mustString(cmd, "output")
	if output != "json" && output != "text" && output != "table" && !(action == "get" && output == "markdown") {
		return baselineFailure(2, "unsupported --output format")
	}
	chapter := mustString(cmd, "chapter")
	if chapter != "" && !engineering.ValidChapter(chapter) {
		return baselineFailure(2, "unsupported --chapter; use overview, domain_rules, architecture or exclusions_and_risks")
	}
	switch action {
	case "get":
		if s := mustString(cmd, "revision"); cmd.Flags().Changed("revision") {
			r, err := baselineRef(s)
			if err != nil {
				return err
			}
			candidate, _ := cmd.Flags().GetBool("candidate")
			if candidate || chapter != "" && chapter != r.chapter {
				return baselineFailure(2, "--revision cannot be combined with --candidate or a different --chapter")
			}
		}
	case "propose":
		if chapter == "" || mustString(cmd, "content-file") == "" || strings.TrimSpace(mustString(cmd, "summary")) == "" {
			return baselineFailure(2, "--chapter, --content-file and --summary are required")
		}
		if utf8.RuneCountInString(mustString(cmd, "summary")) > 500 {
			return baselineFailure(2, "--summary must not exceed 500 characters")
		}
		current, _ := cmd.Flags().GetBool("base-current")
		if current && cmd.Flags().Changed("base") {
			return baselineFailure(2, "--base and --base-current are mutually exclusive")
		}
		for _, flag := range []string{"base", "replaces"} {
			if cmd.Flags().Changed(flag) {
				if _, err := baselineNumber(mustString(cmd, flag)); err != nil {
					return err
				}
			}
		}
	case "diff":
		from, to := mustString(cmd, "from"), mustString(cmd, "to")
		if cmd.Flags().Changed("from") || cmd.Flags().Changed("to") {
			if chapter == "" || from == "" || to == "" {
				return baselineFailure(2, "historical Diff requires --chapter, --from and --to")
			}
			for _, s := range []string{from, to} {
				if _, err := baselineNumber(s); err != nil {
					return err
				}
			}
		}
	case "adopt":
		refs, _ := cmd.Flags().GetStringArray("revision")
		if len(refs) == 0 || len(refs) > 4 {
			return baselineFailure(2, "specify one to four --revision chapter:number values")
		}
		seen := map[string]bool{}
		for _, s := range refs {
			r, err := baselineRef(s)
			if err != nil {
				return err
			}
			if seen[r.chapter] {
				return baselineFailure(2, "each chapter may be decided only once")
			}
			seen[r.chapter] = true
		}
	case "export":
		if cmd.Flags().Changed("single-file") && (mustString(cmd, "single-file") == "" || cmd.Flags().Changed("dir")) {
			return baselineFailure(2, "use either a non-empty --single-file or --dir")
		}
		if mustString(cmd, "dir") == "" {
			return baselineFailure(2, "--dir must not be empty")
		}
	}
	return nil
}

func runLabrastroProjectBaseline(cmd *cobra.Command, args []string) error {
	if err := baselineValidate(cmd); err != nil {
		return err
	}
	var content string
	if cmd.Name() == "propose" {
		var err error
		content, err = baselineReadContent(mustString(cmd, "content-file"))
		if err != nil {
			return err
		}
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return baselineFailure(1, "%s", err)
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	// Keep local reference errors actionable without exposing HTTP response
	// bodies or misclassifying transport failures as invalid parameters.
	var lookupErr error
	project, err := resolveIDByPrefix(ctx, client, "project", args[0], func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
		candidates, err := fetchProjectCandidates(ctx, client)
		lookupErr = err
		return candidates, err
	})
	if err != nil {
		if lookupErr != nil {
			return baselineRequestError(lookupErr, false)
		}
		return baselineFailure(2, "%s", err)
	}
	path := "/api/projects/" + project.ID
	if cmd.Name() == "diff" && mustString(cmd, "from") != "" {
		query := url.Values{"chapter": {mustString(cmd, "chapter")}, "diff_from": {mustString(cmd, "from")}, "diff_to": {mustString(cmd, "to")}}
		var raw json.RawMessage
		if err := client.GetJSON(ctx, path+"/baseline/revisions?"+query.Encode(), &raw); err != nil {
			return baselineRequestError(err, false)
		}
		var diff engineering.Diff
		if json.Unmarshal(raw, &diff) != nil || !engineering.ValidChapter(diff.Chapter) {
			return baselineFailure(1, "invalid baseline Diff response")
		}
		baselinePrintWarnings(cmd, diff)
		if mustString(cmd, "output") == "json" {
			return cli.PrintJSON(cmd.OutOrStdout(), raw)
		}
		baselinePrintDiff(cmd, diff, true)
		return nil
	}
	if cmd.Name() == "adopt" {
		return baselineAdopt(ctx, cmd, client, project.ID)
	}
	if cmd.Name() == "get" && mustString(cmd, "revision") != "" {
		ref, _ := baselineRef(mustString(cmd, "revision"))
		raw, _, err := baselineLookupRevision(ctx, cmd, client, project.ID, ref)
		if err != nil {
			return err
		}
		return baselinePrintSelection(cmd, nil, []baselineCLISelection{{ref.chapter, raw}})
	}
	state, err := baselineReadState(ctx, cmd, client, project.ID)
	if err != nil {
		return err
	}
	switch cmd.Name() {
	case "get":
		selected := []baselineCLISelection{}
		candidate, _ := cmd.Flags().GetBool("candidate")
		for _, c := range state.Chapters {
			if chapter := mustString(cmd, "chapter"); chapter != "" && chapter != c.Chapter {
				continue
			}
			raw := c.Adopted
			if candidate {
				raw = c.Candidate
			}
			selected = append(selected, baselineCLISelection{c.Chapter, raw})
		}
		return baselinePrintSelection(cmd, state.Engineering, selected)
	case "propose":
		return baselinePropose(ctx, cmd, client, project.ID, state, content)
	case "diff":
		diffs := []engineering.Diff{}
		for _, c := range state.Chapters {
			if chapter := mustString(cmd, "chapter"); chapter != "" && chapter != c.Chapter {
				continue
			}
			r, err := baselineDecodeRevision(c.Candidate)
			if err != nil {
				return err
			}
			if r != nil {
				diffs = append(diffs, r.Diff)
				baselinePrintWarnings(cmd, r.Diff)
			}
		}
		if mustString(cmd, "output") == "json" {
			return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"diffs": diffs})
		}
		for _, diff := range diffs {
			baselinePrintDiff(cmd, diff, true)
		}
		if len(diffs) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No candidate revisions.")
		}
		return nil
	case "export":
		return baselineExport(cmd, state)
	}
	return baselineFailure(2, "unknown baseline subcommand")
}

func baselineReadState(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, project string) (baselineCLIState, error) {
	var state baselineCLIState
	err := client.GetJSON(ctx, "/api/projects/"+project+"/engineering", &state)
	if err != nil {
		return state, baselineRequestError(err, false)
	}
	seen := map[string]bool{}
	for _, c := range state.Chapters {
		if !engineering.ValidChapter(c.Chapter) || seen[c.Chapter] {
			return state, baselineFailure(1, "invalid baseline chapters response")
		}
		seen[c.Chapter] = true
		for _, raw := range []json.RawMessage{c.Adopted, c.Candidate} {
			r, err := baselineDecodeRevision(raw)
			if err != nil {
				return state, err
			}
			if r != nil && r.Chapter != c.Chapter {
				return state, baselineFailure(1, "baseline revision belongs to a different chapter")
			}
		}
	}
	if len(seen) != 4 {
		return state, baselineFailure(1, "incomplete baseline chapters response")
	}
	return state, nil
}

// Query immediately before the requested number, avoiding a scan through every
// history page. An exact match is mandatory; never substitute a newer candidate.
func baselineLookupRevision(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, project string, ref baselineCLIRef) (json.RawMessage, *baselineCLIRevision, error) {
	q := url.Values{"chapter": {ref.chapter}, "limit": {"1"}}
	if ref.number < 2147483647 {
		q.Set("before", strconv.FormatInt(int64(ref.number)+1, 10))
	}
	var result struct {
		Revisions []json.RawMessage `json:"revisions"`
	}
	if err := client.GetJSON(ctx, "/api/projects/"+project+"/baseline/revisions?"+q.Encode(), &result); err != nil {
		return nil, nil, baselineRequestError(err, false)
	}
	if len(result.Revisions) == 1 {
		raw := result.Revisions[0]
		r, err := baselineDecodeRevision(raw)
		if err != nil {
			return nil, nil, err
		}
		if r != nil && r.Chapter == ref.chapter && r.Number == ref.number {
			return raw, r, nil
		}
	}
	return nil, nil, baselineFailure(2, "404 not_found: requested chapter revision does not exist")
}

func baselinePropose(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, project string, state baselineCLIState, content string) error {
	chapter := mustString(cmd, "chapter")
	c := baselineChapter(state, chapter)
	adopted, _ := baselineDecodeRevision(c.Adopted)
	candidate, _ := baselineDecodeRevision(c.Candidate)
	var base, replaces *string
	if current, _ := cmd.Flags().GetBool("base-current"); current {
		if adopted != nil {
			base = &adopted.ID
		}
	} else if s := mustString(cmd, "base"); s != "" {
		n, _ := baselineNumber(s)
		_, r, err := baselineLookupRevision(ctx, cmd, client, project, baselineCLIRef{chapter, n})
		if err != nil {
			return err
		}
		base = &r.ID
	} else if adopted != nil {
		return baselineFailure(2, "this chapter has an adopted revision; specify --base <number> or --base-current")
	}
	if s := mustString(cmd, "replaces"); s != "" {
		n, _ := baselineNumber(s)
		_, r, err := baselineLookupRevision(ctx, cmd, client, project, baselineCLIRef{chapter, n})
		if err != nil {
			return err
		}
		replaces = &r.ID
	} else if candidate != nil {
		return baselineFailure(1, "409 baseline_candidate_open: candidate %s:%d (%s) already exists; inspect it before using --replaces %d", chapter, candidate.Number, candidate.ID, candidate.Number)
	}
	body := map[string]any{"chapter": chapter, "content_markdown": content, "change_summary": mustString(cmd, "summary"), "base_revision_id": base}
	if replaces != nil {
		body["replaces_candidate_id"] = replaces
	}
	if source := mustString(cmd, "source-issue"); source != "" {
		ref, err := resolveIssueRef(ctx, client, source)
		if err != nil {
			return baselineRequestError(err, false)
		}
		body["source_issue_id"] = ref.ID
	}
	var raw json.RawMessage
	if err := client.PostJSON(ctx, "/api/projects/"+project+"/baseline/candidates", body, &raw); err != nil {
		if cli.ServerErrorCode(err) == "baseline_candidate_open" {
			// Another session may have opened a candidate after our first read.
			// Read its identity for guidance, without retrying the failed write.
			if latest, readErr := baselineReadState(ctx, cmd, client, project); readErr == nil {
				if r, _ := baselineDecodeRevision(baselineChapter(latest, chapter).Candidate); r != nil {
					return baselineFailure(1, "409 baseline_candidate_open: candidate %s:%d (%s) is now open; inspect it before replacing", chapter, r.Number, r.ID)
				}
			}
		}
		return baselineRequestError(err, true)
	}
	r, err := baselineDecodeRevision(raw)
	if err != nil {
		return err
	}
	if r == nil {
		return baselineFailure(1, "empty candidate response; inspect baseline state before proposing again")
	}
	baselinePrintWarnings(cmd, r.Diff)
	if mustString(cmd, "output") == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), raw)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: revision %d (%s)\n", r.Chapter, r.Number, r.Status)
	baselinePrintDiff(cmd, r.Diff, true)
	if link := baselineApprovalURL(cmd, client, project, r.ID); link != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Review candidate in the engineering overview: %s\n", link)
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "Review candidate in the project's engineering overview on the dashboard (approval link unavailable).")
	}
	return nil
}

func baselineAdopt(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, project string) error {
	refs, _ := cmd.Flags().GetStringArray("revision")
	revisions := []map[string]any{}
	selected := []*baselineCLIRevision{}
	for _, s := range refs {
		ref, _ := baselineRef(s)
		_, r, err := baselineLookupRevision(ctx, cmd, client, project, ref)
		if err != nil {
			return err
		}
		revisions = append(revisions, map[string]any{"revision_id": r.ID, "expected_adopted_revision_id": r.Base})
		selected = append(selected, r)
	}
	action := "adopt"
	if reject, _ := cmd.Flags().GetBool("reject"); reject {
		action = "reject"
	}
	var raw json.RawMessage
	if err := client.PostJSON(ctx, "/api/projects/"+project+"/baseline/decisions", map[string]any{"action": action, "revisions": revisions}, &raw); err != nil {
		if cli.ServerErrorCode(err) == "baseline_human_required" {
			ids := make([]string, 0, len(selected))
			for _, revision := range selected {
				ids = append(ids, revision.ID)
			}
			if link := baselineApprovalURL(cmd, client, project, ids...); link != "" {
				return baselineFailure(1, "基线只能由决定人采用或驳回：%s", link)
			}
			return baselineFailure(1, "基线只能由决定人采用或驳回，请在工程总览中审批候选")
		}
		return baselineRequestError(err, true)
	}
	var result struct {
		Chapters []baselineCLIChapter `json:"chapters"`
		Open     *struct {
			InFlight  []struct{ Identifier, Title, Status string } `json:"in_flight"`
			Queued    []struct{ Identifier, Title, Status string } `json:"queued"`
			Truncated bool                                         `json:"truncated"`
		} `json:"open_issues"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Open == nil {
		return baselineFailure(1, "incomplete decision response; inspect baseline state before deciding again")
	}
	for _, r := range selected {
		baselinePrintWarnings(cmd, r.Diff)
	}
	if mustString(cmd, "output") == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), raw)
	}
	for _, r := range selected {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s:%d\n", action, r.Chapter, r.Number)
		baselinePrintDiff(cmd, r.Diff, false)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Open issues:")
	for _, group := range []struct {
		name   string
		issues []struct{ Identifier, Title, Status string }
	}{{"in_flight", result.Open.InFlight}, {"queued", result.Open.Queued}} {
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%d):\n", group.name, len(group.issues))
		for _, i := range group.issues {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s] %s\n", i.Identifier, i.Status, i.Title)
		}
	}
	if result.Open.Truncated {
		fmt.Fprintln(cmd.ErrOrStderr(), "Open issues truncated by server; more issues exist.")
	}
	return nil
}

func baselineApprovalURL(cmd *cobra.Command, client *cli.APIClient, project string, candidateIDs ...string) string {
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	origin := tryResolveAppURL(cmd)
	if origin == "" {
		var config struct {
			AppURL string `json:"daemon_app_url"`
		}
		if err := client.GetJSON(ctx, "/api/config", &config); err == nil {
			origin = strings.TrimSpace(config.AppURL)
		}
	}
	if origin == "" {
		// Only known cloud deployments have a predictable public app origin.
		// A self-hosted API URL may be an internal container address.
		switch strings.TrimRight(client.BaseURL, "/") {
		case defaultCloudServerURL:
			origin = defaultCloudAppURL
		case "https://api.multica.ai", "https://multica.ai":
			origin = "https://multica.ai"
		}
	}
	app, err := url.Parse(origin)
	if err != nil || app.Host == "" || (app.Scheme != "http" && app.Scheme != "https") || app.User != nil || app.RawQuery != "" || app.Fragment != "" {
		return ""
	}
	if client.WorkspaceID == "" {
		return ""
	}
	var workspace struct {
		Slug string `json:"slug"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+url.PathEscape(client.WorkspaceID), &workspace); err != nil || strings.TrimSpace(workspace.Slug) == "" {
		return ""
	}
	link := strings.TrimRight(origin, "/") + "/" + url.PathEscape(workspace.Slug) + "/projects/" + url.PathEscape(project) + "/engineering"
	if len(candidateIDs) > 0 {
		link += "?" + url.Values{"candidate": {candidateIDs[0]}}.Encode()
	}
	return link
}

func baselineRequestError(err error, writing bool) error {
	var e *cli.HTTPError
	if errors.As(err, &e) {
		code := cli.ServerErrorCode(err)
		if e.StatusCode == 401 {
			// Preserve the task-token stop instruction, even with ambient debug
			// enabled, without retaining a response body in the error chain.
			safe := *e
			safe.Body = ""
			return baselineFailure(1, "%s", cli.FormatError(&safe, false))
		}
		if e.StatusCode >= 500 && writing {
			return baselineFailure(1, "baseline request failed (HTTP %d); result is indeterminate. Inspect baseline state before retrying.", e.StatusCode)
		}
		exit := 1
		if e.StatusCode == 400 || e.StatusCode == 404 {
			exit = 2
		}
		detail := fmt.Sprintf("HTTP %d", e.StatusCode)
		if code != "" {
			detail += ", " + code
		}
		return baselineFailure(exit, "baseline request failed (%s); inspect baseline state before retrying", detail)
	}
	if writing {
		return baselineFailure(1, "baseline request failed; result is indeterminate. Inspect baseline state before retrying.")
	}
	return baselineFailure(1, "baseline read failed; check project reference, connection and access")
}

func baselineReadContent(path string) (string, error) {
	return readProjectInputFile(path, "content-file", 65536)
}

func readProjectInputFile(path, flag string, limit int64) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", baselineFailure(2, "cannot locate current directory")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", baselineFailure(2, "invalid --%s path", flag)
	}
	rel, err := filepath.Rel(cwd, absolute)
	if err != nil || !filepath.IsLocal(rel) {
		return "", baselineFailure(2, "--%s must be inside the current directory", flag)
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return "", baselineFailure(2, "cannot open current directory")
	}
	defer root.Close()
	info, err := root.Stat(rel)
	if err != nil || !info.Mode().IsRegular() {
		return "", baselineFailure(2, "--%s must be a regular file inside the current directory", flag)
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", baselineFailure(2, "cannot open --%s inside the current directory", flag)
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", baselineFailure(2, "--%s must be a regular file", flag)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", baselineFailure(2, "cannot read --%s", flag)
	}
	if int64(len(data)) > limit || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", baselineFailure(2, "--%s must contain at most %d bytes of UTF-8 text without NUL bytes", flag, limit)
	}
	return string(data), nil
}

func baselinePrintSelection(cmd *cobra.Command, config json.RawMessage, selected []baselineCLISelection) error {
	if mustString(cmd, "output") == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"engineering": config, "chapters": selected})
	}
	for _, item := range selected {
		r, err := baselineDecodeRevision(item.Revision)
		if err != nil {
			return err
		}
		if r == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: no revision\n", item.Chapter)
			continue
		}
		if mustString(cmd, "chapter") != "" || mustString(cmd, "revision") != "" || mustString(cmd, "output") == "markdown" {
			fmt.Fprintln(cmd.OutOrStdout(), baselineMarkdown(r))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: revision %d (%s) — %s\n", r.Chapter, r.Number, r.Status, r.Summary)
		}
	}
	return nil
}
func baselineMarkdown(r *baselineCLIRevision) string {
	when := "not adopted"
	if r.DecidedAt != nil && (r.Status == "adopted" || r.Status == "superseded") {
		when = *r.DecidedAt
	}
	return fmt.Sprintf("<!-- Revision: %d; Adopted: %s -->\n\n%s", r.Number, when, r.Content)
}
func baselinePrintWarnings(cmd *cobra.Command, d engineering.Diff) {
	for _, warning := range d.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "[BASELINE WARNING] %s [%s] %s\n", d.Chapter, warning.Anchor, warning.Code)
	}
}
func baselinePrintDiff(cmd *cobra.Command, d engineering.Diff, body bool) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s (base %d): +%d ~%d -%d; unchanged %d\n", d.Chapter, d.BaseRevisionNumber, len(d.Added), len(d.Modified), len(d.Removed), d.UnchangedCount)
	if !body {
		for _, removed := range d.Removed {
			fmt.Fprintf(w, "@@ removed [%s] %s @@\n", removed.Anchor, removed.Title)
		}
		return
	}
	for _, group := range []struct {
		kind    string
		changes []engineering.Change
	}{{"added", d.Added}, {"modified", d.Modified}, {"removed", d.Removed}} {
		for _, c := range group.changes {
			fmt.Fprintf(w, "@@ %s [%s] %s @@\n", group.kind, c.Anchor, c.Title)
			// Display the server's anchored before/after bodies verbatim by line. The
			// CLI does not recompute anchors, warnings or a competing Diff algorithm.
			if group.kind != "added" {
				for _, line := range strings.Split(c.Before, "\n") {
					fmt.Fprintf(w, "-%s\n", line)
				}
			}
			if group.kind != "removed" {
				for _, line := range strings.Split(c.After, "\n") {
					fmt.Fprintf(w, "+%s\n", line)
				}
			}
		}
	}
}

func baselineExport(cmd *cobra.Command, state baselineCLIState) error {
	filenames := []string{"01-overview.md", "02-domain-rules.md", "03-architecture.md", "04-exclusions-and-risks.md"}
	files := []string{}
	var combined strings.Builder
	dir, single := mustString(cmd, "dir"), mustString(cmd, "single-file")
	for i, chapter := range engineering.Chapters {
		r, err := baselineDecodeRevision(baselineChapter(state, chapter).Adopted)
		if err != nil {
			return err
		}
		content := "<!-- Revision: none; Adopted: never -->\n"
		if r != nil {
			content = baselineMarkdown(r) + "\n"
		}
		if single != "" {
			fmt.Fprintf(&combined, "# %s\n\n%s\n", chapter, content)
			continue
		}
		path := filepath.Join(dir, filenames[i])
		if err := baselineWriteSnapshot(path, content); err != nil {
			return err
		}
		files = append(files, path)
	}
	if single != "" {
		if err := baselineWriteSnapshot(single, combined.String()); err != nil {
			return err
		}
		files = append(files, single)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Exported %d read-only baseline snapshot(s).\n", len(files))
	if mustString(cmd, "output") == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"files": files})
	}
	for _, p := range files {
		fmt.Fprintln(cmd.OutOrStdout(), p)
	}
	return nil
}
func baselineWriteSnapshot(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return baselineFailure(1, "cannot create snapshot directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return baselineFailure(1, "cannot open snapshot directory")
	}
	defer root.Close()
	name := filepath.Base(path)
	previous, statErr := root.Lstat(name)
	if statErr == nil && !previous.Mode().IsRegular() {
		return baselineFailure(1, "snapshot target must be a regular file")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return baselineFailure(1, "cannot inspect snapshot target")
	}
	temp := ".baseline-" + rand.Text()
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return baselineFailure(1, "cannot create snapshot file")
	}
	defer root.Remove(temp)
	_, writeErr := io.WriteString(f, content)
	if writeErr == nil {
		writeErr = f.Chmod(0444)
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return baselineFailure(1, "cannot write snapshot file")
	}
	// Windows cannot replace a read-only destination. Restore its original
	// mode if replacement fails; the new snapshot is already read-only.
	if runtime.GOOS == "windows" && previous != nil {
		if err := root.Chmod(name, previous.Mode().Perm()|0200); err != nil {
			return baselineFailure(1, "cannot prepare snapshot replacement")
		}
	}
	if err := root.Rename(temp, name); err != nil {
		if runtime.GOOS == "windows" && previous != nil {
			_ = root.Chmod(name, previous.Mode().Perm())
		}
		return baselineFailure(1, "cannot replace snapshot file")
	}
	return nil
}
