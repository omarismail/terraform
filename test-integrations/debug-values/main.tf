terraform {
  integration "debug" {
    source = "./debug-integration.js"
  }
}

# Example 1: Resource with only configuration values
resource "terraform_data" "config_only" {
  input = {
    name = "test"
    cost = 100
    type = "web-server"
  }
}

# Example 2: Resource with computed values
resource "random_integer" "computed" {
  min = 1
  max = 10
}

# Example 3: Resource that depends on computed values
resource "terraform_data" "depends_on_computed" {
  input = {
    random_value = random_integer.computed.result  # This will be unknown during plan
    static_value = "known-value"
  }
}

# Example 4: Resource with mixed values
resource "random_string" "mixed" {
  length  = 16      # Configuration value (known)
  special = false   # Configuration value (known)
  # 'result' will be computed (unknown during plan)
}