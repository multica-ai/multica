# multica-bot Kubernetes manifests

These manifests are a deployable template for the `multica-bot` namespace.


## Release model

This directory contains the baseline Kubernetes manifests for the `multica-bot` namespace. Release/source-of-truth rules live in `.ci/deploy.md`; follow that file before changing image tags, env injection, or Jenkins-driven deployment behavior.


## Secret handling

`.env.bot` is the source of truth for the `multica-bot-secrets`
Kubernetes Secret. Do not maintain a second key list in Kubernetes manifests.
Sync the whole file through the repository script:

```bash
node k8s/bot/sync-env.mjs \
  --env-file .env.bot \
  --namespace multica-bot \
  --secret multica-bot-secrets
```

By default the script applies `multica-bot-secrets`, then restarts and waits for
`deployment/backend` and `deployment/frontend`. It logs only key names, key
counts, Secret apply status, and rollout results. Secret values must never be
printed in Jenkins or Agent logs.

Use `--dry-run` to validate the parsed key set without calling `kubectl`:

```bash
node k8s/bot/sync-env.mjs --env-file .env.bot --dry-run
```

`DINGTALK_TOKEN_ENCRYPTION_KEY` is optional in application code, but it should be set in production so linked DingTalk tokens are not encrypted with the JWT signing secret.

`GOOGLE_TOKEN_URL` and `GOOGLE_USERINFO_URL` are optional. Leave them empty to use Google's public endpoints directly, or set them to a trusted proxy when the backend cannot reach Google from the cluster network. The proxy must be reachable from the backend pod and must not require exposing `GOOGLE_CLIENT_SECRET` to the browser.

`S3_BUCKET` is optional. Leave it empty to use local upload storage, or set it with the remaining S3/OBS variables before syncing `multica-bot-secrets` to enable attachment direct uploads.

## Apply order

```bash
kubectl apply -f k8s/bot/namespace.yaml
node k8s/bot/sync-env.mjs --env-file .env.bot --no-rollout
kubectl apply -f k8s/bot/postgres.yaml
kubectl apply -f k8s/bot/backend.yaml
kubectl apply -f k8s/bot/frontend.yaml
kubectl apply -f k8s/bot/ingress.yaml
node k8s/bot/sync-env.mjs --env-file .env.bot
```

## DingTalk phase1 smoke check

After rollout, verify the phase1 path end to end:

1. Open `Settings -> Notifications` and connect a DingTalk account.
2. Confirm an `external_account_binding` row is created with `provider = dingtalk` and `status = active`.
3. Enable the `mentioned` DingTalk preference.
4. Mention that user from another account in an issue comment.
5. Confirm a `notification_delivery` row moves from `pending` to `sent`.
