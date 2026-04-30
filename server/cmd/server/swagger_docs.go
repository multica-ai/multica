package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/handler"
)

type SwaggerHealthResponse struct {
	Status string `json:"status"`
}

type SwaggerMessageResponse struct {
	Message string `json:"message"`
}

type SwaggerErrorResponse struct {
	Error string `json:"error"`
}

type SwaggerIssueListResponse struct {
	Issues []handler.IssueResponse `json:"issues"`
	Total  int64                   `json:"total"`
}

type SwaggerProjectListResponse struct {
	Projects []handler.ProjectResponse `json:"projects"`
	Total    int64                     `json:"total"`
}

type SwaggerUpdateCommentRequest struct {
	Content string `json:"content"`
}

type openAPIOperationKey struct {
	Method string
	Path   string
}

type openAPIOperationOverride struct {
	Summary         string
	Description     string
	QueryParameters []openAPIParameter
	RequestType     reflect.Type
	RequestRequired bool
	Responses       map[string]openAPIResponseDoc
}

type openAPIResponseDoc struct {
	Description string
	Type        reflect.Type
}

type openAPIFieldDoc struct {
	Description string
	Example     any
	Enum        []string
}

type openAPITypeDoc struct {
	Description string
	Example     any
	Fields      map[string]openAPIFieldDoc
}

type openAPISchemaRegistry struct {
	schemas        map[string]openAPISchema
	componentNames map[reflect.Type]string
	usedNames      map[string]reflect.Type
}

var (
	issueStatusValues                 = []string{"backlog", "todo", "in_progress", "done", "cancelled"}
	issuePriorityValues               = []string{"urgent", "high", "medium", "low", "none"}
	projectStatusValues               = []string{"planned", "in_progress", "paused", "completed", "cancelled"}
	agentVisibilityValues             = []string{"private", "workspace"}
	agentStatusValues                 = []string{"idle", "working"}
	actorTypeValues                   = []string{"member", "agent"}
	projectLeadTypeValues             = []string{"member", "agent"}
	exampleUUID                       = "3fa85f64-5717-4562-b3fc-2c963f66afa6"
	workspaceResponseReflectType      = typeOf[handler.WorkspaceResponse]()
	createWorkspaceRequestReflectType = typeOf[handler.CreateWorkspaceRequest]()
	updateWorkspaceRequestReflectType = typeOf[handler.UpdateWorkspaceRequest]()
	userResponseReflectType           = typeOf[handler.UserResponse]()
	loginResponseReflectType          = typeOf[handler.LoginResponse]()
	sendCodeRequestReflectType        = typeOf[handler.SendCodeRequest]()
	verifyCodeRequestReflectType      = typeOf[handler.VerifyCodeRequest]()
	updateMeRequestReflectType        = typeOf[handler.UpdateMeRequest]()
	issueResponseReflectType          = typeOf[handler.IssueResponse]()
	createIssueRequestReflectType     = typeOf[handler.CreateIssueRequest]()
	updateIssueRequestReflectType     = typeOf[handler.UpdateIssueRequest]()
	projectResponseReflectType        = typeOf[handler.ProjectResponse]()
	createProjectRequestReflectType   = typeOf[handler.CreateProjectRequest]()
	updateProjectRequestReflectType   = typeOf[handler.UpdateProjectRequest]()
	agentResponseReflectType          = typeOf[handler.AgentResponse]()
	createAgentRequestReflectType     = typeOf[handler.CreateAgentRequest]()
	updateAgentRequestReflectType     = typeOf[handler.UpdateAgentRequest]()
	agentTaskResponseReflectType      = typeOf[handler.AgentTaskResponse]()
	commentResponseReflectType        = typeOf[handler.CommentResponse]()
	createCommentRequestReflectType   = typeOf[handler.CreateCommentRequest]()
	updateCommentRequestReflectType   = typeOf[SwaggerUpdateCommentRequest]()
	errorResponseReflectType          = typeOf[SwaggerErrorResponse]()
	messageResponseReflectType        = typeOf[SwaggerMessageResponse]()
	issueListResponseReflectType      = typeOf[SwaggerIssueListResponse]()
	projectListResponseReflectType    = typeOf[SwaggerProjectListResponse]()
)

