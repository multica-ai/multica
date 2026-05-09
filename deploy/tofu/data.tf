# Auto-discover every vSwitch in the given VPC when var.vswitch_ids is empty.
# ALB requires 2+ vSwitches in distinct AZs; the precondition on the ALB
# resource checks that invariant.
data "alicloud_vswitches" "vpc" {
  vpc_id = var.vpc_id
}

locals {
  vswitches = length(var.vswitch_ids) > 0 ? [
    for vsw in data.alicloud_vswitches.vpc.vswitches :
    vsw if contains(var.vswitch_ids, vsw.id)
  ] : data.alicloud_vswitches.vpc.vswitches

  # Centralize tags so every managed resource carries the same env/app labels.
  common_tags = {
    "app"         = "multica"
    "env"         = var.env
    "managed-by"  = "opentofu"
    "tofu-module" = "deploy/tofu"
  }
}
