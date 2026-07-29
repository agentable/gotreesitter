//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"fmt"
	"os"
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type top50BenchmarkCase struct {
	name   string
	source []byte
	entry  grammars.LangEntry
	report grammars.ParseSupport
	goLang *gotreesitter.Language
	cLang  *sitter.Language
}

func BenchmarkParityTop50ParseFull(b *testing.B) {
	if !parityRunTop50() {
		b.Skip("set GTS_PARITY_MODE=top50 or exhaustive to run top-50 parity benchmarks")
	}
	for _, name := range top50BenchmarkLanguages(b) {
		name := name
		b.Run(name, func(b *testing.B) {
			tc, ok := prepareTop50BenchmarkCase(b, name)
			if !ok {
				return
			}
			verifyTop50BenchmarkStructuralParity(b, tc)

			b.Run("gotreesitter", func(b *testing.B) {
				benchmarkTop50GoParseFull(b, tc)
			})
			b.Run("tree-sitter-c", func(b *testing.B) {
				benchmarkTop50CParseFull(b, tc)
			})
		})
	}
}

func top50BenchmarkLanguages(b *testing.B) []string {
	b.Helper()
	raw := strings.TrimSpace(os.Getenv("GTS_PARITY_BENCH_LANGS"))
	if raw == "" || strings.EqualFold(raw, "top50") {
		return top50ParityLanguages
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		b.Fatalf("GTS_PARITY_BENCH_LANGS=%q did not name any languages", raw)
	}
	return out
}

func prepareTop50BenchmarkCase(b *testing.B, name string) (top50BenchmarkCase, bool) {
	b.Helper()
	if parityLanguageExcluded(name) {
		b.Skipf("language excluded by GTS_PARITY_SKIP_LANGS: %s", name)
	}
	if !top50ParityLanguageSet[name] {
		b.Skipf("%s is not in the top-50 parity set", name)
	}
	if reason := paritySkipReason(name); reason != "" {
		b.Skipf("known mismatch: %s", reason)
	}
	if !hasDedicatedSample[name] {
		b.Skip("no dedicated smoke sample")
	}
	entry, ok := parityEntriesByName[name]
	if !ok {
		b.Fatalf("missing registry entry for %q", name)
	}
	report, ok := paritySupportForName(name)
	if !ok {
		b.Fatalf("missing parse support report for %q", name)
	}
	if report.Backend == grammars.ParseBackendUnsupported {
		b.Skipf("unsupported parse backend for %q", name)
	}
	cLang, err := ParityCLanguage(name)
	if err != nil {
		if skipReason := parityReferenceSkipReason(err); skipReason != "" {
			b.Skipf("skip C reference parser: %s", skipReason)
		}
		b.Fatalf("load C parser: %v", err)
	}
	return top50BenchmarkCase{
		name:   name,
		source: normalizedSource(name, grammars.ParseSmokeSample(name)),
		entry:  entry,
		report: report,
		goLang: entry.Language(),
		cLang:  cLang,
	}, true
}

func verifyTop50BenchmarkStructuralParity(b *testing.B, tc top50BenchmarkCase) {
	b.Helper()
	goParser := gotreesitter.NewParser(tc.goLang)
	goTree := parseTop50GoFull(b, tc, goParser)
	defer releaseGoTree(goTree)

	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(tc.cLang); err != nil {
		if skipReason := parityReferenceSkipReason(err); skipReason != "" {
			b.Skipf("skip C reference parser SetLanguage: %s", skipReason)
		}
		b.Fatalf("C SetLanguage: %v", err)
	}
	cTree := parseTop50CFull(b, cParser, tc.source)
	defer cTree.Close()

	var errs []string
	compareNodes(goTree.RootNode(), tc.goLang, cTree.RootNode(), "root", &errs)
	if len(errs) == 0 {
		return
	}
	b.Fatalf("structural parity mismatch before benchmark for %s: %s", tc.name, firstTop50BenchmarkLines(errs, 12))
}

