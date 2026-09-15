# Aurora 订阅产品线 + Stripe + 权益门禁 — 实现计划（Plan 5）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> 修订：2026-09-13 PR #2 review 回写（17 项修正，详见文末「修订记录」）。

**Goal:** 让 Aurora 达到「可对外销售」里程碑：三档个人订阅（Free/Creator/Pro）+ Stripe 订阅/充值 checkout + webhook 记账 + 月额度发放与**按月过期**结算 + 本地权益门禁（月生成次数/并发）+ 注册赠送 + 前端订阅/充值页。

**Architecture:** 新建 `aurora_subscription` 表（一人一订阅，`UNIQUE(user_id)`）；Stripe 经 `PaymentProvider` 接口隔离（handler 测试用 fake，真实实现走 stripe-go）；webhook 公开路由 + 验签 + 事件幂等落账（复用 Plan 2 `CreditService.Grant`，reference=Stripe 事件 id）；月结算（过期+发放）为**自然月窗口**的幂等循环（每次 tick + 启动时各跑一次，错过 1 号自动补跑）；Free 档月额度**惰性发放**（当月首次创建 generation 时）；权益门禁为**本地 enforcement**（`server/internal/aurora/entitlement.go`，不依赖 cloud entitlement——fail-open 现状已核实）。

**Tech Stack:** Go 1.26、sqlc、pgx/v5、stripe-go（以 `go get github.com/stripe/stripe-go@latest` 当前 major 为准）、`server/internal/testutil`。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§6.1 订阅、§6.2 账本、§6.3 权益门禁、§9.1）。

**依赖：** Plan 1（`findOrCreateUser` 钩子）、Plan 2（`CreditService`）、Plan 3.5（generation 查询）、Plan 4（前端 billing 页占位）。

**产品决策（2026-09-13 用户确认）：**
- 三档：Free $0 月送 200 credits；Creator $9.9/月（年付 $99）月送 3,000；Pro $29/月（年付 $290）月送 12,000。
- 充值档：$5 = 5,000 credits、$20 = 20,000 credits。
- **月额度按月过期**：发放按自然月；月底未用部分以 `expire` 交易扣回。注册赠送与充值档**永不过期**。
- 注册一次性赠送 500 credits。

## Global Constraints

- 不加外键/级联；`aurora_subscription` 与 user 的关联为应用级 id。
- 新建索引 `CREATE UNIQUE INDEX CONCURRENTLY` 单文件 + **只注册 `concurrentIndexCleanups`（up 映射）**：`concurrentDownIndexCleanups` 只收 down 方向重建索引的 migration（`main.go:303-311` + `TestConcurrentIndexCleanupsMatchTheirMigrations` 强制），本计划的 down 只是 `DROP INDEX CONCURRENTLY`，注册进 down 映射会挂测试。up 注册后 pre-hook 自动派生。
- migration 序号以合并时 `server/migrations` 最新为准顺延（本计划假设 460-462）。
- **密钥红线**：Stripe secret/webhook secret 仅服务端读取，绝不进 `/api/config` 响应或任何 `NEXT_PUBLIC_*`；无 key 配置时 checkout 与 webhook 端点均 **503**（fail-closed，不放行）。
- 账本写入走 Plan 2 `CreditService`（幂等：reference 幂等键）；kind 枚举 `topup|deduction|refund|adjustment|expire`（`expire` 由本计划 Task 4 实现）。
- 默认测试不得访问真实 Stripe（`PaymentProvider` 接口 + fake；handler 测试用 `testutil` 模式）。
- 端点全在 auth 组内，**除** Stripe webhook（公开 + 验签）。
- 代码注释英文；gofmt/go vet/显式检查 error；写查询走 sqlc。
- `make sqlc` / `make test` 在**仓库根**执行（无 `server/Makefile`）；`go test` 直接命令在 `server/` 模块内执行。

---

### Task 1: `aurora_subscription` 表 + sqlc 查询

**Files:**
- Create: `server/migrations/460_aurora_subscription.up.sql` / `.down.sql`
- Create: `server/migrations/461_aurora_subscription_user_idx.up.sql` / `.down.sql`
- Create: `server/migrations/462_aurora_subscription_status_period_idx.up.sql` / `.down.sql`
- Create: `server/pkg/db/queries/aurora_subscription.sql`
- Modify: `server/cmd/migrate/main.go`（两个索引注册 `concurrentIndexCleanups`）
- Modify: `server/pkg/db/queries/aurora.sql`（`CountGenerationsThisMonth`/`CountActiveGenerations`，Task 6 用）
- Modify: `server/pkg/db/queries/credit.sql`（`SumCreditLedgerInWindow`/`SumMonthlyGrantInWindow`/`ListMonthlyGrantRecipients`，Task 4 用）
- 自动生成：`make sqlc`

**Interfaces:**
- Produces：
  - 表 `aurora_subscription`：`id uuid PK`、`user_id uuid NOT NULL`、`tier text NOT NULL`（`free|creator|pro`）、`status text NOT NULL`（`active|past_due|canceled`）、`stripe_customer_id text`、`stripe_subscription_id text`、`current_period_end timestamptz`、`cancel_at_period_end bool NOT NULL DEFAULT false`、`created_at`、`updated_at`。唯一索引 `(user_id)`（一人一订阅，个人产品线）；索引 `(status, current_period_end)`（发放 cron 扫描）。
  - 查询：`UpsertAuroraSubscription`、`GetAuroraSubscriptionByUser`、`GetAuroraSubscriptionByStripeID`、`ListActiveSubscriptionsForGrant`、`CountGenerationsThisMonth`、`CountActiveGenerations`、`SumCreditLedgerInWindow`、`SumMonthlyGrantInWindow`、`ListMonthlyGrantRecipients`、`GetPersonalWorkspaceForUser`。

- [ ] **Step 1: 写 migration 文件**

`server/migrations/460_aurora_subscription.up.sql`：

```sql
-- Aurora personal subscription: one row per user (unique index in 461).
-- tier: free | creator | pro. status: active | past_due | canceled.
-- MVP semantics: cancelation stops future grants only; already-granted
-- credits stay spendable until their monthly window expires (Task 4).
CREATE TABLE IF NOT EXISTS aurora_subscription (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    tier TEXT NOT NULL,
    status TEXT NOT NULL,
    stripe_customer_id TEXT,
    stripe_subscription_id TEXT,
    current_period_end timestamptz,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
```

`server/migrations/460_aurora_subscription.down.sql`：

```sql
DROP TABLE IF EXISTS aurora_subscription;
```

`server/migrations/461_aurora_subscription_user_idx.up.sql`：

```sql
-- One personal subscription per user.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS aurora_subscription_user_idx
    ON aurora_subscription (user_id);
```

`server/migrations/461_aurora_subscription_user_idx.down.sql`：

```sql
DROP INDEX CONCURRENTLY IF EXISTS aurora_subscription_user_idx;
```

`server/migrations/462_aurora_subscription_status_period_idx.up.sql`：

```sql
-- Scan window for the monthly grant cron (Task 4).
CREATE INDEX CONCURRENTLY IF NOT EXISTS aurora_subscription_status_period_idx
    ON aurora_subscription (status, current_period_end);
```

`server/migrations/462_aurora_subscription_status_period_idx.down.sql`：

```sql
DROP INDEX CONCURRENTLY IF EXISTS aurora_subscription_status_period_idx;
```

- [ ] **Step 2: 注册 up 映射**

`server/cmd/migrate/main.go`：把 `aurora_subscription_user_idx` 与 `aurora_subscription_status_period_idx` 各加一条进 `concurrentIndexCleanups`（**只注册 up 映射**——down 文件仅 `DROP INDEX CONCURRENTLY`，注册进 `concurrentDownIndexCleanups` 会被 `TestConcurrentIndexCleanupsMatchTheirMigrations` 判为「有 cleanup hook 但该方向没有 CREATE CONCURRENTLY」而挂掉；down 映射只收 down 方向重建索引的 migration，见 `main.go:303-311`）。preMigrationHooks 由 up 注册自动派生，无需手加。

- [ ] **Step 3: 写 sqlc 查询文件**

`server/pkg/db/queries/aurora_subscription.sql`：

