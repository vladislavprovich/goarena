// Command httpcache is an example HTTP key-value service built on
// goarena/store, with a live dashboard.
//
// Routes:
//
//	PUT    /kv/{key}   store the request body under key
//	GET    /kv/{key}   return the value (404 if absent)
//	DELETE /kv/{key}   delete the key
//	GET    /stats      JSON metrics (ops/s, latency, GC, memory, store)
//	GET    /dashboard  self-contained HTML dashboard with live charts
//
// Run with -selfload to generate traffic in-process (useful for demos):
//
//	go run ./examples/httpcache -selfload -keys 2000000
//	go run ./examples/httpcache -selfload -backend gomap   # for comparison
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

type config struct {
	addr     string
	backend  string
	shards   int
	chunkMiB int

	selfload bool
	workers  int
	keys     int
	value    int
	reads    float64
	httpRate int
}

func parseFlags(args []string) (config, error) {
	var c config
	fs := flag.NewFlagSet("httpcache", flag.ContinueOnError)
	fs.StringVar(&c.addr, "addr", "127.0.0.1:8080", "listen address")
	fs.StringVar(&c.backend, "backend", "goarena", "storage backend: goarena or gomap")
	fs.IntVar(&c.shards, "shards", 256, "number of shards")
	fs.IntVar(&c.chunkMiB, "chunk", 64, "goarena chunk size in MiB")
	fs.BoolVar(&c.selfload, "selfload", false, "generate traffic in-process")
	fs.IntVar(&c.workers, "workers", 4, "self-load worker goroutines")
	fs.IntVar(&c.keys, "keys", 1_000_000, "self-load key space")
	fs.IntVar(&c.value, "value", 256, "self-load value size in bytes")
	fs.Float64Var(&c.reads, "reads", 0.9, "self-load read share")
	fs.IntVar(&c.httpRate, "http-rate", 200, "self-load HTTP requests per second (0 = none)")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if c.backend != "goarena" && c.backend != "gomap" {
		return c, fmt.Errorf("unknown backend %q", c.backend)
	}
	if c.workers < 1 || c.keys < 1 || c.value < 8 || c.reads < 0 || c.reads > 1 {
		return c, errors.New("invalid self-load parameters")
	}
	return c, nil
}

func newBackend(c config) (Backend, error) {
	if c.backend == "gomap" {
		return newMapBackend(c.shards), nil
	}
	return newArenaBackend(c.shards, c.chunkMiB)
}

func main() {
	c, err := parseFlags(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := serve(ctx, c, nil); err != nil {
		log.Fatal(err)
	}
}

// serve runs the service until ctx is cancelled. If ready is not nil it
// receives the bound address once the listener is up.
func serve(ctx context.Context, c config, ready chan<- string) error {
	b, err := newBackend(c)
	if err != nil {
		return err
	}
	defer b.Close()
	s := newServer(b)

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.runSampler(time.Second, stopCh) }()

	srv := &http.Server{Addr: c.addr, Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	ln, err := listen(c.addr)
	if err != nil {
		close(stopCh)
		wg.Wait()
		return err
	}
	addr := ln.Addr().String()
	log.Printf("httpcache (%s backend) listening on http://%s/dashboard", b.Name(), addr)

	if c.selfload {
		wg.Add(1)
		go func() { defer wg.Done(); selfLoad(s, c, addr, stopCh) }()
	}
	if ready != nil {
		ready <- addr
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
	case err = <-errCh:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	close(stopCh)
	wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}
