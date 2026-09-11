# Decisions

One paragraph per decision, dated, newest last.

## 2026-09-11 — dev kernel for Week 2: Docker Desktop has no userfaultfd

Docker Desktop's LinuxKit kernel (6.10.14, arm64) is built with
`# CONFIG_USERFAULTFD is not set`: the syscall returns ENOSYS even in a
`--privileged` container, and there is no `/dev/kvm`. Week 1 (netns, tcp link,
farbench) runs fine there; the pager (Day 6+) and Firecracker (Day 9) cannot.
Options: (a) a Lima VM with a stock Debian/Ubuntu kernel running its own Docker
daemon, keeping the same Dockerfile and make targets via `docker context`;
(b) do all pager work directly on the `lan`/`tb` Linux hosts; (c) OrbStack,
kernel config unverified. Recommendation: (a), set up before Day 6; Lima can
also enable nested virtualization for Firecracker on M3+ Macs with macOS 15.
Until then `internal/uffd`'s probe skips on ENOSYS instead of failing.
