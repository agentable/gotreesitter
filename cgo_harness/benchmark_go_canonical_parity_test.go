//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"crypto/sha256"
	"fmt"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	"github.com/agentable/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// BenchmarkParityGoCanonicalFull measures the same authenticated source and
// complete warm-parser lifecycle with gotreesitter and the exact C grammar
// loaded from languages.lock. Grammar identity and deep structural parity are
// mandatory preflights and execute before either timer starts.
func BenchmarkParityGoCanonicalFull(b *testing.B) {
	fixtures := loadCanonicalGoFixtures(b)
	goLang := grammars.GoLanguage()
	cLang := loadCanonicalGoCLanguage(b)
	preflightCanonicalGoFixtures(b, fixtures, goLang, cLang)

	for _, fixture := range fixtures {
		fixture := fixture
		b.Run(fixture.Fixture.ID, func(b *testing.B) {
			b.Run("gotreesitter", func(b *testing.B) {
				benchmarkCanonicalGoWarm(b, fixture, goLang)
			})
			b.Run("tree-sitter-c", func(b *testing.B) {
				benchmarkCanonicalCWarm(b, fixture, cLang)
			})
		})
	}
}

func TestCanonicalGoBenchmarkPreflight(t *testing.T) {
	fixtures := loadCanonicalGoFixtures(t)
	preflightCanonicalGoFixtures(t, fixtures, grammars.GoLanguage(), loadCanonicalGoCLanguage(t))
}

func loadCanonicalGoFixtures(tb testing.TB) []benchfixtures.LoadedFixture {
	tb.Helper()
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		tb.Fatal(err)
	}
	return fixtures
}

func loadCanonicalGoCLanguage(tb testing.TB) *sitter.Language {
	tb.Helper()
	cLang, err := ParityCLanguage("go")
	if err != nil {
		tb.Fatalf("load pinned Go C reference: %v", err)
	}
	lock, ok := parityCRefState.lock["go"]
	if !ok {
		tb.Fatal("pinned Go C reference is missing from languages.lock")
	}
	blob := grammars.BlobByName("go")
	if len(blob) == 0 {
		tb.Fatal("load embedded Go grammar blob: empty result")
	}
	blobSHA256 := fmt.Sprintf("%x", sha256.Sum256(blob))
	if err := benchfixtures.VerifyGoGrammarIdentity(lock.Commit, blobSHA256); err != nil {
		tb.Fatalf("Go oracle identity: %v", err)
	}
	return cLang
}

