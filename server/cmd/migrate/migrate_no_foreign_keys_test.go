package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNewMigrationsStayForeignKeyFree enforces the repository's application-
// layer relationship rule for the current migration era. Older migrations are
// intentionally out of scope because removing their legacy constraints needs
// dedicated expand/contract work.
func TestNewMigrationsKeepRelationshipsAndIndexesOutOfCreateTable(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}

	for _, path := range paths {
		versionText, _, ok := strings.Cut(filepath.Base(path), "_")
		if !ok {
			continue
		}
		version, err := strconv.Atoi(versionText)
		if err != nil || version < 340 {
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sql := bytes.ToUpper(stripSQLLineComments(body))
		if bytes.Contains(sql, []byte("REFERENCES ")) || bytes.Contains(sql, []byte("FOREIGN KEY")) {
			t.Errorf("%s adds a foreign key; enforce the relationship in application code", filepath.Base(path))
		}
		if bytes.Contains(sql, []byte("PRIMARY KEY")) || bytes.Contains(sql, []byte("UNIQUE (")) {
			t.Errorf("%s creates an inline index; build indexes concurrently in separate migrations", filepath.Base(path))
		}
	}
}
