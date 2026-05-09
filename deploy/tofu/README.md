# OpenTofu: cloud resources for Multica on Aliyun

Provisions the **RDS PostgreSQL** database for the Multica ACK deployment.
The ALB is managed from K8s via `AlbConfig` (see `deploy/k8s/base/albconfig.yaml`)
so listener/cert changes can ship alongside Ingress rule changes without a
Tofu round-trip. Everything else (ACK cluster, ACR, VPC, SSL cert, OSS state
bucket) is expected to already exist — this module consumes them.

## Requirements

- OpenTofu 1.7+ (`brew install opentofu`)
- Aliyun credentials exported as env:
  ```
  export ALICLOUD_ACCESS_KEY=...
  export ALICLOUD_SECRET_KEY=...
  export ALICLOUD_REGION=cn-shanghai
  ```
  (Already in `~/.zshrc` in this workstation.)
- State bucket bootstrapped once: `aliyun oss mb oss://lilith-tofu-state` +
  versioning enabled. See `backend.tf`.

## Inputs

Create `terraform.tfvars` (gitignored) with at least:

```hcl
# Defaults in variables.tf already cover env=prod and vpc_id=vpc-uf6650upzfmnylrydbhnk.
# Override only what you need to change, e.g.:
# env = "staging"
# rds_instance_class = "pg.n4.medium.2c"
```

Optional overrides live in `variables.tf` (instance class, storage, PG version,
ALB edition).

## Apply

```bash
cd deploy/tofu
tofu init
tofu plan -out=tofu.plan
tofu apply tofu.plan
```

Typical first-time apply creates:

| Resource | Purpose |
|---|---|
| `alicloud_db_instance.multica` | RDS PG 17 in the ACK VPC, Prepaid/包年包月 |
| `alicloud_db_account.app` | App-level DB user (password from var.rds_account_password, default `Lilith@123` — rotate before prod) |
| `alicloud_db_database.multica` | `multica` database inside the RDS instance |

## Hand-off to Kubernetes

Once the apply succeeds, wire the outputs into the K8s deploy:

```bash
# Grab the DATABASE_URL (sensitive output; never echo to shared terminal)
tofu output -raw database_url | pbcopy   # macOS

# Fill AlbConfig zoneMappings with 2 vSwitches in DIFFERENT AZs
tofu output -json vswitch_zones | jq   # pick two ids in different zones
# Paste them into deploy/k8s/base/albconfig.yaml, alongside the SSL cert id.

# DNS: after `kubectl apply`, the controller provisions the ALB; read its
# address off the Ingress status and CNAME ship.lilithgames.com at it:
kubectl -n multica get ingress multica -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

Then apply the K8s layer:

```bash
kubectl create namespace multica
# fill the Secret with real values — see deploy/k8s/README.md
kubectl apply -k deploy/k8s/overlays/prod
```

## What's intentionally NOT here

- **ACK cluster** — already exists, data-sourced by id
- **ACR** — already exists, images pushed with `docker push` in CI
- **VPC / vSwitches** — reused from the ACK cluster
- **SSL certificate** — upload to Aliyun SSL Certificate Service manually, copy cert-id into ingress.yaml
- **OSS object storage** — not provisioned; Multica can run without uploads in MVP
- **Daemon** — runs on-prem, not in the cluster

When the time comes to add any of these, drop a new `.tf` file and reference
from `outputs.tf` / the K8s manifests.

## Destroy

```bash
tofu destroy
```

Prepaid RDS instances cannot be terminated by Tofu until the subscription
period ends — Aliyun rejects the DELETE API call. Flip the instance to
Postpaid in the console first (有 24 小时冷却期), then destroy.

The ALB is not owned by this module; destroy it via `kubectl delete albconfig
multica` or, if it's been pinned, through the Aliyun console.
