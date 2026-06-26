# Explicit resources + random names

A variant of the perf config that uses **50 explicitly-written resource blocks**
instead of `count`. Each `aws_ssm_parameter` gets a distinct name from its own
`random_pet` resource.

This differs from the parent `../main.tf` (which uses `count = N` on a single
resource) in two ways:

- **Distinct addresses, not instances.** The graph has 50 separate
  `aws_ssm_parameter.pN` nodes (plus 50 `random_pet.pN`), rather than one
  resource with 50 instances. Total: **100 resources**.
- **A second provider + a dependency edge.** Each SSM parameter depends on its
  `random_pet`, so this isn't a pure flat list — there are 50 little
  `random_pet → aws_ssm_parameter` chains, and the refresh touches both
  providers (the `random` reads are local no-ops; the `aws` reads hit the API).

## Files

| File           | Purpose                                                      |
| -------------- | ------------------------------------------------------------ |
| `providers.tf` | aws + random provider requirements.                          |
| `gen.py`       | Regenerate `resources.tf` with N pairs: `python3 gen.py 50`. |
| `resources.tf` | The generated explicit resource blocks (checked in).         |

## Run it (from the parent dir, reusing the built binary)

```bash
cd testing/refresh-artifact-perf            # parent has the built ./terraform-dev
TF="$PWD/terraform-dev"; SUB="$PWD/explicit-random"
export AWS_PROFILE=default

$TF -chdir="$SUB" init
$TF -chdir="$SUB" apply -auto-approve                       # create 100 resources

time $TF -chdir="$SUB" plan                                 # live refresh (slow)
$TF -chdir="$SUB" plan -refresh-out="$SUB/objects.json"     # capture artifact (one-time)
time $TF -chdir="$SUB" plan -with-refresh="$SUB/objects.json"  # cached (fast)

$TF -chdir="$SUB" destroy -auto-approve                     # clean up
```

Change the count with `python3 gen.py 200 && $TF -chdir="$SUB" apply -auto-approve`.
