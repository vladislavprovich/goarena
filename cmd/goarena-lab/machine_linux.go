//go:build linux

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func platformMachineInfo(m *MachineInfo) {
	m.CPUModel = procField("/proc/cpuinfo", "model name")
	if v := procField("/proc/meminfo", "MemTotal"); v != "" {
		m.TotalRAMBytes = parseKiB(v)
	}
	if v := procField("/proc/meminfo", "MemAvailable"); v != "" {
		m.AvailRAMBytes = parseKiB(v)
	}
	if v := procField("/etc/os-release", "PRETTY_NAME"); v != "" {
		m.OS = strings.Trim(v, `"`)
	} else {
		m.OS = "Linux"
	}
}

// procField returns the value of the first "key: value" or "key=value"
// line of a file.
func procField(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		for _, sep := range []string{":", "="} {
			k, v, ok := strings.Cut(line, sep)
			if ok && strings.TrimSpace(k) == key {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// parseKiB parses "32768000 kB".
func parseKiB(v string) uint64 {
	f := strings.Fields(v)
	if len(f) == 0 {
		return 0
	}
	n, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024
}
