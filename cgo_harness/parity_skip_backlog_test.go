//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/agentable/gotreesitter/grammars"
)

func includeParitySkippedBacklog() bool {
	raw := strings.TrimSpace(os.Getenv("GTS_PARITY_RUN_SKIPPED_BACKLOG"))
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// TestParitySkippedBacklogFreshParse is an env-gated diagnostic that executes
// fresh parse parity for languages currently listed in paritySkips and the
// knownDegradedStructural backlog.
//
// Run manually:
//
//	GTS_PARITY_RUN_SKIPPED_BACKLOG=1 GTS_PARITY_IGNORE_KNOWN_SKIPS=1 \
//	  go test -tags treesitter_c_parity -run TestParitySkippedBacklogFreshParse -v
func TestParitySkippedBacklogFreshParse(t *testing.T) {
	if !includeParitySkippedBacklog() {
		t.Skip("set GTS_PARITY_RUN_SKIPPED_BACKLOG=1 to run skip-backlog diagnostics")
	}

	names := make([]string, 0, len(paritySkips)+len(knownDegradedStructural))
	seen := map[string]struct{}{}
	for name := range paritySkips {
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range knownDegradedStructural {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			if !hasDedicatedSample[name] {
				t.Skip("no dedicated smoke sample")
			}
			tc := parityCase{name: name, source: grammars.ParseSmokeSample(name)}
			runParityCase(t, tc, "fresh", normalizedSource(tc.name, tc.source))
		})
	}
}
