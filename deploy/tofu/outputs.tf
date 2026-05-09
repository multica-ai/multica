# Outputs meant to be piped into the K8s layer. Run:
#
#   tofu output -raw database_url | kubectl create secret generic multica-secrets \
#     --namespace=multica --from-file=DATABASE_URL=/dev/stdin
#
# Or consume structured outputs programmatically:
#
#   tofu output -json

output "rds_connection_string_vpc" {
  description = "PostgreSQL host:port reachable from within the cluster VPC."
  value       = "${alicloud_db_instance.multica.connection_string}:${alicloud_db_instance.multica.port}"
}

output "rds_account_name" {
  value = alicloud_db_account.app.account_name
}

output "rds_database_name" {
  value = alicloud_db_database.multica.data_base_name
}

output "database_url" {
  description = "Ready-to-paste DATABASE_URL for the multica-secrets Secret."
  value       = "postgres://${alicloud_db_account.app.account_name}:${var.rds_account_password}@${alicloud_db_instance.multica.connection_string}:${alicloud_db_instance.multica.port}/${alicloud_db_database.multica.data_base_name}?sslmode=require"
  sensitive   = true
}

output "vswitch_ids" {
  description = "vSwitch ids in the target VPC. Paste the first N into deploy/k8s/base/albconfig.yaml zoneMappings so the ALB Ingress Controller knows where to provision the ALB."
  value       = [for vsw in local.vswitches : vsw.id]
}

output "vswitch_zones" {
  description = "vSwitch id → zone id map, useful when the ALB needs 2+ AZs and you want to pick deliberately rather than let Kustomize pick the first two."
  value       = { for vsw in local.vswitches : vsw.id => vsw.zone_id }
}
