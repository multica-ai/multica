package service

import (
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// helper: pgtype.UUID with arbitrary bytes for equality assertions.
func uuidLike(seed byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = seed + byte(i)
	}
	u.Valid = true
	return u
}

func TestExtractBusinessLineTokensSingleObject(t *testing.T) {
	got := extractBusinessLineTokens(map[string]any{
		"option_id":          "child-id",
		"option_name":        "活动中心-Event",
		"parent_option_id":   "parent-id",
		"parent_option_name": "玩家服务组",
	})
	want := []FeishuBusinessLineToken{{
		ID: "child-id", Name: "活动中心-Event",
		ParentID: "parent-id", ParentName: "玩家服务组",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestExtractBusinessLineTokensArray(t *testing.T) {
	got := extractBusinessLineTokens([]any{
		map[string]any{"id": "a-id", "name": "A"},
		map[string]any{"id": "b-id", "name": "B"},
	})
	if len(got) != 2 || got[0].ID != "a-id" || got[1].Name != "B" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractBusinessLineTokensPrimitiveString(t *testing.T) {
	got := extractBusinessLineTokens("opt-1")
	if len(got) != 1 || got[0].ID != "opt-1" || got[0].Name != "" {
		t.Fatalf("got %#v", got)
	}
}

func TestExtractBusinessLineTokensNilEmpty(t *testing.T) {
	if got := extractBusinessLineTokens(nil); got != nil {
		t.Fatalf("nil → %#v", got)
	}
	if got := extractBusinessLineTokens(map[string]any{}); got != nil {
		t.Fatalf("empty map → %#v", got)
	}
}

func TestMatchBusinessLineRouteLeafIDWins(t *testing.T) {
	leafProj := uuidLike(1)
	parentProj := uuidLike(2)
	routes := []db.FeishuProjectBusinessLineRoute{
		{ProjectID: parentProj, BusinessLineID: "parent-id", BusinessLineName: "Parent"},
		{ProjectID: leafProj, BusinessLineID: "leaf-id", BusinessLineName: "Leaf"},
	}
	tokens := []FeishuBusinessLineToken{
		{ID: "leaf-id", Name: "Leaf", ParentID: "parent-id", ParentName: "Parent"},
	}
	got := matchBusinessLineRoute(routes, tokens)
	if got == nil || got.ProjectID != leafProj {
		t.Fatalf("expected leaf route to win, got %#v", got)
	}
}

func TestMatchBusinessLineRouteParentRollup(t *testing.T) {
	parentProj := uuidLike(3)
	routes := []db.FeishuProjectBusinessLineRoute{
		{ProjectID: parentProj, BusinessLineID: "parent-id", BusinessLineName: "Parent"},
	}
	tokens := []FeishuBusinessLineToken{
		{ID: "child-not-mapped", Name: "Child", ParentID: "parent-id", ParentName: "Parent"},
	}
	got := matchBusinessLineRoute(routes, tokens)
	if got == nil || got.ProjectID != parentProj {
		t.Fatalf("parent rollup expected, got %#v", got)
	}
}

func TestMatchBusinessLineRouteNameFallback(t *testing.T) {
	proj := uuidLike(4)
	routes := []db.FeishuProjectBusinessLineRoute{
		{ProjectID: proj, BusinessLineID: "stored-id", BusinessLineName: "Leaf Name"},
	}
	// Token only carries name, no id (Meego sometimes returns just labels).
	tokens := []FeishuBusinessLineToken{{Name: "Leaf Name"}}
	got := matchBusinessLineRoute(routes, tokens)
	if got == nil || got.ProjectID != proj {
		t.Fatalf("name fallback expected, got %#v", got)
	}
}

func TestMatchBusinessLineRouteNoMatch(t *testing.T) {
	routes := []db.FeishuProjectBusinessLineRoute{
		{ProjectID: uuidLike(5), BusinessLineID: "unrelated", BusinessLineName: "Other"},
	}
	tokens := []FeishuBusinessLineToken{{ID: "x", Name: "X", ParentID: "y", ParentName: "Y"}}
	if got := matchBusinessLineRoute(routes, tokens); got != nil {
		t.Fatalf("expected no match, got %#v", got)
	}
}

func TestMatchBusinessLineRouteEmptyInputs(t *testing.T) {
	if got := matchBusinessLineRoute(nil, []FeishuBusinessLineToken{{ID: "x"}}); got != nil {
		t.Fatalf("nil routes → %#v", got)
	}
	if got := matchBusinessLineRoute([]db.FeishuProjectBusinessLineRoute{{BusinessLineID: "a"}}, nil); got != nil {
		t.Fatalf("nil tokens → %#v", got)
	}
}

func TestParseFeishuProjectSearchExtractsBusinessLine(t *testing.T) {
	// Meego search response embeds the biz-line value inside `fields[i].field_value`,
	// keyed by `field_key`. Verify that with a configured field key we pull it out;
	// without, we leave BusinessLineTokens nil so legacy 1:1 sync still skips routing.
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   123,
				"name": "test-item",
				"fields": []any{
					map[string]any{
						"field_key": "business",
						"field_value": map[string]any{
							"option_id":          "leaf-id",
							"option_name":        "活动中心-Event",
							"parent_option_id":   "parent-id",
							"parent_option_name": "玩家服务组",
						},
					},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "proj", "business")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(items[0].BusinessLineTokens) != 1 {
		t.Fatalf("expected 1 token, got %#v", items[0].BusinessLineTokens)
	}
	tok := items[0].BusinessLineTokens[0]
	if tok.ID != "leaf-id" || tok.Name != "活动中心-Event" || tok.ParentID != "parent-id" {
		t.Fatalf("unexpected token: %#v", tok)
	}

	// Without configured field key, no tokens are extracted (1:1 legacy mode).
	items = parseFeishuProjectSearch(payload, "issue", "proj", "")
	if len(items) != 1 || len(items[0].BusinessLineTokens) != 0 {
		t.Fatalf("legacy mode should have no tokens, got %#v", items[0].BusinessLineTokens)
	}
}

func TestParseFeishuProjectBusinessLineTree(t *testing.T) {
	// Shape borrowed from the postman /business/all sample variants — nested children.
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id": "parent-1", "name": "玩家服务组",
				"children": []any{
					map[string]any{"id": "leaf-a", "name": "网页"},
					map[string]any{"id": "leaf-b", "name": "活动中心-Event"},
				},
			},
			map[string]any{"id": "parent-2", "name": "内部产品组"},
		},
	}
	tree := parseFeishuProjectBusinessLineTree(payload)
	if len(tree) != 2 {
		t.Fatalf("expected 2 roots, got %d (%#v)", len(tree), tree)
	}
	if tree[0].ID != "parent-1" || len(tree[0].Children) != 2 {
		t.Fatalf("unexpected root[0]: %#v", tree[0])
	}
	if tree[0].Children[0].ParentID != "parent-1" || tree[0].Children[0].ParentName != "玩家服务组" {
		t.Fatalf("child parent fields not propagated: %#v", tree[0].Children[0])
	}
}

func TestParseFeishuProjectFieldMetasDedupes(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{"field_key": "business", "name": "业务线", "field_type_key": "_select"},
			map[string]any{"field_key": "owner", "name": "负责人"},
			// duplicate — should be skipped
			map[string]any{"field_key": "business", "name": "业务线 dup"},
		},
	}
	fields := parseFeishuProjectFieldMetas(payload)
	if len(fields) != 2 {
		t.Fatalf("expected 2 unique fields, got %d (%#v)", len(fields), fields)
	}
	// Order isn't important for the assertion below — just that both keys appear once.
	seen := map[string]string{}
	for _, f := range fields {
		seen[f.Key] = f.Name
	}
	if seen["business"] != "业务线" || seen["owner"] != "负责人" {
		t.Fatalf("unexpected fields: %#v", seen)
	}
}

