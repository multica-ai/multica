package dbid

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The tables whose primary keys the application mints as UUIDv7, and the sqlc
// query that inserts each one. Keeping the list here — rather than deriving it —
// is deliberate: adding a table to the v7 set is a decision, and it should show
// up as an edit to this list in review.
//
// query is the sqlc query name; arg is the parameter name the query binds the id
// to (CreateRetryTask already had an `id` parameter for the PARENT task, so its
// new id is named new_task_id); field is the Go field on the params struct.
var v7Writes = []struct {
	table string
	file  string
	query string
	arg   string
	field string
}{
	{"activity_log", "activity.sql", "CreateActivity", "id", "ID"},
	{"agent_task_queue", "agent.sql", "CreateAgentTask", "id", "ID"},
	{"agent_task_queue", "agent.sql", "CreateDeferredChannelIssueTask", "id", "ID"},
	{"agent_task_queue", "agent.sql", "CreateQuickCreateTask", "id", "ID"},
	{"agent_task_queue", "agent.sql", "CreateDeferredAgentTask", "id", "ID"},
	{"agent_task_queue", "agent.sql", "CreateRetryTask", "new_task_id", "NewTaskID"},
	{"agent_task_queue", "autopilot.sql", "CreateAutopilotTask", "id", "ID"},
	{"agent_task_queue", "chat.sql", "CreateChatTask", "id", "ID"},
	{"autopilot_run", "autopilot.sql", "CreateAutopilotRun", "id", "ID"},
	{"channel_inbound_audit", "channel.sql", "RecordChannelInboundDrop", "id", "ID"},
	{"chat_message", "chat.sql", "CreateChatMessage", "id", "ID"},
	{"chat_message", "chat.sql", "CreateMikaOnboardingOpening", "id", "ID"},
	{"chat_session", "chat.sql", "CreateChatSession", "id", "ID"},
	{"comment", "comment.sql", "CreateComment", "id", "ID"},
	{"inbox_item", "inbox.sql", "CreateInboxItem", "id", "ID"},
	{"issue", "issue.sql", "CreateIssue", "id", "ID"},
	{"issue", "issue.sql", "CreateIssueWithOrigin", "id", "ID"},
	{"task_token", "task_token.sql", "CreateTaskToken", "id", "ID"},
	{"webhook_delivery", "webhook_delivery.sql", "CreateWebhookDelivery", "id", "ID"},
}

var queryNameRE = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)\s*:(\w+)`)

// TestV7QueriesBindAnIDWithADatabaseFallback locks in the shape of every
// converted INSERT: it must accept an id from the application AND keep
// gen_random_uuid() as the fallback, so a caller that leaves the parameter unset
// degrades to the pre-change behaviour instead of violating NOT NULL.
func TestV7QueriesBindAnIDWithADatabaseFallback(t *testing.T) {
	for _, w := range v7Writes {
		t.Run(w.query, func(t *testing.T) {
			block := queryBlock(t, w.file, w.query)

			want := fmt.Sprintf("COALESCE(sqlc.narg('%s')::uuid, gen_random_uuid())", w.arg)
			if !strings.Contains(block, want) {
				t.Errorf("%s in %s does not bind its id as %s\n%s", w.query, w.file, want, block)
			}

			insert := fmt.Sprintf("INSERT INTO %s (", w.table)
			i := strings.Index(block, insert)
			if i < 0 {
				t.Fatalf("%s does not INSERT INTO %s", w.query, w.table)
			}
			cols := block[i+len(insert):]
			cols = cols[:strings.Index(cols, ")")]
			if !hasColumn(cols, "id") {
				t.Errorf("%s does not list the id column:\ncolumns: %s", w.query, strings.Join(strings.Fields(cols), " "))
			}
		})
	}
}

const (
	dbImportPath   = "github.com/multica-ai/multica/server/pkg/db/generated"
	dbidImportPath = "github.com/multica-ai/multica/server/pkg/dbid"
)

// TestEveryV7ParamsLiteralSetsAnIDAndImportsDBID walks production Go sources
// and fails if a converted params literal leaves its id unset or lives in a file
// that does not import dbid. The AST-based check deliberately does not prescribe
// whether the value is an inline dbid.NewV7 call or a local variable, so routine
// refactors do not weaken this guard by forcing one source spelling.
func TestEveryV7ParamsLiteralSetsAnIDAndImportsDBID(t *testing.T) {
	fields := map[string]string{}
	for _, w := range v7Writes {
		fields[w.query+"Params"] = w.field
	}

	found := map[string]int{}
	for _, path := range productionGoFiles(t) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		dbAliases, importsDBID := relevantImports(t, path, file)
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			selector, ok := literal.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || !dbAliases[pkg.Name] {
				return true
			}
			field, tracked := fields[selector.Sel.Name]
			if !tracked {
				return true
			}

			found[selector.Sel.Name]++
			if !compositeLiteralSetsField(literal, field) {
				position := fset.Position(literal.Pos())
				t.Errorf("%s:%d: %s literal does not set %s; mint it with dbid.NewV7()",
					path, position.Line, selector.Sel.Name, field)
			}
			if !importsDBID {
				position := fset.Position(literal.Pos())
				t.Errorf("%s:%d: %s literal is in a file that does not import %s; add the import and call dbid.NewV7()",
					path, position.Line, selector.Sel.Name, dbidImportPath)
			}
			return true
		})
	}

	for typ := range fields {
		if found[typ] == 0 {
			t.Errorf("no production call site found for %s — was the query removed or renamed?", typ)
		}
	}
}

func relevantImports(t *testing.T, path string, file *ast.File) (map[string]bool, bool) {
	t.Helper()

	dbAliases := map[string]bool{}
	importsDBID := false
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("parse import in %s: %v", path, err)
		}
		switch importPath {
		case dbImportPath:
			name := "db"
			if spec.Name != nil {
				name = spec.Name.Name
			}
			dbAliases[name] = true
		case dbidImportPath:
			importsDBID = true
		}
	}
	return dbAliases, importsDBID
}

func compositeLiteralSetsField(literal *ast.CompositeLit, field string) bool {
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := pair.Key.(*ast.Ident)
		if ok && name.Name == field {
			return true
		}
	}
	return false
}

// queryBlock returns the text of one named sqlc query, from its `-- name:`
// comment to the start of the next one.
func queryBlock(t *testing.T, file, query string) string {
	t.Helper()

	path := filepath.Join(serverDir(t), "pkg", "db", "queries", file)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	marks := queryNameRE.FindAllStringSubmatchIndex(text, -1)
	for i, m := range marks {
		if text[m[2]:m[3]] != query {
			continue
		}
		end := len(text)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		return text[m[0]:end]
	}
	t.Fatalf("query %s not found in %s", query, file)
	return ""
}

func productionGoFiles(t *testing.T) []string {
	t.Helper()

	var out []string
	root := serverDir(t)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// generated/ holds the sqlc output, which never constructs its own
			// params structs, and testdata/ is not compiled.
			if d.Name() == "generated" || d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no production Go files found under %s", root)
	}
	return out
}

func serverDir(t *testing.T) string {
	t.Helper()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve this test's path")
	}
	// .../server/pkg/dbid/writes_test.go -> .../server
	return filepath.Dir(filepath.Dir(filepath.Dir(self)))
}

func hasColumn(list, name string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.TrimSpace(part) == name {
			return true
		}
	}
	return false
}
