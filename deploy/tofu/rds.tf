# Aliyun RDS PostgreSQL for Multica.
#
# Engine notes:
#   * pg_bigm (Chinese bigram FTS, used by migration 032) and pgvector are
#     preinstalled on Aliyun RDS PG — no extra extension install step.
#     Migrations run `CREATE EXTENSION IF NOT EXISTS ...` themselves.
#   * The instance is VPC-attached and placed in a vSwitch of the ACK cluster
#     VPC so pods can reach it without leaving the VPC.
#   * Charge type is Prepaid (包年包月) for predictable billing — adjust via
#     var.rds_period_months / var.rds_auto_renew.

resource "alicloud_db_instance" "multica" {
  engine           = "PostgreSQL"
  engine_version   = var.rds_engine_version
  instance_type    = var.rds_instance_class
  instance_storage = var.rds_storage_gb

  instance_charge_type = "Prepaid"
  period               = var.rds_period_months
  auto_renew           = var.rds_auto_renew
  auto_renew_period    = var.rds_period_months

  instance_name            = "multica-${var.env}"
  db_instance_storage_type = "cloud_essd"

  vswitch_id = local.vswitches[0].id
  # The instance gets a private endpoint reachable from any vSwitch in the VPC;
  # we only need to pick one here. The IP whitelist below controls access.

  # Client connections come from pods on ACK worker nodes. Add every vSwitch
  # CIDR in the VPC — Aliyun accepts IPs or CIDRs.
  security_ips = [for vsw in local.vswitches : vsw.cidr_block]

  tags = local.common_tags

  lifecycle {
    ignore_changes = [
      # Aliyun console sometimes mutates these after creation (maintenance
      # window etc.). Leave alone unless explicitly changed here.
      parameters,
    ]
  }
}

resource "alicloud_db_account" "app" {
  db_instance_id   = alicloud_db_instance.multica.id
  account_name     = var.rds_account_name
  account_password = var.rds_account_password
  account_type     = "Super" # PostgreSQL on Aliyun RDS uses Super for the primary app user.
}

resource "alicloud_db_database" "multica" {
  instance_id    = alicloud_db_instance.multica.id
  data_base_name = var.rds_database_name
  character_set  = "UTF8"

  # Account-level privilege binding happens via alicloud_db_account_privilege
  # for non-Super accounts. The Super account above already has full access.
  depends_on = [alicloud_db_account.app]
}
