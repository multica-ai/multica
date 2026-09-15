# Aurora 积分账本 + 计费 — 实现计划（Plan 2）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> 修订：2026-09-13（评审回写，详见文末「修订记录」）

**Goal:** 在 Go 后端建立 Aurora 的积分账本核心：余额 + 流水 + 预留/退款/发放的幂等服务，以及余额/流水只读 API。订阅产品线与 Stripe 充值**不在本计划**（见「Deferred」）。

**Architecture:** 新建 `credit_balance` / `credit_ledger` 表与 sqlc 查询；新建 `server/internal/aurora/credit.go` 的 `CreditService`（`Reserve`/`Refund`/`Grant`/`Balance`，事务 + 幂等键）；新增 `GET /api/aurora/billing/balance` 与 `GET /api/aurora/billing/transactions` 两个只读端点。契约逐字沿用 `packages/core/types/billing.ts`（micro-credit，1 USD = 1000 credit，kind 枚举 `topup/deduction/refund/expire/adjustment`——已核实 `types/billing.ts:26-31`）。

**Tech Stack:** Go 1.26、sqlc、pgx/v5、`server/internal/testutil`。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§6 计费）。

**依赖：** 依赖 Plan 1 的 `aurora_generation` 表（`credits_reserved`/`credits_charged` 字段由 Plan 3 的预留调用写入）与 handler 基建。

## Global Constraints

- 不加外键/级联；关系与清理在应用代码处理。
- 新建索引用 `CREATE UNIQUE INDEX CONCURRENTLY`，**每个索引单独一个 migration 文件**（`credit_ledger.idempotency_key` 唯一索引单列一个文件），且**必须注册进 `cmd/migrate/main.go` 的 `concurrentIndexCleanups`/`concurrentDownIndexCleanups`**（`TestEveryConcurrentUpBuildHasCleanup` 强制，见 Task 1 Step 2）。
- 代码注释英文；gofmt/go vet/显式检查 error。
- 金额单位 micro-credit（`BIGINT`），`1 USD = 1000 credit`，前端展示除以 1e6。
- kind 枚举与 cloud 钱包契约一致：`topup | deduction | refund | adjustment`（`expire` 由 Plan 5 Task 4 实现——`LedgerKindExpire` + `Expire`）。月额度发放 = `adjustment`，Stripe 充值 = `topup`。
- 账本写入必须**幂等**：先查幂等键短路（快速路径），余额变更与流水插入同事务；流水插入冲突（`pgx.ErrNoRows`）视为已处理（rollback + 返回 nil）。
- 从请求边界读 UUID 用 `parseUUIDOrBadRequest`；不 open-code `INSERT...RETURNING`（走 sqlc）。

---

### Task 1: `credit_balance` 与 `credit_ledger` 表 + 唯一索引 + 并发索引注册

**Files:**
- Create: `server/migrations/453_credit_balance.up.sql` / `.down.sql`
- Create: `server/migrations/454_credit_ledger.up.sql` / `.down.sql`
- Create: `server/migrations/455_credit_ledger_idempotency_key_idx.up.sql` / `.down.sql`
- Create: `server/pkg/db/queries/credit.sql`
- Modify: `server/cmd/migrate/main.go`（注册并发索引 cleanup 映射）
- 自动生成：`make sqlc`

