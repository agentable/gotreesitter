//go:build cgo && treesitter_c_parity

package cgoharness

// Forest-vs-C oracle gate. Unlike TestForestCorpusParity (forest vs the
// production parser), this compares the GSS-forest fast path DIRECTLY against
// tree-sitter-c, skipping the production leg entirely. That matters for
// languages whose production parse is too slow to use as the parity baseline —
// notably haskell, whose O(n^2) deep-merge blowup makes the production-vs-forest
// gate time out. Here the forest is the only gotreesitter parse we run, so a
// pathologically slow production path can't block vetting the forest.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gts "github.com/agentable/gotreesitter"
)

// TestForestVsCOracleParity asserts that, for every real-corpus file the forest
// would DISPATCH (clean, no error node, reaches the last non-whitespace byte),
// the forest tree is deeply identical to the tree-sitter-c oracle: node type,
// byte range, points, named/extra/missing/error flags, all children, and fields.
// Files the forest declines fall back to production at runtime and are NOT
// required to match C here. It also reports dispatch rate and the C-vs-forest
// wall speedup, which is the "is it worth promoting" half of the decision.
//
// The forest tree comes from ParseForestExperimental, which finalizes the root
// through the same finalizeResultRoot -> per-language compatibility pass the
// runtime forest path uses, so it is a faithful stand-in for what promotion
// would return.
//
// Heavy (real corpus + CGo) -> opt-in:
//
//	GTS_FOREST_ORACLE=1 GTS_FOREST_ORACLE_LANGS=haskell \
//	  go test ./cgo_harness -tags treesitter_c_parity -run TestForestVsCOracleParity -v
func TestForestVsCOracleParity(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GTS_FOREST_ORACLE")) == "" {
		t.Skip("set GTS_FOREST_ORACLE=1 to run the forest-vs-C oracle gate")
	}
	setup := loadForestOracleSetup(t)
	anyRun := false
	for _, raw := range setup.langs {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		lang, cLang, files, ok := setup.language(t, name)
		if !ok {
			continue
		}
		anyRun = true
		result := runForestCOracleAudit(t, name, lang, cLang, files, setup)
		if setup.resultOut != "" {
			if err := WriteForestAuditResult(setup.resultOut, result); err != nil {
				t.Fatalf("write forest C-oracle result: %v", err)
			}
		}
	}
	if !anyRun {
		t.Skip("no forest-oracle corpus available for requested languages")
	}
}

type forestOracleSetup struct {
	langs        []string
	loaders      map[string]func() *gts.Language
	manifestPath string
	resultOut    string
	manifest     ForestCorpusManifest
	files        map[string][]string
}

func loadForestOracleSetup(t *testing.T) forestOracleSetup {
	t.Helper()
	setup := forestOracleSetup{
		langs:        strings.Split(envOr("GTS_FOREST_ORACLE_LANGS", "haskell"), ","),
		loaders:      forestLanguageLoaders(),
		manifestPath: strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_MANIFEST")),
		resultOut:    strings.TrimSpace(os.Getenv("GTS_FOREST_AUDIT_RESULT_OUT")),
	}
	var err error
	setup.manifest, setup.files, err = loadForestCorpusManifestLanguages(
		setup.manifestPath,
		strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_ROOT")),
		strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LOCK_PATH")),
		strings.TrimSpace(os.Getenv("GTS_FOREST_GOTREESITTER_REVISION")),
		strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LOCK_SHA256")),
		setup.langs,
	)
	if err != nil {
		t.Fatalf("authenticate forest corpus manifest: %v", err)
	}
	if setup.resultOut != "" && len(setup.langs) != 1 {
		t.Fatal("GTS_FOREST_AUDIT_RESULT_OUT requires one authenticated language")
	}
	return setup
}

