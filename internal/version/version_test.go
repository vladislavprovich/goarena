package version

import (
	"strconv"
	"strings"
	"testing"
)

func TestModuleIsSemanticVersion(t *testing.T) {
	parts := strings.Split(Module, ".")
	if len(parts) != 3 {
		t.Fatalf("Module = %q, want MAJOR.MINOR.PATCH", Module)
	}
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || strconv.Itoa(n) != p {
			t.Fatalf("Module = %q: invalid component %q", Module, p)
		}
	}
}
