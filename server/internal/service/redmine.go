package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RedmineClient proxies requests to a Redmine instance on behalf of a user.
type RedmineClient struct {
	httpClient *http.Client
}

func NewRedmineClient() *RedmineClient {
	return &RedmineClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// ---- shared types ----

type RedmineRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type RedmineProject struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Identifier  string `json:"identifier"`
	Description string `json:"description"`
}

type RedmineIssue struct {
	ID          int        `json:"id"`
	Subject     string     `json:"subject"`
	Description string     `json:"description"`
	Project     RedmineRef `json:"project"`
}

// ---- request types ----

type CreateRedmineProjectReq struct {
	Name        string `json:"name"`
	Identifier  string `json:"identifier"`
	Description string `json:"description"`
}

type CreateRedmineIssueReq struct {
	ProjectID   int    `json:"project_id"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

// ---- API methods ----

func (c *RedmineClient) ListProjects(instanceURL, apiKey string) ([]RedmineProject, error) {
	url := strings.TrimRight(instanceURL, "/") + "/projects.json?limit=100"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Redmine-API-Key", apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("redmine request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("redmine returned status %d", resp.StatusCode)
	}

	var body struct {
		Projects []RedmineProject `json:"projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding redmine response: %w", err)
	}
	return body.Projects, nil
}

func (c *RedmineClient) CreateProject(instanceURL, apiKey string, req CreateRedmineProjectReq) (RedmineProject, error) {
	payload := map[string]any{
		"project": map[string]any{
			"name":        req.Name,
			"identifier":  req.Identifier,
			"description": req.Description,
		},
	}
	b, _ := json.Marshal(payload)

	url := strings.TrimRight(instanceURL, "/") + "/projects.json"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return RedmineProject{}, err
	}
	httpReq.Header.Set("X-Redmine-API-Key", apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return RedmineProject{}, fmt.Errorf("redmine request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return RedmineProject{}, fmt.Errorf("redmine returned status %d", resp.StatusCode)
	}

	var body struct {
		Project RedmineProject `json:"project"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return RedmineProject{}, fmt.Errorf("decoding redmine response: %w", err)
	}
	return body.Project, nil
}

func (c *RedmineClient) ListIssues(instanceURL, apiKey string, projectID int) ([]RedmineIssue, error) {
	url := fmt.Sprintf("%s/issues.json?project_id=%d&limit=100", strings.TrimRight(instanceURL, "/"), projectID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Redmine-API-Key", apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("redmine request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("redmine returned status %d", resp.StatusCode)
	}

	var body struct {
		Issues []RedmineIssue `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding redmine response: %w", err)
	}
	return body.Issues, nil
}

func (c *RedmineClient) CreateIssue(instanceURL, apiKey string, req CreateRedmineIssueReq) (RedmineIssue, error) {
	payload := map[string]any{
		"issue": map[string]any{
			"project_id":  req.ProjectID,
			"subject":     req.Subject,
			"description": req.Description,
		},
	}
	b, _ := json.Marshal(payload)

	url := strings.TrimRight(instanceURL, "/") + "/issues.json"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return RedmineIssue{}, err
	}
	httpReq.Header.Set("X-Redmine-API-Key", apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return RedmineIssue{}, fmt.Errorf("redmine request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return RedmineIssue{}, fmt.Errorf("redmine returned status %d", resp.StatusCode)
	}

	var body struct {
		Issue RedmineIssue `json:"issue"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return RedmineIssue{}, fmt.Errorf("decoding redmine response: %w", err)
	}
	return body.Issue, nil
}
