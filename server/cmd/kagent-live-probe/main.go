package main

import (
	"context"
	"fmt"
	k "github.com/multica-ai/multica/server/pkg/kagent"
	"os"
	"time"
)

func main() {
	model := os.Getenv("MULTICA_KAGENT_LIVE_MODEL_CONFIG")
	if model == "" || model == "default-model-config" {
		panic("explicit non-production MULTICA_KAGENT_LIVE_MODEL_CONFIG required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := k.NewKubernetesStore(k.HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://127.0.0.1:18001"}, []string{"kagent"})
	check(err)
	r, err := k.New(s, k.Config{AllowedNamespaces: []string{"kagent"}})
	check(err)
	dir, err := os.MkdirTemp("", "multica-kagent-live-journal-")
	check(err)
	j, err := k.NewJournal(dir)
	check(err)
	l, err := k.NewLifecycle(r, j)
	check(err)
	id := "35b69a41-efb0-4ec3-96b4-737742c1e3a8"
	d := k.Desired{AgentID: id, AgentName: "isolated-live-reconciler-probe", Namespace: "kagent", HarnessRefName: "multica-integration-isolated", Inline: &k.AgentTemplateSpec{ModelConfig: &k.LocalObjectReference{Name: model}, SystemPrompt: "Answer briefly. You have no tools."}}
	check(l.Enroll(d))
	b, err := l.Reconcile(ctx, id)
	check(err)
	fmt.Printf("create action=%s uid_present=%t rv_present=%t\n", b.Outcome.Action, b.Outcome.UID != "", b.Outcome.ResourceVersion != "")
	for {
		b, err = l.Reconcile(ctx, id)
		check(err)
		if b.Outcome.Ready {
			break
		}
		select {
		case <-ctx.Done():
			panic("readiness timeout: " + b.Outcome.ReadySummary)
		case <-time.After(2 * time.Second):
		}
	}
	fmt.Printf("ready=%t action=%s\n", b.Outcome.Ready, b.Outcome.Action)
	c, err := k.NewA2AClient(k.HTTPConfig{AllowInsecureLoopback: true, Endpoint: "http://127.0.0.1:18083"})
	check(err)
	name, err := k.ResourceName(id)
	check(err)
	t, err := c.Send(ctx, "kagent", name, k.NewID(), "", "Reply with the single word READY.")
	check(err)
	for !t.Terminal() {
		select {
		case <-ctx.Done():
			panic("task timeout")
		case <-time.After(2 * time.Second):
		}
		t, err = c.Get(ctx, "kagent", name, t.ID, t.ContextID)
		check(err)
	}
	fmt.Printf("task state=%s answer=%q\n", t.Status.State, t.Text())
	if t.Status.State != k.StateCompleted || t.Text() != "READY" {
		panic("unexpected response")
	}
	d.Inline.SystemPrompt = "Answer concisely. You have no tools."
	check(l.Enroll(d))
	b, err = l.Reconcile(ctx, id)
	check(err)
	fmt.Printf("update action=%s\n", b.Outcome.Action)
	if b.Outcome.Action != k.ActionUpdated {
		panic("expected update")
	}
	check(l.Delete(ctx, id, true))
	_, err = s.Get(ctx, "kagent", name)
	if err != k.ErrNotFound {
		panic(fmt.Sprintf("expected absence; got %v", err))
	}
	fmt.Println("delete=verified_absent")
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}
