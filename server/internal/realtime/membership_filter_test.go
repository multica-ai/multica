package realtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func membershipCommentFrame(actor string, bodyBytes int) []byte {
	// Include the metadata attached by registerListeners' SubscribeAll path.
	raw, err := json.Marshal(map[string]any{
		"type": "comment:created", "actor_type": actor, "actor_id": "actor-id",
		"payload": map[string]string{"body": strings.Repeat("x", bodyBytes)},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestMembershipRemovalPrefilterSkipsOrdinaryActorMetadata(t *testing.T) {
	for _, actor := range []string{"member", "agent"} {
		raw := membershipCommentFrame(actor, 8192)
		if memberRemovalCandidate(raw) {
			t.Fatalf("actor_type=%q made an ordinary comment a removal candidate", actor)
		}
		if _, removal := parseMemberRemoval(testWorkspaceID, raw); removal {
			t.Fatal("ordinary comment was misclassified as a removal")
		}
	}
}

func BenchmarkMembershipOrdinaryEventRemovalFilter(b *testing.B) {
	for _, n := range []int{8192, 65536} {
		for _, actor := range []string{"agent", "member"} {
			b.Run(fmt.Sprintf("body=%d/actor=%s", n, actor), func(b *testing.B) {
				raw := membershipCommentFrame(actor, n)
				b.ReportAllocs()
				b.SetBytes(int64(len(raw)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					parseMemberRemoval(testWorkspaceID, raw)
				}
			})
		}
	}
}