var openAPIOperationOverrides = map[openAPIOperationKey]openAPIOperationOverride{
	{Method: "get", Path: "/health"}: {
		Summary:     "Health check",
		Description: "Returns the service health status used by local tooling and deployments.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Service is healthy.", Type: typeOf[SwaggerHealthResponse]()},
		},
	},
	{Method: "post", Path: "/auth/send-code"}: {
		Summary:         "Send verification code",
		Description:     "Sends a one-time login code to the provided email address.",
		RequestType:     sendCodeRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Verification code sent.", Type: messageResponseReflectType},
			"400": {Description: "Email is missing or invalid.", Type: errorResponseReflectType},
			"429": {Description: "Too many verification code requests.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/auth/verify-code"}: {
		Summary:         "Verify code and sign in",
		Description:     "Validates the email verification code and returns a bearer token with the signed-in user.",
		RequestType:     verifyCodeRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Verification succeeded.", Type: loginResponseReflectType},
			"400": {Description: "Verification failed or code expired.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/me"}: {
		Summary:     "Get current user",
		Description: "Returns the authenticated user profile.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Authenticated user profile.", Type: userResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"404": {Description: "User not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "patch", Path: "/api/me"}: {
		Summary:         "Update current user",
		Description:     "Updates the authenticated user's profile fields.",
		RequestType:     updateMeRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated user profile.", Type: userResponseReflectType},
			"400": {Description: "Invalid profile update payload.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"404": {Description: "User not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/workspaces"}: {
		Summary:     "List workspaces",
		Description: "Lists workspaces the authenticated user belongs to.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Workspace list.", Type: typeOf[[]handler.WorkspaceResponse]()},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/workspaces"}: {
		Summary:         "Create workspace",
		Description:     "Creates a new workspace and adds the current user as owner.",
		RequestType:     createWorkspaceRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"201": {Description: "Workspace created.", Type: workspaceResponseReflectType},
			"400": {Description: "Workspace payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"409": {Description: "Workspace slug already exists.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/workspaces/{id}"}: {
		Summary:     "Get workspace",
		Description: "Returns the requested workspace.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Workspace detail.", Type: workspaceResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"404": {Description: "Workspace not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "put", Path: "/api/workspaces/{id}"}: {
		Summary:         "Update workspace",
		Description:     "Updates workspace metadata and settings.",
		RequestType:     updateWorkspaceRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated workspace.", Type: workspaceResponseReflectType},
			"400": {Description: "Workspace payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"404": {Description: "Workspace not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "patch", Path: "/api/workspaces/{id}"}: {
		Summary:         "Patch workspace",
		Description:     "Partially updates workspace metadata and settings.",
		RequestType:     updateWorkspaceRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated workspace.", Type: workspaceResponseReflectType},
			"400": {Description: "Workspace payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"404": {Description: "Workspace not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/issues"}: {
		Summary:         "List issues",
		Description:     "Lists issues in the selected workspace with optional filters.",
		QueryParameters: issueListQueryParameters(),
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Issue list with total count.", Type: issueListResponseReflectType},
			"400": {Description: "One or more issue filters are invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/issues"}: {
		Summary:         "Create issue",
		Description:     "Creates a new issue in the selected workspace.",
		RequestType:     createIssueRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"201": {Description: "Issue created.", Type: issueResponseReflectType},
			"400": {Description: "Issue payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied or agent assignment is not allowed.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/issues/{id}"}: {
		Summary:     "Get issue",
		Description: "Returns the full issue detail payload.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Issue detail.", Type: issueResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Issue not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "put", Path: "/api/issues/{id}"}: {
		Summary:         "Update issue",
		Description:     "Updates issue fields such as title, status, assignee, project, and schedule.",
		RequestType:     updateIssueRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated issue detail.", Type: issueResponseReflectType},
			"400": {Description: "Issue update payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Issue not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "delete", Path: "/api/issues/{id}"}: {
		Summary:     "Delete issue",
		Description: "Deletes the requested issue.",
		Responses: map[string]openAPIResponseDoc{
			"204": {Description: "Issue deleted."},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Issue not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/issues/{id}/comments"}: {
		Summary:         "List issue comments",
		Description:     "Lists comments for an issue, optionally paginated or filtered by creation time.",
		QueryParameters: commentListQueryParameters(),
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Comment list.", Type: typeOf[[]handler.CommentResponse]()},
			"400": {Description: "Comment query parameters are invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Issue not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/issues/{id}/comments"}: {
		Summary:         "Create issue comment",
		Description:     "Creates a new comment on the issue and optionally links uploaded attachments.",
		RequestType:     createCommentRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"201": {Description: "Comment created.", Type: commentResponseReflectType},
			"400": {Description: "Comment payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Issue not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "put", Path: "/api/comments/{commentId}"}: {
		Summary:         "Update comment",
		Description:     "Updates an existing comment. Only the author or a workspace admin can edit it.",
		RequestType:     updateCommentRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated comment.", Type: commentResponseReflectType},
			"400": {Description: "Comment payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Only the comment author or an admin can edit the comment.", Type: errorResponseReflectType},
			"404": {Description: "Comment not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "delete", Path: "/api/comments/{commentId}"}: {
		Summary:     "Delete comment",
		Description: "Deletes a comment and its linked attachments. Only the author or a workspace admin can delete it.",
		Responses: map[string]openAPIResponseDoc{
			"204": {Description: "Comment deleted."},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Only the comment author or an admin can delete the comment.", Type: errorResponseReflectType},
			"404": {Description: "Comment not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/projects"}: {
		Summary:         "List projects",
		Description:     "Lists projects in the selected workspace.",
		QueryParameters: projectListQueryParameters(),
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Project list with total count.", Type: projectListResponseReflectType},
			"400": {Description: "Project query parameters are invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/projects"}: {
		Summary:         "Create project",
		Description:     "Creates a project in the selected workspace.",
		RequestType:     createProjectRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"201": {Description: "Project created.", Type: projectResponseReflectType},
			"400": {Description: "Project payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/projects/{id}"}: {
		Summary:     "Get project",
		Description: "Returns a project in the selected workspace.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Project detail.", Type: projectResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Project not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "put", Path: "/api/projects/{id}"}: {
		Summary:         "Update project",
		Description:     "Updates project fields such as title, status, icon, and lead.",
		RequestType:     updateProjectRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated project.", Type: projectResponseReflectType},
			"400": {Description: "Project payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Project not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "delete", Path: "/api/projects/{id}"}: {
		Summary:     "Delete project",
		Description: "Deletes a project from the selected workspace.",
		Responses: map[string]openAPIResponseDoc{
			"204": {Description: "Project deleted."},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Project not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/agents"}: {
		Summary:         "List agents",
		Description:     "Lists agents visible to the current workspace member.",
		QueryParameters: agentListQueryParameters(),
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Agent list.", Type: typeOf[[]handler.AgentResponse]()},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/agents"}: {
		Summary:         "Create agent",
		Description:     "Creates an agent bound to a runtime in the selected workspace.",
		RequestType:     createAgentRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"201": {Description: "Agent created.", Type: agentResponseReflectType},
			"400": {Description: "Agent payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Only workspace owners or admins can create agents.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/agents/{id}"}: {
		Summary:     "Get agent",
		Description: "Returns an agent and its linked skills.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Agent detail.", Type: agentResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Agent not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "put", Path: "/api/agents/{id}"}: {
		Summary:         "Update agent",
		Description:     "Updates an agent. Only the owner or a workspace admin can manage it.",
		RequestType:     updateAgentRequestReflectType,
		RequestRequired: true,
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Updated agent.", Type: agentResponseReflectType},
			"400": {Description: "Agent payload is invalid.", Type: errorResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Only the agent owner or a workspace admin can manage the agent.", Type: errorResponseReflectType},
			"404": {Description: "Agent not found.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/agents/{id}/archive"}: {
		Summary:     "Archive agent",
		Description: "Archives an agent and cancels its pending and active tasks.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Archived agent.", Type: agentResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Only the agent owner or a workspace admin can manage the agent.", Type: errorResponseReflectType},
			"404": {Description: "Agent not found.", Type: errorResponseReflectType},
			"409": {Description: "Agent is already archived.", Type: errorResponseReflectType},
		},
	},
	{Method: "post", Path: "/api/agents/{id}/restore"}: {
		Summary:     "Restore agent",
		Description: "Restores a previously archived agent.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Restored agent.", Type: agentResponseReflectType},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Only the agent owner or a workspace admin can manage the agent.", Type: errorResponseReflectType},
			"404": {Description: "Agent not found.", Type: errorResponseReflectType},
			"409": {Description: "Agent is not archived.", Type: errorResponseReflectType},
		},
	},
	{Method: "get", Path: "/api/agents/{id}/tasks"}: {
		Summary:     "List agent tasks",
		Description: "Lists tasks that have been queued or executed for the agent.",
		Responses: map[string]openAPIResponseDoc{
			"200": {Description: "Agent task list.", Type: typeOf[[]handler.AgentTaskResponse]()},
			"401": {Description: "Authentication required.", Type: errorResponseReflectType},
			"403": {Description: "Workspace access denied.", Type: errorResponseReflectType},
			"404": {Description: "Agent not found.", Type: errorResponseReflectType},
		},
	},
}

var openAPITypeDocs = map[reflect.Type]openAPITypeDoc{
	typeOf[SwaggerHealthResponse](): {
		Description: "Health check response.",
		Example:     map[string]any{"status": "ok"},
		Fields: map[string]openAPIFieldDoc{
			"status": {Description: "Current health status.", Example: "ok"},
		},
	},
	typeOf[SwaggerMessageResponse](): {
		Description: "Generic message response.",
		Example:     map[string]any{"message": "Verification code sent"},
		Fields: map[string]openAPIFieldDoc{
			"message": {Description: "Human-readable status message.", Example: "Verification code sent"},
		},
	},
	typeOf[SwaggerErrorResponse](): {
		Description: "Standard API error payload.",
		Example:     map[string]any{"error": "invalid request body"},
		Fields: map[string]openAPIFieldDoc{
			"error": {Description: "Human-readable error message.", Example: "invalid request body"},
		},
	},
	sendCodeRequestReflectType: {
		Description: "Payload for requesting an email verification code.",
		Example:     map[string]any{"email": "alice@example.com"},
		Fields: map[string]openAPIFieldDoc{
			"email": {Description: "Email address that should receive the verification code.", Example: "alice@example.com"},
		},
	},
	verifyCodeRequestReflectType: {
		Description: "Payload for completing email verification and sign-in.",
		Example:     map[string]any{"email": "alice@example.com", "code": "123456"},
		Fields: map[string]openAPIFieldDoc{
			"email": {Description: "Email address used during the send-code step.", Example: "alice@example.com"},
			"code":  {Description: "Six-digit verification code sent to the email address.", Example: "123456"},
		},
	},
	userResponseReflectType: {
		Description: "Authenticated user profile.",
		Example: map[string]any{
			"id":         exampleUUID,
			"name":       "Alice Chen",
			"email":      "alice@example.com",
			"avatar_url": "https://cdn.multica.ai/avatars/alice.png",
			"created_at": "2026-05-01T09:00:00Z",
			"updated_at": "2026-05-01T09:30:00Z",
		},
		Fields: map[string]openAPIFieldDoc{
			"id":         {Description: "User identifier.", Example: exampleUUID},
			"name":       {Description: "Display name shown across the workspace.", Example: "Alice Chen"},
			"email":      {Description: "Primary email address used for authentication.", Example: "alice@example.com"},
			"avatar_url": {Description: "Avatar image URL, if set.", Example: "https://cdn.multica.ai/avatars/alice.png"},
			"created_at": {Description: "Timestamp when the user was created.", Example: "2026-05-01T09:00:00Z"},
			"updated_at": {Description: "Timestamp when the user profile was last updated.", Example: "2026-05-01T09:30:00Z"},
		},
	},
	loginResponseReflectType: {
		Description: "Login response containing a bearer token and the authenticated user.",
		Example: map[string]any{
			"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
			"user": map[string]any{
				"id":    exampleUUID,
				"name":  "Alice Chen",
				"email": "alice@example.com",
			},
		},
		Fields: map[string]openAPIFieldDoc{
			"token": {Description: "JWT bearer token for authenticated API requests.", Example: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."},
			"user":  {Description: "Authenticated user profile returned after verification."},
		},
	},
	updateMeRequestReflectType: {
		Description: "Fields that can be changed on the authenticated user profile.",
		Example:     map[string]any{"name": "Alice Chen", "avatar_url": "https://cdn.multica.ai/avatars/alice.png"},
		Fields: map[string]openAPIFieldDoc{
			"name":       {Description: "New display name for the current user.", Example: "Alice Chen"},
			"avatar_url": {Description: "New avatar URL for the current user.", Example: "https://cdn.multica.ai/avatars/alice.png"},
		},
	},
	workspaceResponseReflectType: {
		Description: "Workspace detail response.",
		Example: map[string]any{
			"id":           exampleUUID,
			"name":         "Design Systems",
			"slug":         "design-systems",
			"description":  "Workspace for the design systems team.",
			"issue_prefix": "DS",
			"created_at":   "2026-05-01T09:00:00Z",
			"updated_at":   "2026-05-01T09:30:00Z",
		},
		Fields: map[string]openAPIFieldDoc{
			"id":           {Description: "Workspace identifier.", Example: exampleUUID},
			"name":         {Description: "Workspace name shown in the app.", Example: "Design Systems"},
			"slug":         {Description: "Unique workspace slug used in URLs and provisioning.", Example: "design-systems"},
			"description":  {Description: "Optional workspace description.", Example: "Workspace for the design systems team."},
			"context":      {Description: "Optional AI context used by agents in this workspace.", Example: "Design tokens and component contracts live here."},
			"settings":     {Description: "Workspace settings blob.", Example: map[string]any{"notifications": true}},
			"repos":        {Description: "Repository metadata linked to the workspace.", Example: []any{map[string]any{"url": "https://github.com/multica-ai/multica"}}},
			"issue_prefix": {Description: "Issue identifier prefix used when generating issue keys.", Example: "DS"},
			"created_at":   {Description: "Timestamp when the workspace was created.", Example: "2026-05-01T09:00:00Z"},
			"updated_at":   {Description: "Timestamp when the workspace was last updated.", Example: "2026-05-01T09:30:00Z"},
		},
	},
	createWorkspaceRequestReflectType: {
		Description: "Payload for creating a workspace.",
		Example: map[string]any{
			"name":         "Design Systems",
			"slug":         "design-systems",
			"description":  "Workspace for the design systems team.",
			"issue_prefix": "DS",
		},
		Fields: map[string]openAPIFieldDoc{
			"name":         {Description: "Workspace name.", Example: "Design Systems"},
			"slug":         {Description: "Unique, URL-safe workspace slug.", Example: "design-systems"},
			"description":  {Description: "Optional workspace description.", Example: "Workspace for the design systems team."},
			"context":      {Description: "Optional AI context for the workspace.", Example: "Keep naming consistent with our Figma library."},
			"issue_prefix": {Description: "Optional issue key prefix. Defaults from the workspace name.", Example: "DS"},
		},
	},
	updateWorkspaceRequestReflectType: {
		Description: "Partial workspace update payload.",
		Example: map[string]any{
			"description": "Workspace for the design systems team.",
			"issue_prefix": "DS",
		},
		Fields: map[string]openAPIFieldDoc{
			"name":         {Description: "Updated workspace name.", Example: "Design Systems"},
			"description":  {Description: "Updated workspace description.", Example: "Workspace for the design systems team."},
			"context":      {Description: "Updated AI context.", Example: "Prefer explicit API contracts over prose."},
			"settings":     {Description: "Updated workspace settings blob.", Example: map[string]any{"notifications": true}},
			"repos":        {Description: "Updated repository metadata list.", Example: []any{map[string]any{"url": "https://github.com/multica-ai/multica"}}},
			"issue_prefix": {Description: "Updated issue key prefix.", Example: "DS"},
		},
	},
	issueListResponseReflectType: {
		Description: "Paginated issue list response.",
		Example: map[string]any{
			"issues": []any{map[string]any{"id": exampleUUID, "identifier": "MUL-42", "title": "Ship Swagger docs", "status": "todo", "priority": "high"}},
			"total":  1,
		},
		Fields: map[string]openAPIFieldDoc{
			"issues": {Description: "Issues matching the query filters."},
			"total":  {Description: "Total number of matching issues before pagination.", Example: 1},
		},
	},
	issueResponseReflectType: {
		Description: "Issue detail response.",
		Example: map[string]any{
			"id":          exampleUUID,
			"identifier":  "MUL-42",
			"title":       "Ship Swagger docs",
			"status":      "todo",
			"priority":    "high",
			"workspace_id": exampleUUID,
		},
		Fields: map[string]openAPIFieldDoc{
			"id":              {Description: "Issue identifier.", Example: exampleUUID},
			"workspace_id":    {Description: "Workspace that owns the issue.", Example: exampleUUID},
			"number":          {Description: "Monotonic issue number inside the workspace.", Example: 42},
			"identifier":      {Description: "Human-friendly issue key using the workspace prefix.", Example: "MUL-42"},
			"title":           {Description: "Issue title.", Example: "Ship Swagger docs"},
			"description":     {Description: "Optional long-form issue description.", Example: "Add Swagger UI and generated OpenAPI docs for the Go server."},
			"status":          {Description: "Current workflow status.", Example: "todo", Enum: issueStatusValues},
			"priority":        {Description: "Current issue priority.", Example: "high", Enum: issuePriorityValues},
			"assignee_type":   {Description: "Whether the assignee is a member or an agent.", Example: "member", Enum: actorTypeValues},
			"assignee_id":     {Description: "Assignee identifier.", Example: exampleUUID},
			"creator_type":    {Description: "Whether the creator is a member or an agent.", Example: "member", Enum: actorTypeValues},
			"creator_id":      {Description: "Issue creator identifier.", Example: exampleUUID},
			"parent_issue_id": {Description: "Parent issue identifier when this issue is nested.", Example: exampleUUID},
			"project_id":      {Description: "Linked project identifier.", Example: exampleUUID},
			"position":        {Description: "Ordering position used by list and board views.", Example: 1000.0},
			"due_date":        {Description: "Due date in RFC3339 format.", Example: "2026-05-10T17:00:00Z"},
			"start_date":      {Description: "Start date in RFC3339 format.", Example: "2026-05-08T09:00:00Z"},
			"end_date":        {Description: "End date in RFC3339 format.", Example: "2026-05-12T18:00:00Z"},
			"created_at":      {Description: "Timestamp when the issue was created.", Example: "2026-05-01T09:00:00Z"},
			"updated_at":      {Description: "Timestamp when the issue was last updated.", Example: "2026-05-01T09:30:00Z"},
		},
	},
	createIssueRequestReflectType: {
		Description: "Payload for creating a new issue.",
		Example: map[string]any{
			"title":      "Ship Swagger docs",
			"description": "Add Swagger UI and generated OpenAPI docs for the Go server.",
			"status":     "todo",
			"priority":   "high",
		},
		Fields: map[string]openAPIFieldDoc{
			"title":           {Description: "Issue title.", Example: "Ship Swagger docs"},
			"description":     {Description: "Optional issue description.", Example: "Add Swagger UI and generated OpenAPI docs for the Go server."},
			"status":          {Description: "Initial workflow status. Defaults to backlog when omitted.", Example: "todo", Enum: issueStatusValues},
			"priority":        {Description: "Initial issue priority. Defaults to none when omitted.", Example: "high", Enum: issuePriorityValues},
			"assignee_type":   {Description: "Whether the assignee is a member or an agent.", Example: "member", Enum: actorTypeValues},
			"assignee_id":     {Description: "Assignee identifier.", Example: exampleUUID},
			"parent_issue_id": {Description: "Optional parent issue identifier.", Example: exampleUUID},
			"project_id":      {Description: "Optional linked project identifier.", Example: exampleUUID},
			"due_date":        {Description: "Optional due date in RFC3339 format.", Example: "2026-05-10T17:00:00Z"},
			"start_date":      {Description: "Optional start date in RFC3339 format.", Example: "2026-05-08T09:00:00Z"},
			"end_date":        {Description: "Optional end date in RFC3339 format.", Example: "2026-05-12T18:00:00Z"},
		},
	},
	updateIssueRequestReflectType: {
		Description: "Partial issue update payload.",
		Example:     map[string]any{"status": "in_progress", "priority": "urgent", "assignee_type": "agent", "assignee_id": exampleUUID},
		Fields: map[string]openAPIFieldDoc{
			"title":           {Description: "Updated issue title.", Example: "Ship Swagger docs"},
			"description":     {Description: "Updated issue description.", Example: "Add richer request and response schemas to the server Swagger output."},
			"status":          {Description: "Updated workflow status.", Example: "in_progress", Enum: issueStatusValues},
			"priority":        {Description: "Updated issue priority.", Example: "urgent", Enum: issuePriorityValues},
			"assignee_type":   {Description: "Updated assignee type.", Example: "agent", Enum: actorTypeValues},
			"assignee_id":     {Description: "Updated assignee identifier.", Example: exampleUUID},
			"parent_issue_id": {Description: "Updated parent issue identifier.", Example: exampleUUID},
			"position":        {Description: "Updated ordering position.", Example: 1001.0},
			"project_id":      {Description: "Updated linked project identifier.", Example: exampleUUID},
			"due_date":        {Description: "Updated due date in RFC3339 format.", Example: "2026-05-10T17:00:00Z"},
			"start_date":      {Description: "Updated start date in RFC3339 format.", Example: "2026-05-08T09:00:00Z"},
			"end_date":        {Description: "Updated end date in RFC3339 format.", Example: "2026-05-12T18:00:00Z"},
		},
	},
	projectListResponseReflectType: {
		Description: "Project list response.",
		Example: map[string]any{
			"projects": []any{map[string]any{"id": exampleUUID, "title": "Q2 Platform", "status": "in_progress"}},
			"total":    1,
		},
		Fields: map[string]openAPIFieldDoc{
			"projects": {Description: "Projects matching the query filters."},
			"total":    {Description: "Total number of matching projects.", Example: 1},
		},
	},
	projectResponseReflectType: {
		Description: "Project detail response.",
		Example: map[string]any{
			"id":           exampleUUID,
			"workspace_id": exampleUUID,
			"title":        "Q2 Platform",
			"status":       "in_progress",
		},
		Fields: map[string]openAPIFieldDoc{
			"id":           {Description: "Project identifier.", Example: exampleUUID},
			"workspace_id": {Description: "Workspace that owns the project.", Example: exampleUUID},
			"title":        {Description: "Project title.", Example: "Q2 Platform"},
			"description":  {Description: "Optional project description.", Example: "Initiatives for the Q2 platform roadmap."},
			"icon":         {Description: "Optional emoji or icon token for the project.", Example: "rocket"},
			"status":       {Description: "Project lifecycle status.", Example: "in_progress", Enum: projectStatusValues},
			"lead_type":    {Description: "Whether the lead is a member or an agent.", Example: "member", Enum: projectLeadTypeValues},
			"lead_id":      {Description: "Lead identifier.", Example: exampleUUID},
			"created_at":   {Description: "Timestamp when the project was created.", Example: "2026-05-01T09:00:00Z"},
			"updated_at":   {Description: "Timestamp when the project was last updated.", Example: "2026-05-01T09:30:00Z"},
		},
	},
	createProjectRequestReflectType: {
		Description: "Payload for creating a project.",
		Example:     map[string]any{"title": "Q2 Platform", "status": "planned", "lead_type": "member", "lead_id": exampleUUID},
		Fields: map[string]openAPIFieldDoc{
			"title":       {Description: "Project title.", Example: "Q2 Platform"},
			"description": {Description: "Optional project description.", Example: "Initiatives for the Q2 platform roadmap."},
			"icon":        {Description: "Optional emoji or icon token for the project.", Example: "rocket"},
			"status":      {Description: "Initial project status. Defaults to planned when omitted.", Example: "planned", Enum: projectStatusValues},
			"lead_type":   {Description: "Whether the project lead is a member or an agent.", Example: "member", Enum: projectLeadTypeValues},
			"lead_id":     {Description: "Identifier of the project lead.", Example: exampleUUID},
		},
	},
	updateProjectRequestReflectType: {
		Description: "Partial project update payload.",
		Example:     map[string]any{"status": "in_progress", "icon": "tools"},
		Fields: map[string]openAPIFieldDoc{
			"title":       {Description: "Updated project title.", Example: "Q2 Platform"},
			"description": {Description: "Updated project description.", Example: "Execution work for the Q2 platform roadmap."},
			"icon":        {Description: "Updated emoji or icon token.", Example: "tools"},
			"status":      {Description: "Updated project status.", Example: "in_progress", Enum: projectStatusValues},
			"lead_type":   {Description: "Updated project lead type.", Example: "agent", Enum: projectLeadTypeValues},
			"lead_id":     {Description: "Updated lead identifier.", Example: exampleUUID},
		},
	},
	agentResponseReflectType: {
		Description: "Agent detail response.",
		Example: map[string]any{
			"id":                   exampleUUID,
			"workspace_id":         exampleUUID,
			"runtime_id":           exampleUUID,
			"name":                 "Release Bot",
			"visibility":           "private",
			"status":               "idle",
			"max_concurrent_tasks": 6,
		},
		Fields: map[string]openAPIFieldDoc{
			"id":                   {Description: "Agent identifier.", Example: exampleUUID},
			"workspace_id":         {Description: "Workspace that owns the agent.", Example: exampleUUID},
			"runtime_id":           {Description: "Runtime used to execute the agent.", Example: exampleUUID},
			"name":                 {Description: "Agent display name.", Example: "Release Bot"},
			"description":          {Description: "Short summary of what the agent does.", Example: "Coordinates release preparation tasks."},
			"instructions":         {Description: "Long-form system instructions used when the agent runs.", Example: "Always summarize risky changes before applying them."},
			"avatar_url":           {Description: "Optional avatar image URL.", Example: "https://cdn.multica.ai/avatars/release-bot.png"},
			"runtime_mode":         {Description: "Runtime execution mode reported by the selected runtime.", Example: "local"},
			"runtime_config":       {Description: "Runtime-specific configuration blob.", Example: map[string]any{"provider": "codex"}},
			"visibility":           {Description: "Agent visibility within the workspace.", Example: "private", Enum: agentVisibilityValues},
			"status":               {Description: "Current availability state derived from running tasks.", Example: "idle", Enum: agentStatusValues},
			"max_concurrent_tasks": {Description: "Maximum tasks the agent can work on concurrently.", Example: 6},
			"owner_id":             {Description: "Owner member identifier for private agents.", Example: exampleUUID},
			"skills":               {Description: "Skills attached to the agent."},
			"tools":                {Description: "Tool access definition for the agent.", Example: []any{"terminal", "fs"}},
			"triggers":             {Description: "Automatic trigger configuration.", Example: []any{map[string]any{"type": "on_assign", "enabled": true}}},
			"created_at":           {Description: "Timestamp when the agent was created.", Example: "2026-05-01T09:00:00Z"},
			"updated_at":           {Description: "Timestamp when the agent was last updated.", Example: "2026-05-01T09:30:00Z"},
			"archived_at":          {Description: "Timestamp when the agent was archived, if archived.", Example: "2026-05-10T12:00:00Z"},
			"archived_by":          {Description: "Member identifier that archived the agent.", Example: exampleUUID},
		},
	},
	createAgentRequestReflectType: {
		Description: "Payload for creating an agent.",
		Example: map[string]any{
			"name":                 "Release Bot",
			"description":          "Coordinates release preparation tasks.",
			"instructions":         "Summarize risky changes before applying them.",
			"runtime_id":           exampleUUID,
			"visibility":           "private",
			"max_concurrent_tasks": 6,
		},
		Fields: map[string]openAPIFieldDoc{
			"name":                 {Description: "Agent display name.", Example: "Release Bot"},
			"description":          {Description: "Short summary of the agent's purpose.", Example: "Coordinates release preparation tasks."},
			"instructions":         {Description: "System instructions used for agent runs.", Example: "Summarize risky changes before applying them."},
			"avatar_url":           {Description: "Optional avatar image URL.", Example: "https://cdn.multica.ai/avatars/release-bot.png"},
			"runtime_id":           {Description: "Identifier of the runtime that should execute the agent.", Example: exampleUUID},
			"runtime_config":       {Description: "Runtime-specific configuration blob.", Example: map[string]any{"provider": "codex"}},
			"visibility":           {Description: "Agent visibility within the workspace. Defaults to private.", Example: "private", Enum: agentVisibilityValues},
			"max_concurrent_tasks": {Description: "Maximum tasks the agent can process concurrently. Defaults to 6.", Example: 6},
			"tools":                {Description: "Tool access definition for the agent.", Example: []any{"terminal", "fs"}},
			"triggers":             {Description: "Automatic trigger configuration.", Example: []any{map[string]any{"type": "on_assign", "enabled": true}}},
		},
	},
	updateAgentRequestReflectType: {
		Description: "Partial agent update payload.",
		Example:     map[string]any{"visibility": "workspace", "status": "working", "max_concurrent_tasks": 8},
		Fields: map[string]openAPIFieldDoc{
			"name":                 {Description: "Updated agent display name.", Example: "Release Bot"},
			"description":          {Description: "Updated agent description.", Example: "Coordinates release preparation tasks."},
			"instructions":         {Description: "Updated agent instructions.", Example: "Always summarize risky changes before applying them."},
			"avatar_url":           {Description: "Updated avatar image URL.", Example: "https://cdn.multica.ai/avatars/release-bot.png"},
			"runtime_id":           {Description: "Updated runtime identifier.", Example: exampleUUID},
			"runtime_config":       {Description: "Updated runtime-specific configuration.", Example: map[string]any{"provider": "codex"}},
			"visibility":           {Description: "Updated visibility.", Example: "workspace", Enum: agentVisibilityValues},
			"status":               {Description: "Updated status. Usually managed automatically from task activity.", Example: "working", Enum: agentStatusValues},
			"max_concurrent_tasks": {Description: "Updated concurrent task limit.", Example: 8},
			"tools":                {Description: "Updated tool access definition.", Example: []any{"terminal", "fs", "browser"}},
			"triggers":             {Description: "Updated automatic trigger configuration.", Example: []any{map[string]any{"type": "on_comment", "enabled": false}}},
		},
	},
	agentTaskResponseReflectType: {
		Description: "Agent task queue item response.",
		Example: map[string]any{
			"id":          exampleUUID,
			"agent_id":    exampleUUID,
			"issue_id":    exampleUUID,
			"status":      "running",
			"priority":    100,
			"created_at":  "2026-05-01T09:00:00Z",
		},
		Fields: map[string]openAPIFieldDoc{
			"id":                 {Description: "Task identifier.", Example: exampleUUID},
			"agent_id":           {Description: "Agent identifier.", Example: exampleUUID},
			"runtime_id":         {Description: "Runtime identifier that executes the task.", Example: exampleUUID},
			"issue_id":           {Description: "Issue identifier linked to the task.", Example: exampleUUID},
			"workspace_id":       {Description: "Workspace identifier linked to the task.", Example: exampleUUID},
			"status":             {Description: "Task lifecycle status.", Example: "running"},
			"priority":           {Description: "Task priority score.", Example: 100},
			"dispatched_at":      {Description: "Timestamp when the task was dispatched to a runtime.", Example: "2026-05-01T09:01:00Z"},
			"started_at":         {Description: "Timestamp when execution started.", Example: "2026-05-01T09:02:00Z"},
			"completed_at":       {Description: "Timestamp when execution completed.", Example: "2026-05-01T09:05:00Z"},
			"result":             {Description: "Structured task result payload.", Example: map[string]any{"summary": "Done"}},
			"error":              {Description: "Task failure message, if execution failed.", Example: "command timed out"},
			"agent":              {Description: "Optional embedded agent data for claim responses."},
			"repos":              {Description: "Repositories used to prepare the task environment."},
			"created_at":         {Description: "Timestamp when the task was created.", Example: "2026-05-01T09:00:00Z"},
			"prior_session_id":   {Description: "Previous agent session ID for the same issue.", Example: "session_abc123"},
			"prior_work_dir":     {Description: "Working directory from the previous related task.", Example: "/tmp/multica/worktrees/MUL-42"},
			"trigger_comment_id": {Description: "Comment identifier that triggered the task.", Example: exampleUUID},
		},
	},
	commentResponseReflectType: {
		Description: "Issue comment response.",
		Example: map[string]any{
			"id":          exampleUUID,
			"issue_id":    exampleUUID,
			"author_type": "member",
			"author_id":   exampleUUID,
			"content":     "Investigated the auth flow; root cause is session expiration.",
			"type":        "comment",
		},
		Fields: map[string]openAPIFieldDoc{
			"id":          {Description: "Comment identifier.", Example: exampleUUID},
			"issue_id":    {Description: "Issue identifier the comment belongs to.", Example: exampleUUID},
			"author_type": {Description: "Whether the comment author is a member or an agent.", Example: "member", Enum: actorTypeValues},
			"author_id":   {Description: "Comment author identifier.", Example: exampleUUID},
			"content":     {Description: "Comment body in Markdown-compatible plain text.", Example: "Investigated the auth flow; root cause is session expiration."},
			"type":        {Description: "Comment type. Defaults to comment.", Example: "comment"},
			"parent_id":   {Description: "Parent comment identifier when this is a threaded reply.", Example: exampleUUID},
			"created_at":  {Description: "Timestamp when the comment was created.", Example: "2026-05-01T09:00:00Z"},
			"updated_at":  {Description: "Timestamp when the comment was last updated.", Example: "2026-05-01T09:30:00Z"},
			"reactions":   {Description: "Reactions attached to the comment."},
			"attachments": {Description: "Attachments linked to the comment."},
		},
	},
	createCommentRequestReflectType: {
		Description: "Payload for creating an issue comment.",
		Example: map[string]any{
			"content":        "Investigated the auth flow; root cause is session expiration.",
			"type":           "comment",
			"attachment_ids": []string{exampleUUID},
		},
		Fields: map[string]openAPIFieldDoc{
			"content":        {Description: "Comment body.", Example: "Investigated the auth flow; root cause is session expiration."},
			"type":           {Description: "Comment type. Defaults to comment when omitted.", Example: "comment"},
			"parent_id":      {Description: "Optional parent comment ID for threaded replies.", Example: exampleUUID},
			"attachment_ids": {Description: "Uploaded attachment IDs to link to the comment.", Example: []string{exampleUUID}},
		},
	},
	updateCommentRequestReflectType: {
		Description: "Payload for editing an existing comment.",
		Example:     map[string]any{"content": "Updated summary after confirming the fix in staging."},
		Fields: map[string]openAPIFieldDoc{
			"content": {Description: "Updated comment body.", Example: "Updated summary after confirming the fix in staging."},
		},
	},
}

func newOpenAPISchemaRegistry() *openAPISchemaRegistry {
	return &openAPISchemaRegistry{
		schemas:        map[string]openAPISchema{},
		componentNames: map[reflect.Type]string{},
		usedNames:      map[string]reflect.Type{},
	}
}

func (r *openAPISchemaRegistry) requestBodyForType(t reflect.Type, required bool) *openAPIRequestBody {
	if t == nil {
		return nil
	}

	return &openAPIRequestBody{
		Required: required,
		Content: map[string]openAPIMediaType{
			"application/json": {
				Schema: r.schemaForType(t, ""),
			},
		},
	}
}

func (r *openAPISchemaRegistry) responseForDoc(doc openAPIResponseDoc) openAPIResponse {
	response := openAPIResponse{Description: doc.Description}
	if doc.Type == nil {
		return response
	}

	response.Content = map[string]openAPIMediaType{
		"application/json": {
			Schema: r.schemaForType(doc.Type, ""),
		},
	}
	return response
}

func (r *openAPISchemaRegistry) errorResponse(description string) openAPIResponse {
	return r.responseForDoc(openAPIResponseDoc{Description: description, Type: errorResponseReflectType})
}

func (r *openAPISchemaRegistry) schemaForType(t reflect.Type, jsonName string) openAPISchema {
	if t == nil {
		return openAPISchema{}
	}

	nullable := false
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
		nullable = true
	}

	schema := r.nonNullableSchemaForType(t, jsonName)
	if nullable {
		schema.Nullable = true
	}
	return schema
}

func (r *openAPISchemaRegistry) nonNullableSchemaForType(t reflect.Type, jsonName string) openAPISchema {
	if t == nil {
		return openAPISchema{}
	}

	if isTimeType(t) {
		return openAPISchema{Type: "string", Format: "date-time"}
	}

	switch t.Kind() {
	case reflect.Struct:
		return r.schemaRefForComponent(t)
	case reflect.Slice, reflect.Array:
		items := r.schemaForType(t.Elem(), jsonName)
		return openAPISchema{Type: "array", Items: &items}
	case reflect.Map:
		return mapSchemaForType(r, t)
	case reflect.Interface:
		return interfaceSchemaForJSONName(jsonName)
	case reflect.String:
		schema := openAPISchema{Type: "string"}
		return applyStringSchemaHints(schema, jsonName)
	case reflect.Bool:
		return openAPISchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return openAPISchema{Type: "integer", Format: "int32"}
	case reflect.Int64:
		return openAPISchema{Type: "integer", Format: "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return openAPISchema{Type: "integer", Format: "int32"}
	case reflect.Uint64:
		return openAPISchema{Type: "integer", Format: "int64"}
	case reflect.Float32:
		return openAPISchema{Type: "number", Format: "float"}
	case reflect.Float64:
		return openAPISchema{Type: "number", Format: "double"}
	default:
		return openAPISchema{Type: "string"}
	}
}

func (r *openAPISchemaRegistry) schemaRefForComponent(t reflect.Type) openAPISchema {
	if t.Kind() != reflect.Struct {
		return r.nonNullableSchemaForType(t, "")
	}

	name := r.componentName(t)
	if _, exists := r.schemas[name]; !exists {
		r.schemas[name] = openAPISchema{}
		r.schemas[name] = r.buildStructSchema(t)
	}

	return openAPISchema{Ref: "#/components/schemas/" + name}
}

func (r *openAPISchemaRegistry) buildStructSchema(t reflect.Type) openAPISchema {
	properties := make(map[string]openAPISchema)
	required := make([]string, 0)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonName, omitempty, include := parseJSONField(field)
		if !include {
			continue
		}

		fieldSchema := r.schemaForType(field.Type, jsonName)
		fieldSchema = applyStructFieldSchemaOverrides(t, jsonName, fieldSchema)
		fieldSchema = applyTypeFieldDoc(t, jsonName, fieldSchema)
		properties[jsonName] = fieldSchema

		if isRequiredJSONField(field.Type, omitempty) {
			required = append(required, jsonName)
		}
	}

	sort.Strings(required)
	schema := openAPISchema{
		Type:       "object",
		Properties: properties,
	}
	if len(required) > 0 {
		schema.Required = required
	}
	if doc, ok := openAPITypeDocs[t]; ok {
		if doc.Description != "" {
			schema.Description = doc.Description
		}
		if doc.Example != nil {
			schema.Example = doc.Example
		}
	}

	return schema
}

func (r *openAPISchemaRegistry) componentName(t reflect.Type) string {
	if name, ok := r.componentNames[t]; ok {
		return name
	}

	name := t.Name()
	if name == "" {
		name = "AnonymousObject"
	}
	if usedType, ok := r.usedNames[name]; ok && usedType != t {
		pkgName := packageNameFromPath(t.PkgPath())
		name = pkgName + name
	}

	r.componentNames[t] = name
	r.usedNames[name] = t
	return name
}

func parseJSONField(field reflect.StructField) (name string, omitempty bool, include bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}

	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = field.Name
	}
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitempty = true
			break
		}
	}

	return name, omitempty, true
}

func isRequiredJSONField(t reflect.Type, omitempty bool) bool {
	if omitempty {
		return false
	}

	for t.Kind() == reflect.Pointer {
		return false
	}

	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Interface:
		return false
	default:
		return true
	}
}

func mapSchemaForType(r *openAPISchemaRegistry, t reflect.Type) openAPISchema {
	schema := openAPISchema{Type: "object"}
	if t.Key().Kind() != reflect.String {
		schema.AdditionalProperties = true
		return schema
	}

	if t.Elem().Kind() == reflect.Interface {
		schema.AdditionalProperties = true
		return schema
	}

	additional := r.schemaForType(t.Elem(), "")
	schema.AdditionalProperties = additional
	return schema
}

func interfaceSchemaForJSONName(jsonName string) openAPISchema {
	switch jsonName {
	case "settings":
		return openAPISchema{Type: "object", AdditionalProperties: true}
	case "repos":
		item := openAPISchema{Type: "object", AdditionalProperties: true}
		return openAPISchema{Type: "array", Items: &item}
	default:
		return openAPISchema{Type: "object", AdditionalProperties: true}
	}
}

func applyStructFieldSchemaOverrides(ownerType reflect.Type, jsonName string, schema openAPISchema) openAPISchema {
	switch ownerType {
	case workspaceResponseReflectType, updateWorkspaceRequestReflectType:
		switch jsonName {
		case "settings":
			return openAPISchema{Type: "object", AdditionalProperties: true}
		case "repos":
			item := openAPISchema{Type: "object", AdditionalProperties: true}
			return openAPISchema{Type: "array", Items: &item}
		}
	}

	return schema
}

func applyTypeFieldDoc(ownerType reflect.Type, jsonName string, schema openAPISchema) openAPISchema {
	typeDoc, ok := openAPITypeDocs[ownerType]
	if !ok {
		return schema
	}

	fieldDoc, ok := typeDoc.Fields[jsonName]
	if !ok {
		return schema
	}

	if fieldDoc.Description != "" {
		schema.Description = fieldDoc.Description
	}
	if fieldDoc.Example != nil {
		schema.Example = fieldDoc.Example
	}
	if len(fieldDoc.Enum) > 0 {
		schema.Enum = fieldDoc.Enum
	}

	return schema
}

func applyStringSchemaHints(schema openAPISchema, jsonName string) openAPISchema {
	switch {
	case jsonName == "email":
		schema.Format = "email"
		schema.Example = "alice@example.com"
	case jsonName == "avatar_url":
		schema.Format = "uri"
		schema.Example = "https://cdn.multica.ai/avatars/alice.png"
	case jsonName == "id", strings.HasSuffix(jsonName, "_id"):
		schema.Format = "uuid"
		schema.Example = exampleUUID
	case strings.HasSuffix(jsonName, "_at") || jsonName == "due_date" || jsonName == "start_date" || jsonName == "end_date":
		schema.Format = "date-time"
		schema.Example = "2026-05-01T09:00:00Z"
	}

	if jsonName == "view" {
		schema.Enum = []string{"backlog", "today", "upcoming"}
		schema.Example = "backlog"
	}

	return schema
}

func issueListQueryParameters() []openAPIParameter {
	return []openAPIParameter{
		queryParameter("limit", "Maximum number of issues to return.", openAPISchema{Type: "integer", Format: "int32", Example: 50}),
		queryParameter("offset", "Number of issues to skip before returning results.", openAPISchema{Type: "integer", Format: "int32", Example: 0}),
		queryParameter("status", "Filter by issue status.", openAPISchema{Type: "string", Enum: issueStatusValues, Example: "todo"}),
		queryParameter("priority", "Filter by issue priority.", openAPISchema{Type: "string", Enum: issuePriorityValues, Example: "high"}),
		queryParameter("assignee_id", "Filter by assignee ID.", openAPISchema{Type: "string", Format: "uuid", Example: exampleUUID}),
		queryParameter("assignee_type", "Filter by assignee type.", openAPISchema{Type: "string", Enum: actorTypeValues, Example: "member"}),
		queryParameter("creator_id", "Filter by creator ID.", openAPISchema{Type: "string", Format: "uuid", Example: exampleUUID}),
		queryParameter("creator_type", "Filter by creator type.", openAPISchema{Type: "string", Enum: actorTypeValues, Example: "member"}),
		queryParameter("project_id", "Filter by project ID.", openAPISchema{Type: "string", Format: "uuid", Example: exampleUUID}),
		queryParameter("search", "Search by title, issue identifier, number, or UUID.", openAPISchema{Type: "string", Example: "MUL-42"}),
		queryParameter("due_from", "Return issues with due dates on or after this date.", openAPISchema{Type: "string", Format: "date", Example: "2026-05-01"}),
		queryParameter("due_to", "Return issues with due dates on or before this date.", openAPISchema{Type: "string", Format: "date", Example: "2026-05-31"}),
		queryParameter("start_from", "Return issues with start dates on or after this date.", openAPISchema{Type: "string", Format: "date", Example: "2026-05-01"}),
		queryParameter("start_to", "Return issues with start dates on or before this date.", openAPISchema{Type: "string", Format: "date", Example: "2026-05-31"}),
		queryParameter("end_from", "Return issues with end dates on or after this date.", openAPISchema{Type: "string", Format: "date", Example: "2026-05-01"}),
		queryParameter("end_to", "Return issues with end dates on or before this date.", openAPISchema{Type: "string", Format: "date", Example: "2026-05-31"}),
		queryParameter("view", "Named issue list view.", openAPISchema{Type: "string", Enum: []string{"backlog", "today", "upcoming"}, Example: "backlog"}),
	}
}

func projectListQueryParameters() []openAPIParameter {
	return []openAPIParameter{
		queryParameter("status", "Filter by project status.", openAPISchema{Type: "string", Enum: projectStatusValues, Example: "in_progress"}),
	}
}

func agentListQueryParameters() []openAPIParameter {
	return []openAPIParameter{
		queryParameter("include_archived", "Whether archived agents should be included in the result set.", openAPISchema{Type: "boolean", Example: false}),
	}
}

func commentListQueryParameters() []openAPIParameter {
	return []openAPIParameter{
		queryParameter("limit", "Maximum number of comments to return when paginating.", openAPISchema{Type: "integer", Format: "int32", Example: 50}),
		queryParameter("offset", "Number of comments to skip when paginating.", openAPISchema{Type: "integer", Format: "int32", Example: 0}),
		queryParameter("since", "Only return comments created on or after this RFC3339 timestamp.", openAPISchema{Type: "string", Format: "date-time", Example: "2026-05-01T09:00:00Z"}),
	}
}

func queryParameter(name string, description string, schema openAPISchema) openAPIParameter {
	return openAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    false,
		Schema:      schema,
	}
}

func packageNameFromPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func isTimeType(t reflect.Type) bool {
	return t.PkgPath() == "time" && t.Name() == "Time"
}

func typeOf[T any]() reflect.Type {
	var zero *T
	return reflect.TypeOf(zero).Elem()
}

func (r *openAPISchemaRegistry) String() string {
	return fmt.Sprintf("openAPISchemaRegistry(%d schemas)", len(r.schemas))
}