func benchmarkTop50GoParseFull(b *testing.B, tc top50BenchmarkCase) {
	parser := gotreesitter.NewParser(tc.goLang)
	b.ReportAllocs()
	b.SetBytes(int64(len(tc.source)))
	b.ResetTimer()

	var arenaBytes uint64
	var nodes uint64
	var compactLeafMaterialized uint64
	var pendingParentMaterialized uint64
	var transientParents uint64
	var transientChildPointers uint64
	var normalizationRewrites uint64
	for i := 0; i < b.N; i++ {
		tree := parseTop50GoFull(b, tc, parser)
		rt := tree.ParseRuntime()
		arenaBytes += uint64(rt.ArenaBytesAllocated)
		nodes += uint64(rt.NodesAllocated)
		compactLeafMaterialized += uint64(rt.CompactFullLeafMaterialized)
		pendingParentMaterialized += uint64(rt.PendingParentMaterialized)
		transientParents += uint64(rt.TransientParentNodesMaterialized)
		transientChildPointers += uint64(rt.TransientChildPointersMaterialized)
		normalizationRewrites += uint64(rt.NormalizationNodesRewritten)
		releaseGoTree(tree)
	}
	if b.N > 0 {
		n := float64(b.N)
		b.ReportMetric(float64(arenaBytes)/n, "arena_B/op")
		b.ReportMetric(float64(nodes)/n, "nodes/op")
		b.ReportMetric(float64(compactLeafMaterialized)/n, "compact_leaf_mat/op")
		b.ReportMetric(float64(pendingParentMaterialized)/n, "pending_parent_mat/op")
		b.ReportMetric(float64(transientParents)/n, "transient_parent_mat/op")
		b.ReportMetric(float64(transientChildPointers)/n, "transient_child_ptr_mat/op")
		b.ReportMetric(float64(normalizationRewrites)/n, "normalization_rewrites/op")
	}
}

func benchmarkTop50CParseFull(b *testing.B, tc top50BenchmarkCase) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(tc.cLang); err != nil {
		if skipReason := parityReferenceSkipReason(err); skipReason != "" {
			b.Skipf("skip C reference parser SetLanguage: %s", skipReason)
		}
		b.Fatalf("C SetLanguage: %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(tc.source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree := parseTop50CFull(b, parser, tc.source)
		tree.Close()
	}
}

func parseTop50GoFull(tb testing.TB, tc top50BenchmarkCase, parser *gotreesitter.Parser) *gotreesitter.Tree {
	tb.Helper()
	var tree *gotreesitter.Tree
	var err error
	switch tc.report.Backend {
	case grammars.ParseBackendTokenSource:
		if tc.entry.TokenSourceFactory == nil {
			tb.Fatalf("token source backend without factory for %q", tc.name)
		}
		tree, err = parser.ParseWithTokenSource(tc.source, tc.entry.TokenSourceFactory(tc.source, tc.goLang))
	case grammars.ParseBackendDFA, grammars.ParseBackendDFAPartial:
		tree, err = parser.Parse(tc.source)
	default:
		tb.Fatalf("unsupported parse backend %q for %q", tc.report.Backend, tc.name)
	}
	if err != nil {
		tb.Fatalf("gotreesitter parse error: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		tb.Fatalf("gotreesitter returned nil tree for %q", tc.name)
	}
	if got, want := tree.RootNode().EndByte(), uint32(len(tc.source)); got != want {
		rt := tree.ParseRuntime()
		releaseGoTree(tree)
		tb.Fatalf("gotreesitter parse truncated for %q: root.EndByte=%d want=%d %s", tc.name, got, want, rt.Summary())
	}
	return tree
}

func parseTop50CFull(tb testing.TB, parser *sitter.Parser, source []byte) *sitter.Tree {
	tb.Helper()
	tree := parser.Parse(source, nil)
	if tree == nil || tree.RootNode() == nil {
		tb.Fatal("C parser returned nil tree")
	}
	if got, want := uint32(tree.RootNode().EndByte()), uint32(len(source)); got != want {
		tree.Close()
		tb.Fatalf("C parse truncated: root.EndByte=%d want=%d", got, want)
	}
	return tree
}

func firstTop50BenchmarkLines(lines []string, limit int) string {
	if len(lines) <= limit {
		return strings.Join(lines, "\n  ")
	}
	return fmt.Sprintf("%s\n  ... and %d more", strings.Join(lines[:limit], "\n  "), len(lines)-limit)
}

func TestParityTop50BenchmarkLanguageList(t *testing.T) {
	locked, err := os.ReadFile("../grammars/update_tier1_top50.txt")
	if err != nil {
		t.Fatalf("load top50 lock file: %v", err)
	}
	var want []string
	for _, line := range strings.Split(string(locked), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want = append(want, line)
	}
	if len(want) != len(top50ParityLanguages) {
		t.Fatalf("top50 list length mismatch: harness has %d, lock file has %d", len(top50ParityLanguages), len(want))
	}
	for i, name := range want {
		if top50ParityLanguages[i] != name {
			t.Fatalf("top50 list mismatch at index %d: harness has %q, lock file has %q", i, top50ParityLanguages[i], name)
		}
	}
}

func TestParityTop50ParseSmoke(t *testing.T) {
	parityRequireTop50(t, "TestParityTop50ParseSmoke")
	for _, name := range top50ParityLanguages {
		if parityLanguageExcluded(name) {
			continue
		}
		report, ok := paritySupportForName(name)
		if !ok || report.Backend == grammars.ParseBackendUnsupported || !hasDedicatedSample[name] {
			continue
		}
		tc := parityCase{name: name, source: grammars.ParseSmokeSample(name)}
		t.Run(name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			if reason := paritySkipReason(tc.name); reason != "" {
				t.Skipf("known mismatch: %s", reason)
			}
			runParityCase(t, tc, "top50-smoke", normalizedSource(tc.name, tc.source))
		})
	}
}

