package handler

import (
	"net/http"
	"strings"
)

// platformRouteCallables is the server-owned REST-to-callable contract. The
// client header identifies the caller, but never chooses which operation a
// route represents. Returning nil for a platform family with ToolBindings
// fails agent requests closed.
func platformRouteCallables(r *http.Request, capability string) []string {
	if r == nil {
		return nil
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	method := r.Method

	switch capability {
	case createIssuePlatformAction, updateIssuePlatformAction, addCommentPlatformAction:
		return []string{capability}
	case "read_issues":
		return readIssueRouteCallables(method, parts)
	case "update_comment":
		return commentRouteCallables(method, parts)
	case "schedule_agent_wakeup":
		return wakeupRouteCallables(method, parts)
	case "manage_sessions":
		return sessionRouteCallables(method, parts)
	case "manage_artifacts":
		return artifactRouteCallables(method, parts)
	case manageWorkflowsPlatformAction:
		return workflowRouteCallables(method, parts)
	default:
		return nil
	}
}

func readIssueRouteCallables(method string, parts []string) []string {
	isCerebroQuery := method == http.MethodPost && len(parts) == 4 && strings.Join(parts, "/") == "api/cerebro/issues/query"
	if method != http.MethodGet && !isCerebroQuery {
		return nil
	}
	if isCerebroQuery {
		return []string{"search_issues"}
	}
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "cerebro" && parts[2] == "issues" && parts[len(parts)-1] == "sessions" {
		return []string{"list_sessions"}
	}
	if len(parts) < 2 || parts[0] != "api" || parts[1] != "issues" {
		return nil
	}
	if len(parts) == 2 || (len(parts) == 3 && (parts[2] == "child-progress" || parts[2] == "children" || parts[2] == "grouped")) {
		return []string{"list_issues"}
	}
	if len(parts) == 3 && (parts[2] == "search" || parts[2] == "query") {
		return []string{"search_issues"}
	}
	if len(parts) >= 4 && parts[3] == "comments" {
		return []string{"list_comments"}
	}
	if len(parts) >= 4 && parts[3] == "work-sessions" {
		return []string{"list_sessions"}
	}
	return []string{"get_issue"}
}

func commentRouteCallables(method string, parts []string) []string {
	if len(parts) == 3 && parts[0] == "api" && parts[1] == "comments" && parts[2] == "move-to-thread" {
		return []string{"move_comments_to_thread"}
	}
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "comments" {
		return nil
	}
	if len(parts) == 3 {
		switch method {
		case http.MethodPut:
			return []string{"update_comment"}
		case http.MethodDelete:
			return []string{"delete_comment"}
		}
	}
	if len(parts) == 4 {
		switch parts[3] {
		case "resolve":
			if method == http.MethodPost {
				return []string{"resolve_comment"}
			}
			if method == http.MethodDelete {
				return []string{"unresolve_comment"}
			}
		case "reactions":
			if method == http.MethodPost {
				return []string{"add_comment_reaction"}
			}
			if method == http.MethodDelete {
				return []string{"remove_comment_reaction"}
			}
		case "move-to-subissue":
			if method == http.MethodPost {
				return []string{"move_comment_to_subissue"}
			}
		}
	}
	return nil
}

func wakeupRouteCallables(method string, parts []string) []string {
	if len(parts) < 3 || strings.Join(parts[:3], "/") != "api/cerebro/wakeups" {
		return nil
	}
	if method == http.MethodGet {
		return []string{"list_wakeups"}
	}
	if method == http.MethodPost && len(parts) == 3 {
		return []string{"schedule_wakeup"}
	}
	if method == http.MethodPost && len(parts) == 5 && parts[4] == "cancel" {
		return []string{"cancel_wakeup"}
	}
	return nil
}

func sessionRouteCallables(method string, parts []string) []string {
	if len(parts) < 5 || strings.Join(parts[:3], "/") != "api/cerebro/issues" || parts[4] != "sessions" {
		return nil
	}
	if method == http.MethodPost && len(parts) == 6 && parts[5] == "start-fresh" {
		return []string{"handoff_session"}
	}
	if method == http.MethodPatch && len(parts) == 6 {
		return []string{"rename_session"}
	}
	return nil
}

func artifactRouteCallables(method string, parts []string) []string {
	if method == http.MethodGet && len(parts) == 3 && strings.Join(parts[:2], "/") == "api/notes" {
		if parts[2] == "search" {
			return []string{"search_artifacts"}
		}
		if parts[2] != "recent" && parts[2] != "by-reference" {
			return []string{"get_artifact"}
		}
	}
	if method == http.MethodGet && len(parts) == 4 && parts[0] == "api" && (parts[1] == "issues" || parts[1] == "projects") && parts[3] == "artifacts" {
		return []string{"list_artifacts"}
	}
	if len(parts) == 2 && parts[0] == "api" && (parts[1] == "upload-file" || parts[1] == "artifact-uploads") && method == http.MethodPost {
		return []string{"add_attachment"}
	}
	if len(parts) == 3 && strings.Join(parts[:2], "/") == "api/attachments" && method == http.MethodDelete {
		return []string{"delete_attachment"}
	}
	if len(parts) >= 2 && strings.Join(parts[:2], "/") == "api/artifacts" {
		switch len(parts) {
		case 2:
			if method == http.MethodGet {
				return []string{"search_artifacts"}
			}
			if method == http.MethodPost {
				return []string{"create_artifact"}
			}
		case 3:
			switch method {
			case http.MethodGet:
				return []string{"get_artifact"}
			case http.MethodPut:
				return []string{"update_artifact"}
			case http.MethodDelete:
				return []string{"delete_artifact"}
			}
		case 4:
			switch parts[3] {
			case "scope":
				if method == http.MethodPut {
					return []string{"move_artifact"}
				}
			case "folder":
				if method == http.MethodPut {
					return []string{"set_artifact_folder"}
				}
			case "folder-suggestion":
				if method == http.MethodPost {
					return []string{"suggest_artifact_folder"}
				}
				if method == http.MethodGet {
					return []string{"get_artifact_folder_suggestion"}
				}
			}
		}
	}
	if len(parts) >= 2 && strings.Join(parts[:2], "/") == "api/artifact-folder-suggestions" {
		if len(parts) == 2 && method == http.MethodGet {
			return []string{"list_artifact_folder_suggestions"}
		}
		if len(parts) == 4 && method == http.MethodPost {
			if parts[3] == "accept" {
				return []string{"accept_artifact_folder_suggestion"}
			}
			if parts[3] == "reject" {
				return []string{"reject_artifact_folder_suggestion"}
			}
		}
	}
	if len(parts) >= 2 && strings.Join(parts[:2], "/") == "api/artifact-folders" {
		if len(parts) == 2 {
			if method == http.MethodGet {
				return []string{"list_artifact_folders"}
			}
			if method == http.MethodPost {
				return []string{"create_artifact_folder"}
			}
		}
		if len(parts) == 3 {
			if method == http.MethodPut {
				return []string{"update_artifact_folder"}
			}
			if method == http.MethodDelete {
				return []string{"delete_artifact_folder"}
			}
		}
		if len(parts) == 4 && parts[3] == "visibility" && method == http.MethodPut {
			return []string{"set_artifact_folder_visibility"}
		}
	}
	return nil
}

func workflowRouteCallables(method string, parts []string) []string {
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "cerebro" {
		return nil
	}
	root := parts[2]
	rest := parts[3:]
	switch root {
	case "workflows":
		if len(rest) == 2 && rest[0] == "_test" && rest[1] == "cron-sweep" && method == http.MethodPost {
			return []string{"sweep_workflow_cron"}
		}
		if len(rest) == 0 {
			if method == http.MethodGet {
				return []string{"list_workflows"}
			}
			if method == http.MethodPost {
				return []string{"create_workflow"}
			}
		}
		if len(rest) == 1 && rest[0] == "runs" && method == http.MethodGet {
			return []string{"list_workflows"}
		}
		if len(rest) == 2 && rest[0] == "for-issue" && method == http.MethodGet {
			return []string{"get_active_workflow"}
		}
		if len(rest) == 1 {
			switch method {
			case http.MethodGet:
				return []string{"get_workflow"}
			case http.MethodPut:
				return []string{"update_workflow"}
			case http.MethodDelete:
				return []string{"delete_workflow"}
			}
		}
		if len(rest) == 2 {
			switch rest[1] {
			case "toggle":
				if method == http.MethodPost {
					return []string{"toggle_workflow"}
				}
			case "activate":
				if method == http.MethodPost {
					return []string{"activate_workflow"}
				}
			case "regenerate-token":
				if method == http.MethodPost {
					return []string{"regenerate_workflow_token"}
				}
			case "regenerate-signing-secret":
				if method == http.MethodPost {
					return []string{"regenerate_workflow_signing_secret"}
				}
			case "regenerate-outbound-secret":
				if method == http.MethodPost {
					return []string{"regenerate_workflow_outbound_secret"}
				}
			case "runs", "loop-state", "loop-runs":
				if method == http.MethodGet {
					return []string{"get_workflow"}
				}
			}
		}
		if len(rest) == 4 && rest[1] == "human-checks" && rest[3] == "approve" && method == http.MethodPost {
			return []string{"approve_workflow_human_check"}
		}
	case "commands":
		if len(rest) == 0 {
			if method == http.MethodGet {
				return []string{"list_commands"}
			}
			if method == http.MethodPost {
				return []string{"create_command"}
			}
		}
		if len(rest) == 1 {
			switch method {
			case http.MethodGet:
				return []string{"get_command"}
			case http.MethodPut:
				return []string{"update_command"}
			case http.MethodDelete:
				return []string{"delete_command"}
			}
		}
	case "evals":
		if len(rest) == 0 {
			if method == http.MethodGet {
				return []string{"list_evals"}
			}
			if method == http.MethodPost {
				return []string{"create_eval"}
			}
		}
		if len(rest) == 1 && rest[0] == "bindings" {
			if method == http.MethodGet {
				return []string{"list_eval_bindings"}
			}
			if method == http.MethodPost {
				return []string{"bind_eval"}
			}
		}
		if len(rest) == 2 && rest[0] == "bindings" && method == http.MethodDelete {
			return []string{"unbind_eval"}
		}
		if len(rest) == 1 {
			switch method {
			case http.MethodGet:
				return []string{"get_eval"}
			case http.MethodPut:
				return []string{"update_eval"}
			case http.MethodDelete:
				return []string{"delete_eval"}
			}
		}
		if len(rest) == 2 {
			switch rest[1] {
			case "runs":
				if method == http.MethodGet {
					return []string{"list_eval_runs"}
				}
				if method == http.MethodPost {
					return []string{"record_eval_run"}
				}
			case "run":
				if method == http.MethodPost {
					return []string{"run_eval"}
				}
			case "schedule":
				switch method {
				case http.MethodGet:
					return []string{"get_eval_schedule"}
				case http.MethodPut:
					return []string{"set_eval_schedule"}
				case http.MethodDelete:
					return []string{"delete_eval_schedule"}
				}
			}
		}
	}
	return nil
}
