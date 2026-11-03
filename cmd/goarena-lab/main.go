// Command goarena-lab is the "goarena Benchmark Lab": a local web
// application that drives the benchmark harness cmd/goarena-bench.
//
// Usage:
//
//	go run ./cmd/goarena-lab [-addr 127.0.0.1:8090] [-repo DIR] [-bench FILE] [-results DIR] [-open]
//
// The lab serves a single-page UI (embedded, works offline) on a loopback
// address. It runs every selected implementation as a separate
// goarena-bench process, streams the per-second progress lines of the
// harness to the browser with Server-Sent Events, keeps a history of
// sessions in <results>/lab and compares them with the historical result
// files of earlier goarena versions. See docs/BENCHMARK_LAB.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "goarena-lab:", err)
		os.Exit(1)
	}
}

// labFlags are the command-line flags.
type labFlags struct {
	addr        string
	allowRemote bool
	repo        string
	bench       string
	results     string
	open        bool
}

func parseFlags(args []string, stderr io.Writer) (labFlags, error) {
	var f labFlags
	fs := flag.NewFlagSet("goarena-lab", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.addr, "addr", "127.0.0.1:8090", "listen address (loopback only unless -allow-remote)")
	fs.BoolVar(&f.allowRemote, "allow-remote", false, "allow binding to a non-loopback address (the lab starts processes - use with care)")
	fs.StringVar(&f.repo, "repo", "", "goarena repository root (default: walk up from the working directory to go.mod)")
	fs.StringVar(&f.bench, "bench", "", "path to a goarena-bench binary (default: build <repo>/bin/goarena-bench when missing or stale)")
	fs.StringVar(&f.results, "results", "", "results directory (default <repo>/results); history goes to <results>/lab")
	fs.BoolVar(&f.open, "open", false, "open the lab in the default browser")
	err := fs.Parse(args)
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return f, err
}

func run(args []string, stderr io.Writer) error {
	f, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	if err := checkListenAddr(f.addr, f.allowRemote); err != nil {
		return err
	}
	repo := f.repo
	if repo == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		if repo, err = findRepoRoot(wd); err != nil {
			return fmt.Errorf("%w (pass -repo)", err)
		}
	}
	if repo, err = filepath.Abs(repo); err != nil {
		return err
	}
	results := f.results
	if results == "" {
		results = filepath.Join(repo, "results")
	}
	if results, err = filepath.Abs(results); err != nil {
		return err
	}
	logger := log.New(stderr, "goarena-lab: ", log.Ltime)

	bench := newBenchManager(repo, f.bench)
	srv := newServer(serverConfig{
		Repo:        repo,
		ResultsDir:  results,
		AllowRemote: f.allowRemote,
		Bench:       bench,
		Machine:     collectMachineInfo(),
		Logger:      logger,
	})

	ln, err := net.Listen("tcp", f.addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go srv.prepare(ctx) // toolchain info + build/probe the bench binary

	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	url := "http://" + ln.Addr().String() + "/"
	logger.Printf("goarena Benchmark Lab on %s (repo %s, results %s)", url, repo, results)
	if f.open {
		if err := openBrowser(url); err != nil {
			logger.Printf("cannot open the browser: %v", err)
		}
	}

	select {
	case <-ctx.Done():
		logger.Printf("shutting down")
	case err := <-serveErr:
		srv.Shutdown(5 * time.Second)
		return err
	}
	srv.Shutdown(15 * time.Second) // cancels a running session and kills its process
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(sctx)
}

// checkListenAddr refuses addresses that are not loopback unless
// allowRemote is set: the lab starts processes on request.
func checkListenAddr(addr string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid -addr %q: %v", addr, err)
	}
	if allowRemote || isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q: not a loopback address (use 127.0.0.1, localhost or ::1, or pass -allow-remote)", addr)
}

// isLoopbackHost reports whether host (without port) names the loopback
// interface. An empty host means "all interfaces" and is not loopback.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// modulePath is the module whose root the lab looks for.
const modulePath = "github.com/vladislavprovich/goarena"

// findRepoRoot walks up from dir to the directory whose go.mod declares
// modulePath.
func findRepoRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && goModDeclares(b, modulePath) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod of module %s above the working directory", modulePath)
		}
		dir = parent
	}
}

// goModDeclares reports whether the go.mod content declares module path.
func goModDeclares(gomod []byte, path string) bool {
	for _, line := range splitLines(string(gomod)) {
		if len(line) > 7 && line[:7] == "module " {
			m := trimQuotes(line[7:])
			return m == path
		}
	}
	return false
}
