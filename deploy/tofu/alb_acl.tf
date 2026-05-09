# ALB ACL — fully IaC managed (resource + entries).
# The previous out-of-band ACL acl-0vcyku5jmg7b7r3l0q was deleted manually
# in the Aliyun console, leaving the ALB listener without any ACL (open).
# This file recreates the ACL via OpenTofu so future drift is detectable.
#
# After `tofu apply`, the ACL ID will be `alicloud_alb_acl.multica.id`.
# That value must be set on the ingress annotation:
#   alb.ingress.kubernetes.io/acl-id: <new id>
# (See deploy/k8s/base/ingress.yaml in this repo.)

resource "alicloud_alb_acl" "multica" {
  acl_name      = "multica-prod-whitelist"
  resource_group_id = null
  lifecycle {
    # The ACL is referenced by the live ALB listener via ingress annotation.
    # Prevent accidental deletion that would re-open the ALB to the world.
    prevent_destroy = true
  }
}

locals {
  whitelist_cidrs = [
    "182.150.57.127/32",
    "101.207.235.32/29",
    "211.137.105.144/32",
    "101.230.180.125/32",
    "101.230.180.64/26",
    "116.228.131.56/29",
    "116.228.131.80/29",
    "116.228.240.88/29",
    "116.228.240.96/29",
    "116.228.240.136/29",
    "103.84.136.78/32",
    "103.84.139.122/32",
    "103.84.137.95/32",
    "103.84.139.113/32",
    "103.84.136.53/32",
    "103.84.139.119/32",
    "103.84.136.77/32",
    "103.84.139.121/32",
    "103.84.136.65/32",
    "103.84.139.120/32",
    "101.230.180.192/26",
    "222.189.162.136/29",
    "103.4.78.220/32",
    "114.94.35.176/28",
    "114.94.35.160/28",
    "202.65.196.96/32",
    "202.65.196.97/32",
    "202.65.196.102/32",
    "202.65.196.103/32",
    "202.65.196.104/32",
    "202.65.196.101/32",
    "202.65.196.110/32",
    "202.65.196.105/32",
    "171.217.69.130/32",
    "171.217.69.131/32",
    "171.217.69.132/32",
    "103.4.78.214/32",
    "182.139.35.131/32",
    "103.4.78.215/32",
    "103.85.166.232/29",
    "103.4.78.218/32",
    "61.169.46.8/29",
    "103.4.78.219/32",
    "103.4.78.201/32",
    "103.4.78.203/32",
    "103.4.78.204/32",
    "129.227.74.162/32",
    "111.9.16.26/32",
    "8.132.161.22/32",
    "203.208.188.118/32",
    "8.132.161.17/32",
    "203.208.188.113/32",
    "171.223.213.136/29",
    "129.227.81.94/32",
    "129.227.88.34/32",
    "129.227.88.35/32",
    "129.227.88.36/32",
    "47.236.195.41/32",
    "47.236.192.151/32",
    "101.207.144.78/32",
    "101.230.180.67/32",
    "202.65.196.108/32",
    "116.228.240.140/32",
    "202.65.196.106/32",
    "116.228.131.86/32",
    "103.4.78.202/32",
    "182.150.118.37/32",
    "103.4.78.217/32",
    "162.128.229.143/32",
    "162.128.229.144/32",
    "103.4.78.205/32",
    "103.4.78.206/32",
    "103.4.78.193/32",
    "103.4.78.194/32",
    "103.4.78.197/32",
    "103.4.78.195/32",
    "203.208.188.115/32",
    "203.208.188.116/32",
    "203.208.188.117/32",
    "203.208.188.120/32",
    "203.208.188.122/32",
    "203.208.188.123/32",
    "203.208.188.124/32",
    "47.252.55.195/32",
    "47.252.55.196/32",
    # Meego (Feishu Project) webhook source IPs — added 2026-04-30
    # Used by /webhook/meego/* ingest endpoint on the bridge.
    # 78/79/80 came from advertised list; 49.7.49.5 captured from real
    # webhook hit (User-Agent: Go-http-client/1.1). Keep both — Meego may
    # rotate egress.
    "101.126.59.78/32",
    "101.126.59.79/32",
    "101.126.59.80/32",
    "49.7.49.5/32",
  ]
}

resource "alicloud_alb_acl_entry_attachment" "whitelist" {
  for_each = toset(local.whitelist_cidrs)

  acl_id      = alicloud_alb_acl.multica.id
  entry       = each.value
  description = "lilith-whitelist"
}

output "acl_id" {
  value       = alicloud_alb_acl.multica.id
  description = "Set this as alb.ingress.kubernetes.io/acl-id on the ingress."
}
