package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The defect these two guard is a merge that lands the same piece of boot-time
// wiring twice.
//
// On 2026-08-10 the deployed backend logged `wecom deployment locale
// locale=zh-Hans` twice on boot. Two branches each carried the same eleven
// lines of router.go — a comment plus the slog.Info wrapping
// wecom.SetDeploymentLocale — and git merged both without reporting a conflict,
// because the two copies landed at slightly different offsets and neither
// overlapped the other's context. Five branches carry a copy, so any two of
// them merging reproduces it.
//
// Nothing caught it. The compiler and go vet catch a duplicate *declaration*;
// this is a duplicate *statement*, and it survived go build, go vet, gofmt and
// the whole -race suite. It surfaced because a human read the boot log.
//
// That it was harmless is luck: SetDeploymentLocale is last-write-wins, so the
// second call set the same value. The same merge over `wecomTyping.Register(bus)`
// subscribes every task:failed handler twice and writes the failure notice into
// the user's chat twice; over the outbound subscriber, it answers every question
// twice.
//
// Note what the compiler DOES cover, because it decides what is left here: a
// duplicated `x := f()` at the same block scope is "no new variables on left
// side of :=" and never builds. So most of the WeCom block's constructors need
// no guard. What builds cleanly when duplicated is a bare call statement, a
// call inside a fresh scope (`if raw := ...; raw != "" { ... }`), and an append
// — and that is exactly the set covered below.
//
// The two halves split on observability, not on taste:
//
//   - TestWecomBootWiringSubscribesExactlyOnce drives the real boot path and
//     counts what the wiring actually did. It is the stronger evidence, and it
//     catches a duplicate however it arrives — through a helper, from another
//     file, from a second NewRouter call site. It can only see wiring that
//     leaves a countable trace, which here means bus subscriptions.
//
//   - TestWecomBootWiringIsWrittenExactlyOnce reads router.go. It covers what
//     no runtime assertion can: SetTrace and SetMediaAllowedPrefixes both
//     write package state and are idempotent, so a second call leaves nothing
//     behind to count. Source-level counting is the only way to see them, and
//     it is what would have caught the 2026-08-10 defect.

// TestWecomBootWiringSubscribesExactlyOnce asserts that each bus subscription
// the WeCom boot block owns is made exactly once.
//
// Two routers are built on two fresh buses, one with the WeCom key set and one
// without, and the difference between them is what the WeCom block did. The
// delta has to be exactly one per event: the sibling wiring tests assert `> 0`,
// which answers "is it wired" and is blind to the same block running twice.
//
// A nil pool is deliberate: nothing in the WeCom boot block queries the
// database, and metrics_test.go boots the same way.
func TestWecomBootWiringSubscribesExactlyOnce(t *testing.T) {
	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate a wecom secretbox key: %v", err)
	}

	withoutWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withoutWecom, analytics.NoopClient{}, nil)

	t.Setenv("MULTICA_WECOM_SECRET_KEY", base64.StdEncoding.EncodeToString(key))
	withWecom := events.New()
	NewRouter(nil, realtime.NewHub(), withWecom, analytics.NoopClient{}, nil)

	// Every subscription the WeCom block adds, and what a second copy of it
	// does to a user. Each is added by exactly one Register call, so a count of
	// two means that Register ran twice.
	subscriptions := []struct {
		event  string
		wiring string
		twice  string
	}{
		{
			event:  protocol.EventChatDone,
			wiring: "wecom.NewOutbound(...).Register(bus)",
			twice: "every finished chat is enqueued onto the outbound queue twice, so the person who asked " +
				"gets the whole answer twice — once in the stream bubble and once again underneath it",
		},
		{
			event:  protocol.EventInboxNew,
			wiring: "wecom.NewOutbound(...).Register(bus)",
			twice:  "every inbox notification is pushed into the member's 1:1 chat with the bot twice",
		},
		{
			event:  protocol.EventTaskFailed,
			wiring: "wecomTyping.Register(bus)",
			twice: "a failed run writes its failure notice into the chat twice, and the second one arrives " +
				"after the bubble it was meant to replace is already sealed",
		},
		{
			event:  protocol.EventTaskCancelled,
			wiring: "wecomTyping.Register(bus)",
			twice:  "a cancelled run announces itself twice",
		},
		{
			event:  protocol.EventTaskProgress,
			wiring: "wecomTyping.Register(bus)",
			twice: "every progress tick rewrites the bubble twice, doubling the socket traffic a long run " +
				"spends against WeCom's own rate limit",
		},
		{
			event:  protocol.EventTaskMessage,
			wiring: "wecomTyping.Register(bus)",
			twice:  "every transcript step is painted into the bubble twice",
		},
	}

	// Anti-vacuity: if the WeCom block never ran, every delta is zero and each
	// assertion below would fail for a reason it does not name.
	var wired int
	for _, sub := range subscriptions {
		if withWecom.SubscriberCount(sub.event) > withoutWecom.SubscriberCount(sub.event) {
			wired++
		}
	}
	if wired == 0 {
		t.Fatalf("the WeCom boot block did not run: it adds no listener to any of %v with "+
			"MULTICA_WECOM_SECRET_KEY set. Re-point this guard at wherever WeCom is wired now",
			eventNames(subscriptions))
	}

	for _, sub := range subscriptions {
		with := withWecom.SubscriberCount(sub.event)
		without := withoutWecom.SubscriberCount(sub.event)
		switch delta := with - without; {
		case delta == 1:
			// Wired once, which is the whole point.
		case delta < 1:
			t.Errorf("nothing in the WeCom boot path subscribes to %s (%d listeners with WeCom enabled, "+
				"%d without). Check %s.", sub.event, with, without, sub.wiring)
		default:
			t.Errorf("the WeCom boot path subscribes to %s %d times, not once (%d listeners with WeCom "+
				"enabled, %d without). Every %s is now handled %d times over: %s. "+
				"This is what a merge that lands %s twice looks like — the duplicate builds, vets and races "+
				"clean, so this count is the only thing that sees it.",
				sub.event, delta, with, without, sub.event, delta, sub.twice, sub.wiring)
		}
	}
}

