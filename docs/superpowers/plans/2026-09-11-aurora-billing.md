# Aurora 积分账本 + 计费 — 实现计划（Plan 2）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Go 后端建立 Aurora 的积分账本核心：余额 + 流水 + 预留/退款/发放的幂等服务，以及余额/流水只读 API。订阅产品线与 Stripe 充值**不在本计划**（见「Deferred」）。

**Architecture:** 新建 `credit_balance` / `credit_ledger` 表与 sqlc 查询；新建 `server/internal/aurora/credit.go` 的 `CreditService`（`Reserve`/`Refund`/`Grant`/`Balance`，事务 + 幂等键）；新增 `GET /api/aurora/billing/balance` 与 `GET /api/aurora/billing/transactions` 两个只读端点。契约沿用 `packages/core/types/billing.ts`（micro-credit，1 USD = 1000 credit）。

**Tech Stack:** Go 1.26、sqlc、pgx/v5、`server/internal/testutil`。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§6 计费）。

**依赖：** 依赖 Plan 1 的 `aurora_generation` 表（`credits_reserved`/`credits_charged` 字段由 Plan 3 的预留调用写入）与 handler 基建。

## Global Constraints

- 不加外键/级联；关系与清理在应用代码处理。
- 新建索引用 `CREATE UNIQUE INDEX CONCURRENTLY`，**每个索引单独一个 migration 文件**（`credit_ledger.idempotency_key` 唯一索引单列一个文件）。
- 代码注释英文；gofmt/go vet/显式检查 error。
- 金额单位 micro-credit（`BIGINT`），`1 USD = 1000 credit`，前端展示除以 1e6。
- 账本写入必须**幂等**（唯一 `idempotency_key` + `ON CONFLICT DO NOTHING`）且**事务内**完成「余额变更 + 流水插入」。
- 从请求边界读 UUID 用 `parseUUIDOrBadRequest`；不 open-code `INSERT...RETURNING`（走 sqlc）。

---

### Task 1: `credit_balance` 与 `credit_ledger` 表 + 唯一索引

**Files:**
- Create: `server/migrations/453_credit_balance.up.sql` / `.down.sql`
- Create: `server/migrations/454_credit_ledger.up.sql` / `.down.sql`
- Create: `server/migrations/455_credit_ledger_idempotency_key_idx.up.sql` / `.down.sql`
- Create: `server/pkg/db/queries/credit.sql`
- 自动生成：`make sqlc`

**Interfaces:**
- Produces：
  - `credit_balance`：`user_id uuid PK`、`available_micro bigint NOT NULL DEFAULT 0`、`updated_at timestamptz`。
  - `credit_ledger`：`id uuid PK`、`user_id uuid`、`workspace_id uuid`、`kind text`（`grant|deduction|refund`）、`amount_micro bigint`（有符号）、`balance_after_micro bigint`、`idempotency_key text`、`created_at timestamptz`。唯一索引 `credit_ledger(idempotency_key)`。
  - sqlc 查询：`GetCreditBalance`（`:one`，无行时返回 `sql.ErrNoRows`）、`EnsureCreditBalance`（`INSERT ... ON CONFLICT DO NOTHING`）、`DeductCreditBalance`（条件 `available_micro >= $2` 的 UPDATE，0 行=余额不足）、`CreditCreditBalance`（`available_micro + $2`）、`InsertCreditLedger`（`ON CONFLICT (idempotency_key) DO NOTHING RETURNING id`）、`ListCreditTransactions`（`:many`）。

- [ ] **Step 1: 写 migration 文件**

`server/migrations/453_credit_balance.up.sql`：

```sql
-- Aurora credit wallet: one row per user. available_micro is the spendable
-- micro-credit balance (1 credit = 1e6 micro, 1 USD = 1000 credit).
CREATE TABLE IF NOT EXISTS credit_balance (
    user_id UUID PRIMARY KEY,
    available_micro BIGINT NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);
```

`server/migrations/453_credit_balance.down.sql`：

```sql
DROP TABLE IF EXISTS credit_balance;
```

`server/migrations/454_credit_ledger.up.sql`：

```sql
-- Append-only credit ledger. kind: grant | deduction | refund. amount_micro is
-- signed (deduction negative, grant/refund positive). idempotency_key makes
-- retries safe; the unique index enforcing it lives in its own migration.
CREATE TABLE IF NOT EXISTS credit_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL,
    amount_micro BIGINT NOT NULL,
    balance_after_micro BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
```

`server/migrations/454_credit_ledger.down.sql`：

```sql
DROP TABLE IF EXISTS credit_ledger;
```

`server/migrations/455_credit_ledger_idempotency_key_idx.up.sql`：

