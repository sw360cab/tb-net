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

## 2026-09-17 — first `lan` results: the Mac and screwdriver over a direct cable

ultrabench on the Mac (A) against `memsrv.service` on screwdriver (B), deployed
with `deploy/push.sh` at 44c9034. Direct Gigabit cable (Mac side: Belkin USB-C
adapter), static `10.97.0.1`/`10.97.0.2`, MTU 1500, 65536 calls per mode. The
Mac is only the client here; memsrv runs on Linux.

| mode | p50 µs | p99 µs | max µs | MB/s |
|---|---|---|---|---|
| rtt | 349.9 | 389.0 | 3248 | — |
| get_seq | 389.0 | 593.8 | 11601 | 9.9 |
| get_rand | 396.6 | 446.5 | 11753 | 10.0 |
| put_seq | 367.7 | 403.0 | 14290 | 10.6 |

- **Latency is the link, not the code.** A loaded round trip is ~350 µs. Idle
  ICMP ping is slower (avg 1.5 ms Mac → screwdriver, 1.1 ms back) because NICs
  and CPUs sleep between packets 200 ms apart, so ping is not a floor for this
  link. The likely cost is the USB adapter and interrupt coalescing: 4 KB at
  1 Gb/s is only ~33 µs, about the gap between `rtt` and the page-carrying
  modes (+18–47 µs at p50).
- **Throughput is the protocol.** One page per synchronous round trip caps at
  4 KB / RTT ≈ 10 MB/s, a tenth of what the wire carries. Pipelining or
  batching would move it; not before a measurement shows it is the bottleneck
  for the pager (§7).
- **Tails:** max 11–14 ms in the page modes are single events among 65536
  calls. `get_seq`'s p99 above `get_rand`'s is not an access-pattern effect
  (nothing caches yet); unexplained for now, to watch in repeats.
- **Against `local`** (1a32c5f, veth in the Lima VM): ~5× the latency and
  ~1/5 the throughput.
- The first 1000-call smoke run (p50 478 µs) was dropped: too short to warm up.
