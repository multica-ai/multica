package apps

import (
	"os"
	"strings"
	"testing"
)

func TestAppFoldersSupportNestedRenameAndMove(t *testing.T) {
	migration, err := os.ReadFile("../../../migrations/9138_cerebro_app_folders.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(migration)
	for _, contract := range []string{"parent_id UUID REFERENCES cerebro_app_folder", "folder_id UUID REFERENCES cerebro_app_folder", "idx_cerebro_app_folder_sibling_name"} {
		if !strings.Contains(sql, contract) {
			t.Errorf("folder migration is missing %q", contract)
		}
	}
	router, _ := os.ReadFile("../../../cmd/server/router.go")
	for _, route := range []string{"CreateFolder", "UpdateFolder", "MoveAppToFolder", "DeleteFolder"} {
		if !strings.Contains(string(router), route) {
			t.Errorf("folder route %s is missing", route)
		}
	}
}
