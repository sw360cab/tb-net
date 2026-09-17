# ultramem — ultra memory over Thunderbolt, in Go

This file is the project brief and the working agreement for Claude Code.
Read it fully before doing anything. Keep it updated when decisions change.

---

## 1. What we are building

Two Linux hosts joined by a Thunderbolt cable. Host **A** runs workloads
(later: a microVM). Host **B** lends its RAM. A userspace **pager** on A
serves page faults by fetching 4 KB pages from a **memory server** on B and
evicts cold pages back to B.

Three deliverables grow in parallel from day 1:

| Track | Artifact | Purpose |
|---|---|---|
| Code | `memsrv`, `pager`, `internal/link` | the system |
| Bench | `ultrabench` + `bench/results/` | every version is measured the same way |
| Ops | `deploy/` + `env/` | one command to push and run on any environment |

Non-goals for these 3 weeks: macOS, multi-node, security, production hardening,
any inference integration. Linux ↔ Linux only.

## 2. Ground truth (do not re-derive, do not argue with)

- Thunderbolt host-to-host exposes one transport: **XDomain DMA rings**.
  Linux's `thunderbolt-net` builds an Ethernet netdev (`thunderbolt0`) on it,
  MTU up to 65522, 4096-byte frames on the wire.
- No load/store across hosts. Ultra memory is **page-granular**, fault-driven.
- Expected cost per 4 KB miss over TB4: ~12 µs (link RTT ~8–10 µs + fault
  overhead). Bandwidth ~2.5–3 GB/s. That is Optane-SSD-class swap. Design for it.
- Prior art we build on, not against: Infiniswap (design), Firecracker UFFD
  handler (VMM contract), `drivers/thunderbolt/dma_test.c` and
  `hellas-ai/thunderbolt-ibverbs` (raw transport, Week 3).

## 3. Architecture

```
host A                                          host B
┌──────────────┐  uffd   ┌────────────┐  Link  ┌─────────────┐
│ workload /   │◄───────►│   pager    │◄──────►│   memsrv    │
│ microVM      │         │ (Go)       │        │ (Go)        │
└──────────────┘         └────────────┘        └─────────────┘
                              ▲
                              │ same binary, same protocol
                         ┌────┴─────┐
                         │ ultrabench │
                         └──────────┘
```

**`Link` is the seam.** Everything above it must not care what is below it.

```go
// internal/link/link.go
type Link interface {
    Get(ctx context.Context, page uint64, dst []byte) error   // 4 KB in
    Put(ctx context.Context, page uint64, src []byte) error   // 4 KB out, acked
    Ping(ctx context.Context) (time.Duration, error)
    Close() error
}
```

Implementations, in order of arrival:
`tcp` (week 1) → `rawl2` AF_PACKET (week 3) → `xdomain`/`verbs` (week 3, exploratory).

**Wire protocol** (`internal/proto`), fixed 32-byte header, little-endian:

```
magic u32 | op u8 | flags u8 | pageshift u8 | rsvd u8 | seq u64 | page u64 | len u32 | crc u32
ops: PING PONG GET GETR PUT PUTA
```
Payload follows for GETR/PUT. `pageshift` is 12 now; keep it so 2 MB batches
are a flag flip later, not a rewrite.

## 4. Repository layout

```
ultramem/
  cmd/memsrv/        host B: pinned buffer + serve loop
  cmd/pager/         host A: uffd handler + policy
  cmd/ultrabench/      benchmark client (RTT, get/put throughput, guest workload)
  cmd/touch/         static in-guest workload: touch N MB seq/random, print latency pcts
  internal/link/     Link interface + tcp/, rawl2/, verbs/
  internal/proto/    header encode/decode + tests
  internal/pager/    page table, CLOCK eviction, prefetch, writeback queue
  internal/uffd/     thin ioctl wrappers over x/sys/unix
  env/               local/  lan/  tb/   (config + how to bring each up)
  deploy/            push.sh  memsrv.service  pager.service  firecracker/
  bench/
    results/         YYYY-MM-DD-<env>-<link>-<git>-<mode>-<HHMMSS>.json   (committed)
    plots/           <mode>.png from plot.py   (committed)
    plot.py          results → PNG (matplotlib only; make docker-plot)
  docs/              decisions.md  (one paragraph per decision, dated)
  Makefile           build test lint bench push
  Dockerfile         base → build (compile, vet, lint) → test (run --privileged)
```

