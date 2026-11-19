//go:build windows

package daemon

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"kero/internal/proto"
)

var (
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

// LocalLoad reads this machine's load, through the Win32 API: CPU busy
// percent from GetSystemTimes, and memory from GlobalMemoryStatusEx. It
// reads CPU twice, a moment apart, for its busy fraction.
func LocalLoad() proto.SystemLoad {
	used, total := memLoad()
	return proto.SystemLoad{CPU: winCPU(), MemUsed: used, MemTotal: total}
}

// memLoad reads memory used and total, through GlobalMemoryStatusEx.
func memLoad() (used, total uint64) {
	total = winMemTotal()
	used = winMemUsed()
	if used > total {
		used = total
	}
	return used, total
}

// readCPUTicks reads GetSystemTimes once: the busy and total ticks of all
// CPUs, without waiting.
func readCPUTicks() (busy, total uint64, ok bool) {
	var i, k, u windows.Filetime
	r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	if r == 0 {
		return 0, 0, false
	}
	// Kernel time includes idle; busy = (kernel + user) - idle.
	idle := uint64(i.Nanoseconds())
	total = uint64(k.Nanoseconds()) + uint64(u.Nanoseconds())
	if total < idle {
		return 0, 0, false
	}
	return total - idle, total, true
}

func winCPU() int {
	b0, t0, ok := readCPUTicks()
	if !ok {
		return 0
	}
	time.Sleep(120 * time.Millisecond)
	b1, t1, ok := readCPUTicks()
	if !ok {
		return 0
	}
	return cpuPercent(b0, t0, b1, t1)
}

// winMemoryStatus is the MEMORYSTATUSEX struct GlobalMemoryStatusEx fills.
type winMemoryStatus struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys, AvailPhys uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func winMemTotal() uint64 {
	var ms winMemoryStatus
	ms.Length = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return 0
	}
	return ms.TotalPhys
}

func winMemUsed() uint64 {
	var ms winMemoryStatus
	ms.Length = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 || ms.TotalPhys == 0 {
		return 0
	}
	used := ms.TotalPhys - ms.AvailPhys
	if used > ms.TotalPhys {
		return ms.TotalPhys
	}
	return used
}
