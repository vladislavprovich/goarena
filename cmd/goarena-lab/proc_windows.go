//go:build windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// killProcessTree kills a harness process and its children ("taskkill /T"),
// then the process itself in case taskkill is unavailable.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
	err := p.Kill()
	if err == os.ErrProcessDone {
		return nil
	}
	return err
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