## 5. Environments

| name | what | when |
|---|---|---|
| `local` | one machine, two network namespaces joined by veth | daily dev, CI |
| `lan` | two hosts over ordinary Ethernet | sanity check that "real hosts" works |
| `tb` | two hosts over `thunderbolt0` | measurements that count |

Every command takes `-env <name>`; env files define addresses, interface names,
MTU, memsrv size. `local` must always work with no hardware and no root beyond
`ip netns`. If a change breaks `local`, it does not merge.

Dev machine is macOS: everything runs in Linux containers (`make docker-test`,
`make dev`, `make docker-bench`) against the Docker daemon of a Lima VM
(`make vm-up`), because Docker Desktop's kernel has no userfaultfd or KVM.
See `docs/decisions.md`.

Host prerequisites (record actual values in `env/tb/README.md`):
kernel ≥ 6.6, `thunderbolt-net` loaded, `ip link set thunderbolt0 mtu 65520`,
`sysctl vm.unprivileged_userfaultfd=1` (or run pager with CAP_SYS_PTRACE),
`/dev/kvm` access on A, Firecracker binary on A.

## 6. Benchmark contract

`ultrabench` writes one JSON per run to `bench/results/`. Fields are fixed from
day 3 onward so results stay comparable across all versions:

```json
{ "date": "...", "env": "tb", "link": "tcp", "git": "abc123",
  "mode": "rtt|get_seq|get_rand|put_seq|guest_seq|guest_rand",
  "pages": 262144, "mtu": 65520,
  "lat_us": {"p50":0,"p90":0,"p99":0,"p999":0,"max":0},
  "mbps": 0, "faults": 0, "evictions": 0, "prefetch_hits": 0 }
```

Baseline numbers to beat or explain: local DRAM ~0.1 µs; NVMe swap ~80 µs, 7 GB/s;
target here: p50 ≤ 15 µs per miss on `tb`, ≥ 2 GB/s sequential.

## 7. Working agreement for Claude Code

- Work in small commits, each leaving `make test` green on `local`.
- Add the test with the code. `internal/proto` and `internal/pager` are pure and
  must have table-driven unit tests; `internal/link` gets an in-process
  loopback test that runs without any environment.
- No cgo before Week 3, day 13. No kernel modules before Week 3. No policy
  cleverness (LRU variants, ML prefetch, compression) before a measurement in
  `bench/results/` shows the simple version is the bottleneck.
- Never run anything as root, load modules, or touch `env/tb` hosts without
  showing the exact command first and waiting for approval.
- When a design question comes up, write the options in `docs/decisions.md`
  with a recommendation, then proceed with the recommendation unless told
  otherwise. Do not block on it.
- Static binaries (`CGO_ENABLED=0`) for everything that deploys.
- If a day's task is done early, run the benchmark on every available env and
  commit results. Do not start the next day's task.
- If a day's task is not done, note what blocked it at the top of this file
  under "Status" and propose a cut. Scope shrinks; deadline does not move.

## Status

> (update at the end of each session: date, what shipped, what is blocked)_

**2026-09-11 — Day 1 done except the TB hardware pre-check.**
Shipped: Makefile; `internal/proto` (header, ops, Encode/Decode; round-trip,
golden, error, alloc and fuzz tests); `Link` interface; `env/local` netns
scripts; multi-stage Dockerfile (`make docker-test` = compile + vet + lint +
env + tests); Lima VM (`make vm-up`) with userfaultfd and KVM verified; CI
workflow; `docs/decisions.md`.
Open: TB hardware pre-check on both hosts (needs approval to run there); CI
workflow not yet run on GitHub (no remote).

