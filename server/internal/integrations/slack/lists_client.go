package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// listsAPIURL is the Slack Web API root. Tests override it on ListsClient.
const listsAPIURL = "https://slack.com/api"

var slackTokenRE = regexp.MustCompile(`(?i)xox[bporase]-[A-Za-z0-9-]{6,}|xapp-[A-Za-z0-9-]{6,}`)

func redactSlackSecrets(s string) string {
	return slackTokenRE.ReplaceAllString(s, "[REDACTED SLACK TOKEN]")
}

// ListsOption is one select-column choice. Agents send the label; writes use ID.
type ListsOption struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// ListsColumn is one column from a List schema. Column IDs are what
// slackLists.items.create / update require; name/key are for the agent to match.
type ListsColumn struct {
	ID              string        `json:"id"`
	Name            string        `json:"name,omitempty"`
	Key             string        `json:"key,omitempty"`
	Type            string        `json:"type,omitempty"`
	IsPrimaryColumn bool          `json:"is_primary_column,omitempty"`
	ReadOnly        bool          `json:"read_only,omitempty"`
	Options         []ListsOption `json:"options,omitempty"`
}

// ListsSchema is the allowlisted, token-free view of a List.
type ListsSchema struct {
	ListID  string        `json:"list_id"`
	Title   string        `json:"title,omitempty"`
	Columns []ListsColumn `json:"columns"`
}

// ListsField is one cell the agent wants to write. Prefer column_id from a
// prior schema read. column (name or key) is resolved against the schema when
// column_id is empty. text is rewritten as Slack rich_text — List text columns
// reject a bare "text" property on write.
type ListsField struct {
	ColumnID string   `json:"column_id,omitempty"`
	Column   string   `json:"column,omitempty"`
	Text     string   `json:"text,omitempty"`
	RichText []any    `json:"rich_text,omitempty"`
	Select   []string `json:"select,omitempty"`
	Date     []string `json:"date,omitempty"`
	User     []string `json:"user,omitempty"`
	Checkbox *bool    `json:"checkbox,omitempty"`
	Number   []any    `json:"number,omitempty"`
	Email    []string `json:"email,omitempty"`
	Phone    []string `json:"phone,omitempty"`
	Link     []any    `json:"link,omitempty"`
}

// ListsItem is the token-free create/update result.
type ListsItem struct {
	ID     string `json:"id"`
	ListID string `json:"list_id"`
	Title  string `json:"title,omitempty"`
}

// ListsClient calls Slack Lists APIs using a caller-supplied bot token.
// The token is used only as an Authorization header and is never copied into
// returned values or error strings. Schema is read via slackLists.items.list
// + slackLists.items.info (lists:read). files.info is intentionally unused:
// it requires files:read, which this app does not have.
type ListsClient struct {
	HTTP    *http.Client
	BaseURL string
}

func (c *ListsClient) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *ListsClient) base() string {
	if c != nil && c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return listsAPIURL
}

type slackAPIError struct {
	Method string
	Code   string
}

func (e slackAPIError) Error() string {
	if e.Code == "" {
		return "slack " + e.Method + " failed"
	}
	return "slack " + e.Method + ": " + e.Code
}

func (e slackAPIError) APIError() string {
	if e.Code == "" {
		return e.Method + " failed"
	}
	return e.Code
}

type slackListSchemaPayload struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	List  struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Name         string `json:"name"`
		ListMetadata struct {
			Schema []schemaColumn `json:"schema"`
		} `json:"list_metadata"`
	} `json:"list"`
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	Item struct {
		ID string `json:"id"`
	} `json:"item"`
}

type schemaColumn struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Key             string          `json:"key"`
	Type            string          `json:"type"`
	IsPrimaryColumn bool            `json:"is_primary_column"`
	IsReadOnly      bool            `json:"is_read_only"`
	Options         json.RawMessage `json:"options"`
}

