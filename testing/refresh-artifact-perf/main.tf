terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-west-1"
}

variable "count_params" {
  type        = number
  description = "How many aws_ssm_parameter resources to manage."
  default     = 300
}

# Free-tier resources (SSM String parameters cost ~$0). What matters for the
# demo is that each one incurs a real provider ReadResource (refresh) call, so
# the more there are, the more obvious the cost of refreshing on every plan.
#
# This is intentionally a FLAT / WIDE graph: every instance is independent and
# sits at the top level (depth 1, no inter-resource dependencies). Scaling
# var.count_params makes the graph wider, never deeper — which isolates the
# cost of refresh (one ReadResource per instance) from any DAG depth effects.
resource "aws_ssm_parameter" "p" {
  count = var.count_params
  name  = "/tfperf/p${count.index}"
  type  = "String"
  value = "v${count.index}"
}
