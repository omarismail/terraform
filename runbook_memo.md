# Technical Memo: Terraform Runbooks – New Syntax and Behavior

## Overview

Terraform Runbooks introduce a new paradigm for **orchestrating provider-backed actions** through a declarative, step-based execution model. Runbooks are defined in `.tfrunbook.hcl` files and executed via the `terraform runbook` command. This feature enables users to compose multi-step operational workflows that can read data sources, execute provider actions, and produce outputs—all within a familiar HCL syntax.

---

## File Extension and Location

Runbook files use the extension `.tfrunbook.hcl` and are loaded from the current working directory. The system supports:
- **Specific file lookup**: `<runbook_name>.tfrunbook.hcl` is tried first
- **Fallback glob search**: All `*.tfrunbook.hcl` files are scanned if the named file doesn't exist

---

## HCL Syntax Structure

### Top-Level Blocks

A runbook file can contain the following top-level blocks:

| Block | Description |
|-------|-------------|
| `terraform {}` | Provider requirements (for `runbook init`) |
| `variable "<name>" {}` | Input variable declarations |
| `provider "<name>" {}` | Provider configuration |
| `runbook "<name>" {}` | Runbook definition (can define multiple per file) |

### Runbook Block Structure

```hcl
runbook "<name>" {
  locals {
    <name> = <expression>
    ...
  }

  step "<step_name>" {
    data "<type>" "<name>" {
      <config attributes>
    }

    action "<type>" "<name>" {
      config {
        <config attributes>
      }
    }

    invoke {
      actions = [<action_references>]
    }

    output "<name>" {
      description = "<optional description>"
      value       = <expression>
    }
  }
}
```

---

## Key Concepts

### 1. Variables

Variables follow standard Terraform variable syntax with support for `default` and `type`:

```hcl
variable "name" {
  type    = string
  default = "world"
}
```

Variables without defaults prompt for user input at runtime. Referenced via `var.<name>`.

### 2. Locals

Locals are defined within the `locals {}` block inside a runbook and support interpolation:

```hcl
locals {
  greeting = "Hello, ${var.name}!"
}
```

Referenced via `local.<name>`.

### 3. Steps

Steps execute sequentially and form the core workflow structure. Each step has a name label and can contain:
- **Data blocks**: Read-only provider data sources
- **Action blocks**: Provider actions to be invoked
- **Invoke blocks**: Execution control for actions
- **Output blocks**: Display values to the user

### 4. Data Sources

Data sources use standard Terraform data source syntax with provider-type prefix:

```hcl
step "read_file" {
  data "local_file" "foo" {
    filename = "foo.txt"
  }

  output "file_content" {
    value = data.local_file.foo.content
  }
}
```

Data sources are read dynamically and their results are added to the evaluation context as `data.<type>.<name>`.

### 5. Actions (New Provider Capability)

**Actions** are a new provider primitive that enables imperative operations. Unlike resources (which manage state) or data sources (which read data), actions are designed for **side-effect-driven tasks** that don't fit the declarative model.

#### Action Definition Syntax

```hcl
action "<provider>_<action_type>" "<name>" {
  config {
    <configuration attributes>
  }
}
```

The action type follows the pattern `<provider>_<action_name>` (e.g., `local_command`). Actions are:
1. **Declared** in the step but not immediately executed
2. **Referenced** via `action.<type>.<name>`
3. **Executed** via the `invoke {}` block

#### Action Reference Object

When declared, each action creates a reference object with:
- `type`: The action type string
- `name`: The action name string

### 6. Invoke Block

The `invoke {}` block controls which actions are executed and in what order:

```hcl
invoke {
  actions = [action.local_command.echo_hello, action.local_command.another]
}
```

- Actions in the list execute **sequentially** in order
- The list supports expressions (e.g., conditionals, for-expressions)
- Only actions referenced in `invoke.actions` are executed

---

## Provider Action Protocol

Actions use a new provider interface with three methods:

