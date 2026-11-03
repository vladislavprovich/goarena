//go:build !windows && !linux

package main

// platformMachineInfo has no portable way to read the CPU model or the RAM
// size on this platform; the UI shows them as unknown and the memory guard
// falls back to a fixed budget.
func platformMachineInfo(m *MachineInfo) {}
