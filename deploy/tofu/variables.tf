variable "region" {
  description = "Alibaba Cloud region. ACK lives in cn-shanghai today."
  type        = string
  default     = "cn-shanghai"
}

variable "env" {
  description = "Environment name (prod / staging). Used for tagging and resource name suffixes."
  type        = string
  default     = "prod"
}

variable "vpc_id" {
  description = <<EOT
VPC that holds the ACK cluster. RDS and ALB are created in the same VPC so
in-cluster pods can reach them over the private network without NAT.
EOT
  type        = string
  default     = "vpc-uf6650upzfmnylrydbhnk"
}

variable "vswitch_ids" {
  description = <<EOT
Explicit vSwitch ids to use for RDS placement and ALB zone mappings. Leave
empty to auto-discover every vSwitch in var.vpc_id; set explicitly if you
need to pin a subset (e.g. to control AZ choice for ALB).
EOT
  type        = list(string)
  default     = []
}

# ---------------------------------------------------------------------------
# RDS
# ---------------------------------------------------------------------------

variable "rds_instance_class" {
  description = <<EOT
Aliyun RDS PostgreSQL instance class. Default pg.n4.4c.2m (4c/8g HA) matches
prod's current state after a manual console scale-up from pg.n2.2c.2m on
~2026-05-13. The originally-imported value was pg.n2.2c.2m, then prod hit a
load spike during the multi-replica + Redis relay rollout and was bumped.
Keep this var in sync with `aliyun rds DescribeDBInstanceAttribute` for
prod, or the next `tofu apply` will silently propose a downgrade.
EOT
  type        = string
  default     = "pg.n4.4c.2m"
}

variable "rds_storage_gb" {
  description = <<EOT
RDS data disk size in GB. 200GB matches prod's current state after a manual
console expansion from 100GB on ~2026-05-13. Aliyun does not allow online
storage shrink — letting tofu propose 100GB here would yield an apply error,
not a silent revert, so this is a guard variable rather than a load-bearing
default. Bump in tfvars when prod is expanded again.
EOT
  type        = number
  default     = 200
}

variable "rds_engine_version" {
  description = "PostgreSQL major version. Prod runs 18.0; pg_bigm and pgvector are both preinstalled. Migrations target 17+."
  type        = string
  default     = "18.0"
}

variable "rds_database_name" {
  description = "Logical database name created inside the RDS instance."
  type        = string
  default     = "multica"
}

variable "rds_account_name" {
  description = "Application DB user. Granted owner privileges on the database created above."
  type        = string
  default     = "multica"
}

variable "rds_account_password" {
  description = <<EOT
RDS app user password. Aliyun requires 8-32 chars with upper + lower + digit
+ special. Rotate after first deploy; the value ends up in state and in the
K8s Secret. Mark sensitive so tofu won't echo it to stdout.
EOT
  type        = string
  sensitive   = true
  default     = "Lilith@123"
}

variable "rds_period_months" {
  description = "Subscription length in months. 1, 2, 3, 4, 5, 6, 7, 8, 9, 12, 24, 36, 60 are the valid Aliyun values."
  type        = number
  default     = 1
}

variable "redis_password" {
  description = <<EOT
Auth password shared between the test and prod Tair instances. Aliyun
requires 8-32 chars with at least 3 of {upper, lower, digit, special}.
Override via TF_VAR_redis_password or terraform.tfvars; the default below
is convenient for bootstrapping but anyone with VPC access can read it from
test's Secret.
EOT
  type        = string
  sensitive   = true
  default     = "Lilith@123"
}

variable "rds_auto_renew" {
  description = "Whether to auto-renew the prepaid instance when its period elapses."
  type        = bool
  default     = true
}

# ---------------------------------------------------------------------------
# OSS uploads
# ---------------------------------------------------------------------------

variable "oss_bucket_name" {
  description = "Existing OSS bucket used by Multica for shared uploads."
  type        = string
  default     = "lilith-multica"
}

variable "oss_public_domain" {
  description = "Public domain used by browsers to read uploaded objects. Leave empty to store endpoint URLs."
  type        = string
  default     = "multica-bucket.lilithgames.com"
}
