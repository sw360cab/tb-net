# Decisions

One paragraph per decision, dated, newest last.

## 2026-09-11 — custom wire protocol vs NBD

NBD already exports a remote block device over TCP, and Linux can swap to it
with no code of ours. It is the wrong seam here: its unit is a block request on
a TCP socket, tied to the kernel's block and swap path, so it cannot sit under
userfaultfd, cannot carry our seq/pageshift semantics, and cannot move to
AF_PACKET or XDomain rings in Week 3.

Recommendation: the custom 32-byte protocol behind `Link`.
NBD stays as the Day 5 baseline, the number the custom path has to beat.

## 2026-09-11 — dev kernel for Week 2: Docker Desktop has no userfaultfd

Docker Desktop's LinuxKit kernel (6.10.14, arm64) is built with
`# CONFIG_USERFAULTFD is not set`: the syscall returns ENOSYS even in a
`--privileged` container, and there is no `/dev/kvm`.
Week 1 (netns, tcp link,
ultrabench) runs fine there; the pager (Day 6+) and Firecracker (Day 9) cannot.
Options:

- a Lima VM with a stock Debian/Ubuntu kernel running its own Docker
daemon, keeping the same Dockerfile and make targets (the Makefile sets
`DOCKER_HOST` when the VM is up);
- do all pager work directly on the `lan`/`tb` Linux hosts;
- OrbStack, kernel config unverified.

**Current choice**: Lima (`make vm-up`, `env/local/lima.yaml`). Verified on the
dev Mac (M4, macOS 15.7): Ubuntu 26.04, kernel 7.0.0-28-generic,
`CONFIG_USERFAULTFD=y`, and `/dev/kvm` present in the VM and in a `--privileged`
container (nested virtualization, needed for Firecracker; M3+ with macOS 15).
The `internal/uffd` probe still skips on ENOSYS, so kernels without userfaultfd
(Docker Desktop) stay green until Day 6.

## 2026-09-15 — Day 2 wall time on `local`

`cmd/cli0` in netns `fa` against `memsrv -env local` in `fb` (veth, MTU 65520),
inside the Linux container on the Lima VM on the M4 Mac: one TCP connection,
synchronous calls, 10k pages each way, every page verified, 3 runs.

- put: 693–858 ms (69–86 µs/page, 45–56 MB/s)
- get: 772–791 ms (77–79 µs/page, ~50 MB/s)
- ping: 80–160 µs; memsrv logged `errs=0` for every run

This is a sanity number for veth inside a VM, not a transport measurement: on
the Mac's own loopback `BenchmarkPut`/`BenchmarkGet` measure ~13 µs and ~24 µs
per call. No decision follows; ultrabench (Day 3) takes over measuring.

## 2026-09-15 — ultrabench v1: what a result means

The §6 fields stay as specified; these choices fill the gaps around them.

- **File names:** §4's `YYYY-MM-DD-<env>-<link>-<git>.json` collides between
  the modes, and Day 12's repeats, of one commit. Names are now
  `YYYY-MM-DD-<env>-<link>-<git>-<mode>-<HHMMSS>.json`, and ultrabench never
  overwrites a file.
- **Plots:** `.gitignore` excludes `bench/*.png`, yet Day 3 says "commit
  results + PNG". Plots go to `bench/plots/<mode>.png`, which is committed. The
  x axis is runs in date order labelled with the commit, since versions are
  what get compared.
- **matplotlib:** it's not on the Mac, so the image carries `python3-matplotlib`
  and `make docker-plot` runs `make plot` inside it.
- **`mbps`:** MB/s of 2^20 bytes, matching §2's GB/s baselines (bytes, not
  bits); computed over the timed loop's wall time; 0 for `rtt`.
- **`lat_us`:** nearest-rank percentiles over every call, in µs to 0.001.
- **Modes:** `-pages` defaults to every page of `MEMSRV_MB` (pings for `rtt`).
  The get modes Put every page untimed first, so reads return written data.
  `get_rand` visits each page once in a fixed-seed order, so every run and
  version reads the same sequence. `-mode` takes a comma list or `all`.
- **Dirty check:** untracked files count, `bench/results/` does not. ultrabench
  checks before measuring, so a dirty tree fails in milliseconds, not minutes.

First run, `make docker-bench` at 1a32c5f (local, tcp, 131072 calls per mode;
veth in the Lima VM, a baseline for later versions, not a transport number):

| mode | p50 µs | p99 µs | max µs | MB/s |
|---|---|---|---|---|
| rtt | 68.7 | 129.3 | 1116 | — |
| get_seq | 70.6 | 113.0 | 2643 | 54.2 |
| get_rand | 70.0 | 111.3 | 989 | 54.7 |
| put_seq | 68.3 | 111.5 | 9360 | 55.6 |

Sequential and random gets are indistinguishable, as expected with no pager or
prefetch in the path yet: every call is one synchronous TCP round trip.
