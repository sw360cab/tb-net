//go:build linux

package uffd

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// Kernel ABI (include/uapi/linux/userfaultfd.h); Day 6 moves to x/sys/unix.
var sysUserfaultfd = map[string]uintptr{"amd64": 323, "arm64": 282}

const (
	uffdAPI   = 0xAA
	uffdioAPI = 0xc018aa3f // _IOWR(0xAA, 0x3F, struct uffdio_api)
)

type uffdioAPIArg struct{ API, Features, Ioctls uint64 }

// TestUserfaultfdAvailable probes the current system environment, not code:
// the pager needs a kernel with userfaultfd and permission to use it
// (root, CAP_SYS_PTRACE, vm.unprivileged_userfaultfd=1).
func TestUserfaultfdAvailable(t *testing.T) {
	nr, ok := sysUserfaultfd[runtime.GOARCH]
	if !ok {
		t.Skipf("no userfaultfd syscall number for %s", runtime.GOARCH)
	}
	fd, _, errno := syscall.Syscall(nr, syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0, 0)
	switch {
	case errno == syscall.ENOSYS:
		t.Skip("userfaultfd: ENOSYS, kernel built without CONFIG_USERFAULTFD (e.g. Docker Desktop); see docs/decisions.md")
	case errno == syscall.EPERM:
		t.Skip("userfaultfd: EPERM; need root, CAP_SYS_PTRACE or vm.unprivileged_userfaultfd=1 (and no seccomp block)")
	case errno != 0:
		t.Fatalf("userfaultfd: %v", errno)
	}
	defer syscall.Close(int(fd))

	api := uffdioAPIArg{API: uffdAPI}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uffdioAPI, uintptr(unsafe.Pointer(&api))); errno != 0 {
		t.Fatalf("UFFDIO_API: %v", errno)
	}
	t.Logf("userfaultfd ok: features=%#x ioctls=%#x", api.Features, api.Ioctls)
}
