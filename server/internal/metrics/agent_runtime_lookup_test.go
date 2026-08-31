package metrics_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/metrics"
	dto "github.com/prometheus/client_model/go"
)

const runtimeLookupMetric = "multica_agent_runtime_lookup_total"

// TestAgentRuntimeLookupPrewarmsEverySeries asserts the counter ships every
// source x result combination at zero. An absent series and a zero series look
// the same on a dashboard but behave differently: rate() over an absent series
// returns nothing at all, so a source that has genuinely never fired would be
// indistinguishable from one nobody instrumented.
func TestAgentRuntimeLookupPrewarmsEverySeries(t *testing.T) {
	t.Parallel()

	m := metrics.NewBusinessMetrics()
	fam := metrics.GatherForTest(t, m)[runtimeLookupMetric]
	if fam == nil {
		t.Fatalf("%s not registered", runtimeLookupMetric)
	}

	want := map[string]struct{}{}
	for _, source := range metrics.AllRuntimeLookupSources() {
		for _, result := range metrics.AllRuntimeLookupResults() {
			want[source+"/"+result] = struct{}{}
		}
	}
	for _, mtr := range fam.GetMetric() {
		delete(want, labelPair(mtr, "source")+"/"+labelPair(mtr, "result"))
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for k := range want {
			missing = append(missing, k)
		}
		sort.Strings(missing)
		t.Errorf("prewarm missed %d series: %s", len(missing), strings.Join(missing, ", "))
	}
}

// TestRecordAgentRuntimeLookupNormalizesLabels asserts a call site that passes
// an unclassified source or an unknown result cannot mint a new series. The
// unknown result deliberately lands on "error" rather than "ok": a lookup
// nobody classified is not evidence that the read succeeded.
func TestRecordAgentRuntimeLookupNormalizesLabels(t *testing.T) {
	t.Parallel()

	m := metrics.NewBusinessMetrics()
	m.RecordAgentRuntimeLookup(metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultOK)
	m.RecordAgentRuntimeLookup(metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultNotFound)
	m.RecordAgentRuntimeLookup("  HEARTBEAT_WS  ", metrics.RuntimeLookupResultOK)
	m.RecordAgentRuntimeLookup("brand_new_call_site", "who_knows")

	counts := runtimeLookupCounts(t, m)
	for _, tc := range []struct {
		source, result string
		want           float64
	}{
		{metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultOK, 2},
		{metrics.RuntimeLookupSourceHeartbeatWS, metrics.RuntimeLookupResultNotFound, 1},
		{metrics.RuntimeLookupSourceOther, metrics.RuntimeLookupResultError, 1},
	} {
		if got := counts[tc.source+"/"+tc.result]; got != tc.want {
			t.Errorf("%s/%s = %v, want %v", tc.source, tc.result, got, tc.want)
		}
	}

	var total float64
	for _, v := range counts {
		total += v
	}
	if total != 4 {
		t.Errorf("total samples = %v, want 4 (an unknown label must reuse a series, not add one)", total)
	}
}

// TestNoDirectGetAgentRuntimeCalls walks every non-test Go file under
// server/internal and server/cmd and asserts nothing calls the sqlc
// GetAgentRuntime method except service.RuntimeLookup.Get.
//
// The metric is only worth reading if it counts every read. A call site that
// goes straight to Queries.GetAgentRuntime does not fail, does not warn, and
// does not show up — it silently shrinks whichever source it belonged to, which
// is exactly the kind of quiet undercount that would send the follow-up
// optimisation work after the wrong caller.
func TestNoDirectGetAgentRuntimeCalls(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	// The wrapper itself, keyed by path relative to server/ and by the name of
	// the one function allowed to make the call.
	wrapper := filepath.Join(root, "internal", "service", "agent_runtime_lookup.go")

	var offenders []string
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if path == wrapper && fn.Name.Name == "Get" {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "GetAgentRuntime" {
						return true
					}
					offenders = append(offenders, fset.Position(call.Pos()).String()+" (in "+fn.Name.Name+")")
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("found %d direct GetAgentRuntime call(s) — read through service.RuntimeLookup with a source label so %s stays complete:\n  %s",
			len(offenders), runtimeLookupMetric, strings.Join(offenders, "\n  "))
	}
}

// ---- helpers --------------------------------------------------------------

func runtimeLookupCounts(t *testing.T, m *metrics.BusinessMetrics) map[string]float64 {
	t.Helper()

	fam := metrics.GatherForTest(t, m)[runtimeLookupMetric]
	if fam == nil {
		t.Fatalf("%s not registered", runtimeLookupMetric)
	}
	out := map[string]float64{}
	for _, mtr := range fam.GetMetric() {
		out[labelPair(mtr, "source")+"/"+labelPair(mtr, "result")] = mtr.GetCounter().GetValue()
	}
	return out
}

func labelPair(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
