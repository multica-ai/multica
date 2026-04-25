# multica-bot Kubernetes manifests

These manifests are a deployable template for the `multica-bot` namespace.

## Secret handling

Do not commit real credentials into `k8s/bot/secret.yaml`. The checked-in file is an `envsubst` template that should be rendered from a local `.env.bot` file:

```bash
set -a
source .env.bot
set +a
envsubst < k8s/bot/secret.yaml | kubectl apply -f -
```

The following variables are expected by the template:

- `JWT_SECRET`
- `POSTGRES_PASSWORD`
- `DATABASE_URL`
- `RESEND_API_KEY`
- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`
- `DINGTALK_CLIENT_ID`
- `DINGTALK_CLIENT_SECRET`
- `DINGTALK_ROBOT_CODE`
- `DINGTALK_TOKEN_ENCRYPTION_KEY`

`DINGTALK_TOKEN_ENCRYPTION_KEY` is optional in application code, but it should be set in production so linked DingTalk tokens are not encrypted with the JWT signing secret.

## Apply order

```bash
kubectl apply -f k8s/bot/namespace.yaml
kubectl apply -f k8s/bot/postgres.yaml
kubectl apply -f k8s/bot/backend.yaml
kubectl apply -f k8s/bot/frontend.yaml
kubectl apply -f k8s/bot/ingress.yaml
```

## DingTalk phase1 smoke check

After rollout, verify the phase1 path end to end:

1. Open `Settings -> Notifications` and connect a DingTalk account.
2. Confirm an `external_account_binding` row is created with `provider = dingtalk` and `status = active`.
3. Enable the `mentioned` DingTalk preference.
4. Mention that user from another account in an issue comment.
5. Confirm a `notification_delivery` row moves from `pending` to `sent`.