func TestParityTop50ParseMaterializationTrends(t *testing.T) {
	parityRequireTop50(t, "TestParityTop50ParseMaterializationTrends")

	gotreesitter.EnableRuntimeAudit(true)
	t.Cleanup(func() {
		gotreesitter.EnableRuntimeAudit(false)
	})

	for _, name := range top50ParityLanguages {
		if parityLanguageExcluded(name) {
			continue
		}
		report, ok := paritySupportForName(name)
		if !ok || report.Backend == grammars.ParseBackendUnsupported || !hasDedicatedSample[name] {
			continue
		}

		tc := parityCase{name: name, source: grammars.ParseSmokeSample(name)}
		t.Run(name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			if reason := paritySkipReason(tc.name); reason != "" {
				t.Skipf("known mismatch: %s", reason)
			}

			src := normalizedSource(tc.name, tc.source)
			tree, lang, err := parseWithGo(tc, src, nil)
			if err != nil {
				t.Fatalf("gotreesitter parse error: %v", err)
			}
			defer releaseGoTree(tree)

			root := tree.RootNode()
			rt := tree.ParseRuntime()
			divergences, firstDivergence := top50StructuralDivergenceSummary(t, tc, src, tree, lang)

			t.Logf("TOP50_PARSE_MATERIALIZATION language=%s backend=%s bytes=%d root=%q has_error=%v structural_divergences=%d first_divergence=%q stop=%s tokens=%d nodes_alloc=%d final_nodes=%d final_parents=%d final_leaves=%d max_stacks=%d arena_bytes=%d scratch_bytes=%d gss_bytes=%d parent_alloc=%d parent_retained=%d leaf_alloc=%d leaf_retained=%d compact_leaf_created=%d compact_leaf_materialized=%d compact_leaf_final=%d pending_parent_created=%d pending_parent_materialized=%d pending_parent_final=%d transient_parent_alloc=%d transient_parent_materialized=%d transient_child_slices_alloc=%d transient_child_slices_materialized=%d transient_child_ptrs_alloc=%d transient_child_ptrs_materialized=%d normalization_checked=%d normalization_run=%d normalization_rewritten=%d summary=%q",
				tc.name,
				report.Backend,
				len(src),
				root.Type(lang),
				root.HasError(),
				divergences,
				firstDivergence,
				rt.StopReason,
				rt.TokensConsumed,
				rt.NodesAllocated,
				rt.FinalNodes,
				rt.FinalParentNodes,
				rt.FinalLeafNodes,
				rt.MaxStacksSeen,
				rt.ArenaBytesAllocated,
				rt.ScratchBytesAllocated,
				rt.GSSBytesAllocated,
				rt.ParentNodesAllocated,
				rt.ParentNodesRetained,
				rt.LeafNodesAllocated,
				rt.LeafNodesRetained,
				rt.CompactFullLeafCreated,
				rt.CompactFullLeafMaterialized,
				rt.CompactFullLeafMaterializedForFinalTree,
				rt.PendingParentCreated,
				rt.PendingParentMaterialized,
				rt.PendingParentMaterializedForFinalTree,
				rt.TransientParentNodesAllocated,
				rt.TransientParentNodesMaterialized,
				rt.TransientChildSlicesAllocated,
				rt.TransientChildSlicesMaterialized,
				rt.TransientChildPointersAllocated,
				rt.TransientChildPointersMaterialized,
				rt.NormalizationPassesChecked,
				rt.NormalizationPassesRun,
				rt.NormalizationNodesRewritten,
				rt.Summary(),
			)
		})
	}
}

func top50StructuralDivergenceSummary(t *testing.T, tc parityCase, src []byte, goTree *gotreesitter.Tree, goLang *gotreesitter.Language) (int, string) {
	t.Helper()

	cLang, err := ParityCLanguage(tc.name)
	if err != nil {
		if skipReason := parityReferenceSkipReason(err); skipReason != "" {
			return 0, "skip C reference: " + skipReason
		}
		return 1, "load C parser: " + err.Error()
	}

	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		if skipReason := parityReferenceSkipReason(err); skipReason != "" {
			return 0, "skip C SetLanguage: " + skipReason
		}
		return 1, "C SetLanguage: " + err.Error()
	}
	cTree := cParser.Parse(src, nil)
	if cTree == nil || cTree.RootNode() == nil {
		return 1, "C parser returned nil tree"
	}
	defer cTree.Close()

	var errs []string
	compareNodes(goTree.RootNode(), goLang, cTree.RootNode(), "root", &errs)
	if len(errs) == 0 {
		return 0, ""
	}
	return len(errs), strings.TrimSpace(errs[0])
}