```sql
-- Idempotency key uniqueness, in its own file because CREATE UNIQUE INDEX
-- CONCURRENTLY cannot share a statement or run inside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS credit_ledger_idempotency_key_idx
    ON credit_ledger (idempotency_key);
```

`server/migrations/455_credit_ledger_idempotency_key_idx.down.sql`：

```sql
DROP INDEX CONCURRENTLY IF EXISTS credit_ledger_idempotency_key_idx;
```

- [ ] **Step 2: 写 sqlc 查询文件**

`server/pkg/db/queries/credit.sql`：

```sql
-- name: GetCreditBalance :one
SELECT available_micro FROM credit_balance WHERE user_id = $1;

-- name: EnsureCreditBalance :exec
INSERT INTO credit_balance (user_id, available_micro) VALUES ($1, 0)
ON CONFLICT (user_id) DO NOTHING;

-- name: DeductCreditBalance :one
UPDATE credit_balance
SET available_micro = available_micro - $2, updated_at = now()
WHERE user_id = $1 AND available_micro >= $2
RETURNING available_micro;

-- name: CreditCreditBalance :one
UPDATE credit_balance
SET available_micro = available_micro + $2, updated_at = now()
WHERE user_id = $1
RETURNING available_micro;

-- name: InsertCreditLedger :one
INSERT INTO credit_ledger (user_id, workspace_id, kind, amount_micro, balance_after_micro, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;

-- name: ListCreditTransactions :many
SELECT id, user_id, workspace_id, kind, amount_micro, balance_after_micro, idempotency_key, created_at
FROM credit_ledger
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
```

- [ ] **Step 3: 运行 sqlc**

Run: `cd server && make sqlc`
Expected: 生成上述查询；`DeductCreditBalance`/`CreditCreditBalance` 返回 `(int64, error)`（`:one` 单列）。确认 `DeductCreditBalance` 在 `WHERE` 不满足时返回 `pgx.ErrNoRows` 还是 `(0, nil)`——sqlc 的 `:one` UPDATE 无匹配行会返回 `pgx.ErrNoRows`，余额不足用这个错误区分（本计划的 `Reserve` 依赖此语义）。

- [ ] **Step 4: 验证 migration**

Run: `cd server && go test ./internal/migrations/ -run . -count=1`
Expected: 通过。

- [ ] **Step 5: Commit**

```bash
git add server/migrations/453_credit_balance.* server/migrations/454_credit_ledger.* server/migrations/455_credit_ledger_idempotency_key_idx.* server/pkg/db/queries/credit.sql server/pkg/db/generated/
git commit -m "feat(aurora): add credit balance and ledger tables"
```

---

### Task 2: `CreditService`（幂等账本操作）

**Files:**
- Create: `server/internal/aurora/credit.go`
- Create: `server/internal/aurora/credit_test.go`
- Modify: `server/internal/handler/handler.go`（在 `Handler` 加 `Credit *aurora.CreditService` 字段，`New` 里构造）

**Interfaces:**
- Consumes: Task 1 的 sqlc 查询；`pgx.Tx` 事务。
- Produces（Plan 3 的扣费依赖）：
  - `aurora.NewCreditService(q *db.Queries, tx aurora.TxBeginner) *CreditService`
  - `(*CreditService).Reserve(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error` —— 幂等扣减，`idempotency_key = "reserve:"+reference`；余额不足返回 `aurora.ErrInsufficientCredits`。
  - `(*CreditService).Refund(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error` —— 幂等回充，`idempotency_key = "refund:"+reference`。
  - `(*CreditService).Grant(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error` —— 发放（未来 Stripe webhook 调用），`idempotency_key = "grant:"+reference`。
  - `(*CreditService).Balance(ctx, userID pgtype.UUID) (int64, error)` —— 无行返回 `(0, nil)`。

- [ ] **Step 1: 写失败测试**

`server/internal/aurora/credit_test.go`（package `aurora_test`，用 `DATABASE_URL` 建池，跳过逻辑同 handler TestMain）：

