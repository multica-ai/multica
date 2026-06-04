// Package github implements a bidirectional sync between a Multica
// workspace board and a GitHub Projects v2 board.
//
// The connector mirrors the architecture of the Lark integration: an
// inbound path (here, a poller that pulls ProjectV2 items and upserts
// Multica issues) and an outbound path (a bus-subscribing Patcher that
// pushes local status/comment changes back to GitHub). Unlike Lark it
// needs no WS long-conn — GitHub Projects v2 has no realtime push for
// project fields, so polling is the pragmatic transport.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	graphqlEndpoint = "https://api.github.com/graphql"
	restEndpoint    = "https://api.github.com"
)

// Client is a thin GitHub GraphQL + REST client scoped to a single token.
type Client struct {
	token string
	http  *http.Client
}

// NewClient builds a Client bound to the given token. The token needs the
// `project`, `repo`, and `read:org` scopes to read the board and write
// field values + issue comments.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// graphql executes a GraphQL query/mutation and unmarshals data into out.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql http %d: %s", resp.StatusCode, truncate(raw, 500))
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode graphql envelope: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("decode graphql data: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Project + field schema
// ---------------------------------------------------------------------------

// ProjectSchema is the resolved board identity + field/option node ids.
// It is cached on the installation so the patcher does not re-resolve it
// on every push.
type ProjectSchema struct {
	ProjectNodeID string                  `json:"project_node_id"`
	Title         string                  `json:"title"`
	Fields        map[string]ProjectField `json:"fields"` // keyed by field name
}

// ProjectField is one ProjectV2 field. For single-select fields Options
// maps the human option name to its node id (used when writing values).
type ProjectField struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	DataType string            `json:"data_type"`
	Options  map[string]string `json:"options,omitempty"` // option name -> option id
}

