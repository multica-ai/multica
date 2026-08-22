package llm

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

// countingHTTPClient is an option.HTTPClient that records every request the SDK
// hands it and refuses to perform any of them. It exists to assert an absence:
// a test that only checks the returned error cannot tell "returned
// ErrNotConfigured without dialing" apart from "dialed, failed, and mapped the
// failure to ErrNotConfigured".
type countingHTTPClient struct {
	requests atomic.Int64
	urls     []string
}

func (c *countingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests.Add(1)
	c.urls = append(c.urls, req.URL.String())
	return nil, errors.New("upstream must not be contacted by a disabled client")
}

// TestUnconfiguredClientMakesZeroUpstreamRequests pins the contract a
// deployment relies on when it leaves MULTICA_LLM_API_KEY and
// MULTICA_LLM_BASE_URL empty: this layer sends nothing, to anyone. (Only this
// layer — agent runs reach a model by their own path, which no variable here
// governs. See the package doc.)
//
// Both consumers of this package send private chat content upstream — the
// first message of a chat session (auto-titling) and the tail of a conversation
// (follow-up questions). "Leave the LLM variables empty" is the documented
// answer for an operator whose policy forbids that (.env.example, the docs
// environment-variables pages, and GitHub issue #7162), so the behaviour has to
// be a tested guarantee rather than something that happens to be true today.
//
// Every exported call path is exercised, because each one is a place a future
// refactor could start building a request before consulting Enabled().
func TestUnconfiguredClientMakesZeroUpstreamRequests(t *testing.T) {
	// A deployment that set only the model — the shape most likely to be
	// mistaken for "configured" — must be just as inert as an empty config.
	for _, cfg := range []Config{{}, {DefaultModel: "gpt-5.6-luna"}} {
		transport := &countingHTTPClient{}
		cfg.HTTPClient = transport
		c := New(cfg)

		if c.Enabled() {
			t.Fatalf("client with no API key or base URL reports enabled: %+v", cfg)
		}

		ctx := context.Background()
		userMessage := openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("private chat content")},
		}
		calls := []struct {
			name string
			run  func() error
		}{
			{"Chat", func() error {
				_, err := c.Chat(ctx, userMessage)
				return err
			}},
			{"ChatStream", func() error {
				_, err := c.ChatStream(ctx, userMessage)
				return err
			}},
			{"GenerateText", func() error {
				_, err := c.GenerateText(ctx, "", "system", "private chat content")
				return err
			}},
			{"GenerateJSON", func() error {
				_, err := c.GenerateJSON(ctx, "", "system JSON", "private chat content", 0.3, 2048)
				return err
			}},
		}
		for _, call := range calls {
			if err := call.run(); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("%s on a disabled client: got %v, want ErrNotConfigured", call.name, err)
			}
		}

		if n := transport.requests.Load(); n != 0 {
			t.Errorf("disabled client (%+v) made %d upstream request(s): %v", cfg, n, transport.urls)
		}
	}

	// Guard against a vacuous pass. Everything above asserts that a counter
	// stayed at zero, which is also what a seam that stopped being wired would
	// produce: drop option.WithHTTPClient from New and the assertions keep
	// passing while the real client dials OpenAI. So prove the counter can move
	// — a configured client must reach the same transport.
	transport := &countingHTTPClient{}
	configured := New(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1", HTTPClient: transport, MaxRetries: retries(0)})
	if _, err := configured.GenerateText(context.Background(), "", "system", "hi"); err == nil {
		t.Fatal("expected the refusing transport to fail a configured client's call")
	}
	if transport.requests.Load() == 0 {
		t.Fatal("configured client sent nothing through the test transport: the HTTPClient seam is not wired, so the zero-request assertions above prove nothing")
	}
}

// llmPackageDir is this package's path relative to the server module root. The
// two source scans below exempt it: it is the layer they are guarding, not a
// caller of it.
const llmPackageDir = "pkg/llm"

const openAISDKImportPrefix = "github.com/openai/openai-go"

// documentedConsumers is the inventory this package's doc comment,
// .env.example and the environment-variables docs pages all publish to
// operators: these files, and only these, ask this layer to send something
// upstream. The value is the summary each one is documented with.
var documentedConsumers = map[string]string{
	"internal/handler/chat_title.go":                  "chat auto-titling: the first user message of a new chat session",
	"internal/service/chat_quick_actions_generate.go": "chat follow-up questions: the tail of the conversation",
}

// clientCallSurface is every method on Client that can produce an upstream
// request. Enabled and DefaultModel are deliberately absent: asking whether the
// layer is on sends nothing, and consumers are expected to call it.
var clientCallSurface = map[string]bool{
	"Chat":         true,
	"ChatStream":   true,
	"GenerateText": true,
	"GenerateJSON": true,
}