**2026-09-15 — Day 2 done except the commit.**
Shipped: `cmd/memsrv` + `internal/memsrv` (mmap'd, mlocked buffer; goroutine
per connection; `FlagErr` for out-of-range pages; a malformed frame closes the
connection; one log line per connection); `internal/env` (env.conf parser);
`internal/link/tcp` Client (`Dial(ctx, addr)`, deadline from ctx, sticky
broken-link error, mutex for concurrent callers, 0 allocs per call); tests for
all Day 2 items plus concurrency, with Get/Put tests mutation-checked;
`cmd/cli0` run from `fa` against memsrv in `fb`: 10k pages each way verified,
~70–86 µs/page put and ~78 µs/page get (veth inside the Lima VM, a sanity
number; see decisions).
Open: commit Day 2 work; CI still not run on GitHub (no remote); TB hardware
pre-check on both hosts still pending.

**2026-09-15 — Day 3 done, on branch `day3`.**
Shipped: `internal/bench` (nearest-rank percentiles, §6 `Result` writer, git
commit and dirty checks, `rtt`/`get_seq`/`get_rand`/`put_seq` runners) with
tests; `cmd/ultrabench`; `bench/plot.py` → `bench/plots/<mode>.png`;
`make docker-bench` (ultrabench in `fa` vs memsrv in `fb` via `env/local/run.sh`)
and `make docker-plot`. First results at 1a32c5f, local/tcp: p50 ~69–71 µs,
p99 ~111–129 µs, ~55 MB/s for every mode (VM veth baseline). Filename, plots
location and unit choices in decisions.
Open: merge `day3`; CI still not run on GitHub (no remote); TB hardware
pre-check on both hosts still pending.

---

## 8. Three-week plan — 15 sessions × 3–4 h

Each day: same shape. Setup → build blocks → tests → verify → close.
Every day ends by updating **Status** at the top of this file.
Done when every box is checked or explicitly moved to Status as blocked.

### Week 1 — transport, bench, ops (no faults yet)

#### Day 1 — skeleton

Goal: repo compiles, tests run, `local` env exists.

Setup

- [x] `go mod init`, layout from §4, `.gitignore` (binaries, `bench/*.png` excluded, `bench/results/*.json` included)
- [x] `Makefile`: `build test lint env-local-up env-local-down`
- [x] Dev on macOS: `Dockerfile`, `make docker-test`/`dev`/`docker-bench`, Lima VM via `make vm-up`
- [ ] TB hardware pre-check on both hosts (show commands first): kernel ≥ 6.6, TB4/USB4 controller in `lspci`, `modprobe thunderbolt-net`, `thunderbolt0` appears with the cable in; record in `env/tb/README.md`

`internal/proto`

- [x] Header struct + `Encode`/`Decode` (32 bytes, little-endian, CRC-32C over bytes 0..27)
- [x] Op constants `PING PONG GET GETR PUT PUTA`, error flag bit
- [x] Table-driven round-trip tests; fuzz test on `Decode`

`internal/link`

- [x] `Link` interface as in §3, doc comment stating the 4 KB contract

`env/local`

- [x] Script: two netns `fa`/`fb`, veth pair, 10.99.0.1/2, MTU configurable (`env/local/env.conf`)
- [x] `make env-local-up` idempotent; `-down` cleans fully

Close

- [x] CI script runs `make test lint`; green
- [x] `docs/decisions.md` created with entry: "custom wire protocol vs NBD" (see §3 note)
- [x] Update Status

#### Day 2 — tcp link + memsrv

Goal: a client in netns A can Get/Put pages from a memsrv in netns B.

Setup

- [x] `git pull`, `make test` green, `make env-local-up` works

`cmd/memsrv`

- [x] Flags: `-listen`, `-size MB`, `-env`
- [x] Allocate `[]byte`, `mlock`, fail loudly if denied
- [x] Accept loop, goroutine per connection, dispatch GET/PUT/PING
- [x] Bounds check `page`; error flag rather than panic
- [x] One log line per connection, none per request

`internal/link/tcp`

- [x] `Dial(ctx, addr)` (no opts needed yet); single connection, `TCP_NODELAY`
- [x] `Get`/`Put`/`Ping`: write header, read reply, match `seq`
- [x] Deadline from `ctx`; timeout or seq mismatch → error, link marked broken
- [x] Zero allocations per call (check with `-benchmem`)

