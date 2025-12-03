terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.24"
    }
  }
}

provider "aws" {
  region = "us-west-1"
}

runbook "list_action_invoke" {
    step "listEc2Instances" {
        list "aws_instance" "example" {
            provider = aws
        }


        action "aws_ec2_stop_instance" "stop_instance" {
            for_each = list.aws_instance.example
            config {
                instance_id = each.value.identity.id
            }
        }
        invoke {
            actions = [action.aws_ec2_stop_instance.stop_instance]
        }
        output "ec2_instance_id" {
            for_each = list.aws_instance.example
            value = each.value.identity.id
        }
        output "ec2_instance_name" {
            for_each = list.aws_instance.example
            value = each.value.display_name
        }
    }
}