//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
)

// killProcessTree kills a harness process. goarena-bench "run" starts no
// children, so killing the process itself is enough.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	err := p.Kill()
	if err == os.ErrProcessDone {
		return nil
	}
	return err
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}
