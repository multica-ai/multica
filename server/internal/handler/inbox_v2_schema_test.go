package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Schema contracts for the inbox v2 storage shape.
//
// These are properties of the migrations rather than of any Go code, so they
// are asserted against a live database: a partial index that silently indexes
// everything, or a unique index that legacy NULL rows happen to satisfy, would
// look identical in the migration file and only show up as drift under load.

func inboxV2Workspace(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO workspace (id, name, slug) VALUES ($1, 'inbox v2 schema', $2)`,
		id, "inboxv2-schema-"+id[:8]); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

func insertLegacyItem(t *testing.T, workspaceID string, deliveryKey *string) error {
	t.Helper()
	return insertLegacyItemFor(t, workspaceID, uuid.NewString(), deliveryKey)
}

func insertLegacyItemFor(t *testing.T, workspaceID, recipientID string, deliveryKey *string) error {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, title, delivery_key)
VALUES ($1, 'member', $2, 'new_comment', 'schema test', $3)
`, workspaceID, recipientID, deliveryKey)
	return err
}

// The five new columns must stay nullable. Every pre-existing row has NULL in
// them and is claimed lazily, per user — making any of them NOT NULL would turn
// the deploy into a migration that has to finish before the release is safe.
func TestInboxItemV2ColumnsAreNullable(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	rows, err := testPool.Query(context.Background(), `
SELECT column_name, is_nullable
FROM information_schema.columns
WHERE table_name = 'inbox_item'
  AND column_name IN ('group_id', 'event_seq', 'target_kind', 'target_id', 'delivery_key')
`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	seen := map[string]string{}
	for rows.Next() {
		var name, nullable string
		if err := rows.Scan(&name, &nullable); err != nil {
			t.Fatal(err)
		}
		seen[name] = nullable
	}
	for _, col := range []string{"group_id", "event_seq", "target_kind", "target_id", "delivery_key"} {
		got, ok := seen[col]
		if !ok {
			t.Errorf("inbox_item.%s is missing", col)
			continue
		}
		if got != "YES" {
			t.Errorf("inbox_item.%s is NOT NULL; unclaimed rows could not exist", col)
		}
	}
}

// delivery_key deduplicates new deliveries without touching history: legacy
// rows all carry NULL and must remain insertable alongside each other, while a
// repeated key for the SAME recipient must collide.
func TestInboxItemDeliveryKeyUniqueOnlyForNewRows(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ws := inboxV2Workspace(t)

	if err := insertLegacyItem(t, ws, nil); err != nil {
		t.Fatalf("first legacy row: %v", err)
	}
	if err := insertLegacyItem(t, ws, nil); err != nil {
		t.Fatalf("a second NULL delivery_key must not collide with the first: %v", err)
	}

	key := "v1:" + uuid.NewString()
	recipient := uuid.NewString()
	if err := insertLegacyItemFor(t, ws, recipient, &key); err != nil {
		t.Fatalf("first keyed row: %v", err)
	}
	err := insertLegacyItemFor(t, ws, recipient, &key)
	if err == nil {
		t.Fatal("a repeated delivery_key must be rejected for the same recipient")
	}
	if !strings.Contains(err.Error(), "inbox_item_delivery_key_uidx") {
		t.Fatalf("expected the delivery-key index to reject it, got: %v", err)
	}
}

// The uniqueness scope is (workspace, recipient, key), not the key alone.
//
// A global key makes correctness depend on every producer remembering to fold
// the recipient into the string it builds. One that forgets — and
// "issue:<id>:comment:<id>" is the obvious shape to reach for — does not
// produce a duplicate, it produces a MISSING notification: the second
// recipient's insert collides with the first recipient's row and is dropped.
// Scoping the constraint means a producer can only ever deduplicate a person
// against themselves.
func TestInboxItemDeliveryKeyIsScopedToTheRecipient(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ws := inboxV2Workspace(t)
	key := "v1:" + uuid.NewString()

	if err := insertLegacyItemFor(t, ws, uuid.NewString(), &key); err != nil {
		t.Fatalf("first recipient: %v", err)
	}
	if err := insertLegacyItemFor(t, ws, uuid.NewString(), &key); err != nil {
		t.Fatalf("a second recipient must still receive their own copy: %v", err)
	}

	other := inboxV2Workspace(t)
	if err := insertLegacyItemFor(t, other, uuid.NewString(), &key); err != nil {
		t.Fatalf("another tenant must not collide on the same key: %v", err)
	}
}

// The v2 indexes must be PARTIAL. A plain index here would cover the entire
// pre-migration history to enforce constraints that cannot apply to it, and
// would keep growing after the migration is complete instead of shrinking to
// nothing.
func TestInboxV2IndexesArePartial(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	want := map[string]string{
		"inbox_item_delivery_key_uidx": "delivery_key IS NOT NULL",
		"inbox_item_group_seq_uidx":    "group_id IS NOT NULL",
		"inbox_item_unclaimed_idx":     "group_id IS NULL",
	}
	for index, predicate := range want {
		var def string
		if err := testPool.QueryRow(context.Background(),
			`SELECT indexdef FROM pg_indexes WHERE indexname = $1`, index).Scan(&def); err != nil {
			t.Errorf("%s is missing: %v", index, err)
			continue
		}
		if !strings.Contains(def, "WHERE") {
			t.Errorf("%s is not partial: %s", index, def)
			continue
		}
		if !strings.Contains(strings.ReplaceAll(def, "((", "("), predicate) {
			t.Errorf("%s predicate = %q, want it to filter on %q", index, def, predicate)
		}
	}
}

// One person plus one source is exactly one group. This is the constraint the
// old schema could not express, and the reason unread counts, the archived view
// and the jump target could each disagree.
func TestInboxGroupIdentityIsUnique(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ws := inboxV2Workspace(t)
	recipient := uuid.NewString()
	source := uuid.NewString()

	insert := func() error {
		_, err := testPool.Exec(context.Background(), `
INSERT INTO inbox_group (workspace_id, recipient_id, source_kind, source_id)
VALUES ($1, $2, 'issue', $3)
`, ws, recipient, source)
		return err
	}
	if err := insert(); err != nil {
		t.Fatalf("first group: %v", err)
	}
	err := insert()
	if err == nil {
		t.Fatal("a second group for the same (person, source) must be rejected")
	}
	if !strings.Contains(err.Error(), "inbox_group_identity_uidx") {
		t.Fatalf("expected the identity index to reject it, got: %v", err)
	}
}

// source_id is NOT NULL for every kind, standalone included. A nullable column
// would defeat the identity index, because Postgres treats NULLs as distinct.
func TestInboxGroupSourceIDIsRequired(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ws := inboxV2Workspace(t)
	_, err := testPool.Exec(context.Background(), `
INSERT INTO inbox_group (workspace_id, recipient_id, source_kind, source_id)
VALUES ($1, gen_random_uuid(), 'standalone', NULL)
`, ws)
	if err == nil {
		t.Fatal("a group without a source_id must be rejected")
	}
}

// The primary key must be backed by the index built CONCURRENTLY in 267 rather
// than one the constraint built itself under a blocking lock.
func TestInboxGroupPrimaryKeyAdoptedTheConcurrentIndex(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	var constraintName, indexName string
	if err := testPool.QueryRow(context.Background(), `
SELECT conname, conindid::regclass::text
FROM pg_constraint
WHERE conrelid = 'inbox_group'::regclass AND contype = 'p'
`).Scan(&constraintName, &indexName); err != nil {
		t.Fatalf("inbox_group has no primary key: %v", err)
	}
	if constraintName != "inbox_group_pkey" || indexName != "inbox_group_pkey" {
		t.Fatalf("primary key = %s backed by %s, want the adopted index", constraintName, indexName)
	}
}
