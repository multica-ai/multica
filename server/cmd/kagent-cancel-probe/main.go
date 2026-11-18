package main

import (
	"context"
	"fmt"
	a "github.com/multica-ai/multica/server/pkg/agent"
	k "github.com/multica-ai/multica/server/pkg/kagent"
	"os"
	"time"
)

func ck(e error) {
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
	store, e := k.NewKubernetesStore(k.HTTPConfig{Endpoint: "http://127.0.0.1:18001", AllowInsecureLoopback: true}, []string{"kagent"})
	ck(e)
	r, e := k.New(store, k.Config{AllowedNamespaces: []string{"kagent"}})
	ck(e)
	j, e := k.NewJournal("/tmp/multica-kagent-cancel-probe-journal")
	ck(e)
	l, e := k.NewLifecycle(r, j)
	ck(e)
	id := "6a16716a-2673-4c5e-a042-43baad23ada5"
	ck(l.Enroll(k.Desired{AgentID: id, AgentName: "cancel-test", Namespace: "kagent", HarnessRefName: "multica-integration-isolated", Inline: &k.AgentTemplateSpec{ModelConfig: &k.LocalObjectReference{Name: model}, SystemPrompt: "You are an assistant."}}))
	for {
		b, e := l.Reconcile(ctx, id)
		ck(e)
		if b.Outcome.Ready {
			break
		}
		select {
		case <-ctx.Done():
			panic("readiness timeout")
		case <-time.After(2 * time.Second):
		}
	}
	c, e := k.NewA2AClient(k.HTTPConfig{Endpoint: "http://127.0.0.1:18083", AllowInsecureLoopback: true})
	ck(e)
	backend, e := a.NewKagentBackend(a.KagentConfig{Enabled: true, Lifecycle: l, Journal: j, Client: c, AgentID: id, WorkspaceID: "test", ConversationID: "cancel-test", AttemptID: "attempt-1", PollInterval: time.Second})
	ck(e)
	run, stop := context.WithCancel(ctx)
	session, e := backend.Execute(run, "Write a very long detailed essay about every planet in the solar system.", a.ExecOptions{SystemPrompt: "Answer the user. You have no tools."})
	ck(e)
	go func() {
		for range session.Messages {
		}
	}()
	time.Sleep(3 * time.Second)
	stop()
	res := <-session.Result
	fmt.Printf("status=%s terminal_observed=%t\n", res.Status, session.TerminalObserved())
	if !session.TerminalObserved() {
		panic("remote cleanup unconfirmed; agent intentionally retained")
	}
	ck(l.Delete(ctx, id, true))
	fmt.Println("owned_agent_deleted=true")
}
