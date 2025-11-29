variable "name" {
  
  type    = string
}

provider "aws" {
  region = "us-west-2"
}

runbook "hello" {
  locals {
    greeting = "Hello, ${var.name}!"
  }

  step "first" {
    output "message" {
      value = local.greeting
    }
  }
}
