package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Manage project folders",
	Long: "List, create, rename, and delete the workspace's project folders, and file projects in them.\n\n" +
		"A folder holds projects one level deep, and a project is in at most one folder. " +
		"Folder arguments and --folder accept a full folder id, a unique id prefix, or the folder's name " +
		"(names are unique per workspace, case-insensitively). Creating a folder and filing a project in one " +
		"need a workspace plan that includes project folders.",
}

var projectFolderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List project folders and the projects filed in them",
	RunE:  runProjectFolderList,
}

var projectFolderCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a project folder",
	Args:  exactArgs(1),
	RunE:  runProjectFolderCreate,
}

var projectFolderRenameCmd = &cobra.Command{
	Use:   "rename <folder> <new-name>",
	Short: "Rename a project folder",
	Args:  exactArgs(2),
	RunE:  runProjectFolderRename,
}

var projectFolderDeleteCmd = &cobra.Command{
	Use:   "delete <folder>",
	Short: "Delete a project folder; its projects move back to the top level",
	Args:  exactArgs(1),
	RunE:  runProjectFolderDelete,
}

var projectFolderFileCmd = &cobra.Command{
	Use:   "file <project-id>",
	Short: "File a project in a folder, or move it to another one",
	Long: "File a project in a folder. A project already in another folder moves to this one.\n\n" +
		"<project-id> accepts a full project id or a unique prefix from `multica project list`. " +
		"To take a project out of its folder, use `multica project folder unfile`.",
	Args: exactArgs(1),
	RunE: runProjectFolderFile,
}

var projectFolderUnfileCmd = &cobra.Command{
	Use:   "unfile <project-id>",
	Short: "Move a project out of its folder, back to the top level",
	Args:  exactArgs(1),
	RunE:  runProjectFolderUnfile,
}

func init() {
	projectCmd.AddCommand(projectFolderCmd)
	projectFolderCmd.AddCommand(projectFolderListCmd, projectFolderCreateCmd, projectFolderRenameCmd,
		projectFolderDeleteCmd, projectFolderFileCmd, projectFolderUnfileCmd)

	projectFolderListCmd.Flags().String("output", "table", "Output format: table or json")
	projectFolderCreateCmd.Flags().String("output", "json", "Output format: table or json")
	projectFolderRenameCmd.Flags().String("output", "json", "Output format: table or json")
	projectFolderFileCmd.Flags().String("folder", "", "Destination folder: id, unique id prefix, or name (required)")
	projectFolderFileCmd.Flags().String("output", "json", "Output format: table or json")
	projectFolderUnfileCmd.Flags().String("output", "json", "Output format: table or json")
}

type projectFolderList struct {
	Folders []map[string]any `json:"folders"`
	Filed   []struct {
		ProjectID string `json:"project_id"`
		FolderID  string `json:"folder_id"`
	} `json:"filed"`
}

func fetchProjectFolders(ctx context.Context, client *cli.APIClient) (projectFolderList, error) {
	var list projectFolderList
	if err := client.GetJSON(ctx, "/api/project-folders", &list); err != nil {
		return list, fmt.Errorf("list project folders: %w", err)
	}
	return list, nil
}

// resolveProjectFolderRef accepts a full folder id, a unique id prefix, or a
// folder name. Names are unique per workspace regardless of case, so a name
// never matches two folders.
func resolveProjectFolderRef(ctx context.Context, client *cli.APIClient, ref string) (resolvedID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return resolvedID{}, fmt.Errorf("folder is required")
	}
	if uuidRegexp.MatchString(ref) {
		return resolvedID{ID: ref, Display: ref}, nil
	}
	list, err := fetchProjectFolders(ctx, client)
	if err != nil {
		return resolvedID{}, err
	}
	for _, f := range list.Folders {
		if strings.EqualFold(strVal(f, "name"), ref) {
			return resolvedID{ID: strVal(f, "id"), Display: strVal(f, "name")}, nil
		}
	}
	return resolveIDByPrefixWithListHint(ctx, client, "project folder", ref, "run `multica project folder list`",
		func(context.Context, *cli.APIClient) ([]idCandidate, error) {
			candidates := make([]idCandidate, 0, len(list.Folders))
			for _, f := range list.Folders {
				candidates = append(candidates, idCandidate{ID: strVal(f, "id"), Display: strVal(f, "name")})
			}
			return candidates, nil
		})
}

func printProjectFolderSummary(folder map[string]any, output string) error {
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "NAME"}, [][]string{{strVal(folder, "id"), strVal(folder, "name")}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, folder)
}

func runProjectFolderList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	list, err := fetchProjectFolders(ctx, client)
	if err != nil {
		return err
	}
	if list.Folders == nil {
		list.Folders = []map[string]any{}
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, list)
	}

	counts := map[string]int{}
	for _, f := range list.Filed {
		counts[f.FolderID]++
	}
	rows := make([][]string, 0, len(list.Folders))
	for _, f := range list.Folders {
		rows = append(rows, []string{strVal(f, "id"), strVal(f, "name"), fmt.Sprint(counts[strVal(f, "id")])})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "PROJECTS"}, rows)
	return nil
}

func runProjectFolderCreate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var folder map[string]any
	if err := client.PostJSON(ctx, "/api/project-folders", map[string]any{"name": args[0]}, &folder); err != nil {
		return fmt.Errorf("create project folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printProjectFolderSummary(folder, output)
}

func runProjectFolderRename(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ref, err := resolveProjectFolderRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	var folder map[string]any
	if err := client.PatchJSON(ctx, "/api/project-folders/"+url.PathEscape(ref.ID), map[string]any{"name": args[1]}, &folder); err != nil {
		return fmt.Errorf("rename project folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printProjectFolderSummary(folder, output)
}

func runProjectFolderDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ref, err := resolveProjectFolderRef(ctx, client, args[0])
	if err != nil {
		return err
	}
	if err := client.DeleteJSON(ctx, "/api/project-folders/"+url.PathEscape(ref.ID)); err != nil {
		return fmt.Errorf("delete project folder: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Project folder %s deleted; its projects are back at the top level.\n", ref.Display)
	return nil
}

func runProjectFolderFile(cmd *cobra.Command, args []string) error {
	destination, _ := cmd.Flags().GetString("folder")
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("--folder is required: a folder id, unique id prefix, or name (to take the project out of its folder, use `multica project folder unfile`)")
	}
	return fileProject(cmd, args[0], destination)
}

func runProjectFolderUnfile(cmd *cobra.Command, args []string) error {
	return fileProject(cmd, args[0], "")
}

// fileProject files a project in folderRef, or out of every folder when
// folderRef is empty.
func fileProject(cmd *cobra.Command, projectRef, folderRef string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	project, err := resolveProjectID(ctx, client, projectRef)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var folderID any
	if folderRef != "" {
		folder, err := resolveProjectFolderRef(ctx, client, folderRef)
		if err != nil {
			return err
		}
		folderID = folder.ID
	}
	var filed map[string]any
	if err := client.PostJSON(ctx, "/api/project-folders/file", map[string]any{"project_id": project.ID, "folder_id": folderID}, &filed); err != nil {
		return fmt.Errorf("file project: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		folder := strVal(filed, "folder_id")
		if folder == "" {
			folder = "-"
		}
		cli.PrintTable(os.Stdout, []string{"PROJECT", "FOLDER"}, [][]string{{strVal(filed, "project_id"), folder}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, filed)
}
