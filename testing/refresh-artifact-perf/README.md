# Refresh-artifact demo

A one-command demo that shows why reusing a refresh artifact matters: it
compares the cost of running `terraform plan` repeatedly (full refresh every
time) against capturing the refresh once and reusing it.

It manages a configurable number of **free-tier** AWS resources (SSM `String`
parameters, ~$0) so the thing being measured is the number of provider
`ReadResource` (refresh) round-trips, not dollars.

> ⚠️ Creates real (free-tier) AWS resources and destroys them on exit unless
> `SKIP_DESTROY=1`.

There are two entry points:

- **`demo.sh`** — the single-config narrative demo (two scenarios + a "cost grows as
  you keep planning" chart). Good for presenting the idea.
- **`eval.sh`** — the multi-size evaluation that sweeps several config sizes and
  shows how the win scales. Good for the "actual performance test."

## Files

| File         | Purpose                                                               |
| ------------ | --------------------------------------------------------------------- |
| `main.tf`    | `var.count_params` × `aws_ssm_parameter` — a flat / wide graph (depth 1). |
| `demo.sh` / `demo.py` | Single-config demo: two scenarios, prints a table, writes `chart.html`. `demo.py` also has a no-AWS replay mode. |
| `eval.sh` / `eval.py` | Multi-size evaluation across config sizes, writes `eval_chart.html` + `eval_results.csv`. `eval.py` also has a replay mode. |
| `chart.html` / `eval_chart.html` | The viewable outputs (open in a browser).            |

## The two scenarios

- **Scenario 1 (status quo):** `terraform plan` twice — each one performs a full
  live refresh.
- **Scenario 2 (refresh artifact):** `terraform plan -refresh-out=objects.json`
  once (full refresh + write the artifact), then
  `terraform plan -with-refresh=objects.json` (no refresh).

This isolates the first cost of a full refresh from the cost of subsequent
plans, with and without the artifact.

## Run it

```bash
cd testing/refresh-artifact-perf
AWS_PROFILE=default ./demo.sh            # 300 resources (default)
AWS_PROFILE=default ./demo.sh 600        # bigger => more dramatic
```

Then open `chart.html`. Knobs (all optional env vars):

- `TF_BIN=/path/to/terraform` — benchmark an existing binary instead of building.
- `PROJECT_N=20` — how many plans to project on the chart (default 10).
- `SKIP_DESTROY=1` — keep the resources (destroy them yourself afterwards).

## Just the chart (no AWS)

To iterate on the visuals or rebuild the chart from numbers you already have:

```bash
python3 demo.py --full-refresh 174.9 --cached 1.66 --project-n 10
```

## What it shows

The projection is simple math from two measured numbers — `R` = full-refresh
plan time, `C` = cached plan time:

- **Plan every time:** cumulative time after *k* plans = `k × R` (linear growth).
- **Refresh artifact:** cumulative time after *k* plans = `R + (k−1) × C`
  (pay the refresh once, then ~flat).

The first full-refresh cost is identical for both strategies; only the
plan-every-time strategy keeps paying it. Example measured run (400 resources,
us-west-1): `R ≈ 174.9s`, `C ≈ 1.66s` → subsequent plans ~105× faster; after 10
plans, 1749s vs 190s (~1559s saved).

## Multi-size evaluation (`eval.sh`)

Sweeps several config sizes and measures, at each, the median `plan` time **with
a live refresh** vs the median `plan -with-refresh` time. Because the config is a
flat / wide graph (every resource is independent and top-level, depth 1), the
only thing that changes is graph *width* — which isolates refresh cost.

```bash
cd testing/refresh-artifact-perf
AWS_PROFILE=default ./eval.sh                 # sizes 50, 300, 500
AWS_PROFILE=default ./eval.sh 25,100,400,800  # custom sizes
```

The three default sizes are the requested cases:

| size  | role                                                              |
| ----- | ---------------------------------------------------------------- |
| `50`  | **small** — granular look at small resource counts               |
| `300` | **original / medium** — the baseline example                     |
| `500` | **large + flat** — 500 independent top-level resources, wide not deep |

Sizes are applied in ascending order (the state grows incrementally), then all
resources are destroyed at the end. Knobs: `RUNS` (timed runs per variant,
default 2), plus `TF_BIN` / `SKIP_DESTROY` as above.

Output is `eval_chart.html` (plan time vs resource count: a rising live-refresh
line and a flat cached line) and `eval_results.csv`. The key result: the
**live-refresh** line scales ~linearly with resource count, while the **cached**
line stays ~flat — a wide graph is cheap to *compute*; refresh is the variable
cost, and it's the part the artifact removes.

Chart-only (no AWS), from numbers you already have:

```bash
python3 eval.py --replay 50:22.1:1.5,300:131:1.6,500:219:1.7
```
