package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type swaggerTestSpec struct {
	Paths      map[string]map[string]swaggerTestOperation `json:"paths"`
	Components swaggerTestComponents                      `json:"components"`
}

type swaggerTestComponents struct {
	Schemas map[string]swaggerTestSchema `json:"schemas"`
}

type swaggerTestOperation struct {
	Parameters  []swaggerTestParameter            `json:"parameters"`
	RequestBody *swaggerTestRequestBody           `json:"requestBody"`
	Security    []map[string][]string             `json:"security"`
	Responses   map[string]swaggerTestAPIResponse `json:"responses"`
}

type swaggerTestRequestBody struct {
	Required bool                            `json:"required"`
	Content  map[string]swaggerTestMediaType `json:"content"`
}

type swaggerTestAPIResponse struct {
	Description string                          `json:"description"`
	Content     map[string]swaggerTestMediaType `json:"content"`
}

type swaggerTestMediaType struct {
	Schema swaggerTestSchema `json:"schema"`
}

type swaggerTestSchema struct {
	Ref                  string                       `json:"$ref"`
	Type                 string                       `json:"type"`
	Format               string                       `json:"format"`
	Description          string                       `json:"description"`
	Example              any                          `json:"example"`
	Nullable             bool                         `json:"nullable"`
	Enum                 []string                     `json:"enum"`
	Required             []string                     `json:"required"`
	Properties           map[string]swaggerTestSchema `json:"properties"`
	Items                *swaggerTestSchema           `json:"items"`
	AdditionalProperties any                          `json:"additionalProperties"`
}

type swaggerTestParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
}