```sql
-- name: UpsertAuroraSubscription :one
INSERT INTO aurora_subscription (user_id, tier, status, stripe_customer_id, stripe_subscription_id, current_period_end, cancel_at_period_end)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id) DO UPDATE SET
    tier = EXCLUDED.tier,
    status = EXCLUDED.status,
    stripe_customer_id = COALESCE(EXCLUDED.stripe_customer_id, aurora_subscription.stripe_customer_id),
    stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, aurora_subscription.stripe_subscription_id),
    current_period_end = EXCLUDED.current_period_end,
    cancel_at_period_end = EXCLUDED.cancel_at_period_end,
    updated_at = now()
RETURNING id, user_id, tier, status, stripe_customer_id, stripe_subscription_id,
          current_period_end, cancel_at_period_end, created_at, updated_at;

-- name: GetAuroraSubscriptionByUser :one
SELECT id, user_id, tier, status, stripe_customer_id, stripe_subscription_id,
       current_period_end, cancel_at_period_end, created_at, updated_at
FROM aurora_subscription
WHERE user_id = $1;

-- name: GetAuroraSubscriptionByStripeID :one
SELECT id, user_id, tier, status, stripe_customer_id, stripe_subscription_id,
       current_period_end, cancel_at_period_end, created_at, updated_at
FROM aurora_subscription
WHERE stripe_subscription_id = $1;

-- name: ListActiveSubscriptionsForGrant :many
SELECT id, user_id, tier, status, stripe_customer_id, stripe_subscription_id,
       current_period_end, cancel_at_period_end, created_at, updated_at
FROM aurora_subscription
WHERE status = 'active' AND current_period_end > now()
ORDER BY user_id;
```

`server/pkg/db/queries/aurora.sql` 追加（Task 6 用）：

```sql
-- name: CountGenerationsThisMonth :one
SELECT count(*) FROM aurora_generation
WHERE user_id = $1 AND created_at >= date_trunc('month', now());

-- name: CountActiveGenerations :one
SELECT count(*) FROM aurora_generation g
JOIN agent_task_queue t ON t.id = g.task_id
WHERE g.user_id = $1
  AND t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred');
```

`server/pkg/db/queries/credit.sql` 追加（Task 4 用）：

```sql
-- name: SumCreditLedgerInWindow :one
SELECT coalesce(sum(amount_micro), 0)::bigint
FROM credit_ledger
WHERE user_id = sqlc.arg(user_id) AND kind = ANY(sqlc.arg(kinds)::text[])
  AND created_at >= sqlc.arg(from_ts) AND created_at < sqlc.arg(to_ts);

-- name: SumMonthlyGrantInWindow :one
-- Monthly "sub:" grants only — signup bonuses and topups never expire, so
-- the expiry phase must not sum them.
SELECT coalesce(sum(amount_micro), 0)::bigint
FROM credit_ledger
WHERE user_id = sqlc.arg(user_id) AND kind = 'adjustment'
  AND reference LIKE 'sub:%'
  AND created_at >= sqlc.arg(from_ts) AND created_at < sqlc.arg(to_ts);

-- name: ListMonthlyGrantRecipients :many
SELECT DISTINCT user_id FROM credit_ledger
WHERE kind = 'adjustment' AND reference LIKE 'sub:%'
  AND created_at >= sqlc.arg(from_ts) AND created_at < sqlc.arg(to_ts);
```

`server/pkg/db/queries/aurora.sql` 追加（Task 4/5 用，个人空间解析）：

```sql
-- name: GetPersonalWorkspaceForUser :one
SELECT w.id FROM workspace w
JOIN member m ON m.workspace_id = w.id
WHERE m.user_id = $1 AND m.role = 'owner'
ORDER BY w.created_at ASC
LIMIT 1;
```

- [ ] **Step 4: `make sqlc` + `make test` 验证**

Run: `make sqlc && make test`（仓库根；`make test` 先跑 `go run ./cmd/migrate up` 再跑全部 Go 测试）
Expected: 生成全部查询；`TestEveryConcurrentUpBuildHasCleanup` 与 `TestConcurrentIndexCleanupsMatchTheirMigrations` 通过；Go 测试全绿。

- [ ] **Step 5: Commit**

```bash
git add server/migrations/460_* server/migrations/461_* server/migrations/462_* server/pkg/db/queries/ server/pkg/db/generated/ server/cmd/migrate/main.go
git commit -m "feat(aurora): add subscription table and settlement queries"
```

---

### Task 2: Stripe 集成底座（config + `PaymentProvider` + 验签）

**Files:**
- Modify: `server/internal/handler/config.go`（Stripe env 读取，**不进 `/api/config` 响应**）
- Create: `server/internal/aurora/stripe.go`
- Create: `server/internal/aurora/stripe_test.go`
- Modify: `server/go.mod` / `go.sum`（stripe-go 依赖）

**Interfaces:**
- Consumes: `handler.Config`（新增 `StripeSecretKey`/`StripeWebhookSecret`/`AuroraStripePrice*` 字段）。
- Produces（Task 3/4 依赖）：
  - `aurora.PaymentProvider` 接口（见 Step 1 代码）。
  - `aurora.NewStripeProvider(secretKey, webhookSecret string) *StripeProvider`（任一 key 为空时返回 `nil`，handler 侧以 nil 判定 503 fail-closed）。
  - `(*StripeProvider).ConstructEvent(payload []byte, sigHeader string) (aurora.Event, error)` —— 用 stripe-go `webhook.ConstructEvent` 验签，`Event` 含 `ID`（Stripe 事件 id，topup 幂等键）。

- [ ] **Step 1: 写接口与配置读取**

`server/internal/aurora/stripe.go`：

```go
package aurora

import (
	"context"
	"time"

	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/webhook"
)

// Event is the minimal stripe event shape handlers depend on. ID is the
// Stripe event id — the idempotency key for topup grants. Raw carries the
// type-specific object JSON (checkout session or subscription).
type Event struct {
	ID   string
	Type string
	Raw  []byte
}

// PaymentProvider is the narrow Stripe surface Aurora needs. Handlers depend
// on this interface; tests use a fake. The real implementation wraps stripe-go.
type PaymentProvider interface {
	CreateSubscriptionCheckout(ctx context.Context, priceID, successURL, cancelURL, userID, tier string) (url string, err error)
	CreateTopupCheckout(ctx context.Context, priceID, successURL, cancelURL, userID, topupID string) (url string, err error)
	GetSubscriptionPeriodEnd(ctx context.Context, subscriptionID string) (time.Time, error)
	ConstructEvent(payload []byte, sigHeader string) (Event, error)
}

// StripeProvider implements PaymentProvider via stripe-go. ConstructEvent
// verifies the webhook signature before anything else touches the payload.
type StripeProvider struct {
	secret        string
	webhookSecret string
}

// NewStripeProvider returns nil unless both keys are configured — handlers
// treat a nil provider as "payments disabled" and fail closed with 503.
func NewStripeProvider(secretKey, webhookSecret string) *StripeProvider {
	if secretKey == "" || webhookSecret == "" {
		return nil
	}
	return &StripeProvider{secret: secretKey, webhookSecret: webhookSecret}
}

func (p *StripeProvider) CreateSubscriptionCheckout(ctx context.Context, priceID, successURL, cancelURL, userID, tier string) (string, error) {
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems:  []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(priceID), Quantity: stripe.Int64(1)}},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		Metadata:   map[string]string{"userId": userID, "tier": tier},
	}
	s, err := session.New(params)
	if err != nil {
		return "", err
	}
	return s.URL, nil
}

func (p *StripeProvider) CreateTopupCheckout(ctx context.Context, priceID, successURL, cancelURL, userID, topupID string) (string, error) {
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems:  []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(priceID), Quantity: stripe.Int64(1)}},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		Metadata:   map[string]string{"userId": userID, "topupId": topupID},
	}
	s, err := session.New(params)
	if err != nil {
		return "", err
	}
	return s.URL, nil
}

func (p *StripeProvider) GetSubscriptionPeriodEnd(ctx context.Context, subscriptionID string) (time.Time, error) {
	sub, err := sub.Get(subscriptionID, nil)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sub.CurrentPeriodEnd, 0), nil
}

func (p *StripeProvider) ConstructEvent(payload []byte, sigHeader string) (Event, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, p.webhookSecret)
	if err != nil {
		return Event{}, err
	}
	return Event{ID: event.ID, Type: string(event.Type), Raw: event.Data.Raw}, nil
}
```

