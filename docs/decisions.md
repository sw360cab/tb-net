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
farbench) runs fine there; the pager (Day 6+) and Firecracker (Day 9) cannot.
Options:

- a Lima VM with a stock Debian/Ubuntu kernel running its own Docker
daemon, keeping the same Dockerfile and make targets via `docker context`;
- do all pager work directly on the `lan`/`tb` Linux hosts;
- OrbStack, kernel config unverified.

**Current choice**: Lima can also enable nested virtualization for Firecracker on M3+ Macs with macOS 15.
Until then `internal/uffd`'s probe skips on ENOSYS instead of failing.