func eventNames(subs []struct {
	event  string
	wiring string
	twice  string
}) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range subs {
		if !seen[s.event] {
			seen[s.event] = true
			out = append(out, s.event)
		}
	}
	return out
}

// TestWecomBootWiringIsWrittenExactlyOnce counts the once-only wiring inside
// router.go's WeCom block and asserts each appears exactly the number of times
// it is supposed to.
//
// This reads source because the regression is a duplicated call whose effect is
// idempotent — there is no value left over to observe. It is scoped to the
// block rather than the file so that a matcher can key on a local receiver
// (`wecomTyping.Register`) without colliding with the Slack, DingTalk or Lark
// wiring above it, and the block is found by the gate that opens it —
// secretbox.LoadKey("MULTICA_WECOM_SECRET_KEY") — rather than by line number.
//
// Matching is on the call, not on the text around it: a duplicate that differs
// in whitespace, or that arrives with its comment stripped, counts the same.
func TestWecomBootWiringIsWrittenExactlyOnce(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, routerSourceFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", routerSourceFile, err)
	}

	block, ok := wecomBootBlock(file)
	if !ok {
		t.Fatalf("no `if ... := secretbox.LoadKey(%q)` in %s — this guard counts the calls inside that "+
			"block and has just lost the block. Re-point it at wherever the WeCom boot wiring lives now",
			wecomBootGateEnv, routerSourceFile)
	}

	// Everything a merge can duplicate without the compiler noticing: bare call
	// statements and calls inside their own `if` scope. Plain `x := ...`
	// constructors are left out on purpose — duplicating one is "no new
	// variables on left side of :=" and never builds.
	//
	// The 2026-08-10 defect itself was a duplicated SetDeploymentLocale; that
	// knob is not on this branch, so its matcher arrives with it.
	wiring := []struct {
		what  string
		want  int
		twice string
		match func(*ast.CallExpr) bool
	}{
		{
			what: "wecom.SetMediaAllowedPrefixes",
			want: 1,
			twice: "the media guard's SSRF allow-list is applied twice. It replaces rather than appends " +
				"today, so the second call is silent — and it sits inside `if raw := ...; raw != \"\"`, its " +
				"own scope, so a duplicated copy compiles",
			match: calleeIs("wecom.SetMediaAllowedPrefixes"),
		},
		{
			what: "wecom.SetTrace",
			want: 1,
			twice: "frame tracing is switched on twice and the warning that says it is on — the only thing " +
				"stopping a deployment from quietly accumulating message text in its logs — is printed twice",
			match: calleeIs("wecom.SetTrace"),
		},
		{
			what: "wecom.RegisterWecom",
			want: 1,
			twice: "the WeCom channel Factory is registered twice on the channel.Registry. The registry is a " +
				"map, so the second silently wins and every installation is built from whichever ChannelDeps " +
				"the second copy carries",
			match: calleeIs("wecom.RegisterWecom"),
		},
		{
			what: "Register(bus) — the typing indicator and the outbound subscriber",
			want: 2,
			twice: "one of the two bus subscribers is registered twice. TestWecomBootWiringSubscribesExactlyOnce " +
				"says which one and what it does to a user",
			match: func(call *ast.CallExpr) bool {
				if selectorName(call) != "Register" || len(call.Args) != 1 {
					return false
				}
				arg, ok := call.Args[0].(*ast.Ident)
				return ok && arg.Name == "bus"
			},
		},
		{
			what: "channelRouter.Register(wecom.TypeWecom, ...)",
			want: 1,
			twice: "the WeCom resolver set is registered twice on the channel router. It is keyed by channel " +
				"type, so the second overwrites the first and every inbound message is answered through a " +
				"resolver set nobody meant to be the live one",
			match: func(call *ast.CallExpr) bool {
				return selectorName(call) == "Register" &&
					len(call.Args) > 0 &&
					exprText(fset, call.Args[0]) == "wecom.TypeWecom"
			},
		},
		{
			what: "wecom.NewOutbound",
			want: 1,
			twice: "two outbound subscribers exist. Even if only one is Registered, the other holds the same " +
				"senders registry and stream store and will start answering the moment somebody wires it",
			match: calleeIs("wecom.NewOutbound"),
		},
		{
			what: "wecom.NewTypingIndicator",
			want: 1,
			twice: "two typing-indicator managers exist over one stream store, so two of them race to open, " +
				"refresh and seal the same bubble",
			match: calleeIs("wecom.NewTypingIndicator"),
		},
	}

	// Each env var the block reads is read at exactly one place. A duplicated
	// block reads its variable a second time, which makes this the signal that
	// survives when the duplicate has been reformatted or stripped of comments.
	for _, env := range []string{
		wecomMediaAllowEnv,
		"MULTICA_WECOM_TRACE",
	} {
		env := env
		wiring = append(wiring, struct {
			what  string
			want  int
			twice string
			match func(*ast.CallExpr) bool
		}{
			what:  fmt.Sprintf("os.Getenv(%q)", env),
			want:  1,
			twice: "the boot block reads " + env + " in two places, which means the wiring around it landed twice",
			match: func(call *ast.CallExpr) bool {
				return calleeName(call) == "os.Getenv" &&
					len(call.Args) == 1 &&
					stringLit(call.Args[0]) == env
			},
		})
	}

	for _, w := range wiring {
		var at []string
		ast.Inspect(block, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if w.match(call) {
				at = append(at, fset.Position(call.Pos()).String())
			}
			return true
		})

		switch {
		case len(at) == w.want:
			// Written the number of times it is meant to be.
		case len(at) < w.want:
			t.Errorf("%s appears %d time(s) in the WeCom boot block of %s, expected %d — the wiring is gone "+
				"or has moved. If it was moved on purpose, move this guard with it; %s.",
				w.what, len(at), routerSourceFile, w.want, w.twice)
		default:
			t.Errorf("%s appears %d time(s) in the WeCom boot block of %s, expected %d: %s.\n"+
				"A second call means %s.\n"+
				"This is what a merge duplicating boot wiring looks like: git reports no conflict when the "+
				"two copies land at different offsets, and the result builds, vets, gofmts and races clean.",
				w.what, len(at), routerSourceFile, w.want, strings.Join(at, ", "), w.twice)
		}
	}
}

