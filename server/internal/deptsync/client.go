package deptsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("dept sync is not configured")

type Config struct {
	BaseURL  string
	QueryKey string
	Timeout  time.Duration
}

type Client struct {
	baseURL    string
	queryKey   string
	httpClient *http.Client
}

type User struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	UniversalID string `json:"universal_id"`
	DeptID      string `json:"dept_id"`
	DeptName    string `json:"dept_name"`
	IsMain      int    `json:"is_main"`
	Position    string `json:"position"`
	Status      int    `json:"status"`
	DeptPath    string `json:"dept_path"`
}

func (u *User) UnmarshalJSON(data []byte) error {
	var raw struct {
		UserID      nullableString `json:"user_id"`
		Username    nullableString `json:"username"`
		UniversalID nullableString `json:"universal_id"`
		DeptID      nullableString `json:"dept_id"`
		DeptName    nullableString `json:"dept_name"`
		IsMain      int            `json:"is_main"`
		Position    nullableString `json:"position"`
		Status      int            `json:"status"`
		DeptPath    nullableString `json:"dept_path"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*u = User{
		UserID:      string(raw.UserID),
		Username:    string(raw.Username),
		UniversalID: string(raw.UniversalID),
		DeptID:      string(raw.DeptID),
		DeptName:    string(raw.DeptName),
		IsMain:      raw.IsMain,
		Position:    string(raw.Position),
		Status:      raw.Status,
		DeptPath:    string(raw.DeptPath),
	}
	return nil
}

type Department struct {
	DeptID         string       `json:"dept_id"`
	DeptName       string       `json:"dept_name"`
	DeptPath       string       `json:"dept_path"`
	ParentDeptID   string       `json:"parent_dept_id"`
	DeptLevel      int          `json:"dept_level"`
	ChildDeptCount int          `json:"child_dept_count"`
	Children       []Department `json:"children,omitempty"`
}

func (d *Department) UnmarshalJSON(data []byte) error {
	var raw struct {
		DeptID         nullableString      `json:"dept_id"`
		DeptName       nullableString      `json:"dept_name"`
		DeptPath       nullableString      `json:"dept_path"`
		ParentDeptID   nullableString      `json:"parent_dept_id"`
		DeptLevel      int                 `json:"dept_level"`
		ChildDeptCount int                 `json:"child_dept_count"`
		Children       departmentChildList `json:"children"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = Department{
		DeptID:         string(raw.DeptID),
		DeptName:       string(raw.DeptName),
		DeptPath:       string(raw.DeptPath),
		ParentDeptID:   string(raw.ParentDeptID),
		DeptLevel:      raw.DeptLevel,
		ChildDeptCount: raw.ChildDeptCount,
		Children:       []Department(raw.Children),
	}
	return nil
}

type nullableString string

func (s *nullableString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = nullableString(value)
	return nil
}

type departmentChildList []Department

func (children *departmentChildList) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "", "null", `""`:
		*children = nil
		return nil
	}
	var value []Department
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*children = value
	return nil
}

func NewClient(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		queryKey:   strings.TrimSpace(cfg.QueryKey),
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.baseURL != "" && c.queryKey != ""
}

func (c *Client) SearchDepartments(ctx context.Context, query string, limit int) ([]Department, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	u, err := url.Parse(c.baseURL + "/departments/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Query-Key", c.queryKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dept department search request failed with status %d", resp.StatusCode)
	}
	var envelope struct {
		Success bool         `json:"success"`
		Code    string       `json:"code"`
		Data    []Department `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		if envelope.Code == "" {
			envelope.Code = "dept_request_failed"
		}
		return nil, errors.New(envelope.Code)
	}
	return envelope.Data, nil
}

func (c *Client) SearchUsers(ctx context.Context, query string, limit int) ([]User, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	u, err := url.Parse(c.baseURL + "/users/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Query-Key", c.queryKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dept user search request failed with status %d", resp.StatusCode)
	}
	var envelope struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    []User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		if envelope.Code == "" {
			envelope.Code = "dept_request_failed"
		}
		return nil, errors.New(envelope.Code)
	}
	return envelope.Data, nil
}

func (c *Client) GetDepartment(ctx context.Context, deptID string) (*Department, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	deptID = strings.TrimSpace(deptID)
	if deptID == "" {
		return nil, fmt.Errorf("dept_id is required")
	}
	tree, err := c.listDepartmentTree(ctx)
	if err != nil {
		return nil, err
	}
	if department, ok := findDepartment(tree, deptID); ok {
		return &department, nil
	}
	return nil, nil
}

func (c *Client) listDepartmentTree(ctx context.Context) ([]Department, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	u, err := url.Parse(c.baseURL + "/department/tree")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Query-Key", c.queryKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dept tree request failed with status %d", resp.StatusCode)
	}
	var envelope struct {
		Success bool         `json:"success"`
		Code    string       `json:"code"`
		Data    []Department `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		if envelope.Code == "" {
			envelope.Code = "dept_request_failed"
		}
		return nil, errors.New(envelope.Code)
	}
	return envelope.Data, nil
}

func displayDeptPathByID(tree []Department) map[string]string {
	paths := make(map[string]string)
	var walk func([]Department, []string, int)
	walk = func(nodes []Department, ancestors []string, depth int) {
		for _, node := range nodes {
			name := strings.TrimSpace(node.DeptName)
			parts := ancestors
			if depth > 0 && name != "" {
				parts = append(append([]string{}, ancestors...), name)
			}
			if strings.TrimSpace(node.DeptID) != "" && len(parts) > 0 {
				paths[node.DeptID] = strings.Join(parts, "/")
			}
			walk(node.Children, parts, depth+1)
		}
	}
	walk(tree, nil, 0)
	return paths
}

func applyDisplayDeptPaths(users []User, deptPathByID map[string]string) {
	for i := range users {
		if path := deptPathByID[strings.TrimSpace(users[i].DeptID)]; path != "" {
			users[i].DeptPath = path
		}
	}
}

func findDepartment(tree []Department, deptID string) (Department, bool) {
	for _, node := range tree {
		if node.DeptID == deptID {
			node.Children = nil
			return node, true
		}
		if found, ok := findDepartment(node.Children, deptID); ok {
			return found, true
		}
	}
	return Department{}, false
}

func (c *Client) ListDepartmentUsers(ctx context.Context, deptID string, includeChildren bool) ([]User, error) {
	tree, err := c.listDepartmentTree(ctx)
	if err != nil {
		return nil, err
	}
	users, err := c.listDepartmentUsersRaw(ctx, deptID, includeChildren)
	if err != nil {
		return nil, err
	}
	applyDisplayDeptPaths(users, displayDeptPathByID(tree))
	return users, nil
}

func (c *Client) listDepartmentUsersRaw(ctx context.Context, deptID string, includeChildren bool) ([]User, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	deptID = strings.TrimSpace(deptID)
	if deptID == "" {
		return nil, fmt.Errorf("dept_id is required")
	}
	u, err := url.Parse(c.baseURL + "/department/" + url.PathEscape(deptID) + "/users")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if includeChildren {
		q.Set("include_children", "true")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Query-Key", c.queryKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dept users request failed with status %d", resp.StatusCode)
	}
	var envelope struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    []User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.Success {
		if envelope.Code == "" {
			envelope.Code = "dept_request_failed"
		}
		return nil, errors.New(envelope.Code)
	}
	return envelope.Data, nil
}
