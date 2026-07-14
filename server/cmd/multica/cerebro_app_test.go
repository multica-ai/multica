package main

import "testing"

func TestAppCommandExposesCatalogLifecycle(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"app"})
	if err != nil || cmd == rootCmd {
		t.Fatalf("app command is not registered: %v", err)
	}
	for _, name := range []string{"create", "preview", "publish", "rollback", "list"} {
		if child, _, err := cmd.Find([]string{name}); err != nil || child == cmd {
			t.Errorf("app %s command is not registered", name)
		}
	}
}