func preflightCanonicalGoFixtures(tb testing.TB, fixtures []benchfixtures.LoadedFixture, goLang *gotreesitter.Language, cLang *sitter.Language) {
	tb.Helper()
	goParser := gotreesitter.NewParser(goLang)
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		tb.Fatalf("set pinned Go C reference: %v", err)
	}
	goSuiteCoverage := make(benchfixtures.NodeKindCoverage)
	cSuiteCoverage := make(benchfixtures.NodeKindCoverage)

	for _, fixture := range fixtures {
		if err := fixture.Fixture.VerifySource(fixture.Source); err != nil {
			tb.Fatal(err)
		}
		goTree, err := goParser.Parse(fixture.Source)
		if err != nil {
			releaseCanonicalGoTree(goTree)
			tb.Fatalf("%s Go preflight: %v", fixture.Fixture.ID, err)
		}
		if err := validateCanonicalGoTree(goTree, fixture.Source, goLang); err != nil {
			releaseCanonicalGoTree(goTree)
			tb.Fatalf("%s Go preflight: %v", fixture.Fixture.ID, err)
		}

		cTree := cParser.Parse(fixture.Source, nil)
		if err := validateCanonicalCTree(cTree, fixture.Source); err != nil {
			releaseCanonicalGoTree(goTree)
			closeCanonicalCTree(cTree)
			tb.Fatalf("%s C preflight: %v", fixture.Fixture.ID, err)
		}

		goInspection, err := benchfixtures.InspectGoTree(goTree.RootNode(), goLang)
		if err != nil {
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s Go digest: %v", fixture.Fixture.ID, err)
		}
		if err := fixture.Fixture.VerifyDeepTreeDigest(goInspection.SHA256); err != nil {
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s Go digest: %v", fixture.Fixture.ID, err)
		}
		if err := fixture.Fixture.VerifyWorkloadIdentity(goTree.ParseRuntime(), goInspection.NodeKinds); err != nil {
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s Go workload identity: %v", fixture.Fixture.ID, err)
		}
		cInspection, err := canonicalCTreeInspection(cTree.RootNode())
		if err != nil {
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s C digest: %v", fixture.Fixture.ID, err)
		}
		if err := fixture.Fixture.VerifyDeepTreeDigest(cInspection.SHA256); err != nil {
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s C digest: %v", fixture.Fixture.ID, err)
		}
		if err := fixture.Fixture.VerifyNodeKindCoverage(cInspection.NodeKinds); err != nil {
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s C workload identity: %v", fixture.Fixture.ID, err)
		}
		if goInspection.SHA256 != cInspection.SHA256 {
			first := FirstDivergenceDumpV1(goTree.RootNode(), goLang, cTree.RootNode())
			releaseCanonicalGoTree(goTree)
			cTree.Close()
			tb.Fatalf("%s deep parity digest mismatch Go=%s C=%s first=%s", fixture.Fixture.ID, goInspection.SHA256, cInspection.SHA256, formatRealCorpusDivergence(first))
		}
		goSuiteCoverage.Merge(goInspection.NodeKinds)
		cSuiteCoverage.Merge(cInspection.NodeKinds)
		releaseCanonicalGoTree(goTree)
		cTree.Close()
	}
	if err := benchfixtures.VerifyGoFullParseSuiteNodeKindCoverage(goSuiteCoverage); err != nil {
		tb.Fatalf("Go workload identity: %v", err)
	}
	if err := benchfixtures.VerifyGoFullParseSuiteNodeKindCoverage(cSuiteCoverage); err != nil {
		tb.Fatalf("C workload identity: %v", err)
	}
}

func benchmarkCanonicalGoWarm(b *testing.B, fixture benchfixtures.LoadedFixture, lang *gotreesitter.Language) {
	b.Helper()
	gotreesitter.DrainArenaPools()
	parser := gotreesitter.NewParser(lang)
	warmTree, err := parser.Parse(fixture.Source)
	if err != nil {
		releaseCanonicalGoTree(warmTree)
		b.Fatalf("warm parse: %v", err)
	}
	if err := validateCanonicalGoTree(warmTree, fixture.Source, lang); err != nil {
		releaseCanonicalGoTree(warmTree)
		b.Fatalf("warm parse: %v", err)
	}
	warmTree.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.Source)))
	b.ResetTimer()
	var lastRuntime gotreesitter.ParseRuntime
	for i := 0; i < b.N; i++ {
		tree, err := parser.Parse(fixture.Source)
		if err != nil {
			releaseCanonicalGoTree(tree)
			b.Fatalf("parse: %v", err)
		}
		if err := validateCanonicalGoTree(tree, fixture.Source, lang); err != nil {
			releaseCanonicalGoTree(tree)
			b.Fatal(err)
		}
		if i == b.N-1 {
			lastRuntime = tree.ParseRuntime()
		}
		tree.Release()
	}
	b.StopTimer()
	reportCanonicalGoRuntime(b, lastRuntime)
}

func benchmarkCanonicalCWarm(b *testing.B, fixture benchfixtures.LoadedFixture, lang *sitter.Language) {
	b.Helper()
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(lang); err != nil {
		b.Fatalf("set pinned Go C reference: %v", err)
	}
	warmTree := parser.Parse(fixture.Source, nil)
	if err := validateCanonicalCTree(warmTree, fixture.Source); err != nil {
		closeCanonicalCTree(warmTree)
		b.Fatalf("warm parse: %v", err)
	}
	warmTree.Close()

	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.Source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree := parser.Parse(fixture.Source, nil)
		if err := validateCanonicalCTree(tree, fixture.Source); err != nil {
			closeCanonicalCTree(tree)
			b.Fatal(err)
		}
		tree.Close()
	}
}

