package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestRegisterPersistsInstallationMenu(t *testing.T) {
	srv := telegramInstallAPIServer(t, "")
	defer srv.Close()
	q := &fakeTelegramInstallQueries{rowID: telegramTestUUID(9)}
	svc := newTelegramInstallTestService(t, q)
	svc.apiBase, svc.httpClient = srv.URL, srv.Client()
	_, err := svc.Register(context.Background(), RegisterParams{
		WorkspaceID: telegramTestUUID(1), AgentID: telegramTestUUID(2),
		InitiatorID: telegramTestUUID(3), BotToken: "12345:fake", Menu: testMenu(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg installConfig
	if err := json.Unmarshal(q.upsert.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Menu == nil || cfg.Menu.Pages[0].Actions[0].Request != "Show my task status." {
		t.Fatalf("menu was not saved: %s", q.upsert.Config)
	}
}

func TestRegisterRejectsInvalidMenuBeforeContactingTelegram(t *testing.T) {
	q := &fakeTelegramInstallQueries{}
	svc := newTelegramInstallTestService(t, q)
	svc.apiBase = "http://127.0.0.1:1"
	m := testMenu()
	m.Pages[0].Actions[0].Request = ""
	if _, err := svc.Register(context.Background(), RegisterParams{BotToken: "12345:fake", Menu: m}); !errors.Is(err, ErrInvalidMenuConfig) {
		t.Fatalf("want menu validation error before any network call, got %v", err)
	}
	if q.upsertCalled {
		t.Fatal("invalid config persisted")
	}
}