> stripe-go 的包路径/API 以 `go get github.com/stripe/stripe-go@latest` 拉到的当前 major 为准（上面 `v84` 为占位示例，实现时对齐实际 major 与构造参数名）；`session`/`sub` 是包内 import 别名（`checkout/session`、`subscription` 包）。

`server/internal/handler/config.go` 追加 env 读取（与现有 `DISABLE_WORKSPACE_CREATION` 同模式）：

```go
	StripeSecretKey    string // STRIPE_SECRET_KEY — server-side only, never surfaced via /api/config
	StripeWebhookSecret string // STRIPE_WEBHOOK_SECRET
	// AuroraStripePrice* map tier/topup to Stripe price IDs (test/live differ).
	AuroraStripePriceCreatorMonthly string // AURORA_STRIPE_PRICE_CREATOR_MONTHLY
	AuroraStripePriceCreatorYearly   string // AURORA_STRIPE_PRICE_CREATOR_YEARLY
	AuroraStripePriceProMonthly      string // AURORA_STRIPE_PRICE_PRO_MONTHLY
	AuroraStripePriceProYearly       string // AURORA_STRIPE_PRICE_PRO_YEARLY
	AuroraStripePriceTopup5          string // AURORA_STRIPE_PRICE_TOPUP_5
	AuroraStripePriceTopup20         string // AURORA_STRIPE_PRICE_TOPUP_20
```

> 实现时核对 `/api/config` handler 的字段列表，**确保上述 secret 不被序列化**（若 config handler 是显式字段映射则天然安全；若是整体序列化则需改映射）。

- [ ] **Step 2: 写测试**

`server/internal/aurora/stripe_test.go`（package `aurora_test`）：

```go
func TestNewStripeProviderRequiresBothSecrets(t *testing.T) {
	if aurora.NewStripeProvider("", "whsec") != nil {
		t.Fatal("provider with empty secret key should be nil")
	}
	if aurora.NewStripeProvider("sk_test", "") != nil {
		t.Fatal("provider with empty webhook secret should be nil")
	}
	if aurora.NewStripeProvider("sk_test", "whsec") == nil {
		t.Fatal("provider with both secrets should exist")
	}
}

func TestConstructEventRejectsBadSignature(t *testing.T) {
	p := aurora.NewStripeProvider("sk_test", "whsec_test")
	if _, err := p.ConstructEvent([]byte(`{"type":"checkout.session.completed"}`), "t=1,v1=deadbeef"); err == nil {
		t.Fatal("ConstructEvent with invalid signature should fail")
	}
}
```

（真实签名验证的往返测试用 stripe-go `webhook.ComputeSignature` 构造合法头部；不做真实网络调用。）

- [ ] **Step 3: 跑测试确认失败→实现→通过**

Run: `cd server && go test ./internal/aurora/ -run TestConstructEvent`
Expected: 编译失败 → 实现后 PASS。

- [ ] **Step 4: Commit**

```bash
git add server/internal/aurora/stripe.go server/internal/aurora/stripe_test.go server/internal/handler/config.go server/go.mod server/go.sum
git commit -m "feat(aurora): stripe provider with webhook signature verification"
```

---

### Task 3: 档位目录 + checkout 端点 + webhook 记账

**Files:**
- Create: `server/internal/aurora/tiers.go`
- Create: `server/internal/aurora/tiers_test.go`
- Modify: `server/internal/handler/handler.go`（`Handler` 加 `Payments aurora.PaymentProvider`、`Tiers *aurora.TierCatalog`；`New` 内从 `cfg` 构造——key 缺省时 `Payments=nil`，端点 fail-closed 503）
- Modify: `server/internal/handler/aurora.go`（checkout/webhook/subscription 端点）
- Modify: `server/internal/handler/aurora_test.go`
- Modify: `server/cmd/server/router.go`（checkout/subscription 挂 auth 组；webhook 挂公开组）

**Interfaces:**
- Consumes: Task 1 的订阅查询、Task 2 的 `PaymentProvider`、Plan 2 的 `h.Credit`。
- Produces：
  - `aurora.TierCatalog`：三档 + topup 两档的常量定义（credits、`GenerationsPerMonth`、并发从产品决策表）。
  - `POST /api/aurora/billing/checkout` `{tier:"creator", billingCycle:"monthly", successUrl, cancelUrl}` → `200 {checkoutUrl}`；已有 **active/past_due** 订阅 → `409`（canceled 可重新订阅；换档二期）。
  - `POST /api/aurora/billing/topup/checkout` `{topupId:"t5", successUrl, cancelUrl}` → `200 {checkoutUrl}`。
  - `POST /api/aurora/billing/stripe/webhook`（**公开路由**）→ 验签 → 事件路由（见 Step 4）→ `200`。
  - `GET /api/aurora/billing/subscription`（Task 6 扩展用量字段）。

- [ ] **Step 1: 写 tiers 失败测试**

`server/internal/aurora/tiers_test.go`（package `aurora_test`）：

```go
func TestTierCatalog(t *testing.T) {
	tc := aurora.NewTierCatalog("", "", "", "", "", "") // price ids empty in unit tests
	if len(tc.Tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(tc.Tiers))
	}
	creator, ok := tc.Lookup("creator")
	if !ok {
		t.Fatal("creator tier missing")
	}
	if creator.MonthlyCredits != 3000 {
		t.Fatalf("creator monthly credits = %d, want 3000", creator.MonthlyCredits)
	}
	if creator.MonthlyCreditsMicro() != 3000*1_000_000 {
		t.Fatal("creator micro conversion wrong")
	}
	if creator.GenerationsPerMonth != 30 {
		t.Fatalf("creator generations/month = %d, want 30", creator.GenerationsPerMonth)
	}
	pro, _ := tc.Lookup("pro")
	if pro.Concurrency != 5 {
		t.Fatalf("pro concurrency = %d, want 5", pro.Concurrency)
	}
	free, _ := tc.Lookup("free")
	if free.MonthlyCredits != 200 || free.Concurrency != 1 || free.GenerationsPerMonth != 10 {
		t.Fatalf("free tier wrong: %+v", free)
	}
	if len(tc.Topups) != 2 {
		t.Fatalf("expected 2 topup tiers, got %d", len(tc.Topups))
	}
	if _, ok := tc.LookupTopup("t5"); !ok {
		t.Fatal("topup t5 missing")
	}
	if _, ok := tc.LookupTopup("t20"); !ok {
		t.Fatal("topup t20 missing")
	}
}
```

- [ ] **Step 2: 实现 tiers**

`server/internal/aurora/tiers.go`：

```go
package aurora

// Tier is one personal subscription tier. MonthlyCredits is granted per
// natural month (free tier lazily on first generation, Task 6) and the
// unused remainder expires at the end of that month (Task 4).
// GenerationsPerMonth and Concurrency are the local entitlement gates
// (Task 6); all values are product-configurable constants.
type Tier struct {
	Tier               string
	MonthlyCredits     int64 // credits, 1 credit = 1e6 micro
	GenerationsPerMonth int
	Concurrency        int
	StripePriceMonthly string
	StripePriceYearly  string
}

func (t Tier) MonthlyCreditsMicro() int64 { return t.MonthlyCredits * 1_000_000 }

// Topup is a one-time credit purchase (never expires).
type Topup struct {
	ID      string // t5 | t20
	Credits int64
	PriceID string
}

// TierCatalog is the static product catalog. Price IDs come from env because
// they differ between Stripe test and live modes; empty price IDs make the
// corresponding checkout fail-closed (503) rather than charge a wrong price.
type TierCatalog struct {
	Tiers  []Tier // free, creator, pro — fixed order
	Topups []Topup
}

func NewTierCatalog(creatorMonthly, creatorYearly, proMonthly, proYearly, topup5, topup20 string) *TierCatalog {
	return &TierCatalog{
		Tiers: []Tier{
			{Tier: "free", MonthlyCredits: 200, GenerationsPerMonth: 10, Concurrency: 1},
			{Tier: "creator", MonthlyCredits: 3000, GenerationsPerMonth: 30, Concurrency: 2, StripePriceMonthly: creatorMonthly, StripePriceYearly: creatorYearly},
			{Tier: "pro", MonthlyCredits: 12000, GenerationsPerMonth: 100, Concurrency: 5, StripePriceMonthly: proMonthly, StripePriceYearly: proYearly},
		},
		Topups: []Topup{
			{ID: "t5", Credits: 5000, PriceID: topup5},
			{ID: "t20", Credits: 20000, PriceID: topup20},
		},
	}
}

func (c *TierCatalog) Lookup(tier string) (Tier, bool) {
	for _, t := range c.Tiers {
		if t.Tier == tier {
			return t, true
		}
	}
	return Tier{}, false
}

func (c *TierCatalog) LookupTopup(id string) (Topup, bool) {
	for _, t := range c.Topups {
		if t.ID == id {
			return t, true
		}
	}
	return Topup{}, false
}

// FreeMonthlyMicro is the free tier's monthly grant (used by the lazy grant
// in Task 6).
func (c *TierCatalog) FreeMonthlyMicro() int64 {
	t, _ := c.Lookup("free")
	return t.MonthlyCreditsMicro()
}
```

