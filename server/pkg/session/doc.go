// Package session provides the core session lifecycle service for agent sessions.
//
// It manages creation, loading, expiration, and reset of sessions tied to
// specific issue+agent pairs. Sessions persist conversation state, tool results,
// and file modification history across agent runs.
//
// Key features:
//   - Automatic run_number incrementing for each new session
//   - Row-level locking (SELECT ... FOR UPDATE) for concurrent safety
//   - Optimistic concurrency control via version field
//   - Automatic message compression when history exceeds threshold
//   - Configurable inactivity-based expiration
//
// Usage:
//
//	pool, _ := pgxpool.New(ctx, connString)
//	svc := session.NewService(pool, session.DefaultConfig(), logger)
//
//	// First run: creates session with run_number=1
//	sess, _ := svc.CreateSession(ctx, issueID, agentID)
//
//	// Subsequent runs: loads existing active session
//	sess, _ = svc.GetActiveSession(ctx, issueID, agentID)
//
//	// Reset: deactivate current, next run starts fresh
//	svc.ResetSession(ctx, issueID, agentID)
package session
