package auth

// User kinds (user.kind). A service account is a machine identity: a user row
// that belongs to exactly one workspace and is minted and revoked only through
// workspace owner/admin endpoints. Service accounts cannot log in
// interactively (no reachable mailbox to receive a login code) and their
// requests are classified as machine credentials via X-Actor-Source so the
// actor guards can keep them off human-only and account-level routes.
const (
	HumanUserKind   = "human"
	ServiceUserKind = "service"
)