func (setup forestOracleSetup) language(t *testing.T, name string) (*gts.Language, *sitter.Language, []string, bool) {
	t.Helper()
	load, ok := setup.loaders[name]
	if !ok {
		t.Errorf("%s: unknown language (not in grammars.AllLanguages)", name)
		return nil, nil, nil, false
	}
	lang := load()
	cLang, err := ParityCLanguage(name)
	if err != nil {
		if reason := parityReferenceSkipReason(err); reason != "" {
			t.Logf("%s: skip — no C reference parser: %s", name, reason)
			return nil, nil, nil, false
		}
		t.Errorf("%s: ParityCLanguage: %v", name, err)
		return nil, nil, nil, false
	}
	files := setup.files[name]
	if len(files) == 0 {
		t.Errorf("%s: requested language absent from authenticated manifest", name)
		return nil, nil, nil, false
	}
	return lang, cLang, files, true
}

type forestCOracleAudit struct {
	t               *testing.T
	name            string
	lang            *gts.Language
	cLang           *sitter.Language
	metadata        map[string]ForestCorpusManifestFile
	result          ForestAuditResult
	total           int
	dispatched      int
	fellBack        int
	diverged        int
	cNanos          int64
	forestNanos     int64
	divergedFiles   []string
	fallbackReasons map[string]int
}

func runForestCOracleAudit(t *testing.T, name string, lang *gts.Language, cLang *sitter.Language, files []string, setup forestOracleSetup) ForestAuditResult {
	t.Helper()
	result, err := NewForestAuditResult("c_oracle", setup.manifest.GotreesitterRevision, setup.manifestPath, setup.manifest.CorpusLock.SHA256, name)
	if err != nil {
		t.Fatalf("initialize forest C-oracle result: %v", err)
	}
	audit := &forestCOracleAudit{
		t: t, name: name, lang: lang, cLang: cLang, result: result,
		metadata:        forestManifestMetadata(setup.manifest, name, files),
		fallbackReasons: map[string]int{},
	}
	for _, filePath := range files {
		audit.evaluateFile(filePath)
	}
	return audit.finish()
}

func (audit *forestCOracleAudit) evaluateFile(filePath string) {
	meta, ok := audit.metadata[filePath]
	if !ok {
		audit.t.Errorf("%s: authenticated metadata missing for %s", audit.name, filePath)
		return
	}
	src, err := os.ReadFile(filePath)
	if err != nil {
		audit.t.Errorf("%s: read %s: %v", audit.name, filePath, err)
		return
	}
	if err := validateForestAuditSourceIdentity(src, meta); err != nil {
		audit.t.Errorf("%s: authenticated source drift for %s before C-oracle parse: %v", audit.name, filePath, err)
		return
	}
	audit.total++
	notRun := forestAuditNotRunOutcome(src)
	fileResult := ForestAuditFileResult{
		Path: meta.Path, Bytes: meta.Bytes, SHA256: meta.SHA256,
		Forest: notRun, Peer: notRun, Routed: notRun, RoutedProvenance: forestAuditRouteNotRun,
	}
	defer func() { audit.result.Files = append(audit.result.Files, fileResult) }()

	forestTree, forestOK, declineReason, elapsed := parseForestWithBudget(audit.lang, src, forestOracleBudget())
	audit.forestNanos += elapsed
	if declineReason == string(gts.ParseStopTimeout) {
		audit.recordDecline("timeout", &fileResult)
		return
	}
	if forestTree != nil {
		defer forestTree.Release()
	}
	var forestRoot *gts.Node
	if forestTree != nil {
		forestRoot = forestTree.RootNode()
	}
	fileResult.Forest = forestGoTreeOutcome(forestTree, src)
	if audit.forestDeclined(src, forestOK, forestRoot, fileResult.Forest, &fileResult) {
		return
	}
	audit.dispatched++
	fileResult.Disposition = "accepted"
	audit.compareCOracle(filePath, src, forestRoot, &fileResult)
}

func parseForestWithBudget(lang *gts.Language, src []byte, budget time.Duration) (*gts.Tree, bool, string, int64) {
	parser := gts.NewParser(lang)
	timeoutMicros := uint64(budget / time.Microsecond)
	if budget > 0 && timeoutMicros == 0 {
		timeoutMicros = 1
	}
	parser.SetTimeoutMicros(timeoutMicros)
	start := time.Now()
	tree, ok := parser.ParseForestExperimental(src)
	declineReason := ""
	if !ok {
		_, _, declineReason, _ = parser.ForestDeclineInfo()
	}
	return tree, ok, declineReason, time.Since(start).Nanoseconds()
}

