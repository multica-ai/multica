package infoflow

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func validConfig(t *testing.T, box *secretbox.Box) channel.Config {
	t.Helper()
	sealed, err := box.Seal([]byte("app-secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	raw := mustJSON(t, installConfig{
		AppID:              "appkey-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	return channel.Config{
		Type:    TypeInfoFlow,
		Raw:     raw,
		Handler: func(context.Context, channel.InboundMessage) error { return nil },
	}
}

func TestRegisterInfoFlow_BuildsChannel(t *testing.T) {
	box := testBox(t)
	reg := channel.NewRegistry()
	RegisterInfoFlow(reg, ChannelDeps{Box: box})

	if _, ok := reg.Lookup(TypeInfoFlow); !ok {
		t.Fatal("TypeInfoFlow was not registered")
	}
	ch, err := reg.Build(validConfig(t, box))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ch.Type() != TypeInfoFlow {
		t.Errorf("Type() = %q, want %q", ch.Type(), TypeInfoFlow)
	}
	// text|attachment|rich_card: an image goes out as an image, anything else as a
	// Markdown link, and a Markdown answer goes out as an audited dynamic card
	// (card.go) that degrades to a Markdown message when the robot has not been
	// approved for it. Inbound media is covered by attachment too.
	if got, want := ch.Capabilities(), channel.CapText|channel.CapAttachment|channel.CapRichCard; got != want {
		t.Errorf("Capabilities() = %v, want %v", got, want)
	}
}

func TestRegisterInfoFlow_RequiresBox(t *testing.T) {
	reg := channel.NewRegistry()
	RegisterInfoFlow(reg, ChannelDeps{})
	if _, err := reg.Build(channel.Config{Type: TypeInfoFlow, Raw: []byte(`{"app_id":"a","app_secret_encrypted":"x"}`)}); err == nil {
		t.Fatal("expected an error when the encryption box is missing")
	}
}

func TestInfoFlowTypeIsStable(t *testing.T) {
	if TypeInfoFlow != "infoflow" {
		t.Fatalf("TypeInfoFlow = %q, want infoflow", TypeInfoFlow)
	}
}