```go
package aurora_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/aurora"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newTestCreditService(t *testing.T) (*aurora.CreditService, *pgxpool.Pool) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	t.Cleanup(pool.Close)
	return aurora.NewCreditService(db.New(pool), pool), pool
}

func newUUID(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	u, err := aurora.ParseUUID(id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestReserveAndRefundAreIdempotent(t *testing.T) {
	svc, pool := newTestCreditService(t)
	user := newUUID(t, pool)
	ws := newUUID(t, pool)

	_ = svc.Grant(context.Background(), user, ws, 1000, "seed")
	if err := svc.Reserve(context.Background(), user, ws, 300, "gen-1"); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	// 幂等：重复 reserve 同 reference 不应再扣。
	if err := svc.Reserve(context.Background(), user, ws, 300, "gen-1"); err != nil {
		t.Fatalf("Reserve retry: %v", err)
	}
	bal, _ := svc.Balance(context.Background(), user)
	if bal != 700 {
		t.Fatalf("balance after idempotent reserve = %d, want 700", bal)
	}

	if err := svc.Refund(context.Background(), user, ws, 300, "gen-1"); err != nil {
		t.Fatalf("Refund: %v", err)
	}
	bal, _ = svc.Balance(context.Background(), user)
	if bal != 1000 {
		t.Fatalf("balance after refund = %d, want 1000", bal)
	}
}

func TestReserveFailsWhenInsufficient(t *testing.T) {
	svc, pool := newTestCreditService(t)
	user := newUUID(t, pool)
	ws := newUUID(t, pool)
	if err := svc.Reserve(context.Background(), user, ws, 100, "gen-x"); err != aurora.ErrInsufficientCredits {
		t.Fatalf("Reserve with empty balance: err = %v, want ErrInsufficientCredits", err)
	}
}
```

> `aurora.ParseUUID(s) (pgtype.UUID, error)` 是本计划新增的导出 helper（封装 `util.ParseUUID`，避免测试直接 import util）。若不想加，测试里用 `util.MustParseUUID`。实现时二选一并保持一致。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/aurora/ -run 'TestReserve'`
Expected: 编译失败（`CreditService` 不存在）。

- [ ] **Step 3: 实现**

`server/internal/aurora/credit.go`：

```go
package aurora

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrInsufficientCredits = errors.New("insufficient credits")

// TxBeginner is the narrow transaction-starting surface CreditService needs.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ParseUUID is the safe UUID parser for callers outside the handler package.
func ParseUUID(s string) (pgtype.UUID, error) { return util.ParseUUID(s) }

// CreditService owns Aurora credit accounting. Every write is idempotent via a
// derived idempotency_key and runs inside one transaction (balance change +
// ledger insert commit or roll back together).
type CreditService struct {
	queries *db.Queries
	tx      TxBeginner
}

func NewCreditService(q *db.Queries, tx TxBeginner) *CreditService {
	return &CreditService{queries: q, tx: tx}
}