// GetSchema reads column IDs for listID via slackLists.items.list then
// slackLists.items.info. An empty List has no item to inspect, so schema
// cannot be read with lists:read alone.
func (c *ListsClient) GetSchema(ctx context.Context, token, listID string) (ListsSchema, error) {
	var listed slackListSchemaPayload
	if err := c.json(ctx, token, "slackLists.items.list", map[string]any{
		"list_id": listID,
		"limit":   1,
	}, &listed); err != nil {
		return ListsSchema{}, err
	}
	if !listed.OK {
		return ListsSchema{}, slackAPIError{Method: "slackLists.items.list", Code: listed.Error}
	}
	if schema, ok := schemaFromPayload(listID, listed); ok {
		return schema, nil
	}
	itemID := ""
	if len(listed.Items) > 0 {
		itemID = listed.Items[0].ID
	}
	if itemID == "" {
		return ListsSchema{}, slackAPIError{Method: "slackLists.items.list", Code: "list_schema_unavailable"}
	}
	var info slackListSchemaPayload
	if err := c.json(ctx, token, "slackLists.items.info", map[string]any{
		"list_id": listID,
		"id":      itemID,
	}, &info); err != nil {
		return ListsSchema{}, err
	}
	if !info.OK {
		return ListsSchema{}, slackAPIError{Method: "slackLists.items.info", Code: info.Error}
	}
	if schema, ok := schemaFromPayload(listID, info); ok {
		return schema, nil
	}
	return ListsSchema{}, slackAPIError{Method: "slackLists.items.info", Code: "list_schema_unavailable"}
}

func schemaFromPayload(listID string, resp slackListSchemaPayload) (ListsSchema, bool) {
	cols := resp.List.ListMetadata.Schema
	if len(cols) == 0 {
		return ListsSchema{}, false
	}
	out := ListsSchema{ListID: firstNonEmpty(resp.List.ID, listID), Title: firstNonEmpty(resp.List.Title, resp.List.Name)}
	for _, col := range cols {
		out.Columns = append(out.Columns, ListsColumn{
			ID:              col.ID,
			Name:            col.Name,
			Key:             col.Key,
			Type:            col.Type,
			IsPrimaryColumn: col.IsPrimaryColumn,
			ReadOnly:        col.IsReadOnly || columnTypeReadOnly(col.Type),
			Options:         parseColumnOptions(col.Options),
		})
	}
	return out, true
}

func parseColumnOptions(raw json.RawMessage) []ListsOption {
	if len(raw) == 0 {
		return nil
	}
	var wrapped struct {
		Choices []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Choices) > 0 {
		out := make([]ListsOption, 0, len(wrapped.Choices))
		for _, c := range wrapped.Choices {
			out = append(out, ListsOption{ID: c.ID, Label: firstNonEmpty(c.Label, c.Name, c.Value, c.ID)})
		}
		return out
	}
	var arr []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		out := make([]ListsOption, 0, len(arr))
		for _, c := range arr {
			out = append(out, ListsOption{ID: c.ID, Label: firstNonEmpty(c.Label, c.Name, c.Value, c.ID)})
		}
		return out
	}
	return nil
}

func columnTypeReadOnly(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "created_time", "updated_time", "last_updated", "formula", "auto", "computed", "created":
		return true
	default:
		return false
	}
}

// CreateItem posts slackLists.items.create.
func (c *ListsClient) CreateItem(ctx context.Context, token, listID string, fields []map[string]any) (ListsItem, error) {
	body := map[string]any{"list_id": listID}
	if len(fields) > 0 {
		body["initial_fields"] = fields
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Item  struct {
			ID     string           `json:"id"`
			ListID string           `json:"list_id"`
			Fields []map[string]any `json:"fields"`
		} `json:"item"`
	}
	if err := c.json(ctx, token, "slackLists.items.create", body, &resp); err != nil {
		return ListsItem{}, err
	}
	if !resp.OK {
		return ListsItem{}, slackAPIError{Method: "slackLists.items.create", Code: resp.Error}
	}
	return ListsItem{
		ID:     resp.Item.ID,
		ListID: firstNonEmpty(resp.Item.ListID, listID),
		Title:  itemTitle(resp.Item.Fields),
	}, nil
}

