package kagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func ConversationKey(workspaceID, agentID, conversationID string) string {
	raw, _ := json.Marshal([]string{workspaceID, agentID, conversationID})
	return "conversation/" + string(raw)
}

// Identity pins the qualified gateway endpoint without storing its URL in the
// conversation journal. Credentials rotate independently of endpoint identity.
func (c *A2AClient) Identity() string {
	hash := sha256.Sum256([]byte(c.wire.endpoint))
	return hex.EncodeToString(hash[:])
}