func TestFeishuFieldDisplayNameAcceptsBothShapes(t *testing.T) {
	// Filter response uses `name`; meta response uses `field_name`. Same helper.
	if got := feishuFieldDisplayName(map[string]any{"name": "经办人"}); got != "经办人" {
		t.Fatalf("name → %q", got)
	}
	if got := feishuFieldDisplayName(map[string]any{"field_name": "处理人"}); got != "处理人" {
		t.Fatalf("field_name → %q", got)
	}
	if got := feishuFieldDisplayName(map[string]any{"name": ""}); got != "" {
		t.Fatalf("empty → %q", got)
	}
	if got := feishuFieldDisplayName(map[string]any{"name": nil}); got != "" {
		t.Fatalf("nil → %q (was %v, expected empty)", got, "<nil>")
	}
}

func TestParseFeishuProjectSearchIndexesByDisplayName(t *testing.T) {
	// Two work items: one with the standard `owner` field_key, one with a custom
	// `field_xxx` field_key but a "经办人" display name. The owner-email extractor
	// should resolve both.
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   1,
				"name": "standard-owner-item",
				"fields": []any{
					map[string]any{
						"field_key":   "owner",
						"name":        "Owner",
						"field_value": "user-1",
					},
				},
				"user_details": []any{
					map[string]any{"user_key": "user-1", "email": "alice@example.com"},
				},
			},
			map[string]any{
				"id":   2,
				"name": "custom-field-item",
				"fields": []any{
					map[string]any{
						"field_key":   "field_abc123",
						"name":        "经办人",
						"field_value": "user-2",
					},
				},
				"user_details": []any{
					map[string]any{"user_key": "user-2", "email": "bob@example.com"},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "proj", "")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].OwnerEmail != "alice@example.com" {
		t.Fatalf("standard owner: got %q", items[0].OwnerEmail)
	}
	if items[1].OwnerEmail != "bob@example.com" {
		t.Fatalf("custom field via display name 经办人: got %q", items[1].OwnerEmail)
	}
}