// methodNameCollisions are call sites the scan below flags by name without
// being consumers of this layer — some unrelated type that happens to have a
// method called Chat or GenerateText. The scan matches on selector name alone,
// which cannot tell the two apart.
//
// This is deliberately NOT part of documentedConsumers. That map is published
// to operators as "what this deployment sends to a third party"; parking a
// name collision there to quiet a test would corrupt a privacy disclosure to
// save a line. Empty today — the repository has no collisions.
var methodNameCollisions = map[string]bool{}

// TestDocumentedConsumersAreTheOnlyCallers keeps the published consumer
// inventory from going quietly stale.
//
// The operator-facing copy does not merely say this layer can be turned off; it
// enumerates what is sent, feature by feature, so an admin can decide whether
// to turn it on. That list is a promise about the whole server, and a third
// feature calling GenerateText would break it silently: nothing in the build
// notices, the docs keep naming two consumers, and the deployment starts
// sending something no one disclosed.
//
// The import guard below cannot catch that case — a new consumer goes through
// this package exactly as the existing two do, importing no SDK. So this test
// scans production call sites instead and requires the set to match the
// inventory in both directions: an undocumented caller fails, and so does a
// documented one that stopped calling.
func TestDocumentedConsumersAreTheOnlyCallers(t *testing.T) {
	found := map[string][]string{}
	scanServerModule(t, skipTestFiles, func(rel string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && clientCallSurface[sel.Sel.Name] && !methodNameCollisions[rel] {
				found[rel] = append(found[rel], sel.Sel.Name)
			}
			return true
		})
	})

	for rel, methods := range found {
		if _, documented := documentedConsumers[rel]; !documented {
			t.Errorf("undocumented consumer of this layer: %s calls %v.\n"+
				"If it sends content upstream, it must be disclosed before it ships: add it to "+
				"documentedConsumers, to this package's doc comment, and to the operator copy in "+
				".env.example and apps/docs/content/docs/environment-variables*.mdx (all four locales).\n"+
				"If it is an unrelated type that merely shares a method name, add it to "+
				"methodNameCollisions — never to documentedConsumers, which operators read as the "+
				"list of things that send their chat content somewhere.", rel, methods)
		}
	}
	for rel, summary := range documentedConsumers {
		if _, still := found[rel]; !still {
			t.Errorf("%s no longer calls this layer, but is still published as a consumer (%q).\n"+
				"Drop it from documentedConsumers, this package's doc comment, and the operator copy — "+
				"an inventory that overstates what is sent is as misleading as one that understates it.",
				rel, summary)
		}
	}
}

// TestOpenAISDKIsImportedOnlyByThisPackage enforces the import half of the
// single-entry-point rule: nothing outside pkg/llm reaches the OpenAI SDK.
//
// This is narrower than it sounds, and deliberately paired with the inventory
// test above. On its own it proves only that the SDK has one door; it says
// nothing about how many callers walk through it. Together the two mean the
// package doc's consumer list can be trusted: no one can bypass this layer, and
// no one can join it unannounced.
//
// It asserts on source rather than behaviour because the regression is the
// existence of a call site, and no runtime assertion can observe a request the
// test never triggers.
func TestOpenAISDKIsImportedOnlyByThisPackage(t *testing.T) {
	var offenders []string
	scanServerModule(t, includeTestFiles, func(rel string, file *ast.File) {
		for _, imp := range file.Imports {
			if strings.HasPrefix(strings.Trim(imp.Path.Value, `"`), openAISDKImportPrefix) {
				offenders = append(offenders, rel)
				return
			}
		}
	})

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("these files import the OpenAI SDK directly, bypassing pkg/llm:\n  %s\n"+
			"Call this package instead (add a helper here if it lacks what you need), so what this "+
			"deployment's assist layer sends to a third party stays answerable by reading one package.",
			strings.Join(offenders, "\n  "))
	}
}

const (
	skipTestFiles    = false
	includeTestFiles = true
)

// scanServerModule parses every Go file in the server module except this
// package's own, calling visit with the file's module-relative path. Test files
// are included only when withTests is set: a stub in a _test.go is not a
// product feature sending chat content anywhere.
func scanServerModule(t *testing.T, withTests bool, visit func(rel string, file *ast.File)) {
	t.Helper()

	// Tests run in their package directory, so the server module root is two
	// levels up from pkg/llm.
	moduleRoot := filepath.Join("..", "..")
	exempt := filepath.Join(moduleRoot, filepath.FromSlash(llmPackageDir))

	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != moduleRoot && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
				return fs.SkipDir
			}
			if path == exempt {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if !withTests && strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		rel, relErr := filepath.Rel(moduleRoot, path)
		if relErr != nil {
			return relErr
		}
		visit(filepath.ToSlash(rel), file)
		return nil
	})
	if err != nil {
		t.Fatalf("scan server module: %v", err)
	}
}
