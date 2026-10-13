package main

import (
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

func listen(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }

// rng is a xorshift64* generator, one per worker.
type rng uint64

func (r *rng) next() uint64 {
	x := uint64(*r)
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	*r = rng(x)
	return x * 2685821657736338717
}

// appendKey writes "user:" plus 16 hex digits of a scramble of i into
// dst[:0] without allocating (the load generator itself must not create
// garbage, or it would distort the GC figures on the dashboard).
func appendKey(dst []byte, i uint64) []byte {
	const hex = "0123456789abcdef"
	x := i * 0x9E3779B97F4A7C15
	dst = append(dst[:0], "user:"...)
	for s := 60; s >= 0; s -= 4 {
		dst = append(dst, hex[(x>>uint(s))&15])
	}
	return dst
}

// selfLoad populates the key space and then runs a read/write mix with
// c.workers goroutines calling the backend directly, plus a trickle of real
// HTTP requests against addr so that the HTTP path is exercised too.
func selfLoad(s *server, c config, addr string, stop <-chan struct{}) {
	start := time.Now()
	var wg sync.WaitGroup
	per := (c.keys + c.workers - 1) / c.workers
	for w := 0; w < c.workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			key := make([]byte, 0, 32)
			val := bytes.Repeat([]byte{byte('a' + w)}, c.value)
			for i := w * per; i < (w+1)*per && i < c.keys; i++ {
				select {
				case <-stop:
					return
				default:
				}
				key = appendKey(key, uint64(i))
				if err := s.set(w, i%8 == 0, key, val); err != nil {
					log.Printf("selfload populate: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	log.Printf("selfload: populated %d keys in %v", c.keys, time.Since(start).Round(time.Millisecond))

	threshold := uint64(c.reads * float64(1<<32))
	for w := 0; w < c.workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := rng(uint64(w)*0x9E3779B97F4A7C15 + 12345)
			key := make([]byte, 0, 32)
			val := make([]byte, c.value)
			buf := make([]byte, 0, c.value)
			for n := uint64(0); ; n++ {
				if n%256 == 0 {
					select {
					case <-stop:
						return
					default:
					}
				}
				x := r.next()
				key = appendKey(key, (x>>8)%uint64(c.keys))
				timed := n%8 == 0
				if x&0xffffffff < threshold {
					buf, _ = s.get(w, timed, buf, key)
				} else {
					val[0] = byte(n)
					_ = s.set(w, timed, key, val)
				}
			}
		}(w)
	}
	if c.httpRate > 0 {
		wg.Add(1)
		go func() { defer wg.Done(); httpLoad(addr, c, stop) }()
	}
	wg.Wait()
}

// httpLoad issues c.httpRate requests per second over HTTP.
func httpLoad(addr string, c config, stop <-chan struct{}) {
	client := &http.Client{Timeout: 2 * time.Second}
	t := time.NewTicker(time.Second / time.Duration(c.httpRate))
	defer t.Stop()
	r := rng(42)
	key := make([]byte, 0, 32)
	val := bytes.Repeat([]byte("h"), c.value)
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		key = appendKey(key, r.next()%uint64(c.keys))
		url := "http://" + addr + "/kv/" + string(key)
		var req *http.Request
		if r.next()%10 == 0 {
			req, _ = http.NewRequest(http.MethodPut, url, bytes.NewReader(val))
		} else {
			req, _ = http.NewRequest(http.MethodGet, url, nil)
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