func TestFeishuProjectOwnerEmailChineseNameFallback(t *testing.T) {
	// Owner field_keys are absent; the assignee is stored under a Chinese display
	// name like 处理人. We treat the email pattern in the value as ground truth.
	record := map[string]string{
		"处理人": "Carol <carol@example.com>",
	}
	if got := feishuProjectOwnerEmail(record, nil); got != "carol@example.com" {
		t.Fatalf("处理人 fallback: got %q", got)
	}

	// Same but value is just a user_key — resolve via userEmails.
	record2 := map[string]string{
		"经办人": "user-99",
	}
	userEmails := map[string]string{"user-99": "dave@example.com"}
	if got := feishuProjectOwnerEmail(record2, userEmails); got != "dave@example.com" {
		t.Fatalf("经办人 + user_keys: got %q", got)
	}

	// Standard field_key still wins when both present (current_status_operator is
	// the most specific signal — see feishuProjectOwnerEmail comment).
	record3 := map[string]string{
		"current_status_operator": "eve@example.com",
		"处理人":                     "frank@example.com",
	}
	if got := feishuProjectOwnerEmail(record3, nil); got != "eve@example.com" {
		t.Fatalf("expected current_status_operator to win, got %q", got)
	}
}

// resolveAssignee priority chain — owner agent → owner member → fallback agent → empty.
// Hitting these branches end-to-end needs a DB; here we cover the deterministic shape
// (return values for valid/invalid inputs) without exercising the DB-dependent owner
// lookup methods. The "owner found as member" / "owner found as agent" branches are
// covered by the integration test that pre-existed in feishu_project_test.go.

func TestResolveAssigneeFallbackUsedWhenOwnerHasNoMatch(t *testing.T) {
	svc := &FeishuProjectSyncService{Queries: nil} // Queries unused on the fallback path
	fallbackAgent := uuidLike(20)

	// Empty OwnerEmail → resolveOwnerAgent / resolveOwnerMember short-circuit to empty
	// without touching Queries, so we fall through to fallback.
	cfg := db.FeishuProjectIntegration{AssignOpenItemsToOwnerAgent: false}
	item := FeishuProjectWorkItem{OwnerEmail: ""}
	gotType, gotID := svc.resolveAssignee(t.Context(), cfg, item, "todo", pgtype.Text{}, pgtype.UUID{}, fallbackAgent)
	if gotType.String != "agent" || gotID != fallbackAgent {
		t.Fatalf("expected fallback agent, got %v / %v", gotType, gotID)
	}
}

