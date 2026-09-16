#!/usr/bin/env python3
"""Plot bench/results/*.json into bench/plots/<mode>.png (CLAUDE.md Day 3).

One PNG per mode, one series per env x link. The x axis is runs in date
order, labelled with date and commit: versions are what get compared, not
calendar time. Left panel: p50 (solid) and p99 (dashed) latency in
microseconds, log scale. Right panel, for modes that move pages: MB/s.
matplotlib only.
"""
import glob
import json
import os
import sys
from collections import defaultdict
from datetime import datetime

import matplotlib

matplotlib.use("Agg")  # no display in containers or CI
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib.ticker import ScalarFormatter  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RESULTS = os.path.join(ROOT, "bench", "results")
PLOTS = os.path.join(ROOT, "bench", "plots")


def load():
    runs = []
    for path in sorted(glob.glob(os.path.join(RESULTS, "*.json"))):
        with open(path) as f:
            r = json.load(f)
        r["when"] = datetime.fromisoformat(r["date"].replace("Z", "+00:00"))
        runs.append(r)
    return runs


def plot_mode(mode, runs):
    moves = any(r["mbps"] > 0 for r in runs)
    fig, axes = plt.subplots(1, 2 if moves else 1, figsize=(13 if moves else 7, 4.5), squeeze=False)
    lat = axes[0][0]
    thr = axes[0][1] if moves else None

    runs = sorted(runs, key=lambda r: r["when"])
    for i, r in enumerate(runs):
        r["x"] = i
    series = defaultdict(list)
    for r in runs:
        series[(r["env"], r["link"])].append(r)

    for (env, link), rs in sorted(series.items()):
        x = [r["x"] for r in rs]
        label = f"{env}/{link}"
        (line,) = lat.plot(x, [r["lat_us"]["p50"] for r in rs], "o-", label=f"{label} p50")
        lat.plot(x, [r["lat_us"]["p99"] for r in rs], "s--", color=line.get_color(), label=f"{label} p99")
        if thr is not None:
            thr.plot(x, [r["mbps"] for r in rs], "o-", color=line.get_color(), label=label)

    ticks = [r["x"] for r in runs]
    labels = [f'{r["when"]:%m-%d %H:%M}\n{r["git"]}' for r in runs]

    lat.set_yscale("log")
    for axis in (lat.yaxis.set_major_formatter, lat.yaxis.set_minor_formatter):
        fmt = ScalarFormatter()
        fmt.set_scientific(False)
        axis(fmt)
    lat.set_ylabel("latency per call (µs)")
    lat.set_title(f"farbench {mode}: latency")
    lat.legend(fontsize=8)
    lat.grid(True, which="both", alpha=0.3)
    if thr is not None:
        thr.set_ylabel("MB/s")
        thr.set_ylim(bottom=0)
        thr.set_title(f"farbench {mode}: throughput")
        thr.legend(fontsize=8)
        thr.grid(True, alpha=0.3)
    for ax in axes[0]:
        ax.set_xticks(ticks, labels, fontsize=8)
        ax.set_xlim(-0.5, len(runs) - 0.5)
        ax.margins(y=0.15)

    fig.tight_layout()
    path = os.path.join(PLOTS, f"{mode}.png")
    fig.savefig(path, dpi=120)
    plt.close(fig)
    return path


def main():
    runs = load()
    if not runs:
        print(f"no results in {RESULTS}", file=sys.stderr)
        return 1
    os.makedirs(PLOTS, exist_ok=True)
    by_mode = defaultdict(list)
    for r in runs:
        by_mode[r["mode"]].append(r)
    for mode, rs in sorted(by_mode.items()):
        print(f"{plot_mode(mode, rs)}  ({len(rs)} runs)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
