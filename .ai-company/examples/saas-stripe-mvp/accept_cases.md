# Acceptance Cases — saas-stripe-mvp

## Commands

```bash
pnpm typecheck
pnpm test
make test
```

## Functional

- [ ] AC-1: `/` 含 Pricing section（三档价格 **展示**，按钮无真实支付）
- [ ] AC-2: `/dashboard` 需 mock 登录态（环境变量 `DEMO_USER=1`）可进
- [ ] AC-3: `GET /v1/me` 返回固定 demo user JSON

## Forbidden paths check

- [ ] AC-F1: PR diff 不含 `payment/`、`billing/`、`stripe`

## CEO sign-off

- [ ] 确认无 Stripe 密钥进 repo