func validateCanonicalGoTree(tree *gotreesitter.Tree, source []byte, lang *gotreesitter.Language) error {
	if tree == nil || tree.RootNode() == nil {
		return fmt.Errorf("parse returned nil tree or root")
	}
	root := tree.RootNode()
	if root.StartByte() != 0 || root.EndByte() != uint32(len(source)) {
		return fmt.Errorf("root bytes=%d..%d want=0..%d (%s)", root.StartByte(), root.EndByte(), len(source), tree.ParseRuntime().Summary())
	}
	if root.HasError() {
		return fmt.Errorf("root %q has errors (%s)", root.Type(lang), tree.ParseRuntime().Summary())
	}
	if tree.ParseStoppedEarly() {
		return fmt.Errorf("parse stopped early (%s)", tree.ParseRuntime().Summary())
	}
	return nil
}

func validateCanonicalCTree(tree *sitter.Tree, source []byte) error {
	if tree == nil || tree.RootNode() == nil {
		return fmt.Errorf("parse returned nil tree or root")
	}
	root := tree.RootNode()
	if root.StartByte() != 0 || root.EndByte() != uint(len(source)) {
		return fmt.Errorf("root bytes=%d..%d want=0..%d", root.StartByte(), root.EndByte(), len(source))
	}
	if root.HasError() {
		return fmt.Errorf("root %q has errors", root.Kind())
	}
	return nil
}

func canonicalCTreeInspection(root *sitter.Node) (benchfixtures.DeepTreeInspection, error) {
	digest := benchfixtures.NewDeepTreeDigest()
	if err := addCanonicalCDigestNode(digest, root, ""); err != nil {
		return benchfixtures.DeepTreeInspection{}, err
	}
	return digest.Inspection()
}

func addCanonicalCDigestNode(digest *benchfixtures.DeepTreeDigest, node *sitter.Node, field string) error {
	if node == nil {
		return fmt.Errorf("nil C node")
	}
	start := node.StartPosition()
	end := node.EndPosition()
	if err := digest.Add(benchfixtures.DeepNode{
		Type:        node.Kind(),
		Field:       field,
		StartByte:   uint32(node.StartByte()),
		EndByte:     uint32(node.EndByte()),
		StartRow:    uint32(start.Row),
		StartColumn: uint32(start.Column),
		EndRow:      uint32(end.Row),
		EndColumn:   uint32(end.Column),
		Named:       node.IsNamed(),
		Extra:       node.IsExtra(),
		Missing:     node.IsMissing(),
		Error:       node.IsError(),
		HasError:    node.HasError(),
		ChildCount:  uint32(node.ChildCount()),
	}); err != nil {
		return err
	}
	for i := uint32(0); i < uint32(node.ChildCount()); i++ {
		if err := addCanonicalCDigestNode(digest, node.Child(uint(i)), node.FieldNameForChild(i)); err != nil {
			return err
		}
	}
	return nil
}

func reportCanonicalGoRuntime(b *testing.B, rt gotreesitter.ParseRuntime) {
	b.Helper()
	b.ReportMetric(float64(rt.MaxStacksSeen), "max_stacks")
	b.ReportMetric(float64(rt.PeakStackDepth), "peak_depth")
	b.ReportMetric(float64(rt.TokensConsumed), "tokens/op")
	b.ReportMetric(float64(rt.MultiStackIterations), "multi_iters/op")
	b.ReportMetric(float64(rt.MultiStackTokens), "multi_tokens/op")
	b.ReportMetric(float64(rt.NodesAllocated), "nodes/op")
	b.ReportMetric(float64(rt.ArenaBytesAllocated), "arena_B/op")
}

func releaseCanonicalGoTree(tree *gotreesitter.Tree) {
	if tree != nil {
		tree.Release()
	}
}

func closeCanonicalCTree(tree *sitter.Tree) {
	if tree != nil {
		tree.Close()
	}
}
