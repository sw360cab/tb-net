<p align="center">
  <img src="docs/banner.svg" width="900"
       alt="A 4 GB microVM asking for 16 GB; a 32 GB idle host sending it 4 KB pages over a Thunderbolt cable">
</p>

# ultramem

**Give a microVM more RAM than its host has, by borrowing a second machine's memory over a Thunderbolt cable.**

The guest sees one flat address space. Pages it hasn't touched live in the other
host's RAM and arrive on demand, in ~4 KB, over a 40 Gb/s cable: no swap file,
no SSD, no cluster.

```
            host A (Linux)                                 host B (lends RAM)
┌──────────────────────────────────┐                 ┌──────────────────────────┐
│  microVM sees 16 GB              │                 │                          │
│  ┌────────────────────────────┐  │                 │  memsrv                  │
│  │ guest workload             │  │                 │  12 GB locked, mlock'd   │
│  └─────────────┬──────────────┘  │                 │        ▲                 │
│                │ page fault      │                 │        │ GET/PUT 4 KB    │
│  ┌─────────────▼──────────────┐  │   Thunderbolt   │        │                 │
│  │ pager (userfaultfd)        │◄─┼─────────────────┼────────┘                 │
│  │ 4 GB resident + CLOCK      │  │   40 Gb/s       │                          │
│  └────────────────────────────┘  │   target ~12 µs │                          │
└──────────────────────────────────┘                 └──────────────────────────┘
      hot pages stay local                                 cold pages live here
```

## How a miss is served

1. The guest touches a page that isn't resident. The kernel hands the fault to
   the **pager** through `userfaultfd` instead of to swap.
2. The pager asks host B for that page: a 32-byte header plus 4 KB back.
3. It copies the page in and resumes the thread. Nothing else notices.
4. When the local budget is full, the coldest page is written back (`PUT`) and
   dropped, so hot pages keep the fast path.

Everything above the wire speaks one small interface (`Get`, `Put`, `Ping`), so
the transport underneath can be swapped: TCP today, raw Ethernet frames and
Thunderbolt DMA rings next, each measured against the same benchmark.

## Where it stands

| | p50 per 4 KB page | throughput | state |
|---|---|---|---|
| `local` (two netns, one machine) | 70 µs | 55 MB/s | ✅ measured |
| `lan` (gigabit cable between hosts) | 389 µs | 10 MB/s | ✅ measured |
| `tb` (Thunderbolt, host to host) | target ≤ 15 µs | target ≥ 2 GB/s | 🔌 awaiting cable |
| pager + microVM (end to end) | — | — | 🚧 next |

For scale: local DRAM is ~0.1 µs, NVMe swap ~80 µs. Throughput today is capped
by one page per round trip, not by the wire. Every number is produced by
`ultrabench` and committed to [`bench/results/`](bench/results) with the commit
that measured it; the reasoning lives in [`docs/decisions.md`](docs/decisions.md).

## Run it

```sh
make docker-test
make docker-bench MODE=all PAGES=65536
make docker-plot
```

The first command builds, lints and runs the suite on Linux, including a
two-namespace `local` environment with a veth pair standing in for the cable.
The second runs every benchmark mode across it; the third plots
[`bench/plots/`](bench/plots). On macOS these use a Lima VM (`make vm-up`),
because the pager needs a kernel with `userfaultfd`.

Two real hosts: `deploy/push.sh <host> b` installs memsrv as a systemd service,
then `ultrabench -env lan -mode all` measures the link.

## Layout

| path | what |
|---|---|
| `cmd/memsrv` | host B: locks a buffer, serves pages |
| `cmd/ultrabench` | the benchmark; one JSON per run |
| `internal/link` | the `Link` seam: `tcp` today, `rawl2` and `verbs` next |
| `internal/proto` | 32-byte wire header, CRC-checked |
| `internal/pager` | userfaultfd handler, CLOCK eviction (next) |
| `env/`, `deploy/` | one command per environment: `local`, `lan`, `tb` |

Plan and working agreement: [CLAUDE.md](CLAUDE.md).
