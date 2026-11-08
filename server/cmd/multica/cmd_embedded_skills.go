package main

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// These docs ship with the CLI, independently of workspace skills and login.
//
//go:embed skills/multica-cli/SKILL.md skills/multica-cli/references/*.md
var embeddedCLISkills embed.FS

func newEmbeddedSkillsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		GroupID: groupAdditional,
		Short:   "Read bundled CLI guides offline (workspace skills: multica skill)",
		Example: "  multica skills list --output json\n  multica skills read multica-cli\n  multica skills read multica-cli references/workspaces.md",
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List guides bundled with this CLI version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			files, err := fs.Glob(embeddedCLISkills, "skills/multica-cli/references/*.md")
			if err != nil {
				return err
			}
			paths := []string{"SKILL.md"}
			for _, file := range files {
				paths = append(paths, strings.TrimPrefix(file, "skills/multica-cli/"))
			}
			output, _ := cmd.Flags().GetString("output")
			switch output {
			case "json":
				return cli.PrintJSON(cmd.OutOrStdout(), []struct {
					Name        string   `json:"name"`
					Description string   `json:"description"`
					CLIVersion  string   `json:"cli_version"`
					Files       []string `json:"files"`
				}{{"multica-cli", "Operate Multica from an external agent; prepare and reuse execution workspaces", version, paths}})
			case "table":
				cli.PrintTable(cmd.OutOrStdout(), []string{"NAME", "CLI VERSION", "FILES"}, [][]string{{"multica-cli", version, strings.Join(paths, ", ")}})
				return nil
			default:
				return fmt.Errorf("unsupported output format %q: use table or json", output)
			}
		},
	}
	list.Flags().String("output", "table", "Output format: table or json")
	read := &cobra.Command{
		Use:   "read <name> [path]",
		Short: "Print a bundled SKILL.md or reference file",
		Args:  cobra.RangeArgs(1, 2),
		Long:  "Read Markdown embedded in this CLI at build time. No login, network, or skill installation is required. Paths are relative to the named skill; list available files with multica skills list --output json.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "multica-cli" {
				return fmt.Errorf("unknown bundled skill %q: run multica skills list", args[0])
			}
			file := "SKILL.md"
			if len(args) == 2 {
				file = args[1]
			}
			if !fs.ValidPath(file) || strings.Contains(file, "\\") {
				return fmt.Errorf("invalid skill path %q: use a relative path from multica skills list --output json", file)
			}
			content, err := embeddedCLISkills.ReadFile("skills/multica-cli/" + file)
			if err != nil {
				return fmt.Errorf("unknown bundled skill file %q: run multica skills list --output json", file)
			}
			output, _ := cmd.Flags().GetString("output")
			switch output {
			case "markdown":
				_, err = cmd.OutOrStdout().Write(content)
				return err
			case "json":
				return cli.PrintJSON(cmd.OutOrStdout(), struct {
					Name       string `json:"name"`
					Path       string `json:"path"`
					CLIVersion string `json:"cli_version"`
					Content    string `json:"content"`
				}{args[0], file, version, string(content)})
			default:
				return fmt.Errorf("unsupported output format %q: use markdown or json", output)
			}
		},
	}
	read.Flags().String("output", "markdown", "Output format: markdown or json")
	cmd.AddCommand(list, read)
	return cmd
}