func TestParseForestWithBudgetStopsSynchronously(t *testing.T) {
	load := forestLanguageLoaders()["json"]
	if load == nil {
		t.Fatal("json forest language loader is unavailable")
	}
	source := []byte("[" + strings.Repeat("0,", 250_000) + "0]")
	before := forestExperimentalGoroutines(t)

	tree, ok, reason, elapsed := parseForestWithBudget(load(), source, time.Nanosecond)
	if tree != nil {
		tree.Release()
	}
	if ok {
		t.Fatal("forest parse accepted despite the one-microsecond rounded timeout")
	}
	if reason != string(gts.ParseStopTimeout) {
		t.Fatalf("forest decline reason = %q, want %q", reason, gts.ParseStopTimeout)
	}
	if elapsed > int64(time.Second) {
		t.Fatalf("timed forest parse took %s, want <= 1s", time.Duration(elapsed))
	}
	if after := forestExperimentalGoroutines(t); after != before {
		t.Fatalf("ParseForestExperimental goroutines after return = %d, before = %d", after, before)
	}
}

func forestExperimentalGoroutines(t *testing.T) int {
	t.Helper()
	stack := make([]byte, 1<<20)
	n := runtime.Stack(stack, true)
	if n == len(stack) {
		t.Fatal("goroutine stack snapshot was truncated")
	}
	return strings.Count(string(stack[:n]), ".ParseForestExperimental(")
}

func (audit *forestCOracleAudit) forestDeclined(src []byte, ok bool, root *gts.Node, outcome ForestAuditOutcome, fileResult *ForestAuditFileResult) bool {
	if ok && root != nil && outcome.Accepted && outcome.FullSpan && !root.HasError() {
		return false
	}
	reason := forestFallbackReason(ok, root, src)
	if ok && root != nil && (!outcome.Accepted || !outcome.FullSpan) {
		reason = "invalid_outcome"
	}
	audit.recordDecline(reason, fileResult)
	return true
}

func (audit *forestCOracleAudit) recordDecline(reason string, fileResult *ForestAuditFileResult) {
	audit.fellBack++
	audit.fallbackReasons[reason]++
	fileResult.Disposition = "declined"
	fileResult.Decline = reason
}

func (audit *forestCOracleAudit) compareCOracle(filePath string, src []byte, forestRoot *gts.Node, fileResult *ForestAuditFileResult) {
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(audit.cLang); err != nil {
		audit.recordDivergence(filePath, "C SetLanguage: "+err.Error(), fileResult)
		audit.t.Errorf("%s: C SetLanguage: %v", audit.name, err)
		return
	}
	start := time.Now()
	cTree := cParser.Parse(src, nil)
	audit.cNanos += time.Since(start).Nanoseconds()
	if cTree == nil || cTree.RootNode() == nil {
		fileResult.Peer = forestCTreeOutcome(cTree, src)
		fileResult.Disposition = "diverged"
		fileResult.Diff = "C oracle produced no tree"
		audit.diverged++
		audit.t.Errorf("%s: C produced no tree for %s", audit.name, filepath.Base(filePath))
		return
	}
	defer cTree.Close()
	fileResult.Peer = forestCTreeOutcome(cTree, src)
	if !fileResult.Peer.Accepted || !fileResult.Peer.FullSpan {
		audit.recordDivergence(filePath, "C oracle outcome not accepted/full-span", fileResult)
		return
	}
	if diff := completeForestCIdentityDiff(forestRoot, audit.lang, cTree.RootNode()); diff != "" {
		audit.recordDivergence(filePath, diff, fileResult)
		audit.t.Logf("%s: %s forest!=C: %s", audit.name, filepath.Base(filePath), diff)
	}
}

func (audit *forestCOracleAudit) recordDivergence(filePath, diff string, fileResult *ForestAuditFileResult) {
	audit.diverged++
	fileResult.Disposition = "diverged"
	fileResult.Diff = diff
	audit.divergedFiles = append(audit.divergedFiles, filepath.Base(filePath)+": "+diff)
}