func TestRegisterSwaggerRoutesServesOpenAPISpec(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {})
	r.Post("/auth/send-code", func(w http.ResponseWriter, r *http.Request) {})
	r.Route("/api/issues", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/comments", func(w http.ResponseWriter, r *http.Request) {})
			r.Post("/comments", func(w http.ResponseWriter, r *http.Request) {})
			r.Delete("/", func(w http.ResponseWriter, r *http.Request) {})
		})
	})
	r.Route("/api/projects", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {})
			r.Put("/", func(w http.ResponseWriter, r *http.Request) {})
			r.Delete("/", func(w http.ResponseWriter, r *http.Request) {})
		})
	})
	r.Route("/api/agents", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {})
			r.Put("/", func(w http.ResponseWriter, r *http.Request) {})
			r.Post("/archive", func(w http.ResponseWriter, r *http.Request) {})
			r.Post("/restore", func(w http.ResponseWriter, r *http.Request) {})
			r.Get("/tasks", func(w http.ResponseWriter, r *http.Request) {})
		})
	})
	r.Route("/api/comments/{commentId}", func(r chi.Router) {
		r.Put("/", func(w http.ResponseWriter, r *http.Request) {})
		r.Delete("/", func(w http.ResponseWriter, r *http.Request) {})
	})

	if err := registerSwaggerRoutes(r); err != nil {
		t.Fatalf("register swagger routes: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/swagger/openapi.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var spec swaggerTestSpec
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}

	if _, ok := spec.Paths["/health"]; !ok {
		t.Fatalf("expected /health in spec")
	}

	issuesGet, ok := spec.Paths["/api/issues"][strings.ToLower(http.MethodGet)]
	if !ok {
		t.Fatalf("expected GET /api/issues in spec")
	}
	issueListResponse, ok := issuesGet.Responses["200"]
	if !ok {
		t.Fatalf("expected GET /api/issues to include a documented 200 response")
	}
	if issueListResponse.Content["application/json"].Schema.Ref != "#/components/schemas/SwaggerIssueListResponse" {
		t.Fatalf("expected GET /api/issues 200 response to reference SwaggerIssueListResponse, got %q", issueListResponse.Content["application/json"].Schema.Ref)
	}
	if len(issuesGet.Security) == 0 {
		t.Fatalf("expected GET /api/issues to require security")
	}
	if !hasParameter(issuesGet.Parameters, "X-Workspace-ID", "header") {
		t.Fatalf("expected GET /api/issues to require X-Workspace-ID header")
	}
	if !hasParameterNamed(issuesGet.Parameters, "limit", "query") || !hasParameterNamed(issuesGet.Parameters, "search", "query") {
		t.Fatalf("expected GET /api/issues to include documented query parameters")
	}

	sendCodePost, ok := spec.Paths["/auth/send-code"][strings.ToLower(http.MethodPost)]
	if !ok {
		t.Fatalf("expected POST /auth/send-code in spec")
	}
	if sendCodePost.RequestBody == nil || !sendCodePost.RequestBody.Required {
		t.Fatalf("expected POST /auth/send-code to include a required request body")
	}
	if sendCodePost.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/SendCodeRequest" {
		t.Fatalf("expected POST /auth/send-code request body to reference SendCodeRequest, got %q", sendCodePost.RequestBody.Content["application/json"].Schema.Ref)
	}
	if sendCodePost.Responses["200"].Content["application/json"].Schema.Ref != "#/components/schemas/SwaggerMessageResponse" {
		t.Fatalf("expected POST /auth/send-code 200 response to reference SwaggerMessageResponse")
	}

	createIssuePost, ok := spec.Paths["/api/issues"][strings.ToLower(http.MethodPost)]
	if !ok {
		t.Fatalf("expected POST /api/issues in spec")
	}
	if createIssuePost.RequestBody == nil {
		t.Fatalf("expected POST /api/issues to include a request body")
	}
	if createIssuePost.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/CreateIssueRequest" {
		t.Fatalf("expected POST /api/issues request body to reference CreateIssueRequest, got %q", createIssuePost.RequestBody.Content["application/json"].Schema.Ref)
	}
	if createIssuePost.Responses["201"].Content["application/json"].Schema.Ref != "#/components/schemas/IssueResponse" {
		t.Fatalf("expected POST /api/issues 201 response to reference IssueResponse")
	}

	issueDelete, ok := spec.Paths["/api/issues/{id}"][strings.ToLower(http.MethodDelete)]
	if !ok {
		t.Fatalf("expected DELETE /api/issues/{id} in spec")
	}
	if !hasParameter(issueDelete.Parameters, "id", "path") {
		t.Fatalf("expected DELETE /api/issues/{id} to include id path parameter")
	}
	if _, ok := issueDelete.Responses["401"]; !ok {
		t.Fatalf("expected DELETE /api/issues/{id} to include 401 response")
	}
	if _, ok := issueDelete.Responses["403"]; !ok {
		t.Fatalf("expected DELETE /api/issues/{id} to include 403 response")
	}
	if _, ok := issueDelete.Responses["204"]; !ok {
		t.Fatalf("expected DELETE /api/issues/{id} to include a documented 204 response")
	}

	if _, ok := spec.Components.Schemas["IssueResponse"]; !ok {
		t.Fatalf("expected IssueResponse component schema to be present")
	}
	if _, ok := spec.Components.Schemas["CreateIssueRequest"]; !ok {
		t.Fatalf("expected CreateIssueRequest component schema to be present")
	}
	if emailField := spec.Components.Schemas["SendCodeRequest"].Properties["email"]; emailField.Format != "email" {
		t.Fatalf("expected SendCodeRequest.email to be documented as email, got %q", emailField.Format)
	}
	if emailField := spec.Components.Schemas["SendCodeRequest"].Properties["email"]; emailField.Description == "" {
		t.Fatalf("expected SendCodeRequest.email to include a field description")
	}
	if emailField := spec.Components.Schemas["SendCodeRequest"].Properties["email"]; emailField.Example != "alice@example.com" {
		t.Fatalf("expected SendCodeRequest.email to include an example, got %#v", emailField.Example)
	}
	issueStatusField := spec.Components.Schemas["IssueResponse"].Properties["status"]
	if len(issueStatusField.Enum) == 0 || issueStatusField.Enum[0] != "backlog" {
		t.Fatalf("expected IssueResponse.status to include issue status enum values")
	}
	if issueStatusField.Example != "todo" {
		t.Fatalf("expected IssueResponse.status to include a todo example, got %#v", issueStatusField.Example)
	}

	projectsGet, ok := spec.Paths["/api/projects"][strings.ToLower(http.MethodGet)]
	if !ok {
		t.Fatalf("expected GET /api/projects in spec")
	}
	if projectsGet.Responses["200"].Content["application/json"].Schema.Ref != "#/components/schemas/SwaggerProjectListResponse" {
		t.Fatalf("expected GET /api/projects 200 response to reference SwaggerProjectListResponse")
	}
	if !hasParameterNamed(projectsGet.Parameters, "status", "query") {
		t.Fatalf("expected GET /api/projects to document the status query parameter")
	}
	projectStatusField := spec.Components.Schemas["ProjectResponse"].Properties["status"]
	if len(projectStatusField.Enum) == 0 || projectStatusField.Enum[0] != "planned" {
		t.Fatalf("expected ProjectResponse.status to include project status enum values")
	}

	agentsPost, ok := spec.Paths["/api/agents"][strings.ToLower(http.MethodPost)]
	if !ok {
		t.Fatalf("expected POST /api/agents in spec")
	}
	if agentsPost.RequestBody == nil || agentsPost.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/CreateAgentRequest" {
		t.Fatalf("expected POST /api/agents request body to reference CreateAgentRequest")
	}
	agentVisibilityField := spec.Components.Schemas["CreateAgentRequest"].Properties["visibility"]
	if len(agentVisibilityField.Enum) != 2 || agentVisibilityField.Enum[0] != "private" || agentVisibilityField.Enum[1] != "workspace" {
		t.Fatalf("expected CreateAgentRequest.visibility to enumerate private/workspace")
	}
	if agentVisibilityField.Example != "private" {
		t.Fatalf("expected CreateAgentRequest.visibility to include a private example, got %#v", agentVisibilityField.Example)
	}

	commentsGet, ok := spec.Paths["/api/issues/{id}/comments"][strings.ToLower(http.MethodGet)]
	if !ok {
		t.Fatalf("expected GET /api/issues/{id}/comments in spec")
	}
	commentsSchema := commentsGet.Responses["200"].Content["application/json"].Schema
	if commentsSchema.Type != "array" || commentsSchema.Items == nil || commentsSchema.Items.Ref != "#/components/schemas/CommentResponse" {
		t.Fatalf("expected GET /api/issues/{id}/comments 200 response to be an array of CommentResponse")
	}
	if !hasParameterNamed(commentsGet.Parameters, "since", "query") {
		t.Fatalf("expected GET /api/issues/{id}/comments to document the since query parameter")
	}

	commentPut, ok := spec.Paths["/api/comments/{commentId}"][strings.ToLower(http.MethodPut)]
	if !ok {
		t.Fatalf("expected PUT /api/comments/{commentId} in spec")
	}
	if commentPut.RequestBody == nil || commentPut.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/SwaggerUpdateCommentRequest" {
		t.Fatalf("expected PUT /api/comments/{commentId} request body to reference SwaggerUpdateCommentRequest")
	}
	if commentContentField := spec.Components.Schemas["CommentResponse"].Properties["content"]; commentContentField.Description == "" {
		t.Fatalf("expected CommentResponse.content to include a field description")
	}

	if _, ok := spec.Paths["/swagger/openapi.json"]; ok {
		t.Fatalf("swagger routes should not be listed in generated spec")
	}
}

func TestRegisterSwaggerRoutesServesSwaggerUI(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {})

	if err := registerSwaggerRoutes(r); err != nil {
		t.Fatalf("register swagger routes: %v", err)
	}

	redirectReq := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	redirectRec := httptest.NewRecorder()
	r.ServeHTTP(redirectRec, redirectReq)

	if redirectRec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected 307 redirect from /swagger, got %d", redirectRec.Code)
	}
	if location := redirectRec.Header().Get("Location"); location != "/swagger/index.html" {
		t.Fatalf("expected redirect to /swagger/index.html, got %q", location)
	}

	req := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected swagger UI content type to be HTML, got %q", contentType)
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("expected swagger UI HTML body to be non-empty")
	}
}

func hasParameter(parameters []swaggerTestParameter, name string, location string) bool {
	for _, parameter := range parameters {
		if parameter.Name == name && parameter.In == location && parameter.Required {
			return true
		}
	}
	return false
}

func hasParameterNamed(parameters []swaggerTestParameter, name string, location string) bool {
	for _, parameter := range parameters {
		if parameter.Name == name && parameter.In == location {
			return true
		}
	}
	return false
}
