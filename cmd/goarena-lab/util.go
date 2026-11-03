package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// splitLines splits s into trimmed lines.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return lines
}

// trimQuotes removes surrounding spaces and one pair of double quotes or
// back quotes.
func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '`' && s[len(s)-1] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

// writeJSON writes v as an indented JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	_ = enc.Encode(v)
}

// apiError is the body of every error response.
type apiError struct {
	Error    string          `json:"error"`
	Estimate *EstimateReport `json:"estimate,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// humanCount formats a count like 2000000 as "2M" and 1500 as "1.5k".
func humanCount(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1e9:
		return trimFloat(v/1e9, 2) + "G"
	case a >= 1e6:
		return trimFloat(v/1e6, 2) + "M"
	case a >= 1e3:
		return trimFloat(v/1e3, 2) + "k"
	}
	return trimFloat(v, 2)
}

// humanBytes formats a byte size like 4096 as "4 KiB" and 100 as "100 B".
func humanBytes(v float64) string {
	switch {
	case v >= 1<<30:
		return trimFloat(v/(1<<30), 2) + " GiB"
	case v >= 1<<20:
		return trimFloat(v/(1<<20), 2) + " MiB"
	case v >= 1<<10:
		return trimFloat(v/(1<<10), 2) + " KiB"
	}
	return trimFloat(v, 0) + " B"
}

// trimFloat formats v with at most prec decimals and no trailing zeros.
func trimFloat(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// formatFloatArg formats a float for a command-line argument.
func formatFloatArg(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// finite reports whether v is neither NaN nor infinite.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