func (audit *forestCOracleAudit) finish() ForestAuditResult {
	speedup := 0.0
	if audit.forestNanos > 0 {
		speedup = float64(audit.cNanos) / float64(audit.forestNanos)
	}
	audit.t.Logf("%-8s files=%d dispatched=%d fellback=%d diverged=%d | c=%.1fms forest=%.1fms forest_vs_c=%.2fx",
		audit.name, audit.total, audit.dispatched, audit.fellBack, audit.diverged,
		float64(audit.cNanos)/1e6, float64(audit.forestNanos)/1e6, speedup)
	if audit.fellBack > 0 {
		audit.t.Logf("%-8s fallback reasons: %s", audit.name, formatFallbackReasons(audit.fallbackReasons))
	}
	if audit.diverged > 0 {
		audit.t.Errorf("%s: %d/%d dispatched files DIVERGED from the C oracle (blocks forest promotion): %s",
			audit.name, audit.diverged, audit.dispatched, strings.Join(audit.divergedFiles, ", "))
	}
	audit.result.FilesTotal = audit.total
	audit.result.FilesAccepted = audit.dispatched
	audit.result.FilesDeclined = audit.fellBack
	audit.result.FilesDiverged = audit.diverged
	audit.result.ForestNanos = audit.forestNanos
	audit.result.PeerNanos = audit.cNanos
	if audit.diverged > 0 || audit.dispatched == 0 {
		audit.result.Status = "fail"
	}
	sort.Slice(audit.result.Files, func(i, j int) bool { return audit.result.Files[i].Path < audit.result.Files[j].Path })
	return audit.result
}

// forestOracleBudget is the per-file wall budget for a single forest parse.
// Overridable via GTS_FOREST_ORACLE_BUDGET (a Go duration, e.g. "30s").
func forestOracleBudget() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GTS_FOREST_ORACLE_BUDGET")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Second
}

func forestCTreeOutcome(tree *sitter.Tree, source []byte) ForestAuditOutcome {
	outcome := ForestAuditOutcome{
		SourceLen: uint32(len(source)), ExpectedEOF: uint32(len(source)), StopReason: "no_tree",
	}
	if tree == nil || tree.RootNode() == nil {
		return outcome
	}
	root := tree.RootNode()
	outcome.Present = true
	outcome.RootEndByte = uint32(root.EndByte())
	outcome.RootHasError = root.HasError()
	outcome.FullSpan = outcome.RootEndByte >= lastNonWSByte(source)
	outcome.Truncated = !outcome.FullSpan
	switch {
	case !outcome.FullSpan:
		outcome.StopReason = "truncated"
	case outcome.RootHasError:
		outcome.StopReason = "root_error"
	default:
		outcome.StopReason = "accepted"
		outcome.Accepted = true
	}
	return outcome
}

