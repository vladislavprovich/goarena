package main

import (
	"path/filepath"
	"testing"
)

func TestProfileFlag(t *testing.T) {
	stop, err := startProfile(filepath.Join(t.TempDir(), "cpu.prof"))
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if _, err := startProfile(filepath.Join(t.TempDir(), "no", "such", "dir", "x")); err == nil {
		t.Fatal("expected error for bad path")
	}
}