// UpdateItem posts slackLists.items.update. Each field becomes a cell with
// row_id = itemID.
func (c *ListsClient) UpdateItem(ctx context.Context, token, listID, itemID string, fields []map[string]any) (ListsItem, error) {
	cells := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		cell := make(map[string]any, len(f)+1)
		for k, v := range f {
			cell[k] = v
		}
		cell["row_id"] = itemID
		cells = append(cells, cell)
	}
	body := map[string]any{"list_id": listID, "cells": cells}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.json(ctx, token, "slackLists.items.update", body, &resp); err != nil {
		return ListsItem{}, err
	}
	if !resp.OK {
		return ListsItem{}, slackAPIError{Method: "slackLists.items.update", Code: resp.Error}
	}
	return ListsItem{ID: itemID, ListID: listID}, nil
}

func itemTitle(fields []map[string]any) string {
	for _, f := range fields {
		if t, ok := f["text"].(string); ok && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t)
		}
	}
	return ""
}

func (c *ListsClient) json(ctx context.Context, token, method string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	return c.do(req, method, out)
}

func (c *ListsClient) do(req *http.Request, method string, out any) error {
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("slack %s: %s", method, redactSlackSecrets(err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("slack %s: read body: %w", method, err)
	}
	if resp.StatusCode >= 400 {
		return slackAPIError{Method: method, Code: fmt.Sprintf("http_%d", resp.StatusCode)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("slack %s: decode: %w", method, err)
	}
	return nil
}

// EncodeListsFields turns agent field inputs into Slack initial_fields / cells.
// schema is used to resolve column names and to pick rich_text vs other types.
func EncodeListsFields(fields []ListsField, schema ListsSchema) ([]map[string]any, string, error) {
	byID := map[string]ListsColumn{}
	byName := map[string]ListsColumn{}
	var primary ListsColumn
	for _, col := range schema.Columns {
		if col.ID != "" {
			byID[col.ID] = col
		}
		if col.Name != "" {
			byName[strings.ToLower(col.Name)] = col
		}
		if col.Key != "" {
			byName[strings.ToLower(col.Key)] = col
		}
		if col.IsPrimaryColumn {
			primary = col
		}
	}
	out := make([]map[string]any, 0, len(fields))
	title := ""
	for i, f := range fields {
		col, err := resolveFieldColumn(f, byID, byName, primary, i)
		if err != nil {
			return nil, "", err
		}
		encoded, textTitle, err := encodeOneField(f, col)
		if err != nil {
			return nil, "", err
		}
		out = append(out, encoded)
		if (col.IsPrimaryColumn || title == "") && textTitle != "" {
			title = textTitle
		}
	}
	return out, title, nil
}

func resolveFieldColumn(f ListsField, byID, byName map[string]ListsColumn, primary ListsColumn, idx int) (ListsColumn, error) {
	if id := strings.TrimSpace(f.ColumnID); id != "" {
		if col, ok := byID[id]; ok {
			return col, nil
		}
		return ListsColumn{}, fmt.Errorf("unknown column %q", id)
	}
	if name := strings.TrimSpace(f.Column); name != "" {
		if col, ok := byName[strings.ToLower(name)]; ok {
			return col, nil
		}
		return ListsColumn{}, fmt.Errorf("unknown column %q", name)
	}
	if idx == 0 && primary.ID != "" {
		return primary, nil
	}
	return ListsColumn{}, fmt.Errorf("field %d is missing column_id", idx)
}

func encodeOneField(f ListsField, col ListsColumn) (map[string]any, string, error) {
	if col.ReadOnly || columnTypeReadOnly(col.Type) {
		name := firstNonEmpty(col.Name, col.Key, col.ID)
		return nil, "", fmt.Errorf("column %s is read-only", name)
	}
	cell := map[string]any{"column_id": col.ID}
	title := ""
	typ := strings.ToLower(strings.TrimSpace(col.Type))
	switch {
	case len(f.RichText) > 0:
		cell["rich_text"] = f.RichText
	case typ == "select" || typ == "multi_select" || len(f.Select) > 0:
		ids, err := resolveSelectValues(col, f)
		if err != nil {
			return nil, "", err
		}
		cell["select"] = ids
	case typ == "date" || len(f.Date) > 0:
		dates := f.Date
		if len(dates) == 0 && f.Text != "" {
			dates = []string{strings.TrimSpace(f.Text)}
		}
		if len(dates) == 0 {
			return nil, "", fmt.Errorf("column %s has no value", col.ID)
		}
		cell["date"] = dates
	case typ == "user" || typ == "assignee" || len(f.User) > 0:
		users := f.User
		if len(users) == 0 && f.Text != "" {
			users = []string{strings.TrimSpace(f.Text)}
		}
		if len(users) == 0 {
			return nil, "", fmt.Errorf("column %s has no value", col.ID)
		}
		cell["user"] = users
	case f.Checkbox != nil || typ == "checkbox":
		if f.Checkbox != nil {
			cell["checkbox"] = *f.Checkbox
		} else {
			cell["checkbox"] = strings.EqualFold(strings.TrimSpace(f.Text), "true")
		}
	case typ == "number" || len(f.Number) > 0:
		nums := f.Number
		if len(nums) == 0 && f.Text != "" {
			nums = []any{strings.TrimSpace(f.Text)}
		}
		if len(nums) == 0 {
			return nil, "", fmt.Errorf("column %s has no value", col.ID)
		}
		cell["number"] = nums
	case typ == "email" || len(f.Email) > 0:
		emails := f.Email
		if len(emails) == 0 && f.Text != "" {
			emails = []string{strings.TrimSpace(f.Text)}
		}
		cell["email"] = emails
	case typ == "phone" || len(f.Phone) > 0:
		phones := f.Phone
		if len(phones) == 0 && f.Text != "" {
			phones = []string{strings.TrimSpace(f.Text)}
		}
		cell["phone"] = phones
	case len(f.Link) > 0:
		cell["link"] = f.Link
	case f.Text != "" || typ == "text" || typ == "rich_text" || typ == "":
		if strings.TrimSpace(f.Text) == "" {
			return nil, "", fmt.Errorf("column %s has no value", col.ID)
		}
		cell["rich_text"] = richTextFromPlain(f.Text)
		title = strings.TrimSpace(f.Text)
	default:
		return nil, "", fmt.Errorf("column %s has no value", col.ID)
	}
	return cell, title, nil
}

func resolveSelectValues(col ListsColumn, f ListsField) ([]string, error) {
	raw := append([]string{}, f.Select...)
	if len(raw) == 0 && strings.TrimSpace(f.Text) != "" {
		raw = []string{strings.TrimSpace(f.Text)}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("column %s has no value", col.ID)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		id, err := matchSelectOption(col, v)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func matchSelectOption(col ListsColumn, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("column %s has an empty select value", col.ID)
	}
	if len(col.Options) == 0 {
		return value, nil
	}
	for _, opt := range col.Options {
		if opt.ID == value || opt.Label == value {
			return opt.ID, nil
		}
	}
	lower := strings.ToLower(value)
	for _, opt := range col.Options {
		if strings.ToLower(opt.Label) == lower {
			return opt.ID, nil
		}
	}
	return "", fmt.Errorf("unknown select option %q for column %s", value, firstNonEmpty(col.Name, col.ID))
}

func richTextFromPlain(text string) []any {
	return []any{
		map[string]any{
			"type": "rich_text",
			"elements": []any{
				map[string]any{
					"type": "rich_text_section",
					"elements": []any{
						map[string]any{"type": "text", "text": text},
					},
				},
			},
		},
	}
}
