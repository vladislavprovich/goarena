package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatrixDryRun(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	if err := cmdMatrix([]string{"-exp", "all", "-out", dir, "-dry", "-repeats", "3", "-e10-memlimit", "1200"}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"E1_goarena_n10000000", "E2_goarena_n5000000_v128_g16_c256", "E4_gobytes_n2000000_v4096",
		"E5_syncmap_n2000000_v128_g1_", "E6_gomap_n5000000_v128_g16_c64_zipf"} {
		if !strings.Contains(s, want) {
			t.Errorf("dry run lacks %s", want)
		}
	}
	if !strings.Contains(s, "matrix: 282 runs") {
		t.Errorf("unexpected run count: %s", strings.SplitN(s, "\n", 2)[0])
	}
	// existing results are skipped
	o := experiments[0].Runs(matrixOptions{Repeats: 1})[0]
	_ = os.WriteFile(filepath.Join(dir, runFileName(o)), []byte("{}"), 0o644)
	out.Reset()
	if err := cmdMatrix([]string{"-exp", "e1", "-out", dir, "-dry", "-repeats", "1", "-scale", "0.001"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SKIPPED E8_gomap-gogc400_n30000000", "SKIPPED E8_syncmap_n50000000", "E9_goarena_n10000000",
		"E10_gobytes_n5000000_v128_g16_c64_uniform_r90_ml1200_rep3"} {
		if !strings.Contains(s, want) {
			t.Errorf("dry run lacks %s", want)
		}
	}
	if strings.Contains(s, "SKIPPED E8_goarena") || strings.Contains(s, "SKIPPED E8_gomap_n20000000") {
		t.Error("runs that fit the budget must not be skipped")
	}
	if err := cmdMatrix([]string{"-exp", "E10", "-out", dir, "-dry"}, &out); err == nil {
		t.Fatal("E10 without -e10-memlimit must fail")
	}
	if err := cmdMatrix([]string{"-exp", "E8", "-out", dir, "-repeats", "1", "-mem-budget", "0.0001", "-scale", "0.001"}, &out); err != nil {
		t.Fatal(err) // every run is skipped, nothing is executed
	}
	if _, err := os.Stat(filepath.Join(dir, "E8_goarena_n20000_v128_g16_c64_uniform_r90_rep1.skipped.txt")); err != nil {
		t.Fatalf("skip reason file missing: %v", err)
	}
	if _, err := selectExperiments("E99"); err == nil {
		t.Fatal("unknown experiment accepted")
	}
	if scaleN(10, 0.5) != 1000 || scaleN(5000, 1) != 5000 || scaleN(1_000_000, 0.01) != 10000 {
		t.Fatal("scaleN")
	}
	var l bytes.Buffer
	cmdList(&l)
	usage(&l)
	if !strings.Contains(l.String(), "gobytes") || !strings.Contains(l.String(), "E5") {
		t.Fatal("list output")
	}
}
