# `multica-test` overlay — pre-prod environment

Purpose: CI pushes every successful build here automatically; a human then
manually promotes a passing image tag into `overlays/prod/` by editing
`prod/kustomization.yaml`'s `newTag` and re-applying.

| Concern | Prod | Test |
|---|---|---|
| Namespace | `multica` | `multica-test` |
| Host | `multica.lilithgames.com` | `multica-test.lilithgames.com` |
| ALB | `multica-prod` (deletion-protected) | `multica-test` (deletable) |
| IngressClass | `multica-alb` | `multica-test-alb` |
| ACL | `acl-ry04o6lyky6tmscnfu` | separate ACL, same CIDR content |
| Database | prod RDS / `multica` | test RDS / `multica` |
| Redis relay | none | dedicated Tair instance (`test_redis_url`) |
| Feishu OAuth app | `cli_a739addb39e41013` | same app, extra redirect URI |
| Image tag | pinned (e.g. `0.0.1`) | floating `:latest`, `imagePullPolicy: Always` |
| Replicas (server/web) | 1 / 3 | 1 / 1 |

## One-time prerequisites

These have to happen **before** the first `kubectl apply` — none of them are
automated by the kustomization.

1. **Apply `deploy/tofu/test.tf` first** so the test ACL / RDS / Tair exist:

   ```bash
   cd deploy/tofu
   tofu init
   tofu apply
   ```

   Paste the resulting ACL id into two spots:
   - `overlays/test/kustomization.yaml` → AlbConfig patch
     `/spec/listeners/1/aclConfig/aclRelations/0/aclId`
   - `overlays/test/ingress-patch.yaml` → `alb.ingress.kubernetes.io/acl-id`

2. **Whitelist the test Feishu redirect URI**. In the Lark app console
   (app `cli_a739addb39e41013`), add
   `https://multica-test.lilithgames.com/auth/feishu/callback` to the OAuth
   redirect URI list. Otherwise Feishu login on test 4xx's at the OAuth step.

3. **Create the namespace, then copy pull + TLS Secrets into it**. The base
   manifests reference `imagePullSecrets: regcred`, and the wildcard cert
   `*.lilithgames.com` covers `multica-test.lilithgames.com` but lives only
   in the prod namespace today:

   ```bash
   kubectl create namespace multica-test
   kubectl -n multica get secret regcred -o yaml \
     | sed 's/namespace: multica$/namespace: multica-test/' \
     | kubectl apply -f -
   kubectl -n multica get secret multica-tls -o yaml \
     | sed 's/namespace: multica$/namespace: multica-test/' \
     | kubectl apply -f -
   ```

4. **Create the test Secret out-of-band** — same shape as prod, but use the
   OpenTofu outputs for the test RDS / Tair endpoints:

   ```bash
   TEST_DATABASE_URL="$(tofu output -raw test_database_url)"
   TEST_REDIS_URL="$(tofu output -raw test_redis_url)"
   kubectl create secret generic multica-secrets --namespace=multica-test \
     --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
     --from-literal=DATABASE_URL="$TEST_DATABASE_URL" \
     --from-literal=REDIS_URL="$TEST_REDIS_URL" \
     --from-literal=FEISHU_APP_SECRET='same-as-prod' \
     --from-literal=RESEND_API_KEY=''
   ```

   `REDIS_URL` is what makes multi-pod WS fanout safe if you later raise the
   server replica count above 1.

## First deploy

```bash
# Dry-run to see everything that would be applied
kustomize build deploy/k8s/overlays/test | less

# Apply
kubectl apply -k deploy/k8s/overlays/test

# Wait for the migrate Job
kubectl -n multica-test wait --for=condition=complete job/multica-migrate --timeout=5m

# Read the test ALB hostname once the controller binds it
kubectl -n multica-test get ingress multica \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

5. **DNS** — CNAME `multica-test.lilithgames.com` at the test ALB hostname
   from the previous step.

## Routine rollouts

If your CI pushes `:latest` on every merge to `main`, you only need to
restart the workloads so `imagePullPolicy: Always` triggers a fresh pull:

```bash
kubectl -n multica-test rollout restart deployment/multica-server deployment/multica-web
kubectl -n multica-test delete job multica-migrate --ignore-not-found
kubectl apply -k deploy/k8s/overlays/test
```

If you'd rather pin a SHA per rollout (recommended once CI is wired):

```bash
TAG=main-$(git rev-parse --short HEAD)
cd deploy/k8s/overlays/test
kustomize edit set image \
  lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server=lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server:$TAG \
  lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web=lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web:$TAG
kubectl apply -k .
```

Then you can drop `server-deployment-patch.yaml` / `web-deployment-patch.yaml` /
`migrate-job-patch.yaml` from `kustomization.yaml` since pinned tags don't
need `imagePullPolicy: Always`.

## Promoting a tag to prod

After smoke-testing the test environment, promote the same image to prod by
hand:

```bash
TAG=<the tag you just verified on test>
cd deploy/k8s/overlays/prod
kustomize edit set image \
  lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server=lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-server:$TAG \
  lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web=lilith-registry.cn-shanghai.cr.aliyuncs.com/devops/multica-web:$TAG
git add -p && git commit -m "chore(deploy): promote $TAG to prod"
kubectl apply -k .
kubectl -n multica delete job multica-migrate --ignore-not-found
kubectl apply -k .
kubectl -n multica wait --for=condition=complete job/multica-migrate --timeout=5m
kubectl -n multica rollout status deployment/multica-server
kubectl -n multica rollout status deployment/multica-web
```

Committing the prod `newTag` bump leaves an audit trail of every promotion in
the git history — same pattern as the existing `chore(deploy): pin prod image
tag to X.Y.Z` commits.

## Teardown

`deletionProtectionEnabled: false` on the test AlbConfig lets you drop the
whole environment cleanly:

```bash
kubectl delete -k deploy/k8s/overlays/test
# Then delete the test ACL in Aliyun if you don't want to keep paying for it.
```

The wildcard TLS Secret and the test DB are left alone — you may want to
keep them for the next test stand-up.