Tests

- [x] Loopback: in-process memsrv on `127.0.0.1:0`, 1k random pages Get/Put verified
- [x] Get of never-written page returns zeros
- [x] Out-of-range page → error, link still usable
- [x] Server closes → next call errors, no hang
- [x] `go test -race ./...` clean

Verify on `local`

- [x] memsrv in `fb`; ad-hoc client in `fa` Gets/Puts 10k pages; note wall time in decisions

Close

- [x] Commit, CI green, update Status

#### Day 3 — ultrabench v1

Goal: one command produces a comparable JSON result.

`cmd/ultrabench`

- [x] Flags: `-env`, `-link`, `-mode` (comma list or `all`), `-pages`, `-out bench/results/`
- [x] Modes `rtt`, `get_seq`, `get_rand`, `put_seq`
- [x] Latency histogram (sorted slice, nearest rank) → p50/p90/p99/p999/max
- [x] JSON exactly per §6; `git` field from `git rev-parse --short HEAD`
- [x] Refuse to run if working tree is dirty (results must map to a commit); ignore `bench/results/` itself, or the first result blocks the next run

`bench/plot.py`

- [x] Reads all JSON in `bench/results/`, one PNG per mode (`bench/plots/`), series = env×link
- [x] matplotlib only; runs with `make plot` (`make docker-plot` on the Mac)

Tests

- [x] Unit test for percentile math on a known distribution
- [x] Unit test that JSON output validates against §6 field list

Verify on `local`

- [x] Run all four modes via `make docker-bench`; commit results + PNG

Close

- [x] Update Status

#### Day 4 — deploy + first real hosts

Goal: same command runs on `local`, `lan`, `tb`.

`deploy/`

- [ ] `push.sh <host> <role>`: `CGO_ENABLED=0` build, rsync to `/opt/ultramem`, install unit, restart
- [ ] `memsrv.service` (Restart=always, `LimitMEMLOCK=infinity`)
- [ ] `env/lan/`, `env/tb/` configs (addresses, interface, MTU, size)

Hosts (show commands before running)

- [ ] `modprobe thunderbolt-net` on one host; confirm `thunderbolt0` on both
- [ ] `ip link set thunderbolt0 mtu 65520 up` both sides; static IPs from `env/tb`
- [ ] Record kernel, controller, MTU actually accepted in `env/tb/README.md`

Verify

- [ ] ultrabench all modes on `lan` and `tb`; commit results
- [ ] Numbers into `docs/decisions.md`: RTT and MB/s per env

Close

- [ ] Update Status

#### Day 5 — hardening + optional baseline

Goal: `v0.1`; memsrv survives a week unattended.

Hardening

- [ ] tcp link: reconnect with backoff; in-flight call returns error, next call retries
- [ ] Graceful shutdown on SIGTERM both binaries
- [ ] `/debug` HTTP: counters (requests, errors, bytes), `pprof`
- [ ] `-timeout` flag threaded through `ctx`

Baseline (required: the Day 15 report must compare against it)

- [ ] `cmd/nbdsrv` on an existing Go NBD library, exporting the same buffer
- [ ] On A: `nbd-client`, `mkswap`, `swapon`; run a memory hog under `systemd-run -p MemoryMax=…`
- [ ] Record throughput as the "no custom kernel code" baseline

Close

- [ ] Tag `v0.1`; update Status with Week 1 summary

### Week 2 — the pager, then a microVM

#### Day 6 — uffd wrappers + process-level pager

Goal: a process's memory is served from B, fault by fault.

`internal/uffd`

- [ ] `Open()` (`UFFD_USER_MODE_ONLY` off; needs sysctl or CAP_SYS_PTRACE — document)
- [ ] `API()` handshake; `Register(range, MISSING)`; `ReadEvents()`; `Copy(dst, src)`
- [ ] Unit test with a self-registered region in the test process

`cmd/pager` (`-target process`)

- [ ] Create memfd of `-region MB`; register uffd on it
- [ ] Spawn `cmd/touch` with memfd inherited (fd 3) and region size in env
- [ ] Event loop on a `LockOSThread` goroutine: fault → `Get` from memsrv → `Copy`
- [ ] Preallocated page buffer; zero allocations per fault