func TestResolveAssigneeNoFallbackReturnsEmpty(t *testing.T) {
	svc := &FeishuProjectSyncService{Queries: nil}
	cfg := db.FeishuProjectIntegration{AssignOpenItemsToOwnerAgent: false}
	item := FeishuProjectWorkItem{OwnerEmail: ""}
	gotType, gotID := svc.resolveAssignee(t.Context(), cfg, item, "todo", pgtype.Text{}, pgtype.UUID{}, pgtype.UUID{})
	if gotType.Valid || gotID.Valid {
		t.Fatalf("expected empty, got %v / %v", gotType, gotID)
	}
}

func TestResolveAssigneePreservesCurrentForNonAssignableStatus(t *testing.T) {
	// AssignOpenItemsToOwnerAgent ON + status NOT "todo" → preserve current assignee,
	// don't even look at fallback. This protects manual mid-workflow reassignments.
	svc := &FeishuProjectSyncService{Queries: nil}
	cfg := db.FeishuProjectIntegration{AssignOpenItemsToOwnerAgent: true}
	item := FeishuProjectWorkItem{OwnerEmail: "alice@example.com"}
	currentType := pgtype.Text{String: "member", Valid: true}
	currentID := uuidLike(99)
	fallbackAgent := uuidLike(20)

	gotType, gotID := svc.resolveAssignee(t.Context(), cfg, item, "in_progress", currentType, currentID, fallbackAgent)
	if gotType != currentType || gotID != currentID {
		t.Fatalf("expected current preserved, got %v / %v", gotType, gotID)
	}
}

func TestFindFeishuProjectFieldByKey(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{"field_key": "owner", "name": "Owner"},
			map[string]any{
				"tab": "details",
				"fields": []any{
					map[string]any{"field_key": "business", "name": "业务线"},
				},
			},
		},
	}
	got := findFeishuProjectFieldByKey(payload, "business")
	if got == nil {
		t.Fatalf("expected to find nested 'business' field, got nil")
	}
	if got["name"] != "业务线" {
		t.Fatalf("unexpected node: %#v", got)
	}
	if findFeishuProjectFieldByKey(payload, "nonexistent") != nil {
		t.Fatalf("expected nil for unknown key")
	}
}

func TestExtractFeishuProjectFieldOptionTree(t *testing.T) {
	// option with children — the typical 2-level select for biz-line type fields.
	field := map[string]any{
		"field_key": "business",
		"option": []any{
			map[string]any{
				"id": "parent-1", "name": "玩家服务组",
				"children": []any{
					map[string]any{"id": "leaf-a", "name": "网页"},
				},
			},
		},
	}
	tree := extractFeishuProjectFieldOptionTree(field)
	if len(tree) != 1 || tree[0].ID != "parent-1" || len(tree[0].Children) != 1 {
		t.Fatalf("unexpected tree: %#v", tree)
	}

	// `options` plural also accepted (some fields use this).
	field2 := map[string]any{
		"field_key": "module",
		"options": []any{
			map[string]any{"option_id": "m1", "option_name": "Module A"},
		},
	}
	tree2 := extractFeishuProjectFieldOptionTree(field2)
	if len(tree2) != 1 || tree2[0].ID != "m1" || tree2[0].Name != "Module A" {
		t.Fatalf("unexpected tree2: %#v", tree2)
	}

	// Field without options (text field, etc.) — return nil so caller can show empty state.
	field3 := map[string]any{"field_key": "title", "field_type_key": "_text"}
	if tree3 := extractFeishuProjectFieldOptionTree(field3); tree3 != nil {
		t.Fatalf("expected nil for text field, got %#v", tree3)
	}
}
