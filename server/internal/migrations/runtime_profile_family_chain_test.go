package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every runtime_profile family migration DROPs the whole
// runtime_profile_protocol_family_check and rebuilds it from a literal list,
// so the list is the constraint's entire new state -- a family omitted from it
// is revoked, not left alone. That makes the whitelist easy to widen and
// equally easy to silently narrow: a migration authored against an older base
// and merged (or renumbered) after a sibling family landed will drop that
// sibling, and a test that only checks "my own family is present" still passes.
//
// This pins the chain instead: each migration's up must be its predecessor's
// set plus exactly the family its filename names, and its down must restore
// the predecessor's set exactly. Order inside the IN (...) list is not part of
// the contract -- 242, 254 and 313 insert mid-list -- so the comparison is by
// set.
var runtimeProfileFamilyMigrationPattern = regexp.MustCompile(`^(\d+)_runtime_profile_add_([a-z]+)$`)

var protocolFamilyLiteralPattern = regexp.MustCompile(`(?m)^\s*'([a-z]+)',?\s*$`)

func TestRuntimeProfileFamilyMigrationsNeverDropASibling(t *testing.T) {
	type familyMigration struct {
		prefix int
		stem   string
		added  string
	}

	dir := realMigrationsDir(t)
	files := migrationFilesForLint(t, "*_runtime_profile_add_*.up.sql")

	var chain []familyMigration
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		match := runtimeProfileFamilyMigrationPattern.FindStringSubmatch(stem)
		if match == nil {
			t.Fatalf("migration %s does not match the runtime_profile_add_<family> naming the chain check relies on", stem)
		}
		prefix, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration prefix of %s: %v", stem, err)
		}
		chain = append(chain, familyMigration{prefix: prefix, stem: stem, added: match[2]})
	}
	sort.Slice(chain, func(i, j int) bool { return chain[i].prefix < chain[j].prefix })

	if len(chain) < 2 {
		t.Fatalf("expected at least two runtime_profile family migrations, found %d", len(chain))
	}

	readFamilies := func(stem, direction string) map[string]bool {
		path := filepath.Join(dir, stem+"."+direction+".sql")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(body), "runtime_profile_protocol_family_check") {
			t.Fatalf("%s does not rebuild runtime_profile_protocol_family_check", path)
		}
		matches := protocolFamilyLiteralPattern.FindAllStringSubmatch(string(body), -1)
		if len(matches) == 0 {
			t.Fatalf("%s lists no protocol families", path)
		}
		families := make(map[string]bool, len(matches))
		for _, m := range matches {
			if families[m[1]] {
				t.Errorf("%s lists protocol family %q twice", path, m[1])
			}
			families[m[1]] = true
		}
		return families
	}

	previous := readFamilies(chain[0].stem, "up")
	for _, migration := range chain[1:] {
		up := readFamilies(migration.stem, "up")
		down := readFamilies(migration.stem, "down")

		want := make(map[string]bool, len(previous)+1)
		for family := range previous {
			want[family] = true
		}
		want[migration.added] = true

		for family := range want {
			if !up[family] {
				t.Errorf("%s.up.sql drops protocol family %q; the rebuilt CHECK must carry every family the previous migration allowed, plus %q", migration.stem, family, migration.added)
			}
		}
		for family := range up {
			if !want[family] {
				t.Errorf("%s.up.sql adds unexpected protocol family %q; a family migration may only add the family its filename names (%q)", migration.stem, family, migration.added)
			}
		}

		if down[migration.added] {
			t.Errorf("%s.down.sql still allows %q; the rollback must remove the family the migration added", migration.stem, migration.added)
		}
		for family := range previous {
			if !down[family] {
				t.Errorf("%s.down.sql drops protocol family %q; the rollback must restore the previous migration's set exactly", migration.stem, family)
			}
		}
		for family := range down {
			if !previous[family] {
				t.Errorf("%s.down.sql allows %q, which the previous migration did not; the rollback must restore the previous migration's set exactly", migration.stem, family)
			}
		}

		previous = up
	}

	// The tail of the chain is what agent.SupportedTypes must match, so name
	// the families whose loss this test exists to catch.
	for _, family := range []string{"mcode", "dim", "prime"} {
		if !previous[family] {
			t.Errorf("the newest runtime_profile family migration does not allow %q", family)
		}
	}
}
