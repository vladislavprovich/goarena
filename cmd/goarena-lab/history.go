package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// historyIDRe matches session ids: <yyyymmdd-hhmmss>_<slug>.
var historyIDRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}_[a-z0-9][a-z0-9-]{0,63}$`)

var errBadID = errors.New("invalid session id")

// History stores finished sessions as JSON files in <results>/lab.
type History struct {
	dir   string
	mu    sync.Mutex
	cache map[string]histCached
}

type histCached struct {
	mod   time.Time
	size  int64
	entry HistoryEntry
}

// HistoryEntry is the list view of a stored session.
type HistoryEntry struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Preset      string    `json:"preset,omitempty"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at,omitzero"`
	Impls       []string  `json:"impls"`
	Runs        int       `json:"runs"`
	RunsDone    int       `json:"runs_done"`
	N           int       `json:"n"`
	ValueSize   int       `json:"value_size"`
	Profile     string    `json:"profile,omitempty"`
	SweepParam  string    `json:"sweep_param,omitempty"`
	SweepValues []float64 `json:"sweep_values,omitempty"`
	SizeBytes   int64     `json:"size_bytes"`
}

func newHistory(dir string) *History { return &History{dir: dir, cache: map[string]histCached{}} }

func (h *History) path(id string) (string, error) {
	if !historyIDRe.MatchString(id) {
		return "", errBadID
	}
	return filepath.Join(h.dir, id+".json"), nil
}

// Exists reports whether a session file with this id exists.
func (h *History) Exists(id string) bool {
	p, err := h.path(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Save writes a session file atomically.
func (h *History) Save(id string, data []byte) error {
	p, err := h.path(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Read returns the raw session file.
func (h *History) Read(id string) ([]byte, error) {
	p, err := h.path(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Delete removes a session file.
func (h *History) Delete(id string) error {
	p, err := h.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.cache, id)
	h.mu.Unlock()
	return nil
}

// historyHeader is the part of a session file the list needs.
type historyHeader struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Preset     string     `json:"preset"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
	Config     RunConfig  `json:"config"`
	Sweep      *SweepSpec `json:"sweep"`
	Runs       []struct {
		State string `json:"state"`
	} `json:"runs"`
}

// List returns the stored sessions, newest first. Files that cannot be
// parsed are skipped.
func (h *History) List() ([]HistoryEntry, error) {
	des, err := os.ReadDir(h.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []HistoryEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []HistoryEntry{}
	seen := map[string]bool{}
	for _, de := range des {
		id, ok := strings.CutSuffix(de.Name(), ".json")
		if !ok || de.IsDir() || !historyIDRe.MatchString(id) {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		seen[id] = true
		if c, ok := h.cache[id]; ok && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			out = append(out, c.entry)
			continue
		}
		b, err := os.ReadFile(filepath.Join(h.dir, de.Name()))
		if err != nil {
			continue
		}
		var hh historyHeader
		if json.Unmarshal(b, &hh) != nil {
			continue
		}
		e := HistoryEntry{ID: id, Kind: hh.Kind, Preset: hh.Preset, Title: hh.Title, State: hh.State,
			StartedAt: hh.StartedAt, FinishedAt: hh.FinishedAt, Impls: hh.Config.Impls, Runs: len(hh.Runs),
			N: hh.Config.N, ValueSize: hh.Config.ValueSize, Profile: hh.Config.Profile, SizeBytes: fi.Size()}
		if e.Impls == nil {
			e.Impls = []string{}
		}
		for _, r := range hh.Runs {
			if r.State == stateDone {
				e.RunsDone++
			}
		}
		if hh.Sweep != nil {
			e.SweepParam, e.SweepValues = hh.Sweep.Param, hh.Sweep.Values
		}
		h.cache[id] = histCached{mod: fi.ModTime(), size: fi.Size(), entry: e}
		out = append(out, e)
	}
	for id := range h.cache {
		if !seen[id] {
			delete(h.cache, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}
