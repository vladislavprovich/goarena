package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, backend string) (*server, *httptest.Server) {
	t.Helper()
	b, err := newBackend(config{backend: backend, shards: 16, chunkMiB: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(b)
	ts := httptest.NewServer(s.handler())
	t.Cleanup(func() { ts.Close(); _ = b.Close() })
	return s, ts
}

func do(t *testing.T, method, url string, body []byte) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestKVRoutes(t *testing.T) {
	for _, backend := range []string{"goarena", "gomap"} {
		t.Run(backend, func(t *testing.T) {
			s, ts := newTestServer(t, backend)
			if code, _ := do(t, "GET", ts.URL+"/kv/a", nil); code != http.StatusNotFound {
				t.Fatalf("GET missing: %d", code)
			}
			if code, _ := do(t, "PUT", ts.URL+"/kv/a", []byte("hello")); code != http.StatusNoContent {
				t.Fatalf("PUT: %d", code)
			}
			if code, body := do(t, "GET", ts.URL+"/kv/a", nil); code != 200 || string(body) != "hello" {
				t.Fatalf("GET: %d %q", code, body)
			}
			if code, _ := do(t, "PUT", ts.URL+"/kv/big", make([]byte, maxValueSize+1)); code != http.StatusRequestEntityTooLarge {
				t.Fatalf("PUT too large: %d", code)
			}
			if code, _ := do(t, "DELETE", ts.URL+"/kv/a", nil); code != http.StatusNoContent {
				t.Fatalf("DELETE: %d", code)
			}
			if code, _ := do(t, "DELETE", ts.URL+"/kv/a", nil); code != http.StatusNotFound {
				t.Fatalf("DELETE missing: %d", code)
			}
			if code, _ := do(t, "POST", ts.URL+"/kv/a", nil); code != http.StatusMethodNotAllowed {
				t.Fatalf("POST: %d", code)
			}
			_, _ = do(t, "PUT", ts.URL+"/kv/b", []byte("x"))
			s.sample()
			code, body := do(t, "GET", ts.URL+"/stats", nil)
			if code != 200 {
				t.Fatalf("stats: %d", code)
			}
			var doc map[string]any
			if err := json.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			if doc["backend"] != backend || doc["entries"].(float64) != 1 || doc["http_requests"].(float64) < 7 {
				t.Fatalf("stats doc: %v", doc)
			}
			if _, ok := doc["store"]; ok != (backend == "goarena") {
				t.Fatalf("store details presence wrong for %s", backend)
			}
		})
	}
}

func TestDashboardAndHealth(t *testing.T) {
	_, ts := newTestServer(t, "goarena")
	code, body := do(t, "GET", ts.URL+"/dashboard", nil)
	if code != 200 || !strings.Contains(string(body), "<canvas id=\"c-ops\">") {
		t.Fatalf("dashboard: %d", code)
	}
	if strings.Contains(string(body), "http://") || strings.Contains(string(body), "https://") {
		t.Fatal("dashboard must be self-contained (no external URLs)")
	}
	if code, body := do(t, "GET", ts.URL+"/stats/history", nil); code != 200 || !strings.HasPrefix(string(body), "[{") {
		t.Fatalf("history: %d %.40s", code, body)
	}
	if code, body := do(t, "GET", ts.URL+"/healthz", nil); code != 200 || string(body) != "ok\n" {
		t.Fatalf("healthz: %d", code)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/dashboard" {
		t.Fatalf("redirect: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}