func (s *CreditService) Balance(ctx context.Context, userID pgtype.UUID) (int64, error) {
	bal, err := s.queries.GetCreditBalance(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return bal, err
}

// Reserve deducts amountMicro, keyed by "reserve:"+reference so retries are safe.
func (s *CreditService) Reserve(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error {
	return s.adjust(ctx, userID, workspaceID, amountMicro, "deduction", "reserve:"+reference)
}

// Refund credits amountMicro back, keyed by "refund:"+reference.
func (s *CreditService) Refund(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error {
	return s.adjust(ctx, userID, workspaceID, -amountMicro, "refund", "refund:"+reference)
}

// Grant adds amountMicro (future Stripe webhook calls this), keyed by "grant:"+reference.
func (s *CreditService) Grant(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error {
	return s.adjust(ctx, userID, workspaceID, -amountMicro, "grant", "grant:"+reference)
}

// adjust applies a signed delta: negative delta = deduct (must have balance),
// positive delta = credit. Idempotent: if the ledger row already exists the
// transaction commits with no balance change.
func (s *CreditService) adjust(ctx context.Context, userID, workspaceID pgtype.UUID, delta int64, kind, idempotencyKey string) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := s.queries.WithTx(tx)
	if err := qtx.EnsureCreditBalance(ctx, userID); err != nil {
		return err
	}

	var balanceAfter int64
	if delta < 0 {
		balanceAfter, err = qtx.DeductCreditBalance(ctx, db.DeductCreditBalanceParams{
			UserID: userID, Amount: -delta,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInsufficientCredits
		}
	} else {
		balanceAfter, err = qtx.CreditCreditBalance(ctx, db.CreditCreditBalanceParams{
			UserID: userID, Amount: delta,
		})
	}
	if err != nil {
		return err
	}

	if _, err := qtx.InsertCreditLedger(ctx, db.InsertCreditLedgerParams{
		UserID: userID, WorkspaceID: workspaceID, Kind: kind,
		AmountMicro: delta, BalanceAfterMicro: balanceAfter, IdempotencyKey: idempotencyKey,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```

> `DeductCreditBalanceParams` / `CreditCreditBalanceParams` 字段名（`UserID`/`Amount`）以 `make sqlc` 生成为准；`amount_micro` 列会生成 `Amount` 或 `AmountMicro`，按生成结果对齐。`GetCreditBalance` 无行返回 `pgx.ErrNoRows`（sqlc `:one` 语义），`Balance` 里已处理。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/aurora/ -run 'TestReserve'`
Expected: 两个测试 PASS。

- [ ] **Step 5: 接入 `Handler` 并 Commit**

`server/internal/handler/handler.go`：在 `Handler` 结构体加 `Credit *aurora.CreditService`，`New` 里加 `Credit: aurora.NewCreditService(queries, txStarter)`（`txStarter` 已实现 `Begin`）。

```bash
git add server/internal/aurora/credit.go server/internal/aurora/credit_test.go server/internal/handler/handler.go
git commit -m "feat(aurora): idempotent credit ledger service"
```

---

### Task 3: 余额 / 流水只读端点

**Files:**
- Modify: `server/internal/handler/aurora.go`
- Modify: `server/internal/handler/aurora_test.go`
- Modify: `server/cmd/server/router.go`

**Interfaces:**
- Consumes: `h.Credit`（Task 2）、`h.Queries.ListCreditTransactions`。
- Produces：
  - `GET /api/aurora/billing/balance` → `200 {"availableMicro":<int64>}`。
  - `GET /api/aurora/billing/transactions?limit=50` → `200 {"transactions":[{id,kind,amountMicro,balanceAfterMicro,createdAt}]}`。

- [ ] **Step 1: 写失败测试**

`server/internal/handler/aurora_test.go` 追加：

```go
func TestAuroraBillingBalance(t *testing.T) {
	req := newRequest(http.MethodGet, "/api/aurora/billing/balance", nil)
	out := testutil.Decode[struct {
		AvailableMicro int64 `json:"availableMicro"`
	}](t, testHandler.GetAuroraBillingBalance, req, http.StatusOK)
	_ = out // 默认新用户余额 0；断言有字段即可
}

func TestAuroraBillingTransactions(t *testing.T) {
	req := newRequest(http.MethodGet, "/api/aurora/billing/transactions?limit=50", nil)
	out := testutil.Decode[struct {
		Transactions []map[string]any `json:"transactions"`
	}](t, testHandler.ListAuroraBillingTransactions, req, http.StatusOK)
	_ = out // 新用户空列表
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/handler/ -run TestAuroraBilling`
Expected: 编译失败。

- [ ] **Step 3: 实现 handler + 路由**

`server/internal/handler/aurora.go` 追加：

```go
import (
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) GetAuroraBillingBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	bal, err := h.Credit.Balance(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load balance")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"availableMicro": bal})
}

func (h *Handler) ListAuroraBillingTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := h.Queries.ListCreditTransactions(r.Context(), db.ListCreditTransactionsParams{
		UserID: parseUUID(userID), Limit: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load transactions")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id": uuidToString(row.ID), "kind": row.Kind,
			"amountMicro": row.AmountMicro, "balanceAfterMicro": row.BalanceAfterMicro,
			"createdAt": row.CreatedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": out})
}
```

路由（Plan 1 的 workspace 分组内）：

```go
r.Get("/api/aurora/billing/balance", h.GetAuroraBillingBalance)
r.Get("/api/aurora/billing/transactions", h.ListAuroraBillingTransactions)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run TestAuroraBilling`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): billing balance and transactions endpoints"
```

---

## Deferred（不在本计划，明确留给后续）

- **Stripe 充值 / 订阅产品线**：需要 Stripe key、checkout session、webhook 验签、`subscription` 权益表。`Grant` 已预留为 webhook 的落账入口。Plan 2 只做账本 + 只读 API。
- **权益门禁 enforcement**：self-host 下 `entitlement` 云 URL 未配置即全部放行；`GateAuroraGenerations` 门禁扩展留到接 cloud 时再补（`entitlement/types.go` 的 `GateName` 枚举 + `normalizePolicy` 同步）。
- **`aurora_generation.credits_reserved/charged` 写入**：由 Plan 3（执行层）在入队时调 `h.Credit.Reserve` 并把预留金额写回 generation 行。

## Self-Review

- **Spec 覆盖**：§6.2 积分账本（balance/ledger/幂等/预留/退款/发放）→ Task 1/2；§6 余额/流水展示 → Task 3；§6.1 订阅与 §6.3 门禁 → Deferred（已明确）。
- **类型一致性**：`CreditService` 方法签名在测试（Task 2）与实现一致；`DeductCreditBalance`/`CreditCreditBalance` 的 `pgx.ErrNoRows` 语义在 Task 1 Step 3 与 Task 2 代码一致。
- **幂等正确性**：`idempotency_key` 唯一索引（Task 1 单文件）+ `ON CONFLICT DO NOTHING`（Task 2）双层保证重试安全。

## 执行交接

Plan 2 完成。剩余 Plan 3（执行层）、Plan 4（前端）。