| Method | Purpose |
|--------|---------|
| `PlanAction` | Validates configuration, detects potential drift |
| `InvokeAction` | Executes the action, returns event stream |
| `ValidateActionConfig` | Validates action configuration schema |

### InvokeAction Events

Actions emit events during execution:
- `Progress`: Status updates (displayed as `"    Progress: <message>"`)
- `Completed`: Final status with optional diagnostics

---

## CLI Commands

### `terraform runbook init`

Initializes providers required by runbook files.

```bash
terraform runbook init [-upgrade]
```

- Parses all `.tfrunbook.hcl` files for `terraform { required_providers {} }` blocks
- Downloads and installs required providers
- Creates/updates `.terraform.lock.hcl`

### `terraform runbook <name>`

Executes the named runbook.

```bash
terraform runbook [-state=<path>] <runbook_name>
```

Output format:
```
Step 1: <step_name>
  Invoking action: <action_type>.<action_name>
    Progress: <message>
    Action completed successfully
<output_name> = <value>
```

---

## Available Functions

Runbooks support a subset of Terraform functions:

| Category | Functions |
|----------|-----------|
| **String** | `chomp`, `format`, `formatlist`, `join`, `lower`, `regex`, `regexall`, `replace`, `split`, `strrev`, `substr`, `title`, `trim`, `trimprefix`, `trimsuffix`, `trimspace`, `upper` |
| **Collection** | `coalesce`, `concat`, `contains`, `keys`, `length`, `lookup`, `merge`, `one`, `range`, `reverse`, `setintersection`, `setproduct`, `setsubtract`, `setunion`, `slice`, `sort`, `sum`, `transpose`, `values`, `zipmap` |
| **Encoding** | `base64decode`, `base64encode`, `base64gzip`, `csvdecode`, `jsondecode`, `jsonencode`, `urlencode` |
| **Type Conversion** | `tobool`, `tolist`, `tomap`, `tonumber`, `toset`, `tostring` |
| **Math** | `abs`, `ceil`, `floor`, `log`, `max`, `min`, `parseint`, `pow`, `signum` |
| **Date/Time** | `timeadd`, `timestamp` |
| **Hash** | `base64sha256`, `base64sha512`, `md5`, `sha1`, `sha256`, `sha512`, `uuid` |

---

## Complete Example

```hcl
terraform {
  required_providers {
    local = {
      source  = "hashicorp/local"
      version = "~> 2.0"
    }
  }
}

variable "script_args" {
  type    = list(string)
  default = ["arg1", "arg2"]
}

provider "local" {}

runbook "deploy_workflow" {
  locals {
    timestamp = timestamp()
  }

  step "read_config" {
    data "local_file" "config" {
      filename = "config.json"
    }

    output "config_content" {
      description = "Current configuration"
      value       = data.local_file.config.content
    }
  }

  step "execute_deployment" {
    action "local_command" "deploy" {
      config {
        command   = "bash"
        arguments = concat(["deploy.sh"], var.script_args)
        stdin     = jsonencode({
          "timestamp": local.timestamp
        })
      }
    }

    invoke {
      actions = [action.local_command.deploy]
    }
  }
}
```

---

## Key Behavioral Notes

1. **Sequential Execution**: Steps execute in declaration order; actions within an invoke block execute sequentially per the list order.

2. **Scoped Data Context**: Each step builds upon the previous evaluation context. Data sources read in step N are available in step N+1.

3. **No State Management**: Unlike `terraform apply`, runbooks do not manage infrastructure state. Actions are fire-and-forget operations.

4. **Provider Reuse**: Providers are instantiated per data source/action invocation. Provider configuration is currently empty (future enhancement area).

5. **Error Handling**: Execution halts on first error with diagnostic output.

---

## Summary

Terraform Runbooks extend Terraform's capabilities beyond declarative infrastructure management into **operational workflow orchestration**. The key innovations are:
- **Actions**: A new provider primitive for imperative operations
- **Invoke blocks**: Explicit execution control for actions
- **Step-based execution**: Sequential workflow stages with accumulated context
- **Standalone execution**: Runs independent of Terraform state

