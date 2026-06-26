#!/usr/bin/env bash
#
# One-command demo of the refresh-artifact feature.
#
#   ./demo.sh [COUNT]
#
# COUNT = number of aws_ssm_parameter resources to manage (default 300).
# Bigger COUNT = longer full refresh = more dramatic difference.
#
# It builds terraform from this repo, creates COUNT free-tier SSM parameters,
# runs the two scenarios, writes chart.html / chart.svg / results.csv, and
# destroys everything at the end.
#
# Environment:
#   AWS_PROFILE   profile from ~/.aws/credentials (default: "default")
#   TF_BIN        use an existing terraform binary instead of building one
#   PROJECT_N     how many plans to project on the chart (default 10)
#   SKIP_DESTROY  set to 1 to leave the resources in place
#
# WARNING: creates real (free-tier) AWS resources; destroys them on exit unless
# SKIP_DESTROY=1.
set -euo pipefail

COUNT="${1:-300}"
export AWS_PROFILE="${AWS_PROFILE:-default}"
PROJECT_N="${PROJECT_N:-10}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
cd "$HERE"

if [[ -n "${TF_BIN:-}" ]]; then
  TF="$TF_BIN"
else
  TF="$HERE/terraform-dev"
  echo "==> Building terraform from $REPO_ROOT"
  ( cd "$REPO_ROOT" && go build -o "$TF" . )
fi
echo "==> terraform: $("$TF" version | head -1)"
echo "==> COUNT=$COUNT  AWS_PROFILE=$AWS_PROFILE  PROJECT_N=$PROJECT_N"

# Make COUNT available to every terraform command via a tfvars file.
printf '{ "count_params": %s }\n' "$COUNT" > terraform.tfvars.json

cleanup() {
  rm -f objects.json
  if [[ "${SKIP_DESTROY:-0}" == "1" ]]; then
    echo "==> SKIP_DESTROY=1; leaving resources. Destroy later with:"
    echo "    cd $HERE && AWS_PROFILE=$AWS_PROFILE $TF destroy -auto-approve"
    return
  fi
  echo "==> Destroying $COUNT resources"
  "$TF" destroy -auto-approve -no-color | tail -1 || true
}
trap cleanup EXIT

echo "==> terraform init"
"$TF" init -no-color | tail -1
echo "==> Creating $COUNT resources (one-time setup)"
"$TF" apply -auto-approve -no-color | tail -1

# Run the two scenarios and generate the chart.
python3 demo.py --tf "$TF" --project-n "$PROJECT_N"

echo "==> Open $HERE/chart.html to view the demo chart."
