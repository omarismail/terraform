terraform {
  integration "logger" {
    source = "../../sample-integrations/logger/index.js"
  }
}

# Test resources to demonstrate comprehensive logging
resource "random_integer" "test" {
  min = 1
  max = 10
}

resource "random_string" "test" {
  length  = 8
  special = false
}

resource "terraform_data" "test" {
  input = {
    message = "Testing comprehensive logging"
    number  = random_integer.test.result
  }
}

# Test resource lifecycle - update triggers
resource "random_uuid" "test" {}

resource "time_sleep" "wait" {
  create_duration = "1s"
}

output "test_number" {
  value = random_integer.test.result
}

output "test_string" {
  value = random_string.test.result
}

output "test_uuid" {
  value = random_uuid.test.result
}