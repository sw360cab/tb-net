# Hosts

Hardware facts for the machines behind the envs; env READMEs point here.
Record what was measured and when, not what the spec sheet promises.

## screwdriver: Linux, host A (farbench now, pager later)

Checked 2026-09-16. ssh alias `screwdriver`.

- **OS:** LMDE 7 (gigi), kernel `6.12.107+deb13-amd64` (≥ 6.6), amd64, 8 CPUs, 15 GB RAM
- **Thunderbolt controller:** Intel JHL6240 "Alpine Ridge LP", Thunderbolt 3,
  single port, NHI at `06:00.0` (driver `thunderbolt`). PCIe 3.0 x2 behind it
  caps host bandwidth near 16 Gb/s before overhead, whatever the cable does.
- **Thunderbolt bus:** `domain0`, security `none`, `xdomain` = `Y`,
  `iommu_dma_protection` = `0` (no DMA protection: lab use only); no
  `nvm_version` exposed, so firmware version is unknown
- **thunderbolt-net:** module (`CONFIG_USB4_NET=m`), loads cleanly and registers;
  link untested: no cable yet
- **Networking:** NetworkManager manages interfaces; set `thunderbolt0` unmanaged
  before static IPs. Wi-Fi `wlp3s0` (DHCP, default route; ssh alias target).
  Wired `enp0s31f6` = `lan` B: NM profile `lan-direct`, static `10.97.0.2/24`,
  `ipv4.never-default`, cabled directly to the Mac.
- **Later prerequisites:** `/dev/kvm` present, user access through an ACL;
  `vm.unprivileged_userfaultfd` = 0; memlock 8 MB (memsrv needs
  `LimitMEMLOCK=infinity`)

## mac: macOS, host B stand-in (memsrv)

Checked 2026-09-16. The dev machine.

- **Hardware:** Apple M4, macOS 15.7.3, 32 GB RAM
- **Thunderbolt Bridge:** `bridge0` with members `en1` (Thunderbolt 1) and `en2`
  (Thunderbolt 2), valid MTU 1280–65518; members must be raised before the
  bridge accepts a larger MTU
- **Wired:** `Belkin USB-C LAN` (`en5`) = `lan` A: static `10.97.0.1/24`, no
  router, cabled directly to screwdriver. Undo: `networksetup -setdhcp`.
- **Local Network permission** (Sequoia): the app opening connections needs it
  (iTerm, VS Code), or LAN sends fail with `sendto: No route to host`.
