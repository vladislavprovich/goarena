//go:build windows

package gcstats

import (
	"syscall"
	"unsafe"
)

// processMemoryCountersEx mirrors PROCESS_MEMORY_COUNTERS_EX (psapi.h).
type processMemoryCountersEx struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

// K32GetProcessMemoryInfo is exported by kernel32.dll since Windows 7, so
// only the standard library's syscall package is needed.
var procGetProcessMemoryInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func filetimeSeconds(ft syscall.Filetime) float64 {
	return float64(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) * 100e-9
}

func readProcess() ProcessInfo {
	var info ProcessInfo
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return info
	}
	var pmc processMemoryCountersEx
	pmc.cb = uint32(unsafe.Sizeof(pmc))
	if procGetProcessMemoryInfo.Find() == nil {
		r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.cb))
		if r != 0 {
			info.RSSBytes = uint64(pmc.WorkingSetSize)
			info.PeakRSSBytes = uint64(pmc.PeakWorkingSetSize)
			info.PrivateBytes = uint64(pmc.PrivateUsage)
		}
	}
	var creation, exit, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user) == nil {
		info.UserSeconds = filetimeSeconds(user)
		info.KernelSeconds = filetimeSeconds(kernel)
	}
	return info
}
