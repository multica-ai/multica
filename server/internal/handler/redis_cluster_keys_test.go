package handler

import (
	"strings"
	"testing"
)

// hashTag extracts the Redis Cluster hash tag from a key: the substring
// between the first '{' and the first '}' that follows it, when that substring
// is non-empty. This mirrors the algorithm Redis uses to pick the
// slot-determining part of a key. Used here only to assert co-location in
// tests — production routing relies on the real server-side implementation.
func hashTag(key string) string {
	open := strings.IndexByte(key, '{')
	if open < 0 {
		return ""
	}
	rest := key[open+1:]
	closeIdx := strings.IndexByte(rest, '}')
	if closeIdx <= 0 {
		return ""
	}
	return rest[:closeIdx]
}

// TestRuntimePendingStoreKeysColocate asserts that every runtime pending store
// (model list, CLI update, local-skill list/import) tags its record, pending,
// and active keys with the shared {runtime_pending} hash tag, so a create
// pipeline that touches a record key and its pending ZSET together lands on a
// single Redis Cluster slot. Without co-location the create Pipeline would
// return CROSSSLOT under Cluster.
func TestRuntimePendingStoreKeysColocate(t *testing.T) {
	const (
		runtimeID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		reqID     = "req-1234567890"
		wantTag   = "runtime_pending"
	)

	keys := []struct {
		name string
		key  string
	}{
		{"model_list_record", modelListKey(reqID)},
		{"model_list_pending", modelListPendingKey(runtimeID)},
		{"update_record", updateKey(reqID)},
		{"update_pending", updatePendingKey(runtimeID)},
		{"update_active", updateActiveKey(runtimeID)},
		{"local_skill_list_record", localSkillListKey(reqID)},
		{"local_skill_list_pending", localSkillListPendingKey(runtimeID)},
		{"local_skill_import_record", localSkillImportKey(reqID)},
		{"local_skill_import_pending", localSkillImportPendingKey(runtimeID)},
	}
	for _, k := range keys {
		if got := hashTag(k.key); got != wantTag {
			t.Errorf("%s key %q hash tag = %q, want %q", k.name, k.key, got, wantTag)
		}
	}
}
