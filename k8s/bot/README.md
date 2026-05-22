# multica-bot Kubernetes manifests

These manifests are a deployable template for the `multica-bot` namespace.


## Release model

This directory contains the baseline Kubernetes manifests for the `multica-bot` namespace. Release/source-of-truth rules live in `.ci/deploy.md`; follow that file before changing image tags, env injection, or Jenkins-driven deployment behavior.


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
- `RESEND_FROM_EMAIL`
- `SMTP_HOST`
- `SMTP_PORT`
- `SMTP_USERNAME`
- `SMTP_PASSWORD`
- `SMTP_SSL`
- `SMTP_FROM_NAME`
- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`
- `GOOGLE_TOKEN_URL`
- `GOOGLE_USERINFO_URL`
- `DINGTALK_CLIENT_ID`
- `DINGTALK_CLIENT_SECRET`
- `DINGTALK_ROBOT_CODE`
- `DINGTALK_TOKEN_ENCRYPTION_KEY`
- `GITEE_TOKEN` (optional — required for importing skills from private Gitee repos)
- `S3_BUCKET` (optional — enables cloud attachment storage and direct uploads when set)
- `S3_REGION`
- `S3_KEY_PREFIX`
- `S3_FORCE_PATH_STYLE`
- `AWS_ENDPOINT_URL`
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `CLOUDFRONT_KEY_PAIR_ID`
- `CLOUDFRONT_PRIVATE_KEY_SECRET`
- `CLOUDFRONT_PRIVATE_KEY`
- `CLOUDFRONT_DOMAIN`

`DINGTALK_TOKEN_ENCRYPTION_KEY` is optional in application code, but it should be set in production so linked DingTalk tokens are not encrypted with the JWT signing secret.

`GOOGLE_TOKEN_URL` and `GOOGLE_USERINFO_URL` are optional. Leave them empty to use Google's public endpoints directly, or set them to a trusted proxy when the backend cannot reach Google from the cluster network. The proxy must be reachable from the backend pod and must not require exposing `GOOGLE_CLIENT_SECRET` to the browser.

`S3_BUCKET` is optional. Leave it empty to use local upload storage, or set it with the remaining S3/OBS variables before rendering `secret.yaml` to enable attachment direct uploads.

## Apply order

```bash
kubectl apply -f k8s/bot/namespace.yaml
set -a && source .env.bot && set +a
envsubst < k8s/bot/secret.yaml | kubectl apply -f -
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
