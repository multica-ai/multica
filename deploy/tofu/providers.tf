terraform {
  required_version = ">= 1.7"

  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "~> 1.235"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

# Credentials and region are picked up from environment:
#   ALICLOUD_ACCESS_KEY / ALICLOUD_SECRET_KEY / ALICLOUD_REGION
# which your ~/.zshrc already exports. Do not hard-code keys in this file.
provider "alicloud" {
  region = var.region
}
