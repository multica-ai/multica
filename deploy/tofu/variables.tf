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
  type    = list(string)
  default = []
}

# ---------------------------------------------------------------------------
# RDS
# ---------------------------------------------------------------------------

variable "rds_instance_class" {
  description = <<EOT
Aliyun RDS PostgreSQL instance class. Defaults to pg.n4.large.2c (4c/32g HA)
sized for ~500 concurrent users. Downsize to pg.n2.small.2c for POC/dev.
EOT
  type        = string
  default     = "pg.n4.large.2c"
}

variable "rds_storage_gb" {
  description = "RDS data disk size in GB. 500GB covers ~6-12 months of task_message growth for 500 users."
  type        = number
  default     = 500
}

variable "rds_engine_version" {
  description = "PostgreSQL major version. Multica's migrations target 17; pg_bigm and pgvector are both preinstalled."
  type        = string
  default     = "17.0"
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

variable "rds_auto_renew" {
  description = "Whether to auto-renew the prepaid instance when its period elapses."
  type        = bool
  default     = true
}
