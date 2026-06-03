package identitysync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GroupSource yields the current Google Workspace membership for a set of
// group emails. The BigQuery client implements it in production; tests inject
// a fake so the reconciler is exercised without a real data lake.
type GroupSource interface {
	// FetchGroupMembers returns group email -> ACTIVE USER member emails
	// (lowercased). Every requested email is present as a key; a group with no
	// active user members maps to an empty slice.
	FetchGroupMembers(ctx context.Context, groupEmails []string) (map[string][]string, error)
}

// bigQuerySource reads google_admin.group_members from the firtal data lake.
type bigQuerySource struct {
	client  *bigquery.Client
	project string
	dataset string
	table   string
}

// newBigQuerySource constructs a BigQuery-backed GroupSource. The caller owns
// the lifetime; Close releases the underlying client.
func newBigQuerySource(ctx context.Context, cfg config) (*bigQuerySource, error) {
	var opts []option.ClientOption
	if cfg.CredentialJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(cfg.CredentialJSON)))
	}
	client, err := bigquery.NewClient(ctx, cfg.Project, opts...)
	if err != nil {
		return nil, fmt.Errorf("bigquery client: %w", err)
	}
	return &bigQuerySource{
		client:  client,
		project: cfg.Project,
		dataset: cfg.Dataset,
		table:   cfg.Table,
	}, nil
}

// memberRow is one (group, member) pair from the flattened query.
type memberRow struct {
	GroupEmail  string `bigquery:"group_email"`
	MemberEmail string `bigquery:"member_email"`
}

// FetchGroupMembers runs a single query that picks the latest Hevo snapshot
// per group (the table keeps one row per daily load) and flattens its nested
// members array down to active human members.
func (s *bigQuerySource) FetchGroupMembers(ctx context.Context, groupEmails []string) (map[string][]string, error) {
	tableRef := fmt.Sprintf("`%s.%s.%s`", s.project, s.dataset, s.table)
	query := s.client.Query(fmt.Sprintf(`
WITH latest AS (
  SELECT
    g.group.email AS group_email,
    g.members.data.members AS members,
    ROW_NUMBER() OVER (
      PARTITION BY g.group.email
      ORDER BY g.__hevo__loaded_at DESC
    ) AS rn
  FROM %s g
  WHERE g.group.email IN UNNEST(@emails)
)
SELECT latest.group_email AS group_email, m.email AS member_email
FROM latest, UNNEST(members) AS m
WHERE latest.rn = 1
  AND m.status = 'ACTIVE'
  AND m.type = 'USER'
  AND m.email IS NOT NULL
`, tableRef))
	query.Parameters = []bigquery.QueryParameter{{Name: "emails", Value: groupEmails}}

	it, err := query.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery read: %w", err)
	}

	// Seed every requested group so an absent group still reconciles to "empty"
	// rather than silently keeping stale members.
	out := make(map[string][]string, len(groupEmails))
	for _, e := range groupEmails {
		out[strings.ToLower(strings.TrimSpace(e))] = []string{}
	}
	for {
		var row memberRow
		err := it.Next(&row)
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bigquery iterate: %w", err)
		}
		groupEmail := strings.ToLower(strings.TrimSpace(row.GroupEmail))
		memberEmail := strings.ToLower(strings.TrimSpace(row.MemberEmail))
		if memberEmail == "" {
			continue
		}
		out[groupEmail] = append(out[groupEmail], memberEmail)
	}
	return out, nil
}

// Close releases the BigQuery client.
func (s *bigQuerySource) Close() error { return s.client.Close() }
