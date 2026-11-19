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
	load := proto.SystemLoad{MemTotal: winMemTotal(), MemUsed: winMemUsed()}
	load.CPU = winCPU()
	return load
}

func winCPU() int {
	read := func() (idle, kernel, user uint64, ok bool) {
		var i, k, u windows.Filetime
		r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
		if r == 0 {
			return 0, 0, 0, false
		}
		return uint64(i.Nanoseconds()), uint64(k.Nanoseconds()), uint64(u.Nanoseconds()), true
	}
	i0, k0, u0, ok := read()
	if !ok {
		return 0
	}
	time.Sleep(120 * time.Millisecond)
	i1, k1, u1, ok := read()
	if !ok {
		return 0
	}
	// Kernel time includes idle; busy = (kernel + user) - idle.
	idle := i1 - i0
	busy := (k1 - k0) + (u1 - u0) - idle
	total := (k1 - k0) + (u1 - u0)
	if busy <= 0 || total <= 0 {
		return 0
	}
	pct := int(100 * busy / total)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
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
