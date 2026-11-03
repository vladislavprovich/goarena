package main

import (
	"os"
	"runtime"
)

// MachineInfo describes the machine the lab (and the harness) runs on.
type MachineInfo struct {
	CPUModel      string `json:"cpu_model"`
	LogicalCPUs   int    `json:"logical_cpus"`
	TotalRAMBytes uint64 `json:"total_ram_bytes"`
	AvailRAMBytes uint64 `json:"avail_ram_bytes"` // at lab start
	OS            string `json:"os"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	Hostname      string `json:"hostname"`
	LabGoVersion  string `json:"lab_go_version"`
}

// collectMachineInfo gathers the portable part and lets the platform file
// fill in CPU model, RAM and OS name.
func collectMachineInfo() MachineInfo {
	h, _ := os.Hostname()
	m := MachineInfo{
		LogicalCPUs:  runtime.NumCPU(),
		OS:           runtime.GOOS,
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		Hostname:     h,
		LabGoVersion: runtime.Version(),
	}
	platformMachineInfo(&m)
	return m
}
