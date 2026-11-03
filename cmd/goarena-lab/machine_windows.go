//go:build windows

package main

import (
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func platformMachineInfo(m *MachineInfo) {
	m.CPUModel = regString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString")
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	if procGlobalMemoryStatusEx.Find() == nil {
		if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); r != 0 {
			m.TotalRAMBytes, m.AvailRAMBytes = ms.TotalPhys, ms.AvailPhys
		}
	}
	const cv = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	name := regString(cv, "ProductName")
	build := regString(cv, "CurrentBuild")
	if b, err := strconv.Atoi(build); err == nil && b >= 22000 {
		name = strings.Replace(name, "Windows 10", "Windows 11", 1) // ProductName was never updated
	}
	if dv := regString(cv, "DisplayVersion"); dv != "" {
		name += " " + dv
	}
	if build != "" {
		name += " (build " + build + ")"
	}
	if name != "" {
		m.OS = strings.TrimSpace(name)
	} else {
		m.OS = "Windows"
	}
}

// regString reads a REG_SZ value below HKEY_LOCAL_MACHINE.
func regString(path, value string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, syscall.KEY_READ, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	v, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 512)
	size := uint32(len(buf) * 2)
	var typ uint32
	if syscall.RegQueryValueEx(key, v, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil {
		return ""
	}
	if typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ {
		return ""
	}
	n := int(size / 2)
	if n > len(buf) {
		n = len(buf)
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf[:n]))
}