// wecomBootGateEnv is the env var whose presence opens the WeCom boot block.
// TestWecomBootWiringIsWrittenExactlyOnce finds the block by this gate rather
// than by line number, so the guard survives everything above it moving.
const wecomBootGateEnv = "MULTICA_WECOM_SECRET_KEY"

// wecomBootBlock returns the body of the `if ... := secretbox.LoadKey(
// "MULTICA_WECOM_SECRET_KEY"); err == nil` statement in router.go.
func wecomBootBlock(file *ast.File) (ast.Node, bool) {
	var body *ast.BlockStmt
	ast.Inspect(file, func(n ast.Node) bool {
		if body != nil {
			return false
		}
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok || ifStmt.Init == nil {
			return true
		}
		ast.Inspect(ifStmt.Init, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || calleeName(call) != "secretbox.LoadKey" || len(call.Args) != 1 {
				return true
			}
			if stringLit(call.Args[0]) == wecomBootGateEnv {
				body = ifStmt.Body
				return false
			}
			return true
		})
		return true
	})
	return body, body != nil
}

// calleeIs matches a call by its "pkg.Func" target.
func calleeIs(target string) func(*ast.CallExpr) bool {
	return func(call *ast.CallExpr) bool { return calleeName(call) == target }
}

// selectorName returns the method name of a call on some receiver, ignoring
// what the receiver is called. Matching `wecomTyping.Register(bus)` on the
// local variable name would turn a rename into a failure that names the wrong
// problem.
func selectorName(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}
