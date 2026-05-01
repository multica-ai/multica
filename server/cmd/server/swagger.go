package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

var openAPIPathParamPattern = regexp.MustCompile(`\{([^}:]+)(?::[^}]+)?\}`)

type openAPISpec struct {
	OpenAPI    string                     `json:"openapi"`
	Info       openAPIInfo                `json:"info"`
	Servers    []openAPIServer            `json:"servers,omitempty"`
	Tags       []openAPITag               `json:"tags,omitempty"`
	Paths      map[string]openAPIPathItem `json:"paths"`
	Components openAPIComponents          `json:"components,omitempty"`
}

type openAPIInfo struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

type openAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type openAPITag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type openAPIComponents struct {
	SecuritySchemes map[string]openAPISecurityScheme `json:"securitySchemes,omitempty"`
	Schemas         map[string]openAPISchema         `json:"schemas,omitempty"`
}

type openAPISecurityScheme struct {
	Type         string `json:"type"`
	Description  string `json:"description,omitempty"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
}

type openAPIPathItem map[string]openAPIOperation

type openAPIOperation struct {
	Tags        []string                   `json:"tags,omitempty"`
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	OperationID string                     `json:"operationId,omitempty"`
	Parameters  []openAPIParameter         `json:"parameters,omitempty"`
	RequestBody *openAPIRequestBody        `json:"requestBody,omitempty"`
	Security    []map[string][]string      `json:"security,omitempty"`
	Responses   map[string]openAPIResponse `json:"responses"`
}

type openAPIRequestBody struct {
	Description string                      `json:"description,omitempty"`
	Required    bool                        `json:"required,omitempty"`
	Content     map[string]openAPIMediaType `json:"content"`
}

type openAPIParameter struct {
	Name        string        `json:"name"`
	In          string        `json:"in"`
	Description string        `json:"description,omitempty"`
	Required    bool          `json:"required"`
	Schema      openAPISchema `json:"schema"`
}

type openAPISchema struct {
	Ref                  string                   `json:"$ref,omitempty"`
	Type                 string                   `json:"type,omitempty"`
	Format               string                   `json:"format,omitempty"`
	Description          string                   `json:"description,omitempty"`
	Example              any                      `json:"example,omitempty"`
	Nullable             bool                     `json:"nullable,omitempty"`
	Enum                 []string                 `json:"enum,omitempty"`
	Items                *openAPISchema           `json:"items,omitempty"`
	Properties           map[string]openAPISchema `json:"properties,omitempty"`
	Required             []string                 `json:"required,omitempty"`
	AdditionalProperties any                      `json:"additionalProperties,omitempty"`
}

type openAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]openAPIMediaType `json:"content,omitempty"`
}

type openAPIMediaType struct {
	Schema openAPISchema `json:"schema"`
}

func registerSwaggerRoutes(r chi.Router) error {
	specJSON, err := buildOpenAPISpecJSON(r)
	if err != nil {
		return err
	}

	r.Get("/swagger", redirectToSwaggerIndex)
	r.Get("/swagger/", redirectToSwaggerIndex)
	r.Get("/swagger/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(specJSON)
	})
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/openapi.json"),
		httpSwagger.DocExpansion("list"),
		httpSwagger.DeepLinking(true),
	))

	return nil
}

func redirectToSwaggerIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/swagger/index.html", http.StatusTemporaryRedirect)
}

func buildOpenAPISpecJSON(routes chi.Routes) ([]byte, error) {
	spec, err := buildOpenAPISpec(routes)
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(spec, "", "  ")
}

func buildOpenAPISpec(routes chi.Routes) (*openAPISpec, error) {
	schemaRegistry := newOpenAPISchemaRegistry()

	spec := &openAPISpec{
		OpenAPI: "3.0.3",
		Info: openAPIInfo{
			Title:       "Multica Server API",
			Description: "Generated from the live Chi router to keep Swagger coverage aligned with the current Go service.",
			Version:     "dev",
		},
		Servers: []openAPIServer{{
			URL:         "/",
			Description: "Current server",
		}},
		Paths: map[string]openAPIPathItem{},
		Components: openAPIComponents{
			SecuritySchemes: map[string]openAPISecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
					Description:  "Bearer token used by the workspace app, CLI, and daemon API.",
				},
			},
		},
	}

	tagSet := map[string]struct{}{}

	err := chi.Walk(routes, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		path := normalizeOpenAPIPath(route)
		if shouldSkipOpenAPIPath(path) {
			return nil
		}

		operationMethod := strings.ToLower(strings.TrimSpace(method))
		if !isOpenAPIMethod(operationMethod) {
			return nil
		}

		pathItem := spec.Paths[path]
		if pathItem == nil {
			pathItem = openAPIPathItem{}
		}

		tag := inferOpenAPITag(path)
		pathItem[operationMethod] = buildOpenAPIOperation(operationMethod, path, tag, schemaRegistry)
		spec.Paths[path] = pathItem
		tagSet[tag] = struct{}{}

		return nil
	})
	if err != nil {
		return nil, err
	}

	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	spec.Tags = make([]openAPITag, 0, len(tags))
	for _, tag := range tags {
		spec.Tags = append(spec.Tags, openAPITag{
			Name:        tag,
			Description: openAPITagDescription(tag),
		})
	}

	if len(schemaRegistry.schemas) > 0 {
		spec.Components.Schemas = schemaRegistry.schemas
	}

	return spec, nil
}

func shouldSkipOpenAPIPath(path string) bool {
	if path == "/" {
		return true
	}

	return strings.HasPrefix(path, "/swagger")
}

func normalizeOpenAPIPath(path string) string {
	normalized := openAPIPathParamPattern.ReplaceAllString(path, "{$1}")
	if normalized == "" {
		return "/"
	}
	if len(normalized) > 1 {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	if normalized == "" {
		return "/"
	}
	return normalized
}

func isOpenAPIMethod(method string) bool {
	switch method {
	case "get", "post", "put", "patch", "delete", "head", "options":
		return true
	default:
		return false
	}
}

func buildOpenAPIOperation(method string, path string, tag string, schemaRegistry *openAPISchemaRegistry) openAPIOperation {
	parameters := buildOpenAPIParameters(path)
	responses := map[string]openAPIResponse{
		"default": {Description: "Request completed."},
	}

	if requiresOpenAPISecurity(path) {
		responses["401"] = schemaRegistry.errorResponse("Authentication required or token invalid.")
	}
	if requiresWorkspaceHeader(path) {
		responses["400"] = schemaRegistry.errorResponse("X-Workspace-ID header is missing or invalid.")
		responses["403"] = schemaRegistry.errorResponse("Workspace access denied.")
	}

	operation := openAPIOperation{
		Tags:        []string{tag},
		Summary:     fmt.Sprintf("%s %s", strings.ToUpper(method), path),
		Description: openAPIOperationDescription(method, path),
		OperationID: openAPIOperationID(method, path),
		Parameters:  parameters,
		Responses:   responses,
	}

	if requiresOpenAPISecurity(path) {
		operation.Security = []map[string][]string{{
			"bearerAuth": {},
		}}
	}

	if override, ok := openAPIOperationOverrides[openAPIOperationKey{Method: method, Path: path}]; ok {
		if override.Summary != "" {
			operation.Summary = override.Summary
		}
		if override.Description != "" {
			operation.Description = override.Description
		}
		if len(override.QueryParameters) > 0 {
			operation.Parameters = append(operation.Parameters, override.QueryParameters...)
		}
		if override.RequestType != nil {
			operation.RequestBody = schemaRegistry.requestBodyForType(override.RequestType, override.RequestRequired)
		}
		for status, responseDoc := range override.Responses {
			operation.Responses[status] = schemaRegistry.responseForDoc(responseDoc)
		}
	}

	return operation
}

func buildOpenAPIParameters(path string) []openAPIParameter {
	matches := openAPIPathParamPattern.FindAllStringSubmatch(path, -1)
	parameters := make([]openAPIParameter, 0, len(matches)+1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		name := match[1]
		parameter := openAPIParameter{
			Name:        name,
			In:          "path",
			Description: fmt.Sprintf("Path parameter %s.", name),
			Required:    true,
			Schema:      openAPIPathParamSchema(name),
		}
		parameters = append(parameters, parameter)
	}

	if requiresWorkspaceHeader(path) {
		parameters = append(parameters, openAPIParameter{
			Name:        "X-Workspace-ID",
			In:          "header",
			Description: "Workspace context required by workspace-scoped endpoints.",
			Required:    true,
			Schema: openAPISchema{
				Type:   "string",
				Format: "uuid",
			},
		})
	}

	return parameters
}

func openAPIPathParamSchema(name string) openAPISchema {
	schema := openAPISchema{Type: "string"}
	if strings.HasSuffix(strings.ToLower(name), "id") {
		schema.Format = "uuid"
		schema.Example = exampleUUID
	}
	return schema
}

func inferOpenAPITag(path string) string {
	switch {
	case path == "/health":
		return "system"
	case path == "/ws":
		return "realtime"
	case strings.HasPrefix(path, "/auth/"):
		return "auth"
	case strings.HasPrefix(path, "/api/daemon/"):
		return "daemon"
	case strings.HasPrefix(path, "/api/"):
		segments := strings.Split(strings.TrimPrefix(path, "/api/"), "/")
		if len(segments) > 0 && segments[0] != "" {
			return segments[0]
		}
	}

	return "api"
}

func openAPITagDescription(tag string) string {
	switch tag {
	case "auth":
		return "Authentication and sign-in endpoints."
	case "daemon":
		return "Daemon control plane endpoints used by local runtimes."
	case "realtime":
		return "Real-time transport endpoints."
	case "system":
		return "Operational endpoints such as health checks."
	default:
		return fmt.Sprintf("%s API endpoints.", strings.Title(tag))
	}
}

func openAPIOperationDescription(method string, path string) string {
	switch {
	case path == "/health":
		return "Health check endpoint used by local tooling and deployments."
	case path == "/ws":
		return "WebSocket upgrade endpoint used for real-time updates."
	case strings.HasPrefix(path, "/auth/"):
		return "Authentication endpoint."
	case strings.HasPrefix(path, "/api/daemon/"):
		return "Daemon runtime endpoint."
	case requiresWorkspaceHeader(path):
		return fmt.Sprintf("Workspace-scoped %s endpoint. Send X-Workspace-ID with a workspace UUID.", strings.ToUpper(method))
	case strings.HasPrefix(path, "/api/"):
		return "Authenticated API endpoint."
	default:
		return "HTTP endpoint."
	}
}

func openAPIOperationID(method string, path string) string {
	replacer := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_")
	id := replacer.Replace(strings.Trim(path, "/"))
	id = strings.Trim(id, "_")
	if id == "" {
		id = "root"
	}
	return strings.ToLower(method) + "_" + id
}

func requiresOpenAPISecurity(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func requiresWorkspaceHeader(path string) bool {
	workspaceScopedPrefixes := []string{
		"/api/issues",
		"/api/projects",
		"/api/labels",
		"/api/attachments",
		"/api/comments",
		"/api/agents",
		"/api/skills",
		"/api/runtimes",
		"/api/inbox",
	}

	for _, prefix := range workspaceScopedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}
