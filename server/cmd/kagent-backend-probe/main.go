package main

import (
	"context"
	"errors"
	"fmt"
	a "github.com/multica-ai/multica/server/pkg/agent"
	k "github.com/multica-ai/multica/server/pkg/kagent"
	"os"
	"time"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	model := os.Getenv("MULTICA_KAGENT_LIVE_MODEL_CONFIG")
	if model == "" || model == "default-model-config" {
		panic("explicit non-production MULTICA_KAGENT_LIVE_MODEL_CONFIG required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	store, e := k.NewKubernetesStore(k.HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://127.0.0.1:18001"}, []string{"kagent"})
	check(e)
	rec, e := k.New(store, k.Config{AllowedNamespaces: []string{"kagent"}})
	check(e)
	dir, e := os.MkdirTemp("", "multica-kagent-backend-multinode-")
	check(e)
	journal, e := k.NewJournal(dir)
	check(e)
	life, e := k.NewLifecycle(rec, journal)
	check(e)
	id := k.NewID()
	d := k.Desired{AgentID: id, AgentName: "multica-backend-multinode-probe", Namespace: "kagent", HarnessRefName: "multica-integration-isolated", Inline: &k.AgentTemplateSpec{ModelConfig: &k.LocalObjectReference{Name: model}, SystemPrompt: "Answer briefly. You have no tools."}}
	check(life.Enroll(d))
	client, e := k.NewA2AClient(k.HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://127.0.0.1:18083"})
	check(e)
	execute := func(attempt, prompt, resume string) a.Result {
		backend, e := a.NewKagentBackend(a.KagentConfig{Enabled: true, Lifecycle: life, Journal: journal, Client: client, AgentID: id, WorkspaceID: "isolated-test-workspace", ConversationID: "isolated-test-conversation", AttemptID: attempt, PollInterval: time.Second, ReadinessTimeout: 90 * time.Second})
		check(e)
		s, e := backend.Execute(ctx, prompt, a.ExecOptions{SystemPrompt: "Answer briefly; you have no tools.", ResumeSessionID: resume})
		check(e)
		go func() {
			for range s.Messages {
			}
		}()
		r := <-s.Result
		fmt.Printf("attempt=%s status=%s answer=%q terminal=%t\n", attempt, r.Status, r.Output, s.TerminalObserved())
		if r.Status != "completed" {
			panic("backend did not complete: " + r.Error)
		}
		return r
	}
	first := execute("first", "Reply with the single word READY.", "")
	second := execute("second", "What single word did I previously request?", first.SessionID)
	if first.Output != "READY" || second.Output != "READY" || first.SessionID != second.SessionID {
		panic("response or conversation continuity failed")
	}
	fresh := execute("clear", "Reply with the single word FRESH.", "")
	// The synthetic fixture answers READY to every prompt, so the output cannot
	// distinguish a fresh conversation. The real fresh-conversation invariant is
	// a distinct remote session identity, asserted here; prompt-dependent
	// responses require a real model and are out of scope for the fixture.
	if fresh.SessionID == first.SessionID {
		panic("fresh conversation did not create distinct session")
	}
	for {
		err := life.Delete(ctx, id, true)
		if err == nil {
			break
		}
		if !errors.Is(err, k.ErrDeletionPending) {
			panic(err)
		}
		select {
		case <-ctx.Done():
			panic("deletion confirmation timed out")
		case <-time.After(time.Second):
		}
	}
	fmt.Println("owned_agent_deleted_and_absent=true")
}
