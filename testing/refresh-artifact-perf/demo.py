#!/usr/bin/env python3
"""Measure and chart the refresh-artifact win across two scenarios.

Scenario 1 (status quo): run `terraform plan` twice. Each pays a full live
                         refresh.
Scenario 2 (artifact):   run `terraform plan -refresh-out=objects.json` once
                         (full refresh + write the artifact), then
                         `terraform plan -with-refresh=objects.json` (no refresh).

It prints a comparison table and writes, next to this script:
  - results.json   raw measured times
  - results.csv    cumulative-time projection for a chart
  - chart.svg      self-contained projection chart
  - chart.html     the chart + table in one openable file (great for demos)

Two modes:

  Measure (runs terraform):
      python3 demo.py --tf ./terraform-dev [--project-n 10]

  Replay / chart-only (no terraform, regenerate visuals from numbers):
      python3 demo.py --full-refresh 174.9 --cached 1.66 [--project-n 10]
"""
import argparse
import json
import os
import re
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))


# --------------------------------------------------------------------------
# Measurement
# --------------------------------------------------------------------------
def run_plan(tf, env, extra_args, label):
    print(f"  running: terraform plan {' '.join(extra_args)} ...", flush=True)
    t0 = time.monotonic()
    p = subprocess.run(
        [tf, "plan", "-no-color"] + extra_args,
        cwd=HERE, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    dt = time.monotonic() - t0
    if p.returncode not in (0, 2):  # 2 == "changes present" with -detailed-exitcode; we don't use it, but be lenient
        m = re.search(r"(?i)error.*", p.stdout)
        print(p.stdout)
        raise SystemExit(f"terraform plan failed ({label}): {m.group(0) if m else 'see output above'}")
    summary = re.search(r"^(Plan:|No changes).*$", p.stdout, re.M)
    print(f"    {label}: {dt:.2f}s  | {summary.group(0) if summary else ''}")
    return dt


def measure(tf, project_n):
    env = dict(os.environ)
    env.setdefault("AWS_PROFILE", "default")

    print("\n=== Scenario 1: status quo — terraform plan twice (full refresh each) ===")
    a1 = run_plan(tf, env, [], "scenario1 plan #1 (full refresh)")
    a2 = run_plan(tf, env, [], "scenario1 plan #2 (full refresh)")

    print("\n=== Scenario 2: refresh artifact — capture once, reuse ===")
    b1 = run_plan(tf, env, ["-refresh-out=objects.json"], "scenario2 plan #1 (-refresh-out, full refresh)")
    b2 = run_plan(tf, env, ["-with-refresh=objects.json"], "scenario2 plan #2 (-with-refresh, no refresh)")

    # The "full refresh" cost is best estimated by averaging the three plans
    # that actually refreshed; the cached cost is the single -with-refresh plan.
    full_refresh = (a1 + a2 + b1) / 3.0
    cached = b2
    return {
        "scenario1": {"plan1": a1, "plan2": a2, "total": a1 + a2},
        "scenario2": {"plan1_refresh_out": b1, "plan2_with_refresh": b2, "total": b1 + b2},
        "full_refresh_avg": full_refresh,
        "cached_plan": cached,
        "project_n": project_n,
    }


# --------------------------------------------------------------------------
# Reporting
# --------------------------------------------------------------------------
def print_table(R, C, s1=None, s2=None):
    def line(label, val):
        print(f"  {label:<52}{val:>8.2f}s")

    print("\n" + "=" * 66)
    print("RESULTS")
    print("=" * 66)
    if s1:
        print("Scenario 1 — status quo (full refresh on every plan)")
        line("plan #1  (full refresh)", s1["plan1"])
        line("plan #2  (full refresh)", s1["plan2"])
        line("TOTAL", s1["total"])
        print()
        print("Scenario 2 — refresh artifact")
        line("plan #1  -refresh-out   (full refresh + write artifact)", s2["plan1_refresh_out"])
        line("plan #2  -with-refresh  (no refresh)", s2["plan2_with_refresh"])
        line("TOTAL", s2["total"])
        print()
    print(f"Per-plan cost:  full refresh ~{R:.2f}s   vs   cached plan ~{C:.2f}s")
    if C > 0:
        print(f"Subsequent plans are ~{R / C:.0f}x faster, saving ~{R - C:.2f}s each.")
    if s1 and s2:
        saved = s1["total"] - s2["total"]
        print(f"Over these two plans: {s1['total']:.2f}s vs {s2['total']:.2f}s  =>  saved {saved:.2f}s")


def projection_rows(R, C, n):
    """Cumulative time after k plans, for both strategies.

    Status quo:      every plan refreshes        => k * R
    Refresh artifact: refresh once, then reuse    => R + (k-1) * C
    """
    rows = []
    for k in range(1, n + 1):
        repeat = k * R
        artifact = R + (k - 1) * C
        rows.append((k, repeat, artifact, repeat - artifact))
    return rows


def write_csv(rows, path):
    with open(path, "w") as f:
        f.write("num_plans,repeat_full_refresh_s,refresh_artifact_s,time_saved_s\n")
        for k, repeat, artifact, saved in rows:
            f.write(f"{k},{repeat:.2f},{artifact:.2f},{saved:.2f}\n")


# --------------------------------------------------------------------------
# Pure-Python SVG line chart (no dependencies)
# --------------------------------------------------------------------------
def nice_ceiling(v):
    import math
    if v <= 0:
        return 1.0
    mag = 10 ** math.floor(math.log10(v))
    for m in (1, 2, 2.5, 5, 10):
        if v <= m * mag:
            return m * mag
    return 10 * mag


def build_svg(rows, R, C):
    W, H = 960, 560
    ML, MR, MT, MB = 80, 200, 70, 60
    PW, PH = W - ML - MR, H - MT - MB
    n = len(rows)
    ymax = nice_ceiling(rows[-1][1])  # top line is repeated-refresh at k=n

    def x(k):
        return ML + (0 if n == 1 else (k - 1) / (n - 1) * PW)

    def y(v):
        return MT + PH - (v / ymax * PH)

    repeat_pts = " ".join(f"{x(r[0]):.1f},{y(r[1]):.1f}" for r in rows)
    artifact_pts = " ".join(f"{x(r[0]):.1f},{y(r[2]):.1f}" for r in rows)

    # Y gridlines / ticks
    yticks = []
    steps = 5
    for i in range(steps + 1):
        val = ymax * i / steps
        yticks.append((val, y(val)))

    # X ticks (every plan, but cap labels if many)
    xstep = 1 if n <= 12 else max(1, round(n / 10))

    parts = []
    parts.append(f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" font-family="-apple-system,Segoe UI,Roboto,sans-serif">')
    parts.append(f'<rect width="{W}" height="{H}" fill="#ffffff"/>')
    parts.append(f'<text x="{W/2}" y="30" font-size="20" font-weight="700" text-anchor="middle" fill="#1a1a1a">Cumulative terraform plan time as plans repeat</text>')
    parts.append(f'<text x="{W/2}" y="50" font-size="13" text-anchor="middle" fill="#666">Full refresh ≈ {R:.0f}s &#183; cached plan ≈ {C:.2f}s</text>')

    # gridlines + y labels
    for val, yy in yticks:
        parts.append(f'<line x1="{ML}" y1="{yy:.1f}" x2="{ML+PW}" y2="{yy:.1f}" stroke="#eee" stroke-width="1"/>')
        parts.append(f'<text x="{ML-10}" y="{yy+4:.1f}" font-size="12" text-anchor="end" fill="#666">{val:.0f}s</text>')

    # axes
    parts.append(f'<line x1="{ML}" y1="{MT}" x2="{ML}" y2="{MT+PH}" stroke="#999" stroke-width="1.5"/>')
    parts.append(f'<line x1="{ML}" y1="{MT+PH}" x2="{ML+PW}" y2="{MT+PH}" stroke="#999" stroke-width="1.5"/>')

    # x labels
    for r in rows:
        k = r[0]
        if k == 1 or k == n or k % xstep == 0:
            parts.append(f'<text x="{x(k):.1f}" y="{MT+PH+22:.1f}" font-size="12" text-anchor="middle" fill="#666">{k}</text>')
    parts.append(f'<text x="{ML+PW/2:.1f}" y="{H-12}" font-size="13" text-anchor="middle" fill="#444">Number of plans run</text>')
    parts.append(f'<text x="20" y="{MT+PH/2:.1f}" font-size="13" text-anchor="middle" fill="#444" transform="rotate(-90 20 {MT+PH/2:.1f})">Cumulative wall-clock time</text>')

    # shaded savings region (between the two lines)
    area = repeat_pts + " " + " ".join(f"{x(r[0]):.1f},{y(r[2]):.1f}" for r in reversed(rows))
    parts.append(f'<polygon points="{area}" fill="#34c759" opacity="0.10"/>')

    # lines
    parts.append(f'<polyline points="{repeat_pts}" fill="none" stroke="#ff3b30" stroke-width="3"/>')
    parts.append(f'<polyline points="{artifact_pts}" fill="none" stroke="#34c759" stroke-width="3"/>')
    for r in rows:
        parts.append(f'<circle cx="{x(r[0]):.1f}" cy="{y(r[1]):.1f}" r="3.5" fill="#ff3b30"/>')
        parts.append(f'<circle cx="{x(r[0]):.1f}" cy="{y(r[2]):.1f}" r="3.5" fill="#34c759"/>')

    # legend
    lx = ML + PW + 24
    parts.append(f'<rect x="{lx}" y="{MT}" width="14" height="14" fill="#ff3b30"/>')
    parts.append(f'<text x="{lx+20}" y="{MT+12}" font-size="13" fill="#333">Plan every time</text>')
    parts.append(f'<text x="{lx+20}" y="{MT+28}" font-size="11" fill="#888">(refresh on every plan)</text>')
    parts.append(f'<rect x="{lx}" y="{MT+50}" width="14" height="14" fill="#34c759"/>')
    parts.append(f'<text x="{lx+20}" y="{MT+62}" font-size="13" fill="#333">Refresh artifact</text>')
    parts.append(f'<text x="{lx+20}" y="{MT+78}" font-size="11" fill="#888">(refresh once, reuse)</text>')

    final = rows[-1]
    parts.append(f'<text x="{lx}" y="{MT+120}" font-size="12" fill="#333" font-weight="700">After {final[0]} plans:</text>')
    parts.append(f'<text x="{lx}" y="{MT+140}" font-size="12" fill="#ff3b30">repeat:  {final[1]:.0f}s</text>')
    parts.append(f'<text x="{lx}" y="{MT+158}" font-size="12" fill="#34c759">artifact: {final[2]:.0f}s</text>')
    parts.append(f'<text x="{lx}" y="{MT+176}" font-size="12" fill="#1a1a1a" font-weight="700">saved:   {final[3]:.0f}s</text>')

    parts.append('</svg>')
    return "\n".join(parts)


def build_html(svg, rows, R, C, s1=None, s2=None):
    def tr(cells, th=False):
        tag = "th" if th else "td"
        return "<tr>" + "".join(f"<{tag}>{c}</{tag}>" for c in cells) + "</tr>"

    scenario_html = ""
    if s1 and s2:
        scenario_html = f"""
    <h3>Measured</h3>
    <table>
      {tr(["Scenario", "plan #1", "plan #2", "total"], th=True)}
      {tr(["1 — plan every time (full refresh)", f"{s1['plan1']:.2f}s", f"{s1['plan2']:.2f}s", f"<b>{s1['total']:.2f}s</b>"])}
      {tr(["2 — refresh artifact", f"{s2['plan1_refresh_out']:.2f}s (-refresh-out)", f"{s2['plan2_with_refresh']:.2f}s (-with-refresh)", f"<b>{s2['total']:.2f}s</b>"])}
    </table>"""

    proj_rows = "\n".join(tr([k, f"{rep:.0f}s", f"{art:.0f}s", f"{sav:.0f}s"]) for k, rep, art, sav in rows)
    return f"""<!doctype html>
<html><head><meta charset="utf-8"><title>Refresh artifact: plan time</title>
<style>
  body {{ font-family:-apple-system,Segoe UI,Roboto,sans-serif; margin:40px; color:#1a1a1a; }}
  h1 {{ font-size:22px; }} h3 {{ margin-top:28px; }}
  table {{ border-collapse:collapse; margin-top:8px; }}
  th,td {{ border:1px solid #ddd; padding:6px 12px; font-size:13px; text-align:right; }}
  th:first-child, td:first-child {{ text-align:left; }}
  .key {{ color:#666; font-size:13px; }}
</style></head><body>
<h1>Refresh artifact — cost of repeated plans</h1>
<p class="key">Full refresh ≈ {R:.1f}s per plan &middot; cached plan ≈ {C:.2f}s per plan &middot;
subsequent plans ≈ {R/C:.0f}× faster.</p>
{svg}
{scenario_html}
<h3>Projection</h3>
<table>
  {tr(["# plans", "plan every time", "refresh artifact", "time saved"], th=True)}
  {proj_rows}
</table>
<p class="key">Plan every time = k &times; full-refresh. Refresh artifact = one full refresh +
(k&minus;1) &times; cached. The first full-refresh cost is identical for both; only the
repeat strategy keeps paying it.</p>
</body></html>"""


# --------------------------------------------------------------------------
def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--tf", help="path to the terraform binary (measure mode)")
    ap.add_argument("--full-refresh", type=float, help="full-refresh plan seconds (replay mode)")
    ap.add_argument("--cached", type=float, help="cached plan seconds (replay mode)")
    ap.add_argument("--project-n", type=int, default=10, help="how many plans to project on the chart")
    args = ap.parse_args()

    s1 = s2 = None
    if args.tf:
        data = measure(args.tf, args.project_n)
        R, C = data["full_refresh_avg"], data["cached_plan"]
        s1, s2 = data["scenario1"], data["scenario2"]
        json.dump(data, open(os.path.join(HERE, "results.json"), "w"), indent=2)
    elif args.full_refresh is not None and args.cached is not None:
        R, C = args.full_refresh, args.cached
    else:
        ap.error("provide either --tf (measure) or both --full-refresh and --cached (replay)")

    print_table(R, C, s1, s2 and s2)
    rows = projection_rows(R, C, args.project_n)
    write_csv(rows, os.path.join(HERE, "results.csv"))
    svg = build_svg(rows, R, C)
    open(os.path.join(HERE, "chart.svg"), "w").write(svg)
    open(os.path.join(HERE, "chart.html"), "w").write(build_html(svg, rows, R, C, s1, s2))
    print(f"\nWrote: results.csv, chart.svg, chart.html  (in {HERE})")
    print("Open chart.html in a browser for the demo view.")


if __name__ == "__main__":
    main()
