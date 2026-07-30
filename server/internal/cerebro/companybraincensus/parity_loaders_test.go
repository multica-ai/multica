package companybraincensus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestFrozenCensusSnapshotLoaderReturnsVerifiedImmutableSnapshot(t *testing.T) {
	report, _ := parityFixture()
	const (
		workspaceID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		connectionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)
	raw := marshalFrozenCensusSnapshot(t, workspaceID, connectionID, 12, report)
	sum := sha256.Sum256(raw)
	loader := NewFrozenCensusSnapshotLoader(raw, hex.EncodeToString(sum[:]))

	loaded, err := loader.LoadFrozenCensus(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("load frozen census: %v", err)
	}
	if loaded.Version != 12 ||
		loaded.CompanyBrainConnectionID != connectionID ||
		!reflect.DeepEqual(loaded.Report, report) {
		t.Fatalf("loaded snapshot = %#v, want versioned frozen report", loaded)
	}

	loaded.Report.Actors[0].Name = "mutated by caller"
	reloaded, err := loader.LoadFrozenCensus(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("reload frozen census: %v", err)
	}
	if reloaded.Report.Actors[0].Name == loaded.Report.Actors[0].Name {
		t.Fatal("loader returned mutable shared census state")
	}
}

func TestFrozenCensusSnapshotLoaderFailsClosedOnIdentityOrChecksumMismatch(t *testing.T) {
	report, _ := parityFixture()
	const (
		workspaceID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		connectionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)
	raw := marshalFrozenCensusSnapshot(t, workspaceID, connectionID, 12, report)
	sum := sha256.Sum256(raw)
	checksum := hex.EncodeToString(sum[:])

	tests := []struct {
		name      string
		raw       []byte
		checksum  string
		workspace string
		wantError string
	}{
		{
			name:      "wrong workspace",
			raw:       raw,
			checksum:  checksum,
			workspace: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
			wantError: "workspace",
		},
		{
			name:      "tampered snapshot",
			raw:       append(append([]byte(nil), raw...), ' '),
			checksum:  checksum,
			workspace: workspaceID,
			wantError: "checksum",
		},
		{
			name:      "invalid expected checksum",
			raw:       raw,
			checksum:  "not-a-checksum",
			workspace: workspaceID,
			wantError: "checksum",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := NewFrozenCensusSnapshotLoader(test.raw, test.checksum)
			_, err := loader.LoadFrozenCensus(context.Background(), test.workspace)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadFrozenCensus() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDatabaseCurrentTargetPermissionLoaderReadsExactSourceScopedEvidence(t *testing.T) {
	pool := openParityWriterPool(t)
	workspaceID, logicalID := insertParityWriterWorkspace(t, pool, "target-loader")
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, workspaceID) })
	identity := insertParityWriterIdentity(t, pool, workspaceID, logicalID, 7)
	if _, err := pool.Exec(context.Background(), `
		UPDATE workspace_connection
		SET tools = '[
			{"name":"search","description":"Search"},
			{"name":"write","description":"Write"}
		]'::jsonb
		WHERE workspace_id = $1 AND name = 'company-brain'
	`, workspaceID); err != nil {
		t.Fatal(err)
	}

	var seenSources []string
	var seenQueries []toolpolicy.TableQuery
	policy := sourceScopedPolicyFunc(func(
		_ context.Context,
		query toolpolicy.TableQuery,
	) ([]toolpolicy.ConnectionToolVerdict, error) {
		source := query.RequestContext.ArgValues["source_id"]
		if source == "" || query.RequestContext.ArgValues["data_source_id"] != "" {
			return nil, errors.New("policy query lacks the Company Brain source_id context")
		}
		seenSources = append(seenSources, source)
		seenQueries = append(seenQueries, query)
		writeSetting := toolpolicy.SettingAsk
		if source == "shared" || source == "internal" {
			writeSetting = toolpolicy.SettingDeny
		}
		return []toolpolicy.ConnectionToolVerdict{
			{Connection: "company-brain", Tool: "write", Setting: writeSetting},
			{Connection: "another-connection", Tool: "ignored", Setting: toolpolicy.SettingAllow},
			{Connection: "company-brain", Tool: "search", Setting: toolpolicy.SettingAllow},
		}, nil
	})

	loader := NewDatabaseCurrentTargetPermissionLoader(pool, policy)
	report := Report{Connections: []connectionClaim{
		{ConnectionName: "company-brain-commercial"},
		{ConnectionName: "company-brain-internal"},
		{ConnectionName: "company-brain-shared"},
	}}
	got, err := loader.LoadCurrentTargetPermissions(
		context.Background(),
		workspaceID,
		logicalID,
		report,
	)
	if err != nil {
		t.Fatalf("load current target permissions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("target permissions = %#v, want one", got)
	}
	want := TargetPermission{
		PermissionID:             identity.PermissionID,
		CompanyBrainConnectionID: logicalID,
		AgentID:                  identity.AgentID,
		AccessVersion:            7,
		AllowedReadSources:       []string{"commercial", "shared"},
		WriteSource:              "commercial",
		ApprovalOutcomes: []ScopedToolDecision{
			{Source: "commercial", Tool: "search", Decision: "allow"},
			{Source: "commercial", Tool: "write", Decision: "ask"},
			{Source: "internal", Tool: "search", Decision: "allow"},
			{Source: "internal", Tool: "write", Decision: "deny"},
			{Source: "shared", Tool: "search", Decision: "allow"},
			{Source: "shared", Tool: "write", Decision: "deny"},
		},
		CanonicalToolCalls: []string{"search", "write"},
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("target permission differs:\n got: %#v\nwant: %#v", got[0], want)
	}
	sort.Strings(seenSources)
	if !reflect.DeepEqual(seenSources, []string{"commercial", "internal", "shared"}) {
		t.Fatalf("resolved sources = %#v", seenSources)
	}
	for _, query := range seenQueries {
		if util.UUIDToString(query.WorkspaceID) != workspaceID ||
			util.UUIDToString(query.AgentID) != identity.AgentID {
			t.Fatalf("policy identity = %#v, want workspace and agent from target row", query)
		}
	}
}

func TestDatabaseCurrentTargetPermissionLoaderFailsClosedOnIncompletePolicyEvidence(t *testing.T) {
	pool := openParityWriterPool(t)
	workspaceID, logicalID := insertParityWriterWorkspace(t, pool, "target-loader-incomplete")
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, workspaceID) })
	insertParityWriterIdentity(t, pool, workspaceID, logicalID, 8)

	policyErr := errors.New("policy unavailable")
	tests := []struct {
		name      string
		policy    sourceScopedPolicyFunc
		wantError string
	}{
		{
			name: "policy failure",
			policy: func(context.Context, toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error) {
				return nil, policyErr
			},
			wantError: policyErr.Error(),
		},
		{
			name: "missing canonical tool verdict",
			policy: func(context.Context, toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error) {
				return nil, nil
			},
			wantError: "complete canonical tool evidence",
		},
		{
			name: "non-decision verdict",
			policy: func(context.Context, toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error) {
				return []toolpolicy.ConnectionToolVerdict{{
					Connection: "company-brain",
					Tool:       "search",
					Setting:    toolpolicy.SettingInherit,
				}}, nil
			},
			wantError: "Allow/Ask/Deny",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader := NewDatabaseCurrentTargetPermissionLoader(pool, test.policy)
			_, err := loader.LoadCurrentTargetPermissions(
				context.Background(),
				workspaceID,
				logicalID,
				Report{Connections: []connectionClaim{{ConnectionName: "company-brain-commercial"}}},
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadCurrentTargetPermissions() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestDatabaseCurrentTargetPermissionLoaderReturnsNoCrossWorkspaceRows(t *testing.T) {
	pool := openParityWriterPool(t)
	workspaceID, logicalID := insertParityWriterWorkspace(t, pool, "target-loader-owned")
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, workspaceID) })
	insertParityWriterIdentity(t, pool, workspaceID, logicalID, 9)

	otherWorkspaceID, _ := insertParityWriterWorkspace(t, pool, "target-loader-other")
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, otherWorkspaceID) })
	policyCalls := 0
	loader := NewDatabaseCurrentTargetPermissionLoader(pool, sourceScopedPolicyFunc(func(
		context.Context,
		toolpolicy.TableQuery,
	) ([]toolpolicy.ConnectionToolVerdict, error) {
		policyCalls++
		return nil, nil
	}))
	got, err := loader.LoadCurrentTargetPermissions(
		context.Background(),
		otherWorkspaceID,
		logicalID,
		Report{Connections: []connectionClaim{{ConnectionName: "company-brain-commercial"}}},
	)
	if err != nil {
		t.Fatalf("cross-workspace read: %v", err)
	}
	if len(got) != 0 || policyCalls != 0 {
		t.Fatalf("cross-workspace evidence = %#v with %d policy calls", got, policyCalls)
	}
}

func marshalFrozenCensusSnapshot(
	t *testing.T,
	workspaceID string,
	connectionID string,
	version int64,
	report Report,
) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		WorkspaceID              string `json:"workspace_id"`
		CensusVersion            int64  `json:"census_version"`
		CompanyBrainConnectionID string `json:"company_brain_connection_id"`
		Report                   Report `json:"report"`
	}{
		WorkspaceID:              workspaceID,
		CensusVersion:            version,
		CompanyBrainConnectionID: connectionID,
		Report:                   report,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type sourceScopedPolicyFunc func(
	context.Context,
	toolpolicy.TableQuery,
) ([]toolpolicy.ConnectionToolVerdict, error)

func (f sourceScopedPolicyFunc) ConnectionToolVerdicts(
	ctx context.Context,
	query toolpolicy.TableQuery,
) ([]toolpolicy.ConnectionToolVerdict, error) {
	return f(ctx, query)
}
