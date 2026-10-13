//go:build linux

package gcstats

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func readProcess() ProcessInfo {
	var info ProcessInfo
	if f, err := os.Open("/proc/self/status"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			var dst *uint64
			switch {
			case strings.HasPrefix(line, "VmRSS:"):
				dst = &info.RSSBytes
			case strings.HasPrefix(line, "VmHWM:"):
				dst = &info.PeakRSSBytes
			default:
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					*dst = kb << 10
				}
			}
		}
		f.Close()
	}
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		info.UserSeconds = float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6
		info.KernelSeconds = float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
	}
	return info
}