`cmd/touch`

- [ ] Static binary; maps fd 3; modes `seq`/`rand`; writes a per-page pattern then verifies
- [ ] Prints per-access latency percentiles as JSON on stdout

Verify on `local`

- [ ] touch region 256 MB, memsrv 512 MB; completes with zero verify errors

Close

- [ ] Update Status

#### Day 7 — eviction + writeback

Goal: working set larger than local budget runs correctly.

`internal/pager`

- [ ] Page table: resident set, CLOCK hand, per-page reference bit
- [ ] `-resident MB` budget; on fault above budget → pick victim
- [ ] Writeback queue (bounded channel, N workers): `Put` victim to B, then hole-punch memfd
- [ ] Treat every resident page as dirty (no WP tracking) — note in decisions
- [ ] Counters: faults, evictions, writeback queue depth

Tests

- [ ] CLOCK unit tests: victim choice, reference bit clearing
- [ ] Integration: region 2× budget, seq and rand, verify pattern intact

Verify on `local`

- [ ] faults/evictions match expectation for seq (≈ region/4K each) within 5 %

Close

- [ ] Update Status

#### Day 8 — guest modes + measure

Goal: pager numbers on all three envs.

`cmd/ultrabench`

- [ ] Modes `guest_seq`, `guest_rand`: launch pager+touch, collect touch JSON, merge with pager counters
- [ ] Flags `-region`, `-resident` pass through

Measure

- [ ] Run both modes on `local`, `lan`, `tb`; commit
- [ ] If `tb` p50 miss > 20 µs: `pprof` the pager, check allocs/fault, `GOGC`, thread pinning — record findings before changing anything
- [ ] Add sequential prefetch (next 8 pages) only if profile shows RTT-bound; re-measure

Close

- [ ] Update Status

#### Day 9 — microVM, fetch only

Goal: a Firecracker guest boots with its RAM served from B.

Investigate (write answers in `docs/firecracker.md`)

- [ ] Firecracker version installed; uffd handler socket protocol (what it sends: uffd fd + region list)
- [ ] Is guest memory an anonymous private mapping or a shareable memfd in this version?
- [ ] Which events besides PAGEFAULT must be handled (REMOVE on balloon?)

`cmd/pager` (`-target firecracker -sock path`)

- [ ] Unix socket listener; receive fd + JSON regions; register handler
- [ ] Serve MISSING faults with `Get` from memsrv (memsrv pre-loaded with the snapshot `.mem` file via a `memsrv -load` flag)

`deploy/firecracker/`

- [ ] Minimal kernel + rootfs; `boot.sh`, `snapshot.sh`, `restore-uffd.sh`

Verify on `local`

- [ ] Guest restores to a shell; every page came through the pager (faults ≈ touched pages)

Close

- [ ] Update Status

#### Day 10 — VM eviction decision + `v0.2`

Goal: decide how VM pages are evicted; tag Week 2.

Decide (in `docs/decisions.md`, with a recommendation)

- [ ] (a) Firecracker guest memory as memfd the pager can hole-punch — config only?
- [ ] (b) Cloud Hypervisor `shared=on` file backing — config only?
- [ ] (c) defer VM eviction; process-level pager remains the eviction testbed
- [ ] Implement (a) or (b) only if it needs no VMM fork

Measure

- [ ] Copy `cmd/touch` into the rootfs; run `guest_seq`/`guest_rand` inside the VM via ultrabench
- [ ] Commit results

Close

- [ ] Half-page "what we learned" in `docs/week2.md`
- [ ] Tag `v0.2`; update Status

### Week 3 — below the network stack

#### Day 11 — rawl2 link

Goal: same benchmark, no IP.

`internal/link/rawl2`

- [ ] AF_PACKET SOCK_RAW, EtherType `0x88B5`, bound to `-iface`
- [ ] Frame = proto header + up to (MTU − 14 − 32) payload; 4 KB always fits at MTU 65520
- [ ] Sequence numbers; credit window of `-window` frames; retransmit after `-rto`
- [ ] Pure Go via `x/sys/unix`; requires `CAP_NET_RAW` — document in `env/tb/README.md`

