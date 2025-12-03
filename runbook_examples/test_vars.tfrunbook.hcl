
variable "name" {
  type    = string
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
