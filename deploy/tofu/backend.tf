# State backend.
#
# Keeping state local for now (sits in ./terraform.tfstate, gitignored).
# Flip to OSS-backed state once the lilith-tofu-state bucket is bootstrapped:
#
#   aliyun oss mb oss://lilith-tofu-state --region cn-shanghai
#   aliyun oss versioning --bucket lilith-tofu-state --status Enabled
#   # then uncomment the backend block below and:
#   tofu init -migrate-state
#
# OpenTofu 1.7+ also supports client-side state encryption — consider
# enabling alongside the OSS move, since state carries the RDS password.
#
# terraform {
#   backend "oss" {
#     bucket = "lilith-tofu-state"
#     prefix = "multica"
#     key    = "terraform.tfstate"
#     region = "cn-shanghai"
#   }
# }