- [ ] **Step 3: 写 checkout/webhook 失败测试**

`server/internal/handler/aurora_test.go` 追加（fake provider 注入 `testHandler.Payments`——在测试里替换为 fake；`testHandler.Tiers` 用空 price id 的目录）：

```go
type fakePayments struct {
	checkoutURL string
	periodEnd   time.Time
	events      []aurora.Event
}

func (f *fakePayments) CreateSubscriptionCheckout(ctx context.Context, priceID, successURL, cancelURL, userID, tier string) (string, error) {
	return f.checkoutURL, nil
}
func (f *fakePayments) CreateTopupCheckout(ctx context.Context, priceID, successURL, cancelURL, userID, topupID string) (string, error) {
	return f.checkoutURL, nil
}
func (f *fakePayments) GetSubscriptionPeriodEnd(ctx context.Context, subscriptionID string) (time.Time, error) {
	return f.periodEnd, nil
}
func (f *fakePayments) ConstructEvent(payload []byte, sigHeader string) (aurora.Event, error) {
	// Return the first queued event; the handler test controls the queue.
	e := f.events[0]
	f.events = f.events[1:]
	return e, nil
}

func TestCreateSubscriptionCheckout(t *testing.T) {
	fp := &fakePayments{checkoutURL: "https://checkout.stripe.com/c/test"}
	old := testHandler.Payments
	testHandler.Payments = fp
	t.Cleanup(func() { testHandler.Payments = old })

	req := newRequest(http.MethodPost, "/api/aurora/billing/checkout", map[string]string{
		"tier": "creator", "billingCycle": "monthly",
		"successUrl": "https://aurora.example.com/ws-1/billing?checkout=success",
		"cancelUrl":  "https://aurora.example.com/ws-1/billing?checkout=cancel",
	})
	out := testutil.Decode[struct {
		CheckoutURL string `json:"checkoutUrl"`
	}](t, testHandler.CreateAuroraCheckout, req, http.StatusOK)
	if out.CheckoutURL != fp.checkoutURL {
		t.Fatalf("checkoutUrl = %q, want %q", out.CheckoutURL, fp.checkoutURL)
	}
}

func TestCreateSubscriptionCheckoutRequiresReturnURLs(t *testing.T) {
	// Missing successUrl/cancelUrl or non-http(s) URL → 400.
}

func TestCreateSubscriptionCheckoutRejectsActiveSubscription(t *testing.T) {
	// dbfx.Insert aurora_subscription row (status active) for testUserID.
	// POST checkout → 409.
}

func TestCreateSubscriptionCheckoutAllowsResubscribeAfterCancel(t *testing.T) {
	// dbfx.Insert aurora_subscription row (status canceled) for testUserID.
	// POST checkout → 200 (upsert will overwrite the canceled row).
}

func TestStripeWebhookSubscriptionCompleted(t *testing.T) {
	// Event: checkout.session.completed, mode=subscription, metadata
	// userId=testUserID, tier=creator; subscription period end = now+30d.
	// Assert: aurora_subscription row exists (active, creator) + balance
	// increased by 3000*1e6 micro (kind=adjustment, reference="sub:<userID>:<YYYY-MM>").
}

func TestStripeWebhookTopupCompleted(t *testing.T) {
	// Event: checkout.session.completed, mode=payment, metadata
	// userId=testUserID, topupId=t5.
	// Assert: balance +5000*1e6 (kind=topup, reference=event id, replay does
	// not double-credit).
}
```

- [ ] **Step 4: 实现 handler + 路由**

`server/internal/handler/aurora.go` 追加（依赖 `h.Credit`、`h.Payments`、`h.Tiers`）：

```go
func (h *Handler) CreateAuroraCheckout(w http.ResponseWriter, r *http.Request) {
	if h.Payments == nil {
		writeError(w, http.StatusServiceUnavailable, "payments not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Tier         string `json:"tier"`
		BillingCycle string `json:"billingCycle"`
		SuccessURL   string `json:"successUrl"`
		CancelURL    string `json:"cancelUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The client owns the return URLs (it knows its own origin and the
	// workspace slug route /<slug>/billing); the API host must not be used.
	if !validCheckoutURL(req.SuccessURL) || !validCheckoutURL(req.CancelURL) {
		writeError(w, http.StatusBadRequest, "successUrl and cancelUrl must be http(s) URLs")
		return
	}
	tier, ok := h.Tiers.Lookup(req.Tier)
	if !ok || tier.Tier == "free" {
		writeError(w, http.StatusBadRequest, "unknown tier")
		return
	}
	priceID := tier.StripePriceMonthly
	if req.BillingCycle == "yearly" {
		priceID = tier.StripePriceYearly
	}
	if priceID == "" {
		writeError(w, http.StatusServiceUnavailable, "tier price not configured")
		return
	}
	// MVP: no plan switching while an active (or past-due) subscription
	// exists; canceled users may subscribe again (upsert overwrites).
	sub, err := h.Queries.GetAuroraSubscriptionByUser(r.Context(), parseUUID(userID))
	if err == nil && (sub.Status == "active" || sub.Status == "past_due") {
		writeError(w, http.StatusConflict, "subscription already exists")
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return
	}
	url, err := h.Payments.CreateSubscriptionCheckout(r.Context(), priceID,
		req.SuccessURL, req.CancelURL, userID, tier.Tier)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create checkout")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkoutUrl": url})
}

