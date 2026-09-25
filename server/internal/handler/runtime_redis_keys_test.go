package handler

import (
	"strings"
	"testing"
)

func TestRuntimePendingRedisKeysShareClusterHashTag(t *testing.T) {
	for name, keys := range map[string][]string{
		"model list claim": {
			modelListPendingKey("runtime-1"),
			modelListKey("req-1"),
		},
		"local skill list claim": {
			localSkillListPendingKey("runtime-1"),
			localSkillListKey("req-1"),
		},
		"local skill import claim": {
			localSkillImportPendingKey("runtime-1"),
			localSkillImportKey("req-1"),
		},
		"update claim": {
			updatePendingKey("runtime-1"),
			updateKey("req-1"),
		},
		"update active guard": {
			updateActiveKey("runtime-1"),
			updateKey("req-1"),
			updatePendingKey("runtime-1"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			want := redisClusterHashTag(keys[0])
			if want == "" {
				t.Fatalf("%q has no hash tag", keys[0])
			}
			for _, key := range keys[1:] {
				if got := redisClusterHashTag(key); got != want {
					t.Fatalf("key %q hash tag = %q, want %q; keys in one Lua script or transaction must share a Redis Cluster slot", key, got, want)
				}
			}
		})
	}
}

func redisClusterHashTag(key string) string {
	start := strings.IndexByte(key, '{')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(key[start+1:], '}')
	if end <= 0 {
		return ""
	}
	return key[start+1 : start+1+end]
}
