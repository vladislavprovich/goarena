package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/vladislavprovich/goarena/arena"
)

func TestParseFlags(t *testing.T) {
	c, err := parseFlags([]string{"-selfload", "-keys", "10", "-backend", "gomap"})
	if err != nil || !c.selfload || c.keys != 10 || c.backend != "gomap" {
		t.Fatalf("%+v %v", c, err)
	}
	for _, args := range [][]string{{"-backend", "redis"}, {"-workers", "0"}, {"-reads", "2"}, {"-bogus"}} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	if _, err := newBackend(config{backend: "goarena", shards: 3, chunkMiB: 0}); err != nil {
		t.Fatal(err) // shards are rounded up by the store
	}
	if _, err := newBackend(config{backend: "goarena", shards: -1, chunkMiB: 1}); err == nil {
		t.Fatal("invalid shard count accepted")
	}
}

// TestServeWithSelfLoad starts the full service with the load generator,
// checks that traffic shows up in /stats and shuts it down.
func TestServeWithSelfLoad(t *testing.T) {
	c, _ := parseFlags([]string{"-addr", "127.0.0.1:0", "-selfload", "-keys", "5000", "-workers", "2", "-http-rate", "50", "-chunk", "1"})
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- serve(ctx, c, ready) }()
	addr := <-ready
	time.Sleep(1500 * time.Millisecond)
	resp, err := http.Get("http://" + addr + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	var doc statsDoc
	err = json.NewDecoder(resp.Body).Decode(&doc)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Entries < 5000 || doc.OpsTotal == 0 || doc.OpsPerSec <= 0 || doc.Latency.Count == 0 || (arena.OffHeap && doc.OffHeapBytes <= 0) {
		t.Fatalf("no traffic visible: %+v", doc)
	}
	if doc.HTTPRequests == 0 {
		t.Fatal("HTTP load generator did not run")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	// a second server on an address that is already in use must fail
	c2 := c
	c2.addr = "256.0.0.1:1"
	if err := serve(context.Background(), c2, nil); err == nil {
		t.Fatal("expected listen error")
	}
}
