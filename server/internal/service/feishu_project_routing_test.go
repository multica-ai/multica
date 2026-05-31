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

func TestParseFeishuProjectSearchIndexesFieldValuesForLabelSync(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   123,
				"name": "bug-helper-item",
				"fields": []any{
					map[string]any{
						"field_key": "field_c1f194",
						"name":      "BUG提单助手",
						"field_value": map[string]any{
							"key_label_value": map[string]any{
								"key":   "govi_kagd",
								"label": "是",
							},
						},
					},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "partopia", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !feishuProjectLabelRuleMatches(items[0], FeishuProjectLabelSyncRule{
		Enabled:  true,
		FieldKey: "field_c1f194",
		Match:    "是",
	}) {
		t.Fatalf("expected field_key label rule to match, values=%#v", items[0].FieldValues)
	}
	if !feishuProjectLabelRuleMatches(items[0], FeishuProjectLabelSyncRule{
		Enabled:  true,
		FieldKey: "BUG提单助手",
		Match:    "govi_kagd",
	}) {
		t.Fatalf("expected display-name label rule to match option key, values=%#v", items[0].FieldValues)
	}
}

func TestParseFeishuProjectSearchExtractsPriority(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   123,
				"name": "priority-item",
				"fields": []any{
					map[string]any{
						"field_key": "field_priority",
						"name":      "优先级",
						"field_value": map[string]any{
							"key_label_value": map[string]any{
								"key":   "priority_1",
								"label": "P1",
							},
						},
					},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "partopia", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Priority != "P1" {
		t.Fatalf("expected priority P1, got %q", items[0].Priority)
	}
	if got := mapFeishuPriority(items[0].Priority); got != "high" {
		t.Fatalf("expected P1 to map to high, got %q", got)
	}
}

func TestMapFeishuPriority(t *testing.T) {
	cases := map[string]string{
		"P0":     "urgent",
		"严重":     "high",
		"普通":     "medium",
		"低优先级":   "low",
		"无优先级":   "none",
		"custom": "",
		"":       "",
	}
	for in, want := range cases {
		if got := mapFeishuPriority(in); got != want {
			t.Fatalf("mapFeishuPriority(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFeishuProjectMQLUsesOptionLabelForFieldValues(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"issues": []any{
				map[string]any{
					"moql_field_list": []any{
						map[string]any{
							"key": "work_item_id",
							"value": map[string]any{
								"string_value": "7004582524",
							},
						},
						map[string]any{
							"key": "field_c1f194",
							"value": map[string]any{
								"key_label_value": map[string]any{
									"key":   "govi_kagd",
									"label": "是",
								},
							},
						},
					},
				},
			},
		},
	}
	items := parseFeishuProjectMQL(payload, "issue", "partopia")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !feishuProjectLabelRuleMatches(items[0], FeishuProjectLabelSyncRule{
		Enabled:  true,
		FieldKey: "field_c1f194",
		Match:    "是",
	}) {
		t.Fatalf("expected MQL option label to match, values=%#v", items[0].FieldValues)
	}
}

func TestFeishuProjectLabelSyncCleanupKeepsSharedDesiredLabel(t *testing.T) {
	labelID := uuidLike(41)
	binding := db.FeishuProjectLabelSyncBinding{
		RuleID:  "old-rule",
		LabelID: labelID,
	}
	desiredRuleLabels := map[string]pgtype.UUID{
		"active-rule": labelID,
	}
	desiredLabelIDs := map[pgtype.UUID]bool{
		labelID: true,
	}

	deleteBinding, detachLabel := feishuProjectLabelSyncCleanupAction(binding, desiredRuleLabels, desiredLabelIDs)
	if !deleteBinding || detachLabel {
		t.Fatalf("expected stale binding deletion without shared label detach, got delete=%v detach=%v", deleteBinding, detachLabel)
	}
}

func TestFeishuProjectLabelSyncCleanupDetachesUnusedLabel(t *testing.T) {
	labelID := uuidLike(42)
	binding := db.FeishuProjectLabelSyncBinding{
		RuleID:  "old-rule",
		LabelID: labelID,
	}

	deleteBinding, detachLabel := feishuProjectLabelSyncCleanupAction(binding, map[string]pgtype.UUID{}, map[pgtype.UUID]bool{})
	if !deleteBinding || !detachLabel {
		t.Fatalf("expected stale binding deletion with unused label detach, got delete=%v detach=%v", deleteBinding, detachLabel)
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

func TestParseFeishuProjectFieldMetasDedupesAndScopes(t *testing.T) {
	// Mirrors the /field/all response: flat list under "data", each entry carries
	// work_item_scopes. The parser must dedupe by field_key, filter to the requested
	// scope, and surface custom-field display names (e.g. "BUG提单助手" / field_c1f194,
	// which /work_item/issue/meta silently drops — the whole reason we switched).
	payload := map[string]any{
		"data": []any{
			map[string]any{"field_key": "business", "field_name": "业务线", "field_type_key": "_select", "work_item_scopes": []any{"issue"}},
			map[string]any{"field_key": "owner", "field_name": "负责人", "work_item_scopes": []any{"issue", "story"}},
			map[string]any{"field_key": "field_c1f194", "field_name": "BUG提单助手", "field_type_key": "radio", "is_custom_field": true, "work_item_scopes": []any{"issue"}},
			// Different scope — must be filtered out.
			map[string]any{"field_key": "story_only", "field_name": "需求字段", "work_item_scopes": []any{"story"}},
			// Duplicate of "business" — must be skipped.
			map[string]any{"field_key": "business", "field_name": "业务线 dup", "work_item_scopes": []any{"issue"}},
		},
	}
	fields := parseFeishuProjectFieldMetas(payload, "issue")
	seen := map[string]string{}
	for _, f := range fields {
		seen[f.Key] = f.Name
	}
	if seen["business"] != "业务线" || seen["owner"] != "负责人" || seen["field_c1f194"] != "BUG提单助手" {
		t.Fatalf("expected issue-scoped fields with first-name wins, got %#v", seen)
	}
	if _, leaked := seen["story_only"]; leaked {
		t.Fatalf("story-only field leaked into issue scope: %#v", seen)
	}
	if len(fields) != 3 {
		t.Fatalf("expected 3 unique fields (business, owner, field_c1f194), got %d (%#v)", len(fields), fields)
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
	// Two work items, both modelling realistic Meego shapes for the handler/assignee:
	// one with a built-in `issue_operator` (displayed as 经办人), one with a custom
	// `field_xxx` whose display name is 经办人. Both should resolve the user_key to an
	// email via user_details. We don't include a `field_key: "owner"` case — in Meego
	// that field is the CREATOR, not the assignee (see feishuProjectOwnerEmail comment).
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   1,
				"name": "issue-operator-item",
				"fields": []any{
					map[string]any{
						"field_key":   "issue_operator",
						"name":        "经办人",
						"field_value": []any{"user-1"},
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
		t.Fatalf("issue_operator via 经办人 display name: got %q", items[0].OwnerEmail)
	}
	if items[1].OwnerEmail != "bob@example.com" {
		t.Fatalf("custom field via 经办人 display name: got %q", items[1].OwnerEmail)
	}
}

func TestParseFeishuProjectSearchResolvesOwnerFromUserObjects(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   1,
				"name": "owner-object-with-email",
				"fields": []any{
					map[string]any{
						"field_key": "owner",
						"name":      "负责人",
						"field_value": map[string]any{
							"id":    "user-1",
							"name":  "Alice",
							"email": "alice@example.com",
						},
					},
				},
			},
			map[string]any{
				"id":   2,
				"name": "owner-object-with-id",
				"fields": []any{
					map[string]any{
						"field_key": "field_abc123",
						"name":      "经办人",
						"field_value": []any{
							map[string]any{
								"id":   "user-2",
								"name": "Bob",
							},
						},
					},
				},
				"user_details": []any{
					map[string]any{"id": "user-2", "email": "bob@example.com"},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "proj", "")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].OwnerEmail != "alice@example.com" {
		t.Fatalf("owner object email: got %q", items[0].OwnerEmail)
	}
	if items[1].OwnerEmail != "bob@example.com" {
		t.Fatalf("owner object id via user_details: got %q", items[1].OwnerEmail)
	}
}

func TestParseFeishuProjectSearchUsesOperatorRoleAsOwner(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   1,
				"name": "operator-role-owner",
				"fields": []any{
					map[string]any{
						"field_key":   "current_status_operator",
						"name":        "当前负责人",
						"field_value": "current@example.com",
					},
				},
				"work_item_attribute": map[string]any{
					"role_members": []any{
						map[string]any{
							"key":  "operator",
							"name": "处理人",
							"members": []any{
								map[string]any{"email": "operator@example.com", "key": "user-1", "name": "Owner"},
							},
						},
					},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "proj", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].OwnerEmail != "operator@example.com" {
		t.Fatalf("expected operator role owner, got %q", items[0].OwnerEmail)
	}
}

func TestParseFeishuProjectSearchUsesRoleOwnersFieldAsOwner(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"id":   1,
				"name": "role-owners-field-owner",
				"fields": []any{
					map[string]any{
						"field_key":      "role_owners",
						"field_type_key": "role_owners",
						"field_value": []any{
							map[string]any{
								"role":   "role_project_issue_operator",
								"owners": []any{"user-1"},
							},
						},
					},
				},
				"user_details": []any{
					map[string]any{"user_key": "user-1", "email": "operator@example.com"},
				},
			},
		},
	}
	items := parseFeishuProjectSearch(payload, "issue", "proj", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].OwnerEmail != "operator@example.com" {
		t.Fatalf("expected role_owners owner, got %q", items[0].OwnerEmail)
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

	// Meego's `owner` field is the CREATOR, not the assignee — see
	// feishuProjectOwnerEmail for the field_name="创建者" evidence. When the only
	// assignee-shaped signal is a Chinese display name (处理人), that's what should
	// win; the (mis-named) `owner` value must be ignored, and `current_status_operator`
	// is the wrong workflow-position concept either way.
	record3 := map[string]string{
		"current_status_operator": "eve@example.com",
		"处理人":                     "frank@example.com",
		"owner":                   "grace@example.com",
	}
	if got := feishuProjectOwnerEmail(record3, nil); got != "frank@example.com" {
		t.Fatalf("expected 处理人 to win (owner is the creator, must be ignored), got %q", got)
	}

	record4 := map[string]string{
		"current_status_operator": "eve@example.com",
		"当前处理人":                   "frank@example.com",
	}
	if got := feishuProjectOwnerEmail(record4, nil); got != "" {
		t.Fatalf("expected current handler fields to be ignored, got %q", got)
	}

	// Regression for partopia#7004726014: 经办人 is empty in Feishu (no operator role,
	// no 处理人/经办人/负责人 fields), but `owner` carries the creator's user_key.
	// We must NOT use that — the Multica issue should stay unassigned so the
	// integration's fallback agent (or empty) wins. Before the fix, this resolved
	// to the creator's email and silently auto-assigned them.
	record5 := map[string]string{
		"owner":          "7052790644598751234",
		"issue_reporter": "7052790644598751234",
	}
	creatorEmails := map[string]string{"7052790644598751234": "jinnanxu@lilith.com"}
	if got := feishuProjectOwnerEmail(record5, creatorEmails); got != "" {
		t.Fatalf("owner=creator must not leak as assignee, got %q", got)
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