Tests

- [ ] Loopback test on a veth pair in `local` (no IP configured)
- [ ] Drop-injection test: randomly drop 1 % of frames in a test hook; transfers still verify

Verify on `tb`

- [ ] ultrabench `rtt`, `get_*`, `put_seq` with `-link rawl2`; commit next to tcp

Close

- [ ] Update Status

#### Day 12 — compare and decide

Goal: a written verdict on tcp vs rawl2.

Measure

- [ ] Every mode, both links, `tb`, three repeats each; commit
- [ ] `make plot`; PNGs committed

Decide (`docs/decisions.md`)

- [ ] If rawl2 < 20 % better on the miss path: stop investing; keep as optional link
- [ ] If syscalls dominate the profile: add `PACKET_MMAP` TPACKET_v3 receive ring, re-measure
- [ ] Record per-miss time breakdown: fault entry, link RTT, copy, resume

Close

- [ ] Update Status

#### Day 13 — XDomain exploration (read-only day)

Goal: know what the raw path offers on our hardware. No ultramem code changes.

Read (write `docs/xdomain.md`)

- [ ] `drivers/thunderbolt/dma_test.c`: service registration, ring alloc, HopID/paths — what a minimal service driver needs
- [ ] `hellas-ai/thunderbolt-ibverbs`: supported controllers, build steps, known quirks (E2E ring limits)
- [ ] Our controllers on A and B (`lspci`, `/sys/bus/thunderbolt`): are they on the supported list?

Try (show commands; wait for approval)

- [ ] Build thunderbolt-ibverbs against the running kernel on both hosts
- [ ] Load; confirm `usb4_rdma*` in `ibv_devices`
- [ ] Run its perftest for RTT and bandwidth; record numbers

Close

- [ ] `docs/xdomain.md` ends with: works / does not work / partially, and expected RTT
- [ ] Update Status

#### Day 14 — verbs link (exploratory)

Goal: a benchmark result for the raw path, even if the result is "could not run".

If Day 13 produced a working device

- [ ] `internal/link/verbs`: cgo over libibverbs behind `//go:build verbs`
- [ ] RC queue pair; `Get` = one-sided READ, `Put` = WRITE + immediate for ack
- [ ] memsrv registers its buffer as an MR; exchange rkey over the tcp link at startup
- [ ] ultrabench `rtt`, `get_*` on `tb`; commit

If it did not

- [ ] Minimal service module from `dma_test.c`: register service UUID, one TX/RX ring, char device with write→send / read→recv
- [ ] Measure ring round trip with a C ping-pong; record in `docs/xdomain.md`

Close

- [ ] Update Status

#### Day 15 — close out

Goal: `v0.3` and a report someone else could act on.

Results

- [ ] Final table env × link × mode from `bench/results/`; regenerate plots
- [ ] Per-miss breakdown for the best link

Report (`docs/report.md`, one page)

- [ ] Which link to keep and why
- [ ] What limits the miss path now
- [ ] Next 3 weeks: 2 MB pages, WP-based dirty tracking, VM eviction, macOS side, inference cold-KV tier

Housekeeping

- [ ] Replace estimates in §2 with measured numbers
- [ ] Tag `v0.3`; update Status

## 9. Risks and pre-decided responses

| Risk | Response |
|---|---|
| Thunderbolt link unstable / driver quirks | `lan` env keeps the pager work moving; note quirk in decisions |
| Firecracker uffd memory not shareable → no VM eviction | Day 10 option (c); VM eviction becomes next-cycle work |
| Go GC/scheduler on fault path costs > 3 µs | preallocate, `GOGC=off` + `SetMemoryLimit`, `LockOSThread`; revisit only after profiling |
| thunderbolt-ibverbs doesn't load on our controller | Day 14 fallback; report is still the deliverable |
| Scope creep into policy work | §7: no policy without a measurement |

## Coding generation rules

- `structs` stay on top part of file (after const), both exported and private
- when allocated any struct, fields should be one line each
- also private methods should carry comments
