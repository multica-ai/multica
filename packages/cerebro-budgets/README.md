# @multica/cerebro-budgets

Cerebro per-workspace budget controls and the kill-switch / workspace
pause UI that backs them.

- `views/components/kill-switch-section.{tsx,test.tsx}` — pause-the-
  workspace toggle in workspace settings; confirms with an alert dialog
  before flipping `workspace.paused = true`.

## Why the Go budget handlers stay in `server/internal/handler/`

`budget.go`, `budget_preclaim.go`, `workspace_pause.go`, plus the
pricing package, all wire into upstream's `*Handler` struct
(`(h *Handler) GetBudget`, `(h *Handler) SetWorkspacePaused`, etc.) and
their tests reuse the package-level `testHandler` from
`handler_test.go` TestMain. Same fixture-extraction problem documented
in `cerebro-access` and `cerebro-chat` — defer until an upstream sync
demands relocation.