// validCheckoutURL accepts http(s) absolute URLs only — the only shapes the
// frontend will pass; anything else (javascript:, relative, empty) is rejected.
func validCheckoutURL(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func (h *Handler) CreateAuroraTopupCheckout(w http.ResponseWriter, r *http.Request) {
	// Same pattern: requireUserID + body {topupId, successUrl, cancelUrl} +
	// LookupTopup validation + priceID empty→503 + CreateTopupCheckout.
}

func (h *Handler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	if h.Payments == nil {
		writeError(w, http.StatusServiceUnavailable, "payments not configured")
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	event, err := h.Payments.ConstructEvent(payload, r.Header.Get("Stripe-Signature"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}
	if err := h.handleStripeEvent(r.Context(), event); err != nil {
		slog.Error("stripe webhook handling failed", "type", event.Type, "error", err)
		writeError(w, http.StatusInternalServerError, "webhook failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": true})
}
```

`handleStripeEvent`（幂等：reference 键保证重放安全；事件对象经 `event.Raw` 反序列化到 stripe-go 类型——字段名以实际 major 生成为准；**metadata 中的 userId 是请求边界输入，用 `util.ParseUUID` 安全解析，失败即 ack 丢弃**，不用会 panic 的 `parseUUID`）：

```go
// handleStripeEvent routes a verified Stripe event to the right ledger
// write. Every write is idempotent: subscription upserts by user_id, grants
// by month/event reference keys. Invalid metadata is logged and acked (200)
// so Stripe does not retry a permanently-bad event.
func (h *Handler) handleStripeEvent(ctx context.Context, ev aurora.Event) error {
	switch ev.Type {
	case string(stripe.EventTypeCheckoutSessionCompleted):
		var sess checkout.Session
		if err := json.Unmarshal(ev.Raw, &sess); err != nil {
			return err
		}
		userUUID, err := util.ParseUUID(sess.Metadata["userId"])
		if err != nil {
			slog.Error("stripe event with invalid userId metadata", "event_id", ev.ID)
			return nil // ack: retrying cannot fix bad metadata
		}
		if sess.Mode == string(stripe.CheckoutSessionModePayment) {
			topup, ok := h.Tiers.LookupTopup(sess.Metadata["topupId"])
			if !ok {
				slog.Error("stripe topup event with unknown topupId", "event_id", ev.ID)
				return nil
			}
			wsID, err := h.Queries.GetPersonalWorkspaceForUser(ctx, userUUID)
			if err != nil {
				return err
			}
			// Topups never expire; keyed by the Stripe event id.
			return h.Credit.Grant(ctx, userUUID, wsID, topup.Credits*1_000_000, aurora.LedgerKindTopup, ev.ID)
		}
		// Subscription checkout: persist the row, then grant the first
		// month immediately (reference "sub:<userID>:<YYYY-MM>").
		tier, ok := h.Tiers.Lookup(sess.Metadata["tier"])
		if !ok || tier.Tier == "free" {
			slog.Error("stripe subscription event with unknown tier", "event_id", ev.ID)
			return nil
		}
		periodEnd, err := h.Payments.GetSubscriptionPeriodEnd(ctx, sess.Subscription.ID)
		if err != nil {
			return err
		}
		if _, err := h.Queries.UpsertAuroraSubscription(ctx, db.UpsertAuroraSubscriptionParams{
			UserID: userUUID, Tier: tier.Tier, Status: "active",
			StripeCustomerID:     ptrToText(&sess.Customer.ID),
			StripeSubscriptionID: ptrToText(&sess.Subscription.ID),
			CurrentPeriodEnd:     periodEnd,
			CancelAtPeriodEnd:    false,
		}); err != nil {
			return err
		}
		wsID, err := h.Queries.GetPersonalWorkspaceForUser(ctx, userUUID)
		if err != nil {
			return err
		}
		month := time.Now().UTC().Format("2006-01")
		return h.Credit.Grant(ctx, userUUID, wsID, tier.MonthlyCreditsMicro(), aurora.LedgerKindAdjustment,
			"sub:"+uuidToString(userUUID)+":"+month)
	case string(stripe.EventTypeCustomerSubscriptionUpdated):
		// Map Stripe status → row: active→active, past_due/unpaid→past_due,
		// canceled→canceled; update current_period_end and
		// cancel_at_period_end. Find the row via GetAuroraSubscriptionByStripeID.
	case string(stripe.EventTypeCustomerSubscriptionDeleted):
		// status='canceled'; already-granted credits stay spendable until
		// their monthly window expires (no clawback).
	default:
		// Unknown event types are ignored.
	}
	return nil
}
```

路由（`server/cmd/server/router.go`）：checkout/topup-checkout/subscription 挂 auth 组（Plan 1 分组）；**webhook 挂公开组**（`/api/config` 同级，`router.go:1392` 附近）：

```go
r.Post("/api/aurora/billing/stripe/webhook", h.StripeWebhook)
```

> `UpsertAuroraSubscriptionParams.CurrentPeriodEnd` 类型以 sqlc 生成为准（`timestamptz` 列 → `pgtype.Timestamptz` 或 `time.Time`，按生成结果对齐）。

- [ ] **Step 5: 跑测试 + Commit**

Run: `cd server && go test ./internal/handler/ -run 'TestCreateSubscriptionCheckout|TestStripeWebhook'`
Expected: PASS。

```bash
git add server/internal/aurora/tiers.go server/internal/aurora/tiers_test.go server/internal/handler/handler.go server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): subscription checkout and stripe webhook billing"
```

---

### Task 4: 月额度发放 + 按月过期（月结算循环）

**Files:**
- Modify: `server/internal/aurora/credit.go`（加 `LedgerKindExpire` 常量与 `Expire` 方法）
- Create: `server/internal/aurora/settlement.go`
- Create: `server/internal/aurora/settlement_test.go`
- Create: `server/cmd/server/aurora_settlement.go`（启动 goroutine，`runtime_sweeper.go` 同模式）

**Interfaces:**
- Consumes: Task 1 的 `ListActiveSubscriptionsForGrant`/`ListMonthlyGrantRecipients`/`SumCreditLedgerInWindow`/`SumMonthlyGrantInWindow`/`GetPersonalWorkspaceForUser`、Plan 2 `CreditService`。
- Produces：
  - `aurora.LedgerKindExpire = "expire"`
  - `(*CreditService).Expire(ctx, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error` —— 扣减过期额度，kind=`expire`，幂等键 `"expire:"+reference`。
  - `aurora.RunMonthlySettlement(ctx, q *db.Queries, credit *CreditService, tiers *TierCatalog, now time.Time) error` —— 幂等；**每次 tick 与启动时都跑**（不只在 1 号），错过 1 号自动补跑；单用户失败跳过不阻塞其余。

**过期语义（自然月窗口，简化法）**：发放发生在订阅/续订当月（webhook 立即发首月）或 Free 档惰性发放（Task 6），reference 均为 `sub:<userID>:<YYYY-MM>`。过期 = `max(0, 上月 sub 发放额 + 上月净消费)`，其中净消费 = 上月 `kind IN (deduction, refund)` 的 `amount_micro` 之和（deduction 为负、refund 为正，SUM 直接可得净值）。**发放额只统计 `reference LIKE 'sub:%'` 的行**——注册赠送（`signup:`）与充值（topup kind）永不过期，不得计入。消费默认先抵扣「当月过期额度」；跨月退款进入当月净值、补跑延迟（错过 1 号后用户已消费导致余额不足）时 Expire 失败被跳过——两者记录为已知偏差。

- [ ] **Step 1: 扩展 CreditService 失败测试**

`server/internal/aurora/credit_test.go` 追加：

```go
func TestExpireIsIdempotent(t *testing.T) {
	svc, pool := newTestCreditService(t)
	user := newUUID(t, pool)
	ws := newUUID(t, pool)

	if err := svc.Grant(context.Background(), user, ws, 1000, aurora.LedgerKindAdjustment, "sub-u-2026-09"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := svc.Expire(context.Background(), user, ws, 400, "u:2026-09"); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if err := svc.Expire(context.Background(), user, ws, 400, "u:2026-09"); err != nil {
		t.Fatalf("Expire retry: %v", err)
	}
	bal, _ := svc.Balance(context.Background(), user)
	if bal != 600 {
		t.Fatalf("balance after idempotent expire = %d, want 600", bal)
	}
}
```

- [ ] **Step 2: 实现 `Expire`**

`server/internal/aurora/credit.go` 追加（`adjust` 复用；负数即扣除）：

```go
// LedgerKindExpire is the "expire" kind from the cloud wallet contract: the
// unused remainder of a monthly grant, deducted at month end.
const LedgerKindExpire = "expire"

// Expire deducts the unused remainder of a monthly grant, recording an
// "expire" ledger row keyed by "expire:"+reference (reference =
// "<userID>:<YYYY-MM>"). Idempotent like every other ledger write.
func (s *CreditService) Expire(ctx context.Context, userID, workspaceID pgtype.UUID, amountMicro int64, reference string) error {
	return s.adjust(ctx, userID, workspaceID, -amountMicro, LedgerKindExpire, "expire:"+reference, reference)
}
```

- [ ] **Step 3: 写 settlement 失败测试**

`server/internal/aurora/settlement_test.go`（package `aurora_test`）：

```go
func TestRunMonthlySettlementExpiresAndGrants(t *testing.T) {
	// Seed: last month the user got a 1000-micro "sub:" grant and spent 600
	// (deduction). This month an active creator subscription row exists.
	// Run RunMonthlySettlement with now = the 1st of this month.
	// Assert: last month's unused 400 expired (kind=expire); this month
	// granted 3000*1e6 (kind=adjustment, reference contains this month).
}

func TestRunMonthlySettlementCatchUpAfterMissedFirst(t *testing.T) {
	// Run with now = the 2nd of the month: expiry and grant still happen
	// (the window is the previous month; idempotent references make any
	// later rerun a no-op).
}

func TestRunMonthlySettlementDoesNotExpireSignupBonusOrTopup(t *testing.T) {
	// Seed: last month the user got a 500-micro "signup:" bonus and a 2000
	// topup (kind=topup) and spent nothing. Run settlement.
	// Assert: no expire row; balance unchanged by settlement.
}

func TestRunMonthlySettlementSkipsFailingUser(t *testing.T) {
	// Seed: two users with "sub:" grants last month; one has no
	// owner-role workspace (GetPersonalWorkspaceForUser → ErrNoRows).
	// Run settlement. Assert: the healthy user is expired/granted, the
	// broken one is skipped, and no error aborts the run.
}

func TestRunMonthlySettlementIdempotent(t *testing.T) {
	// Run twice with the same now; second run changes no balances or rows.
}
```

- [ ] **Step 4: 实现 settlement**

`server/internal/aurora/settlement.go`：

```go
package aurora

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uuidToStr renders a pgtype.UUID for reference keys. pgx v5's UUID type has
// no String() method; MarshalText yields the canonical hyphenated form.
func uuidToStr(u pgtype.UUID) string {
	b, err := u.MarshalText()
	if err != nil {
		return ""
	}
	return string(b)
}

// RunMonthlySettlement performs the natural-month credit settlement. Call it
// on every tick and once at startup — not just on the 1st: every ledger
// write is idempotent via its month-scoped reference, so reruns are no-ops
// and a missed 1st-of-month tick self-heals on the next tick. Per-user
// failures are logged and skipped so one broken user cannot block the rest.
//
// Phase 1 (expire): for every user who received a "sub:" grant last month,
// the unused remainder expires: expire = max(0, lastGrant + lastNetSpend),
// where lastNetSpend = SUM(amount_micro) over last month's deduction (neg)
// and refund (pos) rows. Signup bonuses and topups never expire and are
// excluded (SumMonthlyGrantInWindow only sums reference LIKE 'sub:%').
//
// Phase 2 (grant): every active subscription (status=active,
// current_period_end > now) receives its tier's monthly credits as an
// "adjustment" with reference "sub:<userID>:<YYYY-MM>". Free-tier users get
// the same reference lazily on their first generation of the month
// (Task 6), so their grant expires the same way.
func RunMonthlySettlement(ctx context.Context, q *db.Queries, credit *CreditService, tiers *TierCatalog, now time.Time) error {
	lastStart := now.UTC().AddDate(0, -1, 0)
	lastMonth := lastStart.Format("2006-01")

	// Phase 1: expire unused monthly grants.
	recipients, err := q.ListMonthlyGrantRecipients(ctx, db.ListMonthlyGrantRecipientsParams{
		FromTs: lastStart, ToTs: now.UTC(),
	})
	if err != nil {
		return err
	}
	for _, userID := range recipients {
		if err := expireUserMonth(ctx, q, credit, userID, lastStart, now.UTC(), lastMonth); err != nil {
			// Known deviation: if the tick was missed and the user already
			// spent below the grant amount, Expire returns
			// ErrInsufficientCredits and we skip — the leftover stays (never
			// over-deduct, never block other users).
			slog.Warn("aurora monthly expiry skipped for user", "user_id", uuidToStr(userID), "error", err)
		}
	}

	// Phase 2: grant current-month credits to active subscribers.
	subs, err := q.ListActiveSubscriptionsForGrant(ctx)
	if err != nil {
		return err
	}
	thisMonth := now.UTC().Format("2006-01")
	for _, sub := range subs {
		tier, ok := tiers.Lookup(sub.Tier)
		if !ok || tier.MonthlyCreditsMicro() <= 0 {
			continue
		}
		wsID, err := q.GetPersonalWorkspaceForUser(ctx, sub.UserID)
		if err != nil {
			slog.Warn("aurora monthly grant skipped for user", "user_id", uuidToStr(sub.UserID), "error", err)
			continue
		}
		if err := credit.Grant(ctx, sub.UserID, wsID, tier.MonthlyCreditsMicro(), LedgerKindAdjustment,
			fmt.Sprintf("sub:%s:%s", uuidToStr(sub.UserID), thisMonth)); err != nil {
			slog.Warn("aurora monthly grant skipped for user", "user_id", uuidToStr(sub.UserID), "error", err)
		}
	}
	return nil
}

// expireUserMonth expires one user's unused monthly grant for lastMonth.
func expireUserMonth(ctx context.Context, q *db.Queries, credit *CreditService, userID pgtype.UUID, from, to time.Time, lastMonth string) error {
	wsID, err := q.GetPersonalWorkspaceForUser(ctx, userID)
	if err != nil {
		return err
	}
	granted, err := q.SumMonthlyGrantInWindow(ctx, db.SumMonthlyGrantInWindowParams{
		UserID: userID, FromTs: from, ToTs: to,
	})
	if err != nil {
		return err
	}
	netSpend, err := q.SumCreditLedgerInWindow(ctx, db.SumCreditLedgerInWindowParams{
		UserID: userID, Kinds: []string{"deduction", "refund"}, FromTs: from, ToTs: to,
	})
	if err != nil {
		return err
	}
	expireAmt := granted + netSpend
	if expireAmt <= 0 {
		return nil
	}
	return credit.Expire(ctx, userID, wsID, expireAmt, fmt.Sprintf("%s:%s", uuidToStr(userID), lastMonth))
}
```

> `SumMonthlyGrantInWindowParams`/`SumCreditLedgerInWindowParams` 字段名（预期 `UserID`/`Kinds`/`FromTs`/`ToTs`）以 `make sqlc` 生成为准（Task 1 Step 4）。

`server/cmd/server/aurora_settlement.go`（`runtime_sweeper.go` 同模式）：

```go
// startAuroraSettlement runs the monthly credit settlement loop. It runs
// once at startup and then daily; every run is idempotent via month-scoped
// ledger references, so a missed tick or a restart across the 1st
// self-heals on the next run.
func startAuroraSettlement(ctx context.Context, queries *db.Queries, credit *aurora.CreditService, tiers *aurora.TierCatalog) {
	run := func() {
		if err := aurora.RunMonthlySettlement(ctx, queries, credit, tiers, time.Now()); err != nil {
			slog.Error("aurora monthly settlement failed", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
```

（在 `server/cmd/server` 的 server 启动处调用 `startAuroraSettlement`；接入点以 `runtime_sweeper` 的启动位置为准。）

- [ ] **Step 5: 跑测试 + Commit**

Run: `cd server && go test ./internal/aurora/ -run 'TestExpire|TestRunMonthlySettlement'`
Expected: PASS。

```bash
git add server/internal/aurora/credit.go server/internal/aurora/settlement.go server/internal/aurora/settlement_test.go server/cmd/server/aurora_settlement.go
git commit -m "feat(aurora): monthly credit grant and expiry settlement"
```

---

### Task 5: 注册赠送 500 credits

**Files:**
- Modify: `server/internal/handler/auth.go`（`findOrCreateUser` 内）
- Modify: `server/internal/handler/auth_personal_workspace_test.go`

**Interfaces:**
- Consumes: Plan 2 `h.Credit`、Plan 1 `ensurePersonalWorkspace`、Task 1 的 `GetPersonalWorkspaceForUser`。
- Produces：注册（含 Google OAuth 首登）成功后一次性 `Grant(kind=adjustment, 500*1_000_000 micro, reference="signup:<userID>")`，幂等；瞬时失败 best-effort（下次登录幂等重试补发）。

- [ ] **Step 1: 写失败测试**

`server/internal/handler/auth_personal_workspace_test.go` 的 `TestFindOrCreateUserProvisionsPersonalWorkspace` 追加断言：

```go
	bal, err := testHandler.Credit.Balance(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if bal != 500*1_000_000 {
		t.Fatalf("signup bonus = %d, want %d", bal, 500*1_000_000)
	}

	// Second login (isNew=false): balance unchanged (idempotent).
	_, _, err = testHandler.findOrCreateUser(context.Background(), email)
	if err != nil {
		t.Fatalf("second findOrCreateUser: %v", err)
	}
	bal2, _ := testHandler.Credit.Balance(context.Background(), user.ID)
	if bal2 != bal {
		t.Fatalf("signup bonus duplicated on relogin: before=%d after=%d", bal, bal2)
	}
```

- [ ] **Step 2: 实现**

`server/internal/handler/auth.go` `findOrCreateUser` 内、`ensurePersonalWorkspace` 之后追加（与它并列 best-effort，幂等 reference 保证只发一次）：

```go
	// One-time signup bonus. Idempotent via the ledger idempotency key
	// ("grant:signup:<userID>"), so a transient failure self-heals on the
	// next login, same as personal-workspace provisioning above. Granted into
	// the user's personal workspace (provisioned just above).
	wsID, err := h.Queries.GetPersonalWorkspaceForUser(ctx, user.ID)
	if err != nil {
		slog.Warn("failed to resolve personal workspace for signup bonus", "error", err, "user_id", uuidToString(user.ID))
	} else if err := h.Credit.Grant(ctx, user.ID, wsID, 500*1_000_000, aurora.LedgerKindAdjustment, "signup:"+uuidToString(user.ID)); err != nil {
		slog.Warn("failed to grant aurora signup bonus", "error", err, "user_id", uuidToString(user.ID))
	}
```

- [ ] **Step 3: 跑测试 + Commit**

Run: `cd server && go test ./internal/handler/ -run TestFindOrCreateUserProvisionsPersonalWorkspace`
Expected: PASS。

```bash
git add server/internal/handler/auth.go server/internal/handler/auth_personal_workspace_test.go
git commit -m "feat(aurora): grant signup bonus credits on first login"
```

---

### Task 6: 本地权益门禁（月次数 / 并发）+ Free 档惰性发放

**Files:**
- Create: `server/internal/aurora/entitlement.go`
- Create: `server/internal/aurora/entitlement_test.go`
- Modify: `server/internal/handler/aurora.go`（`CreateAuroraGeneration` 消费点 + `ensureFreeMonthlyGrant`）
- Modify: `server/internal/handler/aurora_test.go`

**Interfaces:**
- Consumes: Task 1 的 `GetAuroraSubscriptionByUser`/`CountGenerationsThisMonth`/`CountActiveGenerations`、Task 3 的 `TierCatalog`。
- Produces：
  - `aurora.AuroraLimits{ Tier string; GenerationsPerMonth, Concurrency int }` —— 值来自 `TierCatalog`（`tiers.go` 是唯一改动点）。
  - `aurora.LimitsForUser(ctx, q, tiers, userID) (AuroraLimits, error)` —— 无订阅行或非 active → Free 档。
  - `CreateAuroraGeneration` 消费顺序：**limits 检查 → Free 档惰性月发放 →（Plan 3 Task 2 的插行/Reserve/入队）**：`CountGenerationsThisMonth >= GenerationsPerMonth` → `429`；`CountActiveGenerations >= Concurrency` → `429`。

- [ ] **Step 1: 写失败测试**

```go
func TestCreateAuroraGenerationRejectsOverMonthlyLimit(t *testing.T) {
	// Seed 10 generations this month for testUserID (free tier = 10).
	// POST → 429.
}

func TestCreateAuroraGenerationRejectsOverConcurrency(t *testing.T) {
	// Seed 1 queued generation + queued task row (free concurrency = 1).
	// POST → 429.
}

func TestCreateAuroraGenerationGrantsFreeMonthlyCreditsOnce(t *testing.T) {
	// Empty balance, no subscription row. POST → 201, balance = 200*1e6
	// (granted first) minus the reserve. POST again the same month →
	// balance does not gain another 200*1e6 grant (ledger idempotent).
}

func TestLimitsForUserDefaultsToFree(t *testing.T) {
	// No subscription row → free limits (generations 10 / concurrency 1);
	// canceled row → free limits too.
}
```

- [ ] **Step 2: 实现 + 跑测试 + Commit**

`server/internal/aurora/entitlement.go`：

```go
package aurora

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AuroraLimits are the local, self-host entitlement gates for a user. The
// cloud entitlement client stays out of this path: it fails open without a
// cloud URL (verified in entitlement/client.go), so local enforcement is the
// only gate that actually limits anything in a self-host deployment. All
// values come from the TierCatalog (tiers.go is the single edit point).
type AuroraLimits struct {
	Tier                string
	GenerationsPerMonth int
	Concurrency         int
}

// LimitsForUser resolves the user's subscription tier to its limits; users
// without an active subscription (no row, or status != active) get the free
// tier.
func LimitsForUser(ctx context.Context, q *db.Queries, tiers *TierCatalog, userID pgtype.UUID) (AuroraLimits, error) {
	activeTier := "free"
	sub, err := q.GetAuroraSubscriptionByUser(ctx, userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return AuroraLimits{}, err
	case sub.Status == "active":
		activeTier = sub.Tier
	}
	tier, ok := tiers.Lookup(activeTier)
	if !ok {
		tier, _ = tiers.Lookup("free")
	}
	return AuroraLimits{Tier: tier.Tier, GenerationsPerMonth: tier.GenerationsPerMonth, Concurrency: tier.Concurrency}, nil
}
```

`CreateAuroraGeneration` 在插行前追加（**顺序：limits → free 月发放 → Plan 3 Task 2 的插行/Reserve/入队**——free 发放必须先于 Reserve，否则新月份 Free 用户空余额直接 402）：

```go
	limits, err := aurora.LimitsForUser(r.Context(), h.Queries, h.Tiers, userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load limits")
		return
	}
	usedThisMonth, err := h.Queries.CountGenerationsThisMonth(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}
	if usedThisMonth >= int64(limits.GenerationsPerMonth) {
		writeError(w, http.StatusTooManyRequests, "monthly generation limit reached")
		return
	}
	active, err := h.Queries.CountActiveGenerations(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}
	if active >= int64(limits.Concurrency) {
		writeError(w, http.StatusTooManyRequests, "concurrency limit reached")
		return
	}
	if limits.Tier == "free" {
		if err := h.ensureFreeMonthlyGrant(r.Context(), userUUID); err != nil {
			// Best-effort: a failure here only delays the free grant; the
			// next attempt (or next login-triggered grant) retries via the
			// month-scoped idempotency key.
			slog.Warn("failed to grant free monthly credits", "error", err, "user_id", userID)
		}
	}
```

`server/internal/handler/aurora.go` 追加：

```go
// ensureFreeMonthlyGrant grants the free tier's monthly credits to a user
// without an active subscription. Idempotent per month via the ledger key
// ("sub:<userID>:<YYYY-MM>"), so the settlement expiry (Plan 5 Task 4)
// treats free grants exactly like paid ones.
func (h *Handler) ensureFreeMonthlyGrant(ctx context.Context, userID pgtype.UUID) error {
	wsID, err := h.Queries.GetPersonalWorkspaceForUser(ctx, userID)
	if err != nil {
		return err
	}
	month := time.Now().UTC().Format("2006-01")
	return h.Credit.Grant(ctx, userID, wsID, h.Tiers.FreeMonthlyMicro(), aurora.LedgerKindAdjustment,
		"sub:"+uuidToString(userID)+":"+month)
}
```

```bash
cd server && go test ./internal/handler/ -run 'TestCreateAuroraGenerationRejectsOver|TestCreateAuroraGenerationGrantsFree|TestLimitsForUser'
git add server/internal/aurora/entitlement.go server/internal/aurora/entitlement_test.go server/internal/handler/aurora.go server/internal/handler/aurora_test.go
git commit -m "feat(aurora): local entitlement gates and lazy free-tier grant"
```

---

### Task 7: 订阅/权益端点 + 前端订阅页

**Files:**
- Modify: `server/internal/handler/aurora.go`（`GET /api/aurora/billing/subscription` + `GET /api/aurora/billing/topups`）
- Modify: `server/internal/handler/aurora_test.go`
- Modify: `server/cmd/server/router.go`
- Modify: `packages/core/aurora/schema.ts` / `api.ts` / `queries.ts` / `mutations.ts`（Plan 4 文件扩充）
- Modify: `packages/views/aurora/billing.tsx`（订阅卡片 + 充值按钮替换占位）
- Create: `packages/views/aurora/billing.test.tsx`
- Modify: `packages/views/locales/{en,zh-Hans,ko,ja}/aurora.json` + `locales/index.ts`（四语补 subscription 相关 key，`parity.test.ts` 强制）

**Interfaces:**
- Produces：
  - `GET /api/aurora/billing/subscription` → `200 {"subscription":{"tier","status","currentPeriodEnd","cancelAtPeriodEnd","limits":{"generationsPerMonth","concurrency"},"usage":{"generationsUsedThisMonth","activeGenerations"}}}`（无订阅行返回 Free 档 + 空期日）。
  - `GET /api/aurora/billing/topups` → `200 {"topups":[{id,credits}]}`（不暴露 priceId）。
  - 前端 `useAuroraSubscription()`、`useAuroraTopups()`、`useCreateAuroraCheckout()`、`useCreateAuroraTopupCheckout()`；`billing.tsx` 组装订阅卡片（档位/状态/续期日/当月用量进度条）+ 订阅/充值按钮。
  - **query key 说明**：subscription/topups 端点是 **user-scoped**（非 workspace-scoped），query key 不含 wsId——Plan 4 的 wsId 规则只约束 workspace-scoped 查询，此处显式豁免并注释。
  - checkout 跳转：mutation 传客户端构造的 `successUrl`/`cancelUrl`（`${window.location.origin}/${slug}/billing?checkout=success|cancel`，apps/aurora 平台层允许 `window`）。

- [ ] **Step 1: 写 handler 失败测试**（无订阅 → Free + usage 计数；有 creator 订阅 → 对应 limits；topups 列表两条且无 priceId）
- [ ] **Step 2: 实现 handler + 路由 + 跑测试 + Commit**

```bash
cd server && go test ./internal/handler/ -run TestAuroraSubscription
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): subscription status and topup list endpoints"
```

- [ ] **Step 3: 前端 schema/hooks 失败测试**

`packages/core/aurora/schema.ts` 追加（Plan 4 模式，`parseWithFallback` 带 `{endpoint}`）：

```ts
export const auroraSubscriptionSchema = z.object({
  tier: z.string().default("free"),
  status: z.string().default(""),
  currentPeriodEnd: z.string().nullable().default(null),
  cancelAtPeriodEnd: z.boolean().default(false),
  limits: z.object({
    generationsPerMonth: z.number().default(10),
    concurrency: z.number().default(1),
  }).default({ generationsPerMonth: 10, concurrency: 1 }),
  usage: z.object({
    generationsUsedThisMonth: z.number().default(0),
    activeGenerations: z.number().default(0),
  }).default({ generationsUsedThisMonth: 0, activeGenerations: 0 }),
});
export const auroraSubscriptionResponseSchema = z.object({ subscription: auroraSubscriptionSchema });
export const auroraTopupsSchema = z.object({
  topups: z.array(z.object({ id: z.string(), credits: z.number() })).default([]),
});
export const auroraCheckoutResponseSchema = z.object({ checkoutUrl: z.string() });
```

malformed 测试补 fallback 用例（`types.test.ts`）。

- [ ] **Step 4: 前端 hooks + billing 视图 + i18n**

`queries.ts` 加 `useAuroraSubscription()`/`useAuroraTopups()`（user-scoped keys，注释说明豁免 wsId）；`mutations.ts` 加 `useCreateAuroraCheckout()`/`useCreateAuroraTopupCheckout()`（body 带 slug 路由的 successUrl/cancelUrl，成功后跳转 checkoutUrl）。`billing.tsx`：订阅卡片（tier 名称经四语 key、状态、续期日、`usage.generationsUsedThisMonth / limits.generationsPerMonth` 进度条）+ 「升级/订阅」与「充值」按钮（Free/无订阅显示档位表与价格 $0/$9.9/$29 及 topup $5/$20）；组件测试 mock hooks（Plan 4 模式）。四语 aurora.json 补 key（`billing.subscription.*`、`billing.topup.*`、`billing.checkout.*`），中文文案遵循 `apps/docs/content/docs/developers/conventions.mdx`。

- [ ] **Step 5: 跑测试 + Commit**

Run: `pnpm typecheck && pnpm test`（aurora 相关测试）+ `make test`（仓库根）

```bash
git add packages/core/aurora packages/views/aurora packages/views/locales
git commit -m "feat(aurora): subscription and topup UI with checkout"
```

---

## Deferred

- **换档/降级/取消自助**：Stripe Customer Portal（portal 链接二期）；MVP 语义 = 取消仅停止后续发放，已发额度保留至月末过期；canceled 后可重新订阅（本计划已支持）。
- **欠费停服**：`past_due` 只记录不拦截；停服二期。
- **发票/收据 UI**：Stripe dashboard 手动处理。
- **退款流程**：Stripe dashboard 手动退款 + 手动 ledger 冲正（二期做售后工作台）。
- **cloud entitlement `GateAurora*`**：本地门禁（Task 6）已满足 self-host MVP；接 cloud 时同一 PR 改 `entitlement/types.go` GateName + `normalizePolicy`（双 gate 硬校验，`client.go:278-299`）——Plan safety Deferred 已述。
- **FIFO 批次账本**：过期用自然月净消费简化法；批次（`purchase/bonus/adjustment`）精确分摊二期。
- **年付比例定价/首月按比例**：年付只是不同 price id；首月发放整月额度（窗口短），按比例二期。

## Self-Review

- **Spec 覆盖**：§6.1 订阅产品线（三档/月付年付/Stripe 本地）→ Task 1/3；§6.1 权益（月额度+上限）→ Task 4/6；§6.2 topup/月额度发放/过期 → Task 3/4/5（`expire` 交易由 Task 4 实现，Plan 2 预留注释落地）；§6.3 门禁 → Task 6（本地 enforcement，cloud 扩展 Deferred）；§9.1 #5 计费闭环 → Task 3/4/7；§8 前端充值入口 → Task 7。
- **占位符**：无 TBD；stripe-go 版本/字段名以 `go get @latest` 为准的诚实标注（与 Plan 1 同类）；档位数值全部集中在 `tiers.go`（含 `GenerationsPerMonth`）。
- **类型一致性**：`PaymentProvider` 签名在 Task 2 定义、Task 3 使用一致；`Event.ID` 在 Task 2 定义、Task 3 topup 幂等键使用一致；`LedgerKindExpire`/`Expire` 在 Task 4 Step 1/2 一致；`AuroraLimits` 字段在 Task 6 定义与 Task 7 前端 schema 对齐；reference 键格式（幂等键 `sub:`/`signup:`/`expire:` + 月份或事件 id；reference 列存 `sub:<userID>:<YYYY-MM>` / `signup:<userID>` / `<userID>:<YYYY-MM>` / 事件 id）在 Task 3/4/5 一致。
- **幂等与容错**：结算循环每次 tick 可重跑（月维度幂等键），错过 1 号自动补跑；单用户失败跳过不阻塞。

## 执行交接

Plan 5 完成后，spec §9.1 的「可对外销售」里程碑即可交付（安全计划 Plan safety 同步完成时）。实现顺序：Task 1 → 2 → 3 → 4 → 5 → 6 → 7；每任务 `make test` 全绿再进下一个。

## 修订记录（2026-09-13 PR #2 review 回写）

| # | 修正 |
|---|------|
| 1 | `aurora.Event` 补 `ID` 字段（topup 幂等键依赖） |
| 2 | 过期结算改为只统计 `reference LIKE 'sub:%'`（新增 `SumMonthlyGrantInWindow`），注册赠送/充值永不过期不被误扣 |
| 3 | 结算循环单用户失败改为跳过（slog + continue），不再中止整轮 |
| 4 | checkout 409 只对 active/past_due；canceled 可重新订阅（测试补 `AllowsResubscribeAfterCancel`） |
| 5 | Free 档 200/月改为**惰性发放**（当月首次创建 generation 时，幂等 sub: 键），并先于 Reserve 执行 |
| 6 | 结算循环不再只在 1 号执行：启动时 + 每次 tick 都跑（幂等），错过 1 号自动补跑 |
| 7 | checkout 回跳地址改由客户端传入（`successUrl`/`cancelUrl` 含 workspace slug 路由），后端只校验 http(s)，不再用 API host 拼 URL |
| 8 | fail-closed 统一 503（checkout 与 webhook）；删除声明未用的 `ErrPaymentsDisabled` 与「sentinel」表述 |
| 9 | sqlc 参数名注释对齐 `FromTs`/`ToTs`；多参数查询调用改 Params 结构体 |
| 10 | reference 键生成改用 `uuidToStr`（MarshalText），不再 `fmt.Sprintf("%s", pgtype.UUID)` |
| 11 | 明确 reference 格式契约：`sub:`/`signup:` 前缀与 expire 无前缀行；Plan 2 注释与 Plan 4 billing 映射按 kind 兜底（同步修改） |
| 12 | spec §6.3 / Plan safety 措辞同步：本地门禁为本里程碑方案，cloud `GateAurora*` 待接云（同步修改） |
| 13 | Plan 4 契约/composer 补 429 语义（同步修改） |
| 14 | `GenerationsPerMonth` 移入 `TierCatalog`（`tiers.go` 是唯一改动点），`entitlement.go` 从 tier 读值 |
| 15 | Plan 2 的「expire 二期」表述更新为「由 Plan 5 Task 4 实现」（同步修改） |
| 16 | 各文档「后续 Plan 5/尚未编写」陈旧措辞同步为已编写（同步修改） |
| 17 | 并发索引只注册 `concurrentIndexCleanups`（down 仅 DROP，注册 down-map 挂测试）；`make sqlc`/`make test` 改仓库根执行（无 server/Makefile）（Plan 1/2/3.5 同步修改） |
