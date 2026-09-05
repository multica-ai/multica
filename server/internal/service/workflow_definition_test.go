package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestValidateWorkflowDefinition(t *testing.T) {
	valid := `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`
	manyStages := make([]string, 33)
	for i := range manyStages {
		manyStages[i] = fmt.Sprintf(`{"key":"s%d","name":"Stage %d"}`, i, i)
	}

	cases := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"valid", valid, true},
		{"wrong schema version", `{"schema_version":2,"stages":[{"key":"build","name":"Build"}]}`, false},
		{"zero stages", `{"schema_version":1,"stages":[]}`, false},
		{"too many stages", `{"schema_version":1,"stages":[` + strings.Join(manyStages, ",") + `]}`, false},
		{"blank key", `{"schema_version":1,"stages":[{"key":"  ","name":"Build"}]}`, false},
		{"duplicate key", `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"build","name":"Again"}]}`, false},
		{"blank stage name", `{"schema_version":1,"stages":[{"key":"build","name":"  "}]}`, false},
		{"unknown field", `{"schema_version":1,"stages":[{"key":"build","name":"Build","extra":true}]}`, false},
		{"trailing json", valid + ` {}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ValidateWorkflowDefinition(json.RawMessage(tc.raw))
			if tc.ok {
				if err != nil {
					t.Fatalf("ValidateWorkflowDefinition: %v", err)
				}
				if spec.SchemaVersion != 1 || len(spec.Stages) != 1 {
					t.Fatalf("spec = %+v", spec)
				}
				return
			}
			if !errors.Is(err, ErrInvalidWorkflowDefinition) {
				t.Fatalf("err = %v, want ErrInvalidWorkflowDefinition", err)
			}
		})
	}
}

func TestWorkflowCreateDefinitionRejectsBlankName(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	_, err := fx.service.CreateDefinition(context.Background(), CreateWorkflowDefinitionParams{
		WorkspaceID: fx.workspaceID,
		Name:        "   ",
		Definition:  json.RawMessage(`{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`),
		CreatedBy:   fx.userID,
	})
	if !errors.Is(err, ErrInvalidWorkflowDefinition) {
		t.Fatalf("err = %v, want ErrInvalidWorkflowDefinition", err)
	}
}

func TestWorkflowCreateDefinitionVersionsImmutably(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	first := fx.createDefinition(t, "Release", `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`)
	firstBytes := append([]byte(nil), first.Definition...)
	second := fx.createDefinition(t, "release", `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`)
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("versions = %d,%d, want 1,2", first.Version, second.Version)
	}
	if second.Name != first.Name {
		t.Fatalf("second name = %q, want preserved casing %q", second.Name, first.Name)
	}
	reloaded, err := fx.service.Queries.GetWorkflowDefinitionInWorkspace(context.Background(), db.GetWorkflowDefinitionInWorkspaceParams{ID: first.ID, WorkspaceID: fx.workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if string(reloaded.Definition) != string(firstBytes) {
		t.Fatalf("v1 mutated: got %s want %s", reloaded.Definition, firstBytes)
	}
}

func TestWorkflowCreateDefinitionConcurrentVersions(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	start := make(chan struct{})
	versions := make([]int, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			row, err := fx.service.CreateDefinition(context.Background(), CreateWorkflowDefinitionParams{
				WorkspaceID: fx.workspaceID,
				Name:        "Concurrent",
				Definition:  json.RawMessage(`{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`),
				CreatedBy:   fx.userID,
			})
			errs[i] = err
			versions[i] = int(row.Version)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	sort.Ints(versions)
	if versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("versions = %v, want [1 2]", versions)
	}
}
