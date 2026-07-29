//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
)

func makeGoGLRCanarySource(callCount int) []byte {
	var b strings.Builder
	b.Grow(callCount * 24)
	b.WriteString("package main\n\n")
	b.WriteString("func glrCanaryBenchmark() {\n")
	for i := 0; i < callCount; i++ {
		// Each line is the generic-instantiation / type-conversion conflict
		// Foo[int](a). Production forks the generic_type/type_conversion arm
		// against the index_expression/call arm, so the runtime records GLR
		// branching. Post-flip the compact route declines this conflict and falls
		// back to production, so the assertion measures production's fork count,
		// not the compact route's single stack.
		b.WriteString("\t_ = Foo[int](a)\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func assertGLRCanaryRuntime(t *testing.T, tc parityCase, src []byte, minStacks int) {
	t.Helper()

	entry, ok := parityEntriesByName[tc.name]
	if !ok {
		t.Fatalf("[%s/%s] missing gotreesitter registry entry", tc.name, "glr-canary")
	}
	goLang := entry.Language()
	parser := gotreesitter.NewParser(goLang)
	// This assertion verifies the live production GLR engine's branching
	// telemetry. Compact admission establishes structural parity in runParityCase
	// above but intentionally does not reconstruct live stack counters.
	parser.SetAdmissionCandidateRoute(false)
	goTree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("[%s/%s] gotreesitter parse error: %v", tc.name, "glr-canary", err)
	}
	defer goTree.Release()

	root := goTree.RootNode()
	if root == nil {
		t.Fatalf("[%s/%s] nil root", tc.name, "glr-canary")
	}
	if got, want := root.EndByte(), uint32(len(src)); got != want {
		t.Fatalf("[%s/%s] root.EndByte=%d want=%d", tc.name, "glr-canary", got, want)
	}

	rt := goTree.ParseRuntime()
	if rt.Truncated || goTree.ParseStoppedEarly() {
		t.Fatalf("[%s/%s] unexpected early stop: %s", tc.name, "glr-canary", rt.Summary())
	}
	if rt.StopReason != gotreesitter.ParseStopAccepted {
		t.Fatalf("[%s/%s] stop reason=%s want=%s (%s)",
			tc.name, "glr-canary", rt.StopReason, gotreesitter.ParseStopAccepted, rt.Summary())
	}
	if root.HasError() {
		t.Fatalf("[%s/%s] root has error: type=%q %s", tc.name, "glr-canary", root.Type(goLang), rt.Summary())
	}
	if rt.ForestFastPath {
		t.Logf("[%s/%s] accepted through forest fast path: %s", tc.name, "glr-canary", rt.Summary())
		return
	}
	if rt.MaxStacksSeen < minStacks {
		t.Fatalf("[%s/%s] expected GLR branching (min=%d), maxStacks=%d %s",
			tc.name, "glr-canary", minStacks, rt.MaxStacksSeen, rt.Summary())
	}
}

// TestParityGLRCanaryGo ensures we keep one adversarial GLR canary in parity
// coverage: force branching, require C-structural parity, and assert no
// truncation/early-stop diagnostics on the Go runtime tree.
func TestParityGLRCanaryGo(t *testing.T) {
	const callCount = 200
	src := normalizedSource("go", string(makeGoGLRCanarySource(callCount)))
	tc := parityCase{name: "go", source: string(src)}

	runParityCase(t, tc, "glr-canary", src)
	assertGLRCanaryRuntime(t, tc, src, 2)
}