// FetchProjectSchema resolves the project node id and the full field set
// (including single-select option ids) for an org project board.
func (c *Client) FetchProjectSchema(ctx context.Context, org string, number int) (ProjectSchema, error) {
	const q = `
query($org:String!, $number:Int!) {
  organization(login:$org) {
    projectV2(number:$number) {
      id
      title
      fields(first:50) {
        nodes {
          ... on ProjectV2FieldCommon { id name dataType }
          ... on ProjectV2SingleSelectField { id name options { id name } }
        }
      }
    }
  }
}`
	var resp struct {
		Organization struct {
			ProjectV2 struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Fields struct {
					Nodes []struct {
						ID       string `json:"id"`
						Name     string `json:"name"`
						DataType string `json:"dataType"`
						Options  []struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"options"`
					} `json:"nodes"`
				} `json:"fields"`
			} `json:"projectV2"`
		} `json:"organization"`
	}
	if err := c.graphql(ctx, q, map[string]any{"org": org, "number": number}, &resp); err != nil {
		return ProjectSchema{}, err
	}
	p := resp.Organization.ProjectV2
	if p.ID == "" {
		return ProjectSchema{}, fmt.Errorf("project %s/#%d not found or not accessible", org, number)
	}
	schema := ProjectSchema{ProjectNodeID: p.ID, Title: p.Title, Fields: map[string]ProjectField{}}
	for _, f := range p.Fields.Nodes {
		if f.ID == "" {
			continue
		}
		pf := ProjectField{ID: f.ID, Name: f.Name, DataType: f.DataType}
		if len(f.Options) > 0 {
			pf.Options = map[string]string{}
			for _, o := range f.Options {
				pf.Options[o.Name] = o.ID
			}
		}
		schema.Fields[f.Name] = pf
	}
	return schema, nil
}

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

// Item is a normalized ProjectV2 board row.
type Item struct {
	ItemID       string // ProjectV2Item node id
	ContentID    string // backing Issue/DraftIssue node id
	ContentType  string // "ISSUE" | "DRAFT_ISSUE" | "PULL_REQUEST"
	Title        string
	Body         string
	Repo         string // owner/name (empty for drafts)
	Number       int    // repo issue/PR number (0 for drafts)
	State        string // OPEN | CLOSED (issues)
	URL          string
	Labels       []string
	ParentItemID string            // parent ProjectV2Item id, if any
	ParentNumber int               // parent repo issue number, if any
	Fields       map[string]string // resolved field name -> value (single-select name / text)
}

// FetchItems pages through every item on the board and returns normalized
// rows. It resolves single-select + text field values by field name.
func (c *Client) FetchItems(ctx context.Context, projectNodeID string) ([]Item, error) {
	const q = `
query($project:ID!, $cursor:String) {
  node(id:$project) {
    ... on ProjectV2 {
      items(first:50, after:$cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          type
          content {
            ... on Issue {
              id number title body url state
              repository { nameWithOwner }
              labels(first:20) { nodes { name } }
              parent { number }
            }
            ... on PullRequest {
              id number title body url state
              repository { nameWithOwner }
              labels(first:20) { nodes { name } }
            }
            ... on DraftIssue { id title body }
          }
          fieldValues(first:30) {
            nodes {
              __typename
              ... on ProjectV2ItemFieldSingleSelectValue { name field { ... on ProjectV2FieldCommon { name } } }
              ... on ProjectV2ItemFieldTextValue { text field { ... on ProjectV2FieldCommon { name } } }
              ... on ProjectV2ItemFieldDateValue { date field { ... on ProjectV2FieldCommon { name } } }
            }
          }
        }
      }
    }
  }
}`
	var items []Item
	var cursor *string
	for {
		var resp struct {
			Node struct {
				Items struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						ID      string `json:"id"`
						Type    string `json:"type"`
						Content struct {
							ID         string `json:"id"`
							Number     int    `json:"number"`
							Title      string `json:"title"`
							Body       string `json:"body"`
							URL        string `json:"url"`
							State      string `json:"state"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
							Labels struct {
								Nodes []struct {
									Name string `json:"name"`
								} `json:"nodes"`
							} `json:"labels"`
							Parent struct {
								Number int `json:"number"`
							} `json:"parent"`
						} `json:"content"`
						FieldValues struct {
							Nodes []struct {
								Typename string `json:"__typename"`
								Name     string `json:"name"`
								Text     string `json:"text"`
								Date     string `json:"date"`
								Field    struct {
									Name string `json:"name"`
								} `json:"field"`
							} `json:"nodes"`
						} `json:"fieldValues"`
					} `json:"nodes"`
				} `json:"items"`
			} `json:"node"`
		}
		vars := map[string]any{"project": projectNodeID}
		if cursor != nil {
			vars["cursor"] = *cursor
		}
		if err := c.graphql(ctx, q, vars, &resp); err != nil {
			return nil, err
		}
		for _, n := range resp.Node.Items.Nodes {
			it := Item{
				ItemID:       n.ID,
				ContentType:  n.Type,
				ContentID:    n.Content.ID,
				Title:        n.Content.Title,
				Body:         n.Content.Body,
				Repo:         n.Content.Repository.NameWithOwner,
				Number:       n.Content.Number,
				State:        n.Content.State,
				URL:          n.Content.URL,
				ParentNumber: n.Content.Parent.Number,
				Fields:       map[string]string{},
			}
			for _, l := range n.Content.Labels.Nodes {
				it.Labels = append(it.Labels, l.Name)
			}
			for _, fv := range n.FieldValues.Nodes {
				if fv.Field.Name == "" {
					continue
				}
				switch {
				case fv.Name != "":
					it.Fields[fv.Field.Name] = fv.Name
				case fv.Text != "":
					it.Fields[fv.Field.Name] = fv.Text
				case fv.Date != "":
					it.Fields[fv.Field.Name] = fv.Date
				}
			}
			items = append(items, it)
		}
		if !resp.Node.Items.PageInfo.HasNextPage {
			break
		}
		end := resp.Node.Items.PageInfo.EndCursor
		cursor = &end
	}
	return items, nil
}

// ---------------------------------------------------------------------------
// Mutations (outbound sync-back)
// ---------------------------------------------------------------------------

// SetSingleSelectValue writes a single-select field option on a board item.
func (c *Client) SetSingleSelectValue(ctx context.Context, projectNodeID, itemID, fieldID, optionID string) error {
	const m = `
mutation($project:ID!, $item:ID!, $field:ID!, $option:String!) {
  updateProjectV2ItemFieldValue(input:{
    projectId:$project, itemId:$item, fieldId:$field,
    value:{ singleSelectOptionId:$option }
  }) { projectV2Item { id } }
}`
	return c.graphql(ctx, m, map[string]any{
		"project": projectNodeID, "item": itemID, "field": fieldID, "option": optionID,
	}, nil)
}

// AddIssueComment posts a comment on the backing repo issue via REST.
// repo is "owner/name", number is the issue number.
func (c *Client) AddIssueComment(ctx context.Context, repo string, number int, bodyText string) error {
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", restEndpoint, repo, number)
	payload, _ := json.Marshal(map[string]string{"body": bodyText})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("add comment http %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
