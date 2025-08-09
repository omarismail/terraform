terraform {
  integration "policy_validator" {
    source = "../../sample-integrations/policy-validator/index.js"
  }
}

# Policy Rules:
# 1. random_integer: max value must be <= 10
# 2. random_string: length must be between 8 and 32
# 3. random_password: must include special characters

# COMPLIANT RESOURCES

resource "random_integer" "valid_small" {
  min = 1
  max = 10  # ✅ Compliant: max = 10
}

resource "random_integer" "valid_range" {
  min = 5
  max = 8   # ✅ Compliant: max < 10
}

resource "random_string" "valid_length" {
  length  = 16  # ✅ Compliant: 8 <= 16 <= 32
  special = false
}

resource "random_password" "valid_secure" {
  length  = 20
  special = true  # ✅ Compliant: includes special chars
}

# NON-COMPLIANT RESOURCES (uncomment to test policy failures)

resource "random_integer" "invalid_large" {
  min = 1
  max = 100  # ❌ Violates: max > 10
}

# resource "random_integer" "invalid_medium" {
#   min = 10
#   max = 20   # ❌ Violates: max > 10
# }

# resource "random_string" "too_short" {
#   length  = 5   # ❌ Violates: length < 8
#   special = false
# }

# resource "random_string" "too_long" {
#   length  = 64  # ❌ Violates: length > 32
#   special = false
# }

# resource "random_password" "weak_password" {
#   length  = 12
#   special = false  # ❌ Violates: no special characters
# }

# Test resource without policies (should pass)
resource "terraform_data" "no_policy" {
  input = "This resource type has no policies"
}

output "compliant_number" {
  value = random_integer.valid_small.result
}

output "compliant_string" {
  value = random_string.valid_length.result
}