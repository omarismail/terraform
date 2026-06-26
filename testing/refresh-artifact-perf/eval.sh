#!/usr/bin/env bash
#
# Evaluate the refresh-artifact win across several config sizes.
#
#   ./eval.sh [SIZES]
#
# SIZES = comma-separated resource counts (default "50,300,500"):
#   50  = small         (granular look at small resource counts)
#   300 = original/med  (the baseline example)
#   500 = large + flat  (500 independent top-level resources, wide not deep)
#
# Builds terraform from the repo, applies each size in turn (state grows),
# benchmarks plan vs plan -with-refresh at each, writes eval_chart.html /
# eval_results.csv, and destroys everything at the end.
#
# Env: AWS_PROFILE (default "default"), TF_BIN, RUNS (default 2), SKIP_DESTROY=1.
#
# WARNING: creates real free-tier AWS resources; destroyed on exit unless SKIP_DESTROY=1.
set -euo pipefail

SIZES="${1:-50,300,500}"
export AWS_PROFILE="${AWS_PROFILE:-default}"
RUNS="${RUNS:-2}"

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
echo "==> sizes=$SIZES  runs=$RUNS  AWS_PROFILE=$AWS_PROFILE"

cleanup() {
  rm -f objects.json
  if [[ "${SKIP_DESTROY:-0}" == "1" ]]; then
    echo "==> SKIP_DESTROY=1; leaving resources. Destroy later with:"
    echo "    cd $HERE && AWS_PROFILE=$AWS_PROFILE $TF destroy -auto-approve"
    return
  fi
  echo "==> Destroying resources"
  "$TF" destroy -auto-approve -no-color | tail -1 || true
}
trap cleanup EXIT

echo "==> terraform init"
"$TF" init -no-color | tail -1

python3 eval.py --tf "$TF" --sizes "$SIZES" --runs "$RUNS"

echo "==> Open $HERE/eval_chart.html for the cross-size comparison."