**Interfaces:**
- Produces：
  - `credit_balance`：`user_id uuid PK`、`available_micro bigint NOT NULL DEFAULT 0`、`updated_at timestamptz`。
  - `credit_ledger`：`id uuid PK`、`user_id uuid`、`workspace_id uuid`、`kind text`（`topup|deduction|refund|adjustment|expire`）、`amount_micro bigint`（有符号，deduction/expire 为负）、`balance_after_micro bigint`、`reference text`（操作对象：generation id / Stripe 事件 id / Plan 5 发放键 `sub:`/`signup:`/`<userID>:<YYYY-MM>`）、`idempotency_key text`、`created_at timestamptz`。唯一索引 `credit_ledger(idempotency_key)`。
  - sqlc 查询：`GetCreditBalance`（`:one`，无行时返回 `pgx.ErrNoRows`）、`EnsureCreditBalance`（`INSERT ... ON CONFLICT DO NOTHING`）、`DeductCreditBalance`（条件 `available_micro >= $2` 的 UPDATE，0 行=余额不足 → `pgx.ErrNoRows`）、`CreditCreditBalance`（`available_micro + $2`）、`GetCreditLedgerByIdempotencyKey`（`:one`，幂等快速路径）、`InsertCreditLedger`（`ON CONFLICT (idempotency_key) DO NOTHING RETURNING id`，冲突时 `pgx.ErrNoRows`）、`ListCreditTransactions`（`:many`）。

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
-- Append-only credit ledger. kind follows the cloud wallet contract
-- (packages/core/types/billing.ts): topup | deduction | refund | adjustment
-- | expire (expire is implemented by Plan 5's monthly settlement).
-- amount_micro is signed (deduction/expire negative, others positive).
-- reference names the operation's subject — a generation id, a Stripe event
-- id, or a Plan 5 grant key ("sub:<userID>:<YYYY-MM>", "signup:<userID>",
-- "<userID>:<YYYY-MM>") — so the transactions UI can label each row; the
-- UI falls back to kind-based labels for non-generation references.
-- idempotency_key makes retries safe; the unique index enforcing it lives
-- in its own migration file.
CREATE TABLE IF NOT EXISTS credit_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL,
    amount_micro BIGINT NOT NULL,
    balance_after_micro BIGINT NOT NULL,
    reference TEXT NOT NULL,
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

- [ ] **Step 2: 注册并发索引 cleanup 映射（2026-09-13 评审新增）**

`server/cmd/migrate/main.go`：仓库测试 `TestEveryConcurrentUpBuildHasCleanup`（`cmd/migrate/migrate_mul5999_index_retry_test.go:73-124`）会 glob 全部真实 migration，强制每个 up 方向使用 `CREATE [UNIQUE] INDEX CONCURRENTLY` 的 migration 在 `concurrentIndexCleanups`（`main.go:141`）注册**同名条目**；不注册则测试挂。为 `credit_ledger_idempotency_key_idx` 在 up 映射加一条即可——**不要注册 `concurrentDownIndexCleanups`**：down 文件只是 `DROP INDEX CONCURRENTLY`，而 down 映射只收「down 方向重建索引」的 migration（`main.go:303-311`），注册进去会被 `TestConcurrentIndexCleanupsMatchTheirMigrations` 判挂。up 注册后 `preMigrationHooks` 自动派生，无需手加。

- [ ] **Step 3: 写 sqlc 查询文件**

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

-- name: GetCreditLedgerByIdempotencyKey :one
SELECT id FROM credit_ledger WHERE idempotency_key = $1;

-- name: InsertCreditLedger :one
INSERT INTO credit_ledger (user_id, workspace_id, kind, amount_micro, balance_after_micro, reference, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;

-- name: ListCreditTransactions :many
SELECT id, user_id, workspace_id, kind, amount_micro, balance_after_micro, reference, idempotency_key, created_at
FROM credit_ledger
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
```

- [ ] **Step 4: 运行 sqlc**

Run: `make sqlc`（仓库根执行）
Expected: 生成上述查询。`DeductCreditBalance`/`CreditCreditBalance` 为 `(int64, error)`（`:one` 单列）；**无匹配行时返回 `pgx.ErrNoRows`**（sqlc `:one` 语义，仓库先例 `UpdateIssueStatus` + `workspace_scope_guard_test.go:89-100`）；`InsertCreditLedger` 冲突时同样返回 `pgx.ErrNoRows`（先例 `CreateRetryTask`，`fail_task_successor_test.go:135-170`）。字段名以生成为准：预期 `amount_micro` → `AmountMicro`（sqlc 仅对 `id` 做首字母大写特判，其余 snake→Camel，先例 `AvatarUrl`/`McpConfig`）。

- [ ] **Step 5: 验证 migration**

Run: `make test`（仓库根执行；先跑 `go run ./cmd/migrate up` 再跑全部 Go 测试；`go test ./internal/migrations/` 不会应用新迁移）
Expected: 迁移成功应用；`TestEveryConcurrentUpBuildHasCleanup` 通过（Step 2 已注册）；Go 测试全绿。

- [ ] **Step 6: Commit**

```bash
git add server/migrations/453_credit_balance.* server/migrations/454_credit_ledger.* server/migrations/455_credit_ledger_idempotency_key_idx.* server/pkg/db/queries/credit.sql server/pkg/db/generated/ server/cmd/migrate/main.go
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
  - `(*CreditService).Reserve(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error` —— 幂等扣减（kind=`deduction`），`idempotency_key = "reserve:"+reference`（reference = generation id）；余额不足返回 `aurora.ErrInsufficientCredits`。
  - `(*CreditService).Refund(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error` —— 幂等回充（kind=`refund`），`idempotency_key = "refund:"+reference`。
  - `(*CreditService).Grant(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, kind, reference string) error` —— 发放（kind ∈ `topup|adjustment`，非法 kind 报错；月额度发放用 `adjustment`，未来 Stripe webhook 充值用 `topup`），`idempotency_key = "grant:"+reference`。
  - `(*CreditService).Balance(ctx, userID pgtype.UUID) (int64, error)` —— 无行返回 `(0, nil)`。

- [ ] **Step 1: 写失败测试**

`server/internal/aurora/credit_test.go`（package `aurora_test`，用 `DATABASE_URL` 建池，跳过逻辑同 handler TestMain；raw pgxpool + fallback DSN 是仓库先例，见 `internal/service/task_claim_race_test.go:22-43`）：

```go
package aurora_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/aurora"
	"github.com/multica-ai/multica/server/internal/util"
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
	return util.MustParseUUID(id)
}

func TestReserveAndRefundAreIdempotent(t *testing.T) {
	svc, pool := newTestCreditService(t)
	user := newUUID(t, pool)
	ws := newUUID(t, pool)

	if err := svc.Grant(context.Background(), user, ws, 1000, aurora.LedgerKindAdjustment, "seed"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
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
	// 幂等：重复 refund 不应再加。
	if err := svc.Refund(context.Background(), user, ws, 300, "gen-1"); err != nil {
		t.Fatalf("Refund retry: %v", err)
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

func TestGrantRejectsInvalidKind(t *testing.T) {
	svc, pool := newTestCreditService(t)
	user := newUUID(t, pool)
	ws := newUUID(t, pool)
	if err := svc.Grant(context.Background(), user, ws, 100, "bogus", "seed"); err == nil {
		t.Fatal("Grant with invalid kind should fail")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/aurora/ -run 'TestReserve|TestGrant'`
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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrInsufficientCredits = errors.New("insufficient credits")

// Ledger kinds follow the cloud wallet contract
// (packages/core/types/billing.ts): topup | deduction | refund | adjustment.
// "expire" is added by Plan 5 (LedgerKindExpire + Expire, monthly
// settlement).
const (
	LedgerKindTopup      = "topup"
	LedgerKindDeduction  = "deduction"
	LedgerKindRefund     = "refund"
	LedgerKindAdjustment = "adjustment"
)

// TxBeginner is the narrow transaction-starting surface CreditService needs.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// CreditService owns Aurora credit accounting. Every write is idempotent via a
// derived idempotency_key: the pre-check short-circuits retries before any
// balance change, and a concurrent duplicate that loses the ledger-insert race
// rolls back and returns nil (the committed transaction already applied it).
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

// Reserve deducts amountMicro and records a "deduction" ledger row, keyed by
// "reserve:"+reference (the generation id) so retries are safe.
func (s *CreditService) Reserve(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error {
	return s.adjust(ctx, userID, workspaceID, -amountMicro, LedgerKindDeduction, "reserve:"+reference, reference)
}

// Refund credits amountMicro back and records a "refund" ledger row, keyed by
// "refund:"+reference.
func (s *CreditService) Refund(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error {
	return s.adjust(ctx, userID, workspaceID, amountMicro, LedgerKindRefund, "refund:"+reference, reference)
}

// Grant adds amountMicro and records a ledger row of the given kind. Monthly
// quota grants use LedgerKindAdjustment; Stripe purchases use LedgerKindTopup
// (the Plan 5 webhook calls this). Keyed by "grant:"+reference.
func (s *CreditService) Grant(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, kind, reference string) error {
	if kind != LedgerKindTopup && kind != LedgerKindAdjustment {
		return fmt.Errorf("invalid grant kind %q", kind)
	}
	return s.adjust(ctx, userID, workspaceID, amountMicro, kind, "grant:"+reference, reference)
}

// adjust applies a signed delta: negative delta = deduct (must have balance),
// positive delta = credit. Idempotent: the fast-path pre-check makes retries
// no-ops; a concurrent duplicate loses the ledger-insert race, rolls back its
// redundant balance change and returns nil.
func (s *CreditService) adjust(ctx context.Context, userID, workspaceID pgtype.UUID, delta int64, kind, idempotencyKey, reference string) error {
	// Fast path: if the ledger row already exists, the operation was applied.
	if _, err := s.queries.GetCreditLedgerByIdempotencyKey(ctx, idempotencyKey); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

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
			UserID: userID, AmountMicro: -delta,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInsufficientCredits
		}
	} else {
		balanceAfter, err = qtx.CreditCreditBalance(ctx, db.CreditCreditBalanceParams{
			UserID: userID, AmountMicro: delta,
		})
	}
	if err != nil {
		return err
	}

	if _, err := qtx.InsertCreditLedger(ctx, db.InsertCreditLedgerParams{
		UserID: userID, WorkspaceID: workspaceID, Kind: kind,
		AmountMicro: delta, BalanceAfterMicro: balanceAfter,
		Reference: reference, IdempotencyKey: idempotencyKey,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Concurrent duplicate: the other transaction already committed
			// this operation; our rolled-back balance change was redundant.
			return nil
		}
		return err
	}
	return tx.Commit(ctx)
}
```

> `DeductCreditBalanceParams` / `CreditCreditBalanceParams` 字段名（预期 `UserID`/`AmountMicro`）以 `make sqlc` 生成为准（Task 1 Step 4）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/aurora/ -run 'TestReserve|TestGrant'`
Expected: 三个测试 PASS。

- [ ] **Step 5: 接入 `Handler` 并 Commit**

`server/internal/handler/handler.go`：在 `Handler` 结构体加 `Credit *aurora.CreditService`，`New` 里加 `Credit: aurora.NewCreditService(queries, txStarter)`（`txStarter` 已实现 `Begin`，`handler.go:58-59`）。

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
  - `GET /api/aurora/billing/transactions?limit=50` → `200 {"transactions":[{id,kind,amountMicro,balanceAfterMicro,reference,createdAt}]}`。

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
		Transactions []struct {
			Kind             string `json:"kind"`
			AmountMicro      int64  `json:"amountMicro"`
			BalanceAfterMicro int64  `json:"balanceAfterMicro"`
			Reference        string `json:"reference"`
		} `json:"transactions"`
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
			"reference": row.Reference, "createdAt": row.CreatedAt.Time,
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

- **Stripe 充值 / 订阅产品线**：需要 Stripe key、checkout session、webhook 验签、`subscription` 权益表。`Grant(kind="topup")` 已预留为 webhook 的落账入口。Plan 2 只做账本 + 只读 API。
- **权益门禁 enforcement**：self-host 下 `entitlement` 云 URL 未配置即 fail-open——`BaseURL` 为空 → `enabled=false`、Provider 为 nil、所有 gate 返回 `ActionOff`（`entitlement/client.go:48-50/95-100`，`router.go:459-463`）。另注意 `normalizePolicy` 目前**硬性要求两个 gate 都在**（`client.go:278-299`）：将来加 `GateAurora*` 时旧 policy 会整体被拒，需与 `normalizePolicy` 同 PR 修改（Plan 5/safety 处理）。
- **`aurora_generation.credits_reserved/charged` 写入**：由 Plan 3（执行层）在入队时调 `h.Credit.Reserve` 并把预留金额写回 generation 行；失败/卡死退款也由 Plan 3 Task 4 接现有 sweeper 终态路径。
- **两步冻结账本**：二期（spec §6.2）；`expire` 交易已由 Plan 5 Task 4 实现（`LedgerKindExpire` + `Expire` + 月结算循环）。

## Self-Review

- **Spec 覆盖**：§6.2 积分账本（balance/ledger/幂等/预留/退款/发放/`reference` 列）→ Task 1/2；§6 余额/流水展示 → Task 3；§6.1 订阅与 §6.3 门禁 → Deferred（已明确）。
- **契约一致性**：kind 枚举逐字对齐 `packages/core/types/billing.ts:26-31`（`topup|deduction|refund|adjustment`）；micro-credit 单位、`availableMicro`/`amountMicro` 字段名与 Plan 4 schema 一致。
- **幂等正确性**：三层——`idempotency_key` 唯一索引（Task 1，并发索引已注册 cleanup 映射）+ 快速路径预查（`GetCreditLedgerByIdempotencyKey`）+ 插入冲突 `pgx.ErrNoRows` 视为已处理（回滚冗余余额变更）；并发同 key 双事务下仅赢家落账。
- **语义正确性**：`adjust` 约定 negative=deduct/positive=credit，`Reserve` 传负、`Refund`/`Grant` 传正；`DeductCreditBalance` 的 `available_micro >= $2` 条件保证不超扣（0 行 → `ErrInsufficientCredits`）。

## 执行交接

Plan 2 完成。后续顺序：Plan 3（执行层）→ Plan 3.5（进度与作品库 API）→ Plan 4（前端）→ Plan safety → Plan 5（订阅 + Stripe + 权益门禁，`2026-09-13-aurora-subscriptions-payments.md`，已编写）。

## 修订记录（2026-09-13 评审回写）

| 处 | 修正 |
|----|------|
| 全局 | kind 枚举 `grant\|deduction\|refund` → 对齐契约 `topup\|deduction\|refund\|adjustment`（`types/billing.ts:26-31`）；`Grant` 加 kind 参数 |
| Task 1 | `credit_ledger` 加 `reference` 列；新增 Step 2：并发索引必须注册 `cmd/migrate/main.go` 的 `concurrentIndexCleanups`/`concurrentDownIndexCleanups`（否则 `TestEveryConcurrentUpBuildHasCleanup` 挂）；验证命令改 `make test` |
| Task 2 | 修复三重缺陷：符号反转（`Reserve`/`Refund`/`Grant` 的 delta 传反）、幂等失效（余额先改、冲突被当错误）、`ON CONFLICT DO NOTHING` 的 `pgx.ErrNoRows` 语义（仓库先例 `CreateRetryTask`）；改为快速路径预查 + 冲突视为已处理；`ParseUUID` 包装移除（测试直接用 `util.MustParseUUID`）；`_ = svc.Grant` 忽略错误改为显式检查；补 `TestGrantRejectsInvalidKind` |
| Task 3 | 流水响应加 `reference` 字段 |
| Deferred | entitlement fail-open 与 `normalizePolicy` 双 gate 硬校验已核实并注明 |
