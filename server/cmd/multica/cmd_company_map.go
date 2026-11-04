package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var companyMapCmd = &cobra.Command{
	Use:   "map",
	Short: "Show the org map: floors and chiefs, squads and leaders, members, projects and open work",
	Long: `Show who is in the company and where work sits. Read-only and bounded: at most 200
squads, and at most 50 members per squad (members_truncated says when more exist).

Use --floor <id> to narrow to one floor, and --output json for the full structure.`,
	Args: cobra.NoArgs,
	RunE: runCompanyMap,
}

func init() {
	companyMapCmd.Flags().String("floor", "", "Show only this floor's squads (floor id)")
	companyMapCmd.Flags().String("output", "table", "Output format: table or json")
	companyCmd.AddCommand(companyMapCmd)
}

// orgMapView mirrors the JSON of GET /api/company/org-map. Only the fields the
// tables print are decoded; --output json prints the server response unchanged.
type orgMapView struct {
	Floors []struct {
		ID    string       `json:"id"`
		Name  string       `json:"name"`
		Chief *orgMapAgent `json:"chief"`
	} `json:"floors"`
	Squads []struct {
		ID         string       `json:"id"`
		Name       string       `json:"name"`
		FloorName  string       `json:"floor_name"`
		Leader     *orgMapAgent `json:"leader"`
		HasCharter bool         `json:"has_charter"`
		Project    *struct {
			Title        string `json:"title"`
			WorkflowName string `json:"workflow_name"`
		} `json:"project"`
		OpenIssues       int64 `json:"open_issues"`
		MemberCount      int64 `json:"member_count"`
		MembersTruncated bool  `json:"members_truncated"`
		Members          []struct {
			Type     string   `json:"type"`
			Name     string   `json:"name"`
			Role     string   `json:"role"`
			IsLeader bool     `json:"is_leader"`
			Skills   []string `json:"skills"`
		} `json:"members"`
	} `json:"squads"`
	SquadLimit      int  `json:"squad_limit"`
	SquadsTruncated bool `json:"squads_truncated"`
}

type orgMapAgent struct {
	Name          string `json:"name"`
	RuntimeStatus string `json:"runtime_status"`
	Archived      bool   `json:"archived"`
}

func runCompanyMap(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/company/org-map"
	if floor, _ := cmd.Flags().GetString("floor"); strings.TrimSpace(floor) != "" {
		path += "?" + url.Values{"floor_id": {strings.TrimSpace(floor)}}.Encode()
	}

	var raw json.RawMessage
	if err := client.GetJSON(ctx, path, &raw); err != nil {
		return fmt.Errorf("read org map: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, raw)
	}

	var view orgMapView
	if err := json.Unmarshal(raw, &view); err != nil {
		return fmt.Errorf("read org map: %w", err)
	}
	return printOrgMap(view)
}

func printOrgMap(v orgMapView) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "FLOOR\tCHIEF\tCHIEF RUNTIME")
	for _, f := range v.Floors {
		chief, runtime := "-", "-"
		if f.Chief != nil {
			chief, runtime = agentLabel(f.Chief), f.Chief.RuntimeStatus
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", f.Name, chief, runtime)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "SQUAD\tFLOOR\tLEADER\tLEADER RUNTIME\tPROJECT\tWORKFLOW\tOPEN\tMEMBERS\tCHARTER")
	for _, s := range v.Squads {
		leader, runtime := "-", "-"
		if s.Leader != nil {
			leader, runtime = agentLabel(s.Leader), s.Leader.RuntimeStatus
		}
		project, workflow := "-", "-"
		if s.Project != nil {
			project = s.Project.Title
			if s.Project.WorkflowName != "" {
				workflow = s.Project.WorkflowName
			}
		}
		charter := "no"
		if s.HasCharter {
			charter = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			s.Name, orDash(s.FloorName), leader, runtime, project, workflow, s.OpenIssues, s.MemberCount, charter)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "SQUAD\tTYPE\tNAME\tROLE\tSKILLS")
	for _, s := range v.Squads {
		for _, m := range s.Members {
			skills := "-"
			if len(m.Skills) > 0 {
				skills = strings.Join(m.Skills, ", ")
			}
			role := m.Role
			if m.IsLeader {
				role = strings.TrimSpace("leader " + role)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Name, m.Type, m.Name, orDash(role), skills)
		}
		if s.MembersTruncated {
			fmt.Fprintf(w, "%s\t\t(%d more members)\t\t\n", s.Name, s.MemberCount-int64(len(s.Members)))
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if v.SquadsTruncated {
		fmt.Fprintf(os.Stdout, "\nShowing the first %d squads; use --floor to narrow.\n", v.SquadLimit)
	}
	return nil
}

func agentLabel(a *orgMapAgent) string {
	if a.Archived {
		return a.Name + " (archived)"
	}
	return a.Name
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