func completeForestCIdentityDiff(goNode *gts.Node, goLang *gts.Language, cNode *sitter.Node) string {
	if goNode == nil || cNode == nil {
		if goNode == nil && cNode == nil {
			return ""
		}
		return fmt.Sprintf("root presence go=%t c=%t", goNode != nil, cNode != nil)
	}
	cursor := cNode.Walk()
	defer cursor.Close()

	path := make([]int, 0, 32)
	var walk func(*gts.Node) string
	walk = func(goNode *gts.Node) string {
		location := forestIdentityPath(path)
		cCurrent := cursor.Node()
		if goNode == nil || cCurrent == nil {
			if goNode == nil && cCurrent == nil {
				return ""
			}
			return fmt.Sprintf("%s presence go=%t c=%t", location, goNode != nil, cCurrent != nil)
		}
		if goNode.Type(goLang) != cCurrent.Kind() {
			return fmt.Sprintf("%s type go=%q c=%q", location, goNode.Type(goLang), cCurrent.Kind())
		}
		if goNode.StartByte() != uint32(cCurrent.StartByte()) || goNode.EndByte() != uint32(cCurrent.EndByte()) {
			return fmt.Sprintf("%s range go=%d:%d c=%d:%d", location, goNode.StartByte(), goNode.EndByte(), cCurrent.StartByte(), cCurrent.EndByte())
		}
		goStart, goEnd := goNode.StartPoint(), goNode.EndPoint()
		cStart, cEnd := cCurrent.StartPosition(), cCurrent.EndPosition()
		if goStart.Row != uint32(cStart.Row) || goStart.Column != uint32(cStart.Column) ||
			goEnd.Row != uint32(cEnd.Row) || goEnd.Column != uint32(cEnd.Column) {
			return fmt.Sprintf("%s points go=%v:%v c=%v:%v", location, goStart, goEnd, cStart, cEnd)
		}
		if goNode.IsNamed() != cCurrent.IsNamed() {
			return fmt.Sprintf("%s named go=%t c=%t", location, goNode.IsNamed(), cCurrent.IsNamed())
		}
		if goNode.IsExtra() != cCurrent.IsExtra() {
			return fmt.Sprintf("%s extra go=%t c=%t", location, goNode.IsExtra(), cCurrent.IsExtra())
		}
		if goNode.IsMissing() != cCurrent.IsMissing() {
			return fmt.Sprintf("%s missing go=%t c=%t", location, goNode.IsMissing(), cCurrent.IsMissing())
		}
		if goNode.HasError() != cCurrent.HasError() {
			return fmt.Sprintf("%s has_error go=%t c=%t", location, goNode.HasError(), cCurrent.HasError())
		}
		goChildren, cChildren := goNode.ChildCount(), int(cCurrent.ChildCount())
		if goChildren != cChildren {
			return fmt.Sprintf("%s child_count go=%d c=%d", location, goChildren, cChildren)
		}
		if goChildren == 0 {
			return ""
		}
		if !cursor.GotoFirstChild() {
			return fmt.Sprintf("%s child_count go=%d c=0", location, goChildren)
		}
		for i := 0; i < goChildren; i++ {
			path = append(path, i)
			goField, cField := goNode.FieldNameForChild(i, goLang), cursor.FieldName()
			if goField != cField {
				diff := fmt.Sprintf("%s field go=%q c=%q", forestIdentityPath(path), goField, cField)
				path = path[:len(path)-1]
				return diff
			}
			if diff := walk(goNode.Child(i)); diff != "" {
				path = path[:len(path)-1]
				return diff
			}
			path = path[:len(path)-1]
			if i+1 < goChildren && !cursor.GotoNextSibling() {
				return fmt.Sprintf("%s child_count go=%d c=%d", location, goChildren, i+1)
			}
		}
		if cursor.GotoNextSibling() {
			return fmt.Sprintf("%s child_count go=%d c>%d", location, goChildren, cChildren)
		}
		if !cursor.GotoParent() {
			return fmt.Sprintf("%s cursor_parent_unavailable", location)
		}
		return ""
	}
	return walk(goNode)
}

func TestCompleteForestCIdentityWideNodeIsLinear(t *testing.T) {
	const elements = 50_000
	load := forestLanguageLoaders()["json"]
	if load == nil {
		t.Fatal("json forest language loader is unavailable")
	}
	goLang := load()
	source := []byte(`{"wide":[` + strings.Repeat("0,", elements-1) + "0]}")
	goTree, err := gts.NewParser(goLang).Parse(source)
	if err != nil || goTree == nil || goTree.RootNode() == nil {
		t.Fatalf("parse Go JSON fixture: tree_nil=%t err=%v", goTree == nil, err)
	}
	defer goTree.Release()
	object := goTree.RootNode().Child(0)
	if object == nil || object.ChildCount() < 2 {
		t.Fatal("wide fixture root has no object pair")
	}
	pair := object.Child(1)
	wide := pair.ChildByFieldName("value", goLang)
	if wide == nil {
		t.Fatal("wide fixture pair has no value field")
	}
	if wide.ChildCount() < elements {
		t.Fatalf("wide fixture child count = %d, want at least %d", wide.ChildCount(), elements)
	}

	cLang, err := ParityCLanguage("json")
	if err != nil {
		t.Fatalf("load pinned C JSON grammar: %v", err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("set C JSON grammar: %v", err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C JSON parse returned no tree")
	}
	defer cTree.Close()

	start := time.Now()
	if diff := completeForestCIdentityDiff(goTree.RootNode(), goLang, cTree.RootNode()); diff != "" {
		t.Fatalf("wide identical trees differ: %s", diff)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("wide identity comparison took %s, want <= 5s", elapsed)
	}
}
