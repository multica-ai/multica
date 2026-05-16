# Existing OSS bucket + least-privilege RAM credentials for Multica uploads.
#
# The bucket itself is intentionally not managed here because it was created
# out-of-band. This module only creates application credentials scoped to it.

locals {
  multica_oss_bucket_name          = var.oss_bucket_name
  multica_oss_s3_endpoint          = "https://s3.oss-${var.region}.aliyuncs.com"
  multica_oss_s3_internal_endpoint = "https://s3.oss-${var.region}-internal.aliyuncs.com"
}

data "alicloud_ram_policy_document" "multica_uploads" {
  version = "1"

  statement {
    effect = "Allow"
    action = [
      "oss:GetBucketInfo",
      "oss:GetBucketStat",
      "oss:ListObjects",
    ]
    resource = [
      "acs:oss:*:*:${local.multica_oss_bucket_name}",
    ]
  }

  statement {
    effect = "Allow"
    action = [
      "oss:PutObject",
      "oss:GetObject",
      "oss:DeleteObject",
    ]
    resource = [
      "acs:oss:*:*:${local.multica_oss_bucket_name}/*",
    ]
  }
}

resource "alicloud_ram_user" "multica_uploads" {
  name         = "multica-uploads"
  display_name = "multica uploads"
  comments     = "Multica server uploads and deletes objects in ${local.multica_oss_bucket_name}."
  force        = true
}

resource "alicloud_ram_policy" "multica_uploads" {
  policy_name     = "MulticaUploadsOSSAccess"
  description     = "Least-privilege OSS access for Multica uploads bucket ${local.multica_oss_bucket_name}."
  policy_document = data.alicloud_ram_policy_document.multica_uploads.document
  force           = true
}

resource "alicloud_ram_user_policy_attachment" "multica_uploads" {
  policy_name = alicloud_ram_policy.multica_uploads.policy_name
  policy_type = alicloud_ram_policy.multica_uploads.type
  user_name   = alicloud_ram_user.multica_uploads.name
}

resource "alicloud_ram_access_key" "multica_uploads" {
  user_name   = alicloud_ram_user.multica_uploads.name
  secret_file = "${path.module}/.secrets/multica-uploads-access-key.txt"
}
