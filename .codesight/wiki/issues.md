# Issues

> **Navigation aid.** Route list and file locations extracted via AST. Read the source files listed below before implementing or modifying this subsystem.

The Issues subsystem handles **22 routes** and touches: auth, db.

## Routes

- `GET` `/api/issues/search` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/child-progress` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues/batch-update` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues/batch-delete` [auth, db, upload]
  `server/cmd/server/router.go`
- `PUT` `/api/issues` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/issues` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues/comments` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/comments` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/timeline` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/subscribers` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues/unsubscribe` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/active-task` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues/tasks/{taskId}/cancel` params(taskId) [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/task-runs` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/usage` [auth, db, upload]
  `server/cmd/server/router.go`
- `POST` `/api/issues/reactions` [auth, db, upload]
  `server/cmd/server/router.go`
- `DELETE` `/api/issues/reactions` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/attachments` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/api/issues/children` [auth, db, upload]
  `server/cmd/server/router.go`
- `GET` `/issues/{issueId}/gc-check` params(issueId) [auth, db, upload]
  `server/cmd/server/router.go`

## Source Files

Read these before implementing or modifying this subsystem:
- `server/cmd/server/router.go`

---
_Back to [overview.md](./overview.md)_