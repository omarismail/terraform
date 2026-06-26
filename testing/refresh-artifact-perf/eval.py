#!/usr/bin/env python3
"""Evaluate the refresh-artifact win across several config sizes.

For each size N it: applies N flat aws_ssm_parameter resources, measures the
median `terraform plan` time WITH a live refresh, captures a refresh artifact,
then measures the median `terraform plan -with-refresh` time (no refresh). It
records (N, full_refresh_s, cached_s) and emits a combined table, CSV, and a
"plan time vs resource count" chart.

The config is a flat/wide graph (depth 1), so N only widens the graph. That
isolates refresh cost: the full-refresh line should scale ~linearly with N
while the cached line stays roughly flat (it is graph + fixed overhead, with no
provider refresh).

Modes:

  Measure (runs terraform; sizes applied in ascending order, state grows):
      python3 eval.py --tf ./terraform-dev --sizes 50,300,500 [--runs 2]

  Replay / chart-only (no terraform):
      python3 eval.py --replay 50:22.0:1.5,300:131:1.6,500:219:1.7
"""
import argparse
import json
import math
import os
import re
import statistics
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))


# --------------------------------------------------------------------------
# Terraform helpers
# --------------------------------------------------------------------------
def tf_run(tf, env, args, timed=False):
    t0 = time.monotonic()
    p = subprocess.run([tf] + args + ["-no-color"], cwd=HERE, env=env,
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    dt = time.monotonic() - t0
    if p.returncode not in (0, 2):
        print(p.stdout[-2000:])
        raise SystemExit(f"terraform {' '.join(args)} failed (rc={p.returncode})")
    return (dt, p.stdout) if timed else p.stdout


def set_size(n):
    with open(os.path.join(HERE, "terraform.tfvars.json"), "w") as f:
        json.dump({"count_params": n}, f)


def measure(tf, sizes, runs):
    env = dict(os.environ)
    env.setdefault("AWS_PROFILE", "default")
    results = []
    for n in sizes:
        print(f"\n========== size = {n} resources ==========")
        set_size(n)
        print(f"  apply -> {n} resources ...", flush=True)
        tf_run(tf, env, ["apply", "-auto-approve"])

        refresh_samples, cached_samples = [], []
        for i in range(runs):
            dt, _ = tf_run(tf, env, ["plan"], timed=True)
            refresh_samples.append(dt)
            print(f"    plan (full refresh) #{i+1}: {dt:8.2f}s", flush=True)

        # capturing the artifact is itself a full-refresh plan -> use as a sample
        dt, _ = tf_run(tf, env, ["plan", "-refresh-out=objects.json"], timed=True)
        refresh_samples.append(dt)
        print(f"    plan -refresh-out (full refresh): {dt:8.2f}s", flush=True)

        for i in range(runs):
            dt, _ = tf_run(tf, env, ["plan", "-with-refresh=objects.json"], timed=True)
            cached_samples.append(dt)
            print(f"    plan -with-refresh (cached)  #{i+1}: {dt:8.2f}s", flush=True)

        R = statistics.median(refresh_samples)
        C = statistics.median(cached_samples)
        results.append({"n": n, "full_refresh_s": R, "cached_s": C,
                        "speedup": R / C if C else 0,
                        "refresh_share": (R - C) / R if R else 0})
        print(f"  => full refresh {R:.2f}s | cached {C:.2f}s | {R/C:.0f}x faster")
    return results


# --------------------------------------------------------------------------
# Reporting
# --------------------------------------------------------------------------
def print_table(results):
    print("\n" + "=" * 74)
    print(f"{'resources':>10} | {'full refresh':>13} | {'cached plan':>12} | {'speedup':>8} | {'refresh %':>9}")
    print("-" * 74)
    for r in results:
        print(f"{r['n']:>10} | {r['full_refresh_s']:>11.2f}s | {r['cached_s']:>10.2f}s | "
              f"{r['speedup']:>6.0f}x | {r['refresh_share']*100:>7.1f}%")
    print("=" * 74)
    print("refresh % = share of plan wall-clock that is provider refresh ((R-C)/R).")
    cached = [r["cached_s"] for r in results]
    print(f"cached plan time across all sizes: {min(cached):.2f}s..{max(cached):.2f}s "
          f"(≈flat — a wide graph is cheap to compute; refresh is the variable cost).")


def write_csv(results, path):
    with open(path, "w") as f:
        f.write("resources,full_refresh_s,cached_s,speedup,refresh_share\n")
        for r in results:
            f.write(f"{r['n']},{r['full_refresh_s']:.2f},{r['cached_s']:.2f},"
                    f"{r['speedup']:.2f},{r['refresh_share']:.4f}\n")


# --------------------------------------------------------------------------
# Pure-Python SVG chart: plan time vs resource count
# --------------------------------------------------------------------------
def nice_ceiling(v):
    if v <= 0:
        return 1.0
    mag = 10 ** math.floor(math.log10(v))
    for m in (1, 1.5, 2, 2.5, 5, 10):
        if v <= m * mag:
            return m * mag
    return 10 * mag


def build_svg(results):
    W, H, ML, MR, MT, MB = 760, 420, 64, 150, 30, 52
    PW, PH = W - ML - MR, H - MT - MB
    xs = [r["n"] for r in results]
    xmax = max(xs) * 1.04
    ymax = nice_ceiling(max(r["full_refresh_s"] for r in results))

    def X(n):
        return ML + n / xmax * PW

    def Y(v):
        return MT + PH - v / ymax * PH

    p = []
    p.append(f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" '
             f'font-family="-apple-system,Segoe UI,Roboto,sans-serif">')
    p.append(f'<rect width="{W}" height="{H}" fill="#0b0c12"/>')
    p.append(f'<text x="{W/2}" y="20" font-size="15" font-weight="700" fill="#eceef5" '
             f'text-anchor="middle">terraform plan time vs. config size (flat graph)</text>')

    for i in range(6):
        v = ymax * i / 5
        y = Y(v)
        p.append(f'<line x1="{ML}" y1="{y:.1f}" x2="{ML+PW}" y2="{y:.1f}" stroke="rgba(255,255,255,.08)"/>')
        p.append(f'<text x="{ML-8}" y="{y+4:.1f}" font-size="11" fill="#9aa0b8" '
                 f'text-anchor="end" font-family="ui-monospace,monospace">{v:.0f}s</text>')
    p.append(f'<line x1="{ML}" y1="{MT}" x2="{ML}" y2="{MT+PH}" stroke="#7d8398"/>')
    p.append(f'<line x1="{ML}" y1="{MT+PH}" x2="{ML+PW}" y2="{MT+PH}" stroke="#7d8398"/>')
    for r in results:
        x = X(r["n"])
        p.append(f'<text x="{x:.1f}" y="{MT+PH+20:.1f}" font-size="11" fill="#9aa0b8" '
                 f'text-anchor="middle" font-family="ui-monospace,monospace">{r["n"]}</text>')
    p.append(f'<text x="{ML+PW/2:.1f}" y="{H-6}" font-size="12" fill="#7d8398" '
             f'text-anchor="middle">number of resources (all top-level, no dependencies)</text>')

    rpts = " ".join(f'{X(r["n"]):.1f},{Y(r["full_refresh_s"]):.1f}' for r in results)
    cpts = " ".join(f'{X(r["n"]):.1f},{Y(r["cached_s"]):.1f}' for r in results)
    p.append(f'<polyline points="{rpts}" fill="none" stroke="#ff5c6c" stroke-width="3"/>')
    p.append(f'<polyline points="{cpts}" fill="none" stroke="#34d399" stroke-width="3"/>')
    for r in results:
        x, yr, yc = X(r["n"]), Y(r["full_refresh_s"]), Y(r["cached_s"])
        p.append(f'<circle cx="{x:.1f}" cy="{yr:.1f}" r="4" fill="#ff5c6c"/>')
        p.append(f'<circle cx="{x:.1f}" cy="{yc:.1f}" r="4" fill="#34d399"/>')
        p.append(f'<text x="{x:.1f}" y="{yr-10:.1f}" font-size="11" fill="#ff8a8f" '
                 f'text-anchor="middle" font-family="ui-monospace,monospace">{r["full_refresh_s"]:.0f}s</text>')

    lx = ML + PW + 20
    p.append(f'<rect x="{lx}" y="{MT+6}" width="12" height="12" rx="3" fill="#ff5c6c"/>')
    p.append(f'<text x="{lx+18}" y="{MT+16}" font-size="12" fill="#eceef5">plan (live refresh)</text>')
    p.append(f'<rect x="{lx}" y="{MT+30}" width="12" height="12" rx="3" fill="#34d399"/>')
    p.append(f'<text x="{lx+18}" y="{MT+40}" font-size="12" fill="#eceef5">plan -with-refresh</text>')
    last = results[-1]
    p.append(f'<text x="{lx}" y="{MT+74}" font-size="12" fill="#34d399" font-weight="700">'
             f'cached ≈ {last["cached_s"]:.1f}s</text>')
    p.append(f'<text x="{lx}" y="{MT+90}" font-size="11" fill="#9aa0b8">flat across all sizes</text>')
    p.append(f'<text x="{lx}" y="{MT+118}" font-size="12" fill="#eceef5" font-weight="700">'
             f'{last["speedup"]:.0f}× at {last["n"]}</text>')
    p.append('</svg>')
    return "\n".join(p)


def build_html(svg, results):
    rows = "\n".join(
        f"<tr><td>{r['n']}</td><td>{r['full_refresh_s']:.2f}s</td>"
        f"<td>{r['cached_s']:.2f}s</td><td><b>{r['speedup']:.0f}×</b></td>"
        f"<td>{r['refresh_share']*100:.1f}%</td></tr>"
        for r in results)
    return f"""<!doctype html><html><head><meta charset="utf-8">
<title>Refresh artifact — evaluation across config sizes</title>
<style>
 body{{font-family:-apple-system,Segoe UI,Roboto,sans-serif;margin:40px;background:#0b0c12;color:#eceef5}}
 h1{{font-size:21px}} .k{{color:#9aa0b8;font-size:13px}}
 table{{border-collapse:collapse;margin-top:14px}}
 th,td{{border:1px solid #2a2c38;padding:7px 14px;font-size:13px;text-align:right}}
 th:first-child,td:first-child{{text-align:left}} th{{color:#9aa0b8}}
</style></head><body>
<h1>Refresh artifact — evaluation across config sizes</h1>
<p class="k">Flat / wide graph (depth 1). The live-refresh plan scales with resource
count; the cached plan stays ~flat because it does no provider refresh.</p>
{svg}
<table>
<tr><th>resources</th><th>plan (live refresh)</th><th>plan -with-refresh</th><th>speedup</th><th>refresh share</th></tr>
{rows}
</table>
<p class="k">refresh share = (live − cached) / live: the fraction of plan wall-clock that is provider refresh.</p>
</body></html>"""


# --------------------------------------------------------------------------
def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--tf", help="path to terraform binary (measure mode)")
    ap.add_argument("--sizes", default="50,300,500", help="comma-separated resource counts")
    ap.add_argument("--runs", type=int, default=2, help="timed runs per variant")
    ap.add_argument("--replay", help="chart-only: 'N:R:C,N:R:C,...'")
    args = ap.parse_args()

    if args.replay:
        results = []
        for chunk in args.replay.split(","):
            n, R, C = chunk.split(":")
            R, C = float(R), float(C)
            results.append({"n": int(n), "full_refresh_s": R, "cached_s": C,
                            "speedup": R / C, "refresh_share": (R - C) / R})
    elif args.tf:
        sizes = sorted(int(s) for s in args.sizes.split(","))
        results = measure(args.tf, sizes, args.runs)
        json.dump(results, open(os.path.join(HERE, "eval_results.json"), "w"), indent=2)
    else:
        ap.error("provide either --tf (measure) or --replay (chart-only)")

    print_table(results)
    write_csv(results, os.path.join(HERE, "eval_results.csv"))
    svg = build_svg(results)
    open(os.path.join(HERE, "eval_chart.svg"), "w").write(svg)
    open(os.path.join(HERE, "eval_chart.html"), "w").write(build_html(svg, results))
    print(f"\nWrote eval_results.csv, eval_chart.svg, eval_chart.html in {HERE}")


if __name__ == "__main__":
    main()
