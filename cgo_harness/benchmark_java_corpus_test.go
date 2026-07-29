//go:build cgo && treesitter_c_bench_legacy

package cgoharness

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/smacker/go-tree-sitter"
	sitterjava "github.com/smacker/go-tree-sitter/java"
)

type javaCorpusFile struct {
	path   string
	source []byte
}

type javaParseMode string

const (
	javaParseModeDFA            javaParseMode = "dfa"
	javaParseModeDFANoCompat    javaParseMode = "dfa_no_compat"
	javaParseModeDFANoTree      javaParseMode = "dfa_no_tree"
	javaParseModeTokenSource    javaParseMode = "token_source"
	javaParseModeAspectFallback javaParseMode = "aspect_fallback"
)

func loadJavaCorpus(tb testing.TB) []javaCorpusFile {
	tb.Helper()

	root := strings.TrimSpace(os.Getenv("GOT_JAVA_CORPUS_ROOT"))
	if root == "" {
		for _, candidate := range []string{"corpus_real/java", filepath.Join("cgo_harness", "corpus_real", "java")} {
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				root = candidate
				break
			}
		}
	}
	if root == "" {
		tb.Fatal("set GOT_JAVA_CORPUS_ROOT or run from the repository/cgo_harness root")
	}

	maxFiles := javaEnvInt(tb, "GOT_JAVA_CORPUS_MAX_FILES", 0)
	maxBytes := int64(javaEnvInt(tb, "GOT_JAVA_CORPUS_MAX_BYTES", 0))
	minBytes := javaEnvInt(tb, "GOT_JAVA_CORPUS_MIN_BYTES", 0)
	maxFileBytes := javaEnvInt(tb, "GOT_JAVA_CORPUS_MAX_FILE_BYTES", 0)
	order := strings.TrimSpace(os.Getenv("GOT_JAVA_CORPUS_ORDER"))
	if order == "" {
		order = "path"
	}
	randomSeed := int64(javaEnvInt(tb, "GOT_JAVA_CORPUS_RANDOM_SEED", 1))

	var files []javaCorpusFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gradle", "bazel-bin", "bazel-out", "bazel-testlogs", "build", "node_modules", "target":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".java" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(src) < minBytes {
			return nil
		}
		if maxFileBytes > 0 && len(src) > maxFileBytes {
			return nil
		}
		files = append(files, javaCorpusFile{path: path, source: src})
		return nil
	})
	if err != nil {
		tb.Fatalf("load java corpus %s: %v", root, err)
	}
	if len(files) == 0 {
		tb.Fatalf("no .java files under %s", root)
	}
	switch order {
	case "path":
		sort.Slice(files, func(i, j int) bool {
			return files[i].path < files[j].path
		})
	case "largest":
		sort.Slice(files, func(i, j int) bool {
			if len(files[i].source) != len(files[j].source) {
				return len(files[i].source) > len(files[j].source)
			}
			return files[i].path < files[j].path
		})
	case "smallest":
		sort.Slice(files, func(i, j int) bool {
			if len(files[i].source) != len(files[j].source) {
				return len(files[i].source) < len(files[j].source)
			}
			return files[i].path < files[j].path
		})
	case "random":
		sort.Slice(files, func(i, j int) bool {
			return files[i].path < files[j].path
		})
		rng := rand.New(rand.NewSource(randomSeed))
		rng.Shuffle(len(files), func(i, j int) {
			files[i], files[j] = files[j], files[i]
		})
	default:
		tb.Fatalf("invalid GOT_JAVA_CORPUS_ORDER=%q; want path, largest, smallest, or random", order)
	}

	availableFiles := len(files)
	availableBytes := totalJavaCorpusBytes(files)
	selected := make([]javaCorpusFile, 0, len(files))
	var selectedBytes int64
	for _, file := range files {
		if maxFiles > 0 && len(selected) >= maxFiles {
			break
		}
		fileBytes := int64(len(file.source))
		if maxBytes > 0 && selectedBytes+fileBytes > maxBytes {
			continue
		}
		selected = append(selected, file)
		selectedBytes += fileBytes
	}
	if len(selected) == 0 {
		tb.Fatalf("java corpus filters selected no files under %s", root)
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].path < selected[j].path
	})
	tb.Logf(
		"java corpus: root=%s order=%s random_seed=%d files=%d/%d bytes=%d/%d min_bytes=%d max_file_bytes=%d max_files=%d max_bytes=%d",
		root,
		order,
		randomSeed,
		len(selected),
		availableFiles,
		selectedBytes,
		availableBytes,
		minBytes,
		maxFileBytes,
		maxFiles,
		maxBytes,
	)
	return selected
}

func totalJavaCorpusBytes(files []javaCorpusFile) int64 {
	var total int64
	for _, file := range files {
		total += int64(len(file.source))
	}
	return total
}

func javaEnvInt(tb testing.TB, name string, fallback int) int {
	tb.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		tb.Fatalf("invalid %s=%q", name, raw)
	}
	return n
}

func javaEnvBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func javaTimeoutSweep(tb testing.TB) []uint64 {
	tb.Helper()
	raw := strings.TrimSpace(os.Getenv("GOT_JAVA_TIMEOUT_SWEEP"))
	if raw == "" {
		raw = "100ms,500ms,2s,0"
	}
	parts := strings.Split(raw, ",")
	out := make([]uint64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		timeout, err := parseJavaTimeout(part)
		if err != nil {
			tb.Fatalf("invalid GOT_JAVA_TIMEOUT_SWEEP value %q: %v", part, err)
		}
		out = append(out, timeout)
	}
	if len(out) == 0 {
		tb.Fatal("GOT_JAVA_TIMEOUT_SWEEP produced no timeout values")
	}
	return out
}

func javaBenchTimeout(tb testing.TB) uint64 {
	tb.Helper()
	raw := strings.TrimSpace(os.Getenv("GOT_JAVA_BENCH_TIMEOUT"))
	if raw == "" {
		return 0
	}
	timeout, err := parseJavaTimeout(raw)
	if err != nil {
		tb.Fatalf("invalid GOT_JAVA_BENCH_TIMEOUT=%q: %v", raw, err)
	}
	return timeout
}

func parseJavaTimeout(raw string) (uint64, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "none", "off":
		return 0, nil
	}
	if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
		return n, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("duration must be non-negative")
	}
	return uint64(d / time.Microsecond), nil
}

func formatJavaTimeout(timeoutMicros uint64) string {
	if timeoutMicros == 0 {
		return "none"
	}
	return (time.Duration(timeoutMicros) * time.Microsecond).String()
}

func newCTreeSitterJavaParser(tb testing.TB) *sitter.Parser {
	tb.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(sitterjava.GetLanguage())
	return parser
}

type javaCorpusStats struct {
	files             int
	bytes             int64
	duration          time.Duration
	ok                int
	hasError          int
	incomplete        int
	stopped           int
	timeout           int
	fallback          int
	maxDuration       time.Duration
	maxFile           string
	firstIssueFile    string
	firstIssueSummary string
	firstIssueHasErr  bool
	stopReasons       map[gotreesitter.ParseStopReason]int
	runtime           javaRuntimeStats
	shape             javaTreeShapeStats
	perf              gotreesitter.PerfCounters
	ambiguity         []gotreesitter.AmbiguityStat
	issues            []javaCorpusIssue
	issueCount        int
}

type javaRuntimeStats struct {
	tokensConsumed            uint64
	nodesAllocated            uint64
	parentNodesAllocated      uint64
	parentNodesRetained       uint64
	parentNodesDroppedSameTok uint64
	leafNodesAllocated        uint64
	leafNodesRetained         uint64
	leafNodesDroppedSameTok   uint64
	transientChildSlicesAlloc uint64
	transientChildPtrsAlloc   uint64
	transientChildSlicesMat   uint64
	transientChildPtrsMat     uint64
	transientParentNodesAlloc uint64
	transientParentNodesMat   uint64
	gssNodesAllocated         uint64
	gssNodesRetained          uint64
	gssNodesDroppedSameTok    uint64
	singleStackGSSNodes       uint64
	multiStackGSSNodes        uint64
	singleStackIterations     uint64
	multiStackIterations      uint64
	mergeStacksIn             uint64
	mergeStacksOut            uint64
	mergeSlotsUsed            uint64
	globalCullStacksIn        uint64
	globalCullStacksOut       uint64
	pendingParentCreated      uint64
	pendingParentMaterialized uint64
	pendingParentMatParent    uint64
	pendingParentMatFinal     uint64
	pendingParentDropped      uint64
	pendingParentFlattened    uint64
	pendingChildRefsFlattened uint64
	pendingChildEntriesAlloc  uint64
	pendingChildEntryCapacity uint64
	pendingChildEntryWaste    uint64
	pendingParentCandidates   uint64
	pendingParentRejects      gotreesitter.PendingParentRejectStats
	pendingParentFieldRejects gotreesitter.PendingParentFieldRejectStats
	finalChildRefParents      uint64
	finalChildRefs            uint64
	finalChildRefRangeParents uint64
	finalChildRefRangeChild   uint64
	finalChildRefSingleAccess uint64
	finalChildRefSingleChild  uint64
	maxStacksSeen             int
	maxArenaBytesAllocated    int64
	maxScratchBytesAllocated  int64
	maxGSSBytesAllocated      int64
	maxEntryScratchBytesAlloc int64
}

type javaCorpusIssue struct {
	path       string
	bytes      int
	duration   time.Duration
	hasError   bool
	incomplete bool
	stopped    bool
	rootEnd    uint32
	runtime    gotreesitter.ParseRuntime
}

type javaTreeShapeStats struct {
	nodes      uint64
	parents    uint64
	leaves     uint64
	childEdges uint64
	maxArity   int
	arity      [10]uint64
}

type javaParseResult struct {
	tree      *gotreesitter.Tree
	fallback  bool
	duration  time.Duration
	parseMode javaParseMode
}

func parseJavaWithMode(pool *gotreesitter.ParserPool, lang *gotreesitter.Language, mode javaParseMode, source []byte) (javaParseResult, error) {
	start := time.Now()
	switch mode {
	case javaParseModeDFA:
		tree, err := pool.Parse(source)
		return javaParseResult{tree: tree, duration: time.Since(start), parseMode: mode}, err
	case javaParseModeDFANoCompat:
		tree, err := pool.ParseNoResultCompatibilityBenchmarkOnly(source)
		return javaParseResult{tree: tree, duration: time.Since(start), parseMode: mode}, err
	case javaParseModeDFANoTree:
		tree, err := pool.ParseNoTreeBenchmarkOnly(source)
		return javaParseResult{tree: tree, duration: time.Since(start), parseMode: mode}, err
	case javaParseModeTokenSource:
		tree, err := pool.ParseWithTokenSourceFactory(source, func(src []byte) (gotreesitter.TokenSource, error) {
			return grammars.NewJavaTokenSource(src, lang)
		})
		return javaParseResult{tree: tree, duration: time.Since(start), parseMode: mode}, err
	case javaParseModeAspectFallback:
		tree, err := pool.Parse(source)
		if err != nil {
			return javaParseResult{tree: tree, duration: time.Since(start), parseMode: mode}, err
		}
		if tree == nil || tree.RootNode() == nil || !tree.RootNode().HasError() {
			return javaParseResult{tree: tree, duration: time.Since(start), parseMode: mode}, nil
		}
		tree.Release()
		fallbackTree, err := pool.ParseWithTokenSourceFactory(source, func(src []byte) (gotreesitter.TokenSource, error) {
			return grammars.NewJavaTokenSource(src, lang)
		})
		return javaParseResult{tree: fallbackTree, fallback: true, duration: time.Since(start), parseMode: mode}, err
	default:
		return javaParseResult{duration: time.Since(start), parseMode: mode}, fmt.Errorf("unknown java parse mode %q", mode)
	}
}

func javaParseModes(tb testing.TB) []javaParseMode {
	tb.Helper()
	raw := strings.TrimSpace(os.Getenv("GOT_JAVA_PARSE_MODES"))
	if raw == "" {
		raw = "dfa,token_source,aspect_fallback"
	}
	parts := strings.Split(raw, ",")
	modes := make([]javaParseMode, 0, len(parts))
	for _, part := range parts {
		mode := javaParseMode(strings.TrimSpace(part))
		switch mode {
		case "":
			continue
		case javaParseModeDFA, javaParseModeDFANoCompat, javaParseModeDFANoTree, javaParseModeTokenSource, javaParseModeAspectFallback:
			modes = append(modes, mode)
		default:
			tb.Fatalf("invalid GOT_JAVA_PARSE_MODES value %q; want dfa, dfa_no_compat, dfa_no_tree, token_source, or aspect_fallback", part)
		}
	}
	if len(modes) == 0 {
		tb.Fatal("GOT_JAVA_PARSE_MODES produced no parse modes")
	}
	return modes
}

func runJavaCorpus(files []javaCorpusFile, mode javaParseMode, timeoutMicros uint64) (javaCorpusStats, error) {
	lang := grammars.JavaLanguage()
	if javaEnvBool("GOT_JAVA_RUNTIME_AUDIT") {
		gotreesitter.EnableRuntimeAudit(true)
		defer gotreesitter.EnableRuntimeAudit(false)
	}
	shapeEnabled := javaEnvBool("GOT_JAVA_TREE_SHAPE")
	perfEnabled := javaEnvBool("GOT_JAVA_PERF_STATS")
	if perfEnabled {
		gotreesitter.ResetPerfCounters()
	}
	opts := []gotreesitter.ParserPoolOption{gotreesitter.WithParserPoolTimeoutMicros(timeoutMicros)}
	var ambiguity *gotreesitter.AmbiguityProfile
	if javaEnvBool("GOT_JAVA_AMBIGUITY_PROFILE") {
		ambiguity = gotreesitter.NewAmbiguityProfile()
		opts = append(opts, gotreesitter.WithParserPoolAmbiguityProfile(ambiguity))
	}
	pool := gotreesitter.NewParserPool(lang, opts...)
	stats := javaCorpusStats{stopReasons: make(map[gotreesitter.ParseStopReason]int)}
	maxIssues, err := javaCorpusMaxIssues()
	if err != nil {
		return stats, err
	}

	for _, file := range files {
		result, err := parseJavaWithMode(pool, lang, mode, file.source)
		if err != nil {
			if result.tree != nil {
				result.tree.Release()
			}
			return stats, fmt.Errorf("%s: %w", file.path, err)
		}
		tree := result.tree
		stats.files++
		stats.bytes += int64(len(file.source))
		stats.duration += result.duration
		if result.fallback {
			stats.fallback++
		}
		if result.duration > stats.maxDuration {
			stats.maxDuration = result.duration
			stats.maxFile = file.path
		}
		if tree == nil || tree.RootNode() == nil {
			stats.incomplete++
			stats.addIssue(javaCorpusIssue{
				path:       file.path,
				bytes:      len(file.source),
				duration:   result.duration,
				incomplete: true,
			}, maxIssues)
			if tree != nil {
				tree.Release()
			}
			continue
		}
		root := tree.RootNode()
		rt := tree.ParseRuntime()
		stats.runtime.add(rt)
		stats.stopReasons[rt.StopReason]++
		if root.HasError() {
			stats.hasError++
		}
		if tree.ParseStoppedEarly() {
			stats.stopped++
		}
		if rt.StopReason == gotreesitter.ParseStopTimeout {
			stats.timeout++
		}
		if root.EndByte() != uint32(len(file.source)) || rt.Truncated {
			stats.incomplete++
		}
		hasIssue := root.HasError() || tree.ParseStoppedEarly() || root.EndByte() != uint32(len(file.source)) || rt.Truncated
		if hasIssue {
			stats.addIssue(javaCorpusIssue{
				path:       file.path,
				bytes:      len(file.source),
				duration:   result.duration,
				hasError:   root.HasError(),
				incomplete: root.EndByte() != uint32(len(file.source)) || rt.Truncated,
				stopped:    tree.ParseStoppedEarly(),
				rootEnd:    root.EndByte(),
				runtime:    rt,
			}, maxIssues)
		}
		if stats.firstIssueFile == "" && hasIssue {
			stats.firstIssueFile = file.path
			stats.firstIssueSummary = rt.Summary()
			stats.firstIssueHasErr = root.HasError()
		}
		if !root.HasError() && !tree.ParseStoppedEarly() && root.EndByte() == uint32(len(file.source)) && !rt.Truncated {
			stats.ok++
		}
		if shapeEnabled {
			stats.shape.add(root)
		}
		tree.Release()
	}
	if ambiguity != nil {
		stats.ambiguity = ambiguity.SnapshotTop(20)
	}
	if perfEnabled {
		stats.perf = gotreesitter.PerfCountersSnapshot()
	}
	return stats, nil
}

func javaCorpusMaxIssues() (int, error) {
	raw := strings.TrimSpace(os.Getenv("GOT_JAVA_CORPUS_MAX_ISSUES"))
	if raw == "" {
		return 10, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid GOT_JAVA_CORPUS_MAX_ISSUES=%q", raw)
	}
	return n, nil
}

func (s *javaCorpusStats) addIssue(issue javaCorpusIssue, maxIssues int) {
	s.issueCount++
	if maxIssues <= 0 || len(s.issues) >= maxIssues {
		return
	}
	s.issues = append(s.issues, issue)
}

func (s *javaRuntimeStats) add(rt gotreesitter.ParseRuntime) {
	s.tokensConsumed += rt.TokensConsumed
	s.nodesAllocated += uint64(rt.NodesAllocated)
	s.parentNodesAllocated += rt.ParentNodesAllocated
	s.parentNodesRetained += rt.ParentNodesRetained
	s.parentNodesDroppedSameTok += rt.ParentNodesDroppedSameToken
	s.leafNodesAllocated += rt.LeafNodesAllocated
	s.leafNodesRetained += rt.LeafNodesRetained
	s.leafNodesDroppedSameTok += rt.LeafNodesDroppedSameToken
	s.transientChildSlicesAlloc += rt.TransientChildSlicesAllocated
	s.transientChildPtrsAlloc += rt.TransientChildPointersAllocated
	s.transientChildSlicesMat += rt.TransientChildSlicesMaterialized
	s.transientChildPtrsMat += rt.TransientChildPointersMaterialized
	s.transientParentNodesAlloc += rt.TransientParentNodesAllocated
	s.transientParentNodesMat += rt.TransientParentNodesMaterialized
	s.gssNodesAllocated += rt.GSSNodesAllocated
	s.gssNodesRetained += rt.GSSNodesRetained
	s.gssNodesDroppedSameTok += rt.GSSNodesDroppedSameToken
	s.singleStackGSSNodes += rt.SingleStackGSSNodes
	s.multiStackGSSNodes += rt.MultiStackGSSNodes
	s.singleStackIterations += uint64(rt.SingleStackIterations)
	s.multiStackIterations += uint64(rt.MultiStackIterations)
	s.mergeStacksIn += rt.MergeStacksIn
	s.mergeStacksOut += rt.MergeStacksOut
	s.mergeSlotsUsed += rt.MergeSlotsUsed
	s.globalCullStacksIn += rt.GlobalCullStacksIn
	s.globalCullStacksOut += rt.GlobalCullStacksOut
	s.pendingParentCreated += rt.PendingParentCreated
	s.pendingParentMaterialized += rt.PendingParentMaterialized
	s.pendingParentMatParent += rt.PendingParentMaterializedForParentReduce
	addPendingParentRejectStats(&s.pendingParentRejects, rt.PendingParentMaterializedForParentReject)
	addPendingParentFieldRejectStats(&s.pendingParentFieldRejects, rt.PendingParentMaterializedForFieldReject)
	s.pendingParentMatFinal += rt.PendingParentMaterializedForFinalTree
	s.pendingParentDropped += rt.PendingParentDropped
	s.pendingParentFlattened += rt.PendingParentsFlattened
	s.pendingChildRefsFlattened += rt.PendingChildRefsFlattened
	s.pendingChildEntriesAlloc += rt.PendingChildEntriesAllocated
	s.pendingChildEntryCapacity += rt.PendingChildEntryCapacity
	s.pendingChildEntryWaste += rt.PendingChildEntryWaste
	s.pendingParentCandidates += rt.PendingParentCandidates
	s.finalChildRefParents += rt.FinalChildRefParents
	s.finalChildRefs += rt.FinalChildRefs
	s.finalChildRefRangeParents += rt.FinalChildRefMaterializedParents
	s.finalChildRefRangeChild += rt.FinalChildRefMaterializedChildren
	s.finalChildRefSingleAccess += rt.FinalChildRefSingleChildAccesses
	s.finalChildRefSingleChild += rt.FinalChildRefSingleChildMaterializedChildren
	if rt.MaxStacksSeen > s.maxStacksSeen {
		s.maxStacksSeen = rt.MaxStacksSeen
	}
	if rt.ArenaBytesAllocated > s.maxArenaBytesAllocated {
		s.maxArenaBytesAllocated = rt.ArenaBytesAllocated
	}
	if rt.ScratchBytesAllocated > s.maxScratchBytesAllocated {
		s.maxScratchBytesAllocated = rt.ScratchBytesAllocated
	}
	if rt.GSSBytesAllocated > s.maxGSSBytesAllocated {
		s.maxGSSBytesAllocated = rt.GSSBytesAllocated
	}
	if rt.EntryScratchBytesAllocated > s.maxEntryScratchBytesAlloc {
		s.maxEntryScratchBytesAlloc = rt.EntryScratchBytesAllocated
	}
}

func (s javaRuntimeStats) summary() string {
	return fmt.Sprintf(
		"tokens=%d nodes=%d parent_alloc=%d parent_retained=%d parent_dropped_same_token=%d leaf_alloc=%d leaf_retained=%d leaf_dropped_same_token=%d transient_child_slices_alloc=%d transient_child_slices_materialized=%d transient_child_ptrs_alloc=%d transient_child_ptrs_materialized=%d transient_parent_alloc=%d transient_parent_materialized=%d transient_parent_dropped=%d gss_alloc=%d gss_retained=%d gss_dropped_same_token=%d single_gss=%d multi_gss=%d single_iters=%d multi_iters=%d merge_in=%d merge_out=%d merge_slots=%d global_cull_in=%d global_cull_out=%d pending_created=%d pending_materialized=%d pending_materialized_parent=%d pending_materialized_parent_reject_fields=%d pending_materialized_field_hidden_child=%d pending_materialized_field_hidden_child_plain=%d pending_materialized_field_hidden_child_with_fields=%d pending_materialized_field_all_visible_direct=%d pending_dropped=%d pending_flattened=%d pending_child_refs_flattened=%d pending_child_entries=%d pending_child_entry_capacity=%d pending_child_entry_waste=%d pending_candidates=%d final_child_ref_parents=%d final_child_refs=%d final_child_ref_range_parents=%d final_child_ref_range_children=%d final_child_ref_single_accesses=%d final_child_ref_single_children=%d max_stacks=%d max_arena_bytes=%d max_scratch_bytes=%d max_gss_bytes=%d max_entry_scratch_bytes=%d",
		s.tokensConsumed,
		s.nodesAllocated,
		s.parentNodesAllocated,
		s.parentNodesRetained,
		s.parentNodesDroppedSameTok,
		s.leafNodesAllocated,
		s.leafNodesRetained,
		s.leafNodesDroppedSameTok,
		s.transientChildSlicesAlloc,
		s.transientChildSlicesMat,
		s.transientChildPtrsAlloc,
		s.transientChildPtrsMat,
		s.transientParentNodesAlloc,
		s.transientParentNodesMat,
		s.transientParentNodesAlloc-s.transientParentNodesMat,
		s.gssNodesAllocated,
		s.gssNodesRetained,
		s.gssNodesDroppedSameTok,
		s.singleStackGSSNodes,
		s.multiStackGSSNodes,
		s.singleStackIterations,
		s.multiStackIterations,
		s.mergeStacksIn,
		s.mergeStacksOut,
		s.mergeSlotsUsed,
		s.globalCullStacksIn,
		s.globalCullStacksOut,
		s.pendingParentCreated,
		s.pendingParentMaterialized,
		s.pendingParentMatParent,
		s.pendingParentRejects.Fields,
		s.pendingParentFieldRejects.HiddenChild,
		s.pendingParentFieldRejects.HiddenChildPlain,
		s.pendingParentFieldRejects.HiddenChildWithFields,
		s.pendingParentFieldRejects.AllVisibleDirect,
		s.pendingParentDropped,
		s.pendingParentFlattened,
		s.pendingChildRefsFlattened,
		s.pendingChildEntriesAlloc,
		s.pendingChildEntryCapacity,
		s.pendingChildEntryWaste,
		s.pendingParentCandidates,
		s.finalChildRefParents,
		s.finalChildRefs,
		s.finalChildRefRangeParents,
		s.finalChildRefRangeChild,
		s.finalChildRefSingleAccess,
		s.finalChildRefSingleChild,
		s.maxStacksSeen,
		s.maxArenaBytesAllocated,
		s.maxScratchBytesAllocated,
		s.maxGSSBytesAllocated,
		s.maxEntryScratchBytesAlloc,
	)
}

func (s *javaTreeShapeStats) add(root *gotreesitter.Node) {
	if root == nil {
		return
	}
	stack := []*gotreesitter.Node{root}
	for len(stack) > 0 {
		last := len(stack) - 1
		n := stack[last]
		stack = stack[:last]
		if n == nil {
			continue
		}
		s.nodes++
		arity := n.ChildCount()
		if arity == 0 {
			s.leaves++
		} else {
			s.parents++
		}
		s.childEdges += uint64(arity)
		if arity > s.maxArity {
			s.maxArity = arity
		}
		bucket := arity
		if bucket >= len(s.arity) {
			bucket = len(s.arity) - 1
		}
		s.arity[bucket]++
		for i := arity - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
}

func (s javaTreeShapeStats) summary() string {
	if s.nodes == 0 {
		return ""
	}
	return fmt.Sprintf(
		"nodes=%d parents=%d leaves=%d child_edges=%d max_arity=%d arity0=%d arity1=%d arity2=%d arity3=%d arity4=%d arity5=%d arity6=%d arity7=%d arity8=%d arity9plus=%d",
		s.nodes,
		s.parents,
		s.leaves,
		s.childEdges,
		s.maxArity,
		s.arity[0],
		s.arity[1],
		s.arity[2],
		s.arity[3],
		s.arity[4],
		s.arity[5],
		s.arity[6],
		s.arity[7],
		s.arity[8],
		s.arity[9],
	)
}

func formatJavaPerfStats(p gotreesitter.PerfCounters) string {
	if p.ParentChildPointers == 0 &&
		p.MergeCalls == 0 &&
		p.StackEquivalentCalls == 0 &&
		p.LexTokens == 0 &&
		p.ReduceChainSteps == 0 {
		return ""
	}
	return fmt.Sprintf(
		"parent_child_ptrs=%d reduce_fast_gss=%d reduce_all_visible=%d reduce_scratch=%d reduce_scratch_no_alias=%d reduce_scratch_general=%d extra_nodes=%d error_nodes=%d merge_calls=%d merge_overflow=%d merge_replacements=%d stackeq_calls=%d stackeq_true=%d stackeq_hash_miss_skips=%d stackcmp_calls=%d conflicts_rr=%d conflicts_rs=%d conflicts_other=%d reduce_chain_steps=%d reduce_chain_max_len=%d reduce_chain_break_multi=%d reduce_chain_break_shift=%d reduce_chain_break_accept=%d forks=%d max_stacks=%d lex_bytes=%d lex_tokens=%d",
		p.ParentChildPointers,
		p.ReduceChildrenFastGSS,
		p.ReduceChildrenAllVis,
		p.ReduceChildrenScratch,
		p.ReduceScratchNoAlias,
		p.ReduceScratchGeneral,
		p.ExtraNodes,
		p.ErrorNodes,
		p.MergeCalls,
		p.MergePerKeyOverflow,
		p.MergeReplacements,
		p.StackEquivalentCalls,
		p.StackEquivalentTrue,
		p.StackEqHashMissSkips,
		p.StackCompareCalls,
		p.ConflictRR,
		p.ConflictRS,
		p.ConflictOther,
		p.ReduceChainSteps,
		p.ReduceChainMaxLen,
		p.ReduceChainBreakMulti,
		p.ReduceChainBreakShift,
		p.ReduceChainBreakAccept,
		p.ForkCount,
		p.MaxConcurrentStacks,
		p.LexBytes,
		p.LexTokens,
	)
}

func formatJavaStopReasons(counts map[gotreesitter.ParseStopReason]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for reason := range counts {
		keys = append(keys, string(reason))
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, key := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(strconv.Itoa(counts[gotreesitter.ParseStopReason(key)]))
	}
	return sb.String()
}

func formatJavaAmbiguityTop(stats []gotreesitter.AmbiguityStat, lang *gotreesitter.Language) string {
	if len(stats) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, stat := range stats {
		if i > 0 {
			sb.WriteString("; ")
		}
		fmt.Fprintf(
			&sb,
			"state=%d lookahead=%s(%d) actions=%d shifts=%d reduces=%d hits=%d stack_total=%d stack_max=%d action_set=%s",
			stat.State,
			javaSymbolName(lang, stat.Lookahead),
			stat.Lookahead,
			stat.ActionCount,
			stat.ShiftCount,
			stat.ReduceCount,
			stat.Hits,
			stat.StackInTotal,
			stat.StackInMax,
			formatJavaAmbiguityActions(stat.Actions, lang),
		)
	}
	return sb.String()
}

func formatJavaAmbiguityActions(actions []gotreesitter.ParseAction, lang *gotreesitter.Language) string {
	if len(actions) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		switch action.Type {
		case gotreesitter.ParseActionShift:
			parts = append(parts, fmt.Sprintf("shift:%d", action.State))
		case gotreesitter.ParseActionReduce:
			parts = append(parts, fmt.Sprintf("reduce:%s/%d#%d", javaSymbolName(lang, action.Symbol), action.Symbol, action.ProductionID))
		case gotreesitter.ParseActionAccept:
			parts = append(parts, "accept")
		case gotreesitter.ParseActionRecover:
			parts = append(parts, fmt.Sprintf("recover:%d", action.State))
		default:
			parts = append(parts, fmt.Sprintf("type%d", action.Type))
		}
	}
	return strings.Join(parts, "|")
}

func formatJavaCorpusIssues(issues []javaCorpusIssue) string {
	if len(issues) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, issue := range issues {
		if i > 0 {
			sb.WriteString("; ")
		}
		rt := issue.runtime
		fmt.Fprintf(
			&sb,
			"path=%q bytes=%d duration=%s has_error=%v incomplete=%v stopped=%v root_end=%d stop=%s tokens=%d last_token_end=%d expected_eof=%d max_stacks=%d single_iters=%d multi_iters=%d single_gss=%d multi_gss=%d arena=%d scratch=%d gss=%d entry_scratch=%d truncated=%v summary=%q",
			issue.path,
			issue.bytes,
			issue.duration,
			issue.hasError,
			issue.incomplete,
			issue.stopped,
			issue.rootEnd,
			rt.StopReason,
			rt.TokensConsumed,
			rt.LastTokenEndByte,
			rt.ExpectedEOFByte,
			rt.MaxStacksSeen,
			rt.SingleStackIterations,
			rt.MultiStackIterations,
			rt.SingleStackGSSNodes,
			rt.MultiStackGSSNodes,
			rt.ArenaBytesAllocated,
			rt.ScratchBytesAllocated,
			rt.GSSBytesAllocated,
			rt.EntryScratchBytesAllocated,
			rt.Truncated,
			rt.Summary(),
		)
	}
	return sb.String()
}

func javaSymbolName(lang *gotreesitter.Language, sym gotreesitter.Symbol) string {
	if lang != nil && int(sym) >= 0 && int(sym) < len(lang.SymbolNames) {
		return lang.SymbolNames[sym]
	}
	return "?"
}

func TestJavaCorpusTimeoutSweep(t *testing.T) {
	files := loadJavaCorpus(t)
	modes := javaParseModes(t)
	lang := grammars.JavaLanguage()
	for _, timeoutMicros := range javaTimeoutSweep(t) {
		for _, mode := range modes {
			stats, err := runJavaCorpus(files, mode, timeoutMicros)
			if err != nil {
				t.Fatalf("java corpus timeout=%s mode=%s: %v", formatJavaTimeout(timeoutMicros), mode, err)
			}
			nsPerByte := float64(0)
			if stats.bytes > 0 {
				nsPerByte = float64(stats.duration.Nanoseconds()) / float64(stats.bytes)
			}
			t.Logf(
				"java-timeout-sweep timeout=%s mode=%s files=%d bytes=%d total=%s ns_per_byte=%.2f ok=%d has_error=%d incomplete=%d stopped=%d timeouts=%d fallback=%d max=%s max_file=%s stops=%s first_issue=%s first_issue_has_error=%v first_issue_runtime=%q issue_count=%d issues=%q runtime=%q shape=%q perf=%q ambiguity_top=%q",
				formatJavaTimeout(timeoutMicros),
				mode,
				stats.files,
				stats.bytes,
				stats.duration,
				nsPerByte,
				stats.ok,
				stats.hasError,
				stats.incomplete,
				stats.stopped,
				stats.timeout,
				stats.fallback,
				stats.maxDuration,
				stats.maxFile,
				formatJavaStopReasons(stats.stopReasons),
				stats.firstIssueFile,
				stats.firstIssueHasErr,
				stats.firstIssueSummary,
				stats.issueCount,
				formatJavaCorpusIssues(stats.issues),
				stats.runtime.summary(),
				stats.shape.summary(),
				formatJavaPerfStats(stats.perf),
				formatJavaAmbiguityTop(stats.ambiguity, lang),
			)
		}
	}
}

func benchmarkJavaCorpusGoTreeSitter(b *testing.B, mode javaParseMode) {
	files := loadJavaCorpus(b)
	timeoutMicros := javaBenchTimeout(b)
	totalBytes := totalJavaCorpusBytes(files)
	lang := grammars.JavaLanguage()
	pool := gotreesitter.NewParserPool(lang, gotreesitter.WithParserPoolTimeoutMicros(timeoutMicros))

	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()

	var runtime javaRuntimeStats
	for i := 0; i < b.N; i++ {
		for _, file := range files {
			result, err := parseJavaWithMode(pool, lang, mode, file.source)
			if err != nil {
				if result.tree != nil {
					result.tree.Release()
				}
				b.Fatalf("%s: %v", file.path, err)
			}
			tree := result.tree
			if tree == nil || tree.RootNode() == nil {
				b.Fatalf("%s: parse returned nil root", file.path)
			}
			root := tree.RootNode()
			rt := tree.ParseRuntime()
			runtime.add(rt)
			if root.HasError() || tree.ParseStoppedEarly() || root.EndByte() != uint32(len(file.source)) || rt.Truncated {
				tree.Release()
				b.Fatalf("%s: incomplete parse mode=%s timeout=%s has_error=%v runtime=%s", file.path, mode, formatJavaTimeout(timeoutMicros), root.HasError(), rt.Summary())
			}
			tree.Release()
		}
	}
	if runtime.tokensConsumed != 0 {
		tokens := float64(runtime.tokensConsumed)
		b.ReportMetric(float64(runtime.pendingParentCreated)/tokens, "pending_parent_created/token")
		b.ReportMetric(float64(runtime.pendingParentMaterialized)/tokens, "pending_parent_materialized/token")
		b.ReportMetric(float64(runtime.pendingParentMatParent)/tokens, "pending_parent_materialized_parent/token")
		reportPendingParentRejectStats(b, runtime.pendingParentRejects, tokens, "pending_parent_materialized_parent_reject")
		reportPendingParentFieldRejectStats(b, runtime.pendingParentFieldRejects, tokens, "pending_parent_materialized_parent_reject_fields")
		b.ReportMetric(float64(runtime.pendingParentMatFinal)/tokens, "pending_parent_materialized_final/token")
		b.ReportMetric(float64(runtime.pendingParentDropped)/tokens, "pending_parent_dropped/token")
		b.ReportMetric(float64(runtime.pendingParentFlattened)/tokens, "pending_parent_flattened/token")
		b.ReportMetric(float64(runtime.pendingChildRefsFlattened)/tokens, "pending_child_refs_flattened/token")
		b.ReportMetric(float64(runtime.pendingChildEntriesAlloc)/tokens, "pending_child_entries_allocated/token")
		b.ReportMetric(float64(runtime.pendingChildEntryCapacity)/tokens, "pending_child_entry_capacity/token")
		b.ReportMetric(float64(runtime.pendingChildEntryWaste)/tokens, "pending_child_entry_waste/token")
		b.ReportMetric(float64(runtime.pendingParentCandidates)/tokens, "pending_parent_candidate/token")
		b.ReportMetric(float64(runtime.finalChildRefParents)/tokens, "final_child_ref_parents/token")
		b.ReportMetric(float64(runtime.finalChildRefs)/tokens, "final_child_refs/token")
		b.ReportMetric(float64(runtime.finalChildRefRangeParents)/tokens, "final_child_ref_range_materialized_parents/token")
		b.ReportMetric(float64(runtime.finalChildRefRangeChild)/tokens, "final_child_ref_range_materialized_children/token")
		b.ReportMetric(float64(runtime.finalChildRefSingleAccess)/tokens, "final_child_ref_single_child_accesses/token")
		b.ReportMetric(float64(runtime.finalChildRefSingleChild)/tokens, "final_child_ref_single_child_materialized/token")
	}
}

func BenchmarkJavaCorpusGoTreeSitterParseDFA(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitter(b, javaParseModeDFA)
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoCompat(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitter(b, javaParseModeDFANoCompat)
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoTree(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitter(b, javaParseModeDFANoTree)
}

func BenchmarkJavaCorpusGoTreeSitterParseTokenSource(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitter(b, javaParseModeTokenSource)
}

func BenchmarkJavaCorpusGoTreeSitterParseAspectFallback(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitter(b, javaParseModeAspectFallback)
}

type javaCorpusTreeUse func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64

var javaCorpusBenchmarkSink int64

func benchmarkJavaCorpusGoTreeSitterWithUse(b *testing.B, mode javaParseMode, metric string, use javaCorpusTreeUse) {
	files := loadJavaCorpus(b)
	timeoutMicros := javaBenchTimeout(b)
	totalBytes := totalJavaCorpusBytes(files)
	lang := grammars.JavaLanguage()
	pool := gotreesitter.NewParserPool(lang, gotreesitter.WithParserPoolTimeoutMicros(timeoutMicros))

	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()

	var metricTotal int64
	var runtime javaRuntimeStats
	for i := 0; i < b.N; i++ {
		for _, file := range files {
			result, err := parseJavaWithMode(pool, lang, mode, file.source)
			if err != nil {
				if result.tree != nil {
					result.tree.Release()
				}
				b.Fatalf("%s: %v", file.path, err)
			}
			tree := result.tree
			root := requireCompleteGoTree(b, tree, file.source, file.path, mode, timeoutMicros)
			if use != nil {
				metricTotal += use(b, lang, tree, file)
			}
			if root == nil {
				b.Fatalf("%s: parse returned nil root", file.path)
			}
			runtime.add(tree.ParseRuntime())
			tree.Release()
		}
	}
	javaCorpusBenchmarkSink = metricTotal
	if metric != "" && b.N > 0 {
		b.ReportMetric(float64(metricTotal)/float64(b.N), metric)
	}
	if runtime.tokensConsumed != 0 {
		tokens := float64(runtime.tokensConsumed)
		b.ReportMetric(float64(runtime.finalChildRefParents)/tokens, "final_child_ref_parents/token")
		b.ReportMetric(float64(runtime.finalChildRefs)/tokens, "final_child_refs/token")
		b.ReportMetric(float64(runtime.finalChildRefRangeParents)/tokens, "final_child_ref_range_materialized_parents/token")
		b.ReportMetric(float64(runtime.finalChildRefRangeChild)/tokens, "final_child_ref_range_materialized_children/token")
		b.ReportMetric(float64(runtime.finalChildRefSingleAccess)/tokens, "final_child_ref_single_child_accesses/token")
		b.ReportMetric(float64(runtime.finalChildRefSingleChild)/tokens, "final_child_ref_single_child_materialized/token")
	}
}

func requireCompleteGoTree(tb testing.TB, tree *gotreesitter.Tree, src []byte, phase string, mode javaParseMode, timeoutMicros uint64) *gotreesitter.Node {
	tb.Helper()
	if tree == nil || tree.RootNode() == nil {
		tb.Fatalf("%s: parse returned nil root", phase)
	}
	root := tree.RootNode()
	rt := tree.ParseRuntime()
	if root.HasError() || tree.ParseStoppedEarly() || root.EndByte() != uint32(len(src)) || rt.Truncated {
		tb.Fatalf("%s: incomplete parse mode=%s timeout=%s has_error=%v runtime=%s", phase, mode, formatJavaTimeout(timeoutMicros), root.HasError(), rt.Summary())
	}
	return root
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithSExpr(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "sexpr_bytes/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		sexpr := tree.RootNode().SExpr(lang)
		if sexpr == "" {
			b.Fatalf("%s: SExpr returned empty string", file.path)
		}
		return int64(len(sexpr))
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoCompatWithSExpr(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFANoCompat, "sexpr_bytes/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		sexpr := tree.RootNode().SExpr(lang)
		if sexpr == "" {
			b.Fatalf("%s: SExpr returned empty string", file.path)
		}
		return int64(len(sexpr))
	})
}

const javaCorpusRepresentativeQuery = `
[
  (class_declaration name: (identifier) @type)
  (interface_declaration name: (identifier) @type)
  (enum_declaration name: (identifier) @type)
  (method_declaration name: (identifier) @function.method)
  (constructor_declaration name: (identifier) @constructor)
]
`

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithQuery(b *testing.B) {
	lang := grammars.JavaLanguage()
	query, err := gotreesitter.NewQuery(javaCorpusRepresentativeQuery, lang)
	if err != nil {
		b.Fatalf("compile java corpus query: %v", err)
	}
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "query_captures/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		cursor := query.Exec(tree.RootNode(), lang, file.source)
		var captures int64
		for {
			match, ok := cursor.NextMatch()
			if !ok {
				break
			}
			captures += int64(len(match.Captures))
		}
		return captures
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoCompatWithQuery(b *testing.B) {
	lang := grammars.JavaLanguage()
	query, err := gotreesitter.NewQuery(javaCorpusRepresentativeQuery, lang)
	if err != nil {
		b.Fatalf("compile java corpus query: %v", err)
	}
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFANoCompat, "query_captures/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		cursor := query.Exec(tree.RootNode(), lang, file.source)
		var captures int64
		for {
			match, ok := cursor.NextMatch()
			if !ok {
				break
			}
			captures += int64(len(match.Captures))
		}
		return captures
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithImportExtract(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "imports/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return int64(len(gotreesitter.ExtractImports(tree)))
	})
}

func BenchmarkJavaCorpusSourceImportExtract(b *testing.B) {
	files := loadJavaCorpus(b)
	totalBytes := totalJavaCorpusBytes(files)
	lang := grammars.JavaLanguage()

	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()

	var imports int64
	var fallbacks int64
	for i := 0; i < b.N; i++ {
		for _, file := range files {
			report := gotreesitter.ExtractImportsFromSourceWithReport(lang, file.source)
			imports += int64(len(report.Imports))
			if report.FallbackRecommended {
				fallbacks++
			}
		}
	}
	javaCorpusBenchmarkSink = imports
	if b.N > 0 {
		b.ReportMetric(float64(imports)/float64(b.N), "imports/op")
		b.ReportMetric(float64(fallbacks)/float64(b.N), "fallbacks/op")
	}
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithNamedTraversal(b *testing.B) {
	var scratch []*gotreesitter.Node
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "named_nodes/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return countNamedJavaCorpusNodes(tree.RootNode(), &scratch)
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoCompatWithNamedTraversal(b *testing.B) {
	var scratch []*gotreesitter.Node
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFANoCompat, "named_nodes/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return countNamedJavaCorpusNodes(tree.RootNode(), &scratch)
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithFirstParent(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "parent_calls/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		root := tree.RootNode()
		if root == nil || root.ChildCount() == 0 {
			return 0
		}
		child := root.Child(0)
		if child == nil {
			return 0
		}
		if parent := child.Parent(); parent != root {
			b.Fatalf("%s: first child parent mismatch", file.path)
		}
		return 1
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithParentTraversal(b *testing.B) {
	var scratch []*gotreesitter.Node
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "parent_checks/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return countJavaCorpusParentChecks(b, file.path, tree.RootNode(), &scratch)
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoCompatWithParentTraversal(b *testing.B) {
	var scratch []*gotreesitter.Node
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFANoCompat, "parent_checks/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return countJavaCorpusParentChecks(b, file.path, tree.RootNode(), &scratch)
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithSiblingWalk(b *testing.B) {
	var scratch []*gotreesitter.Node
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "sibling_steps/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return countJavaCorpusSiblingSteps(b, file.path, tree.RootNode(), &scratch)
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFANoCompatWithSiblingWalk(b *testing.B) {
	var scratch []*gotreesitter.Node
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFANoCompat, "sibling_steps/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		return countJavaCorpusSiblingSteps(b, file.path, tree.RootNode(), &scratch)
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithNodeEdit(b *testing.B) {
	benchmarkJavaCorpusGoTreeSitterWithUse(b, javaParseModeDFA, "edit_calls/op", func(b *testing.B, lang *gotreesitter.Language, tree *gotreesitter.Tree, file javaCorpusFile) int64 {
		root := tree.RootNode()
		if root == nil {
			return 0
		}
		// Isolate the first Node.Edit API touch: this forces deferred parent
		// links, while real edit history/reuse is covered by the incremental
		// benchmark below.
		root.Edit(gotreesitter.InputEdit{
			StartByte:   uint32(len(file.source)),
			OldEndByte:  uint32(len(file.source)),
			NewEndByte:  uint32(len(file.source)),
			StartPoint:  root.EndPoint(),
			OldEndPoint: root.EndPoint(),
			NewEndPoint: root.EndPoint(),
		})
		return 1
	})
}

func BenchmarkJavaCorpusGoTreeSitterParseDFAWithIncrementalReuse(b *testing.B) {
	files := loadJavaCorpus(b)
	timeoutMicros := javaBenchTimeout(b)
	totalBytes := totalJavaCorpusBytes(files)
	lang := grammars.JavaLanguage()
	pool := gotreesitter.NewParserPool(lang, gotreesitter.WithParserPoolTimeoutMicros(timeoutMicros))
	edits := make([]javaCorpusSingleByteEdit, len(files))
	for i, file := range files {
		edits[i] = prepareJavaCorpusSingleByteEdit(b, file)
	}

	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()

	var incrementalParses int64
	for i := 0; i < b.N; i++ {
		for fileIndex, file := range files {
			result, err := parseJavaWithMode(pool, lang, javaParseModeDFA, file.source)
			if err != nil {
				if result.tree != nil {
					result.tree.Release()
				}
				b.Fatalf("%s: %v", file.path, err)
			}
			tree := result.tree
			requireCompleteGoTree(b, tree, file.source, file.path, javaParseModeDFA, timeoutMicros)
			tree.Edit(edits[fileIndex].edit)

			incrementalResult, err := pool.ParseWith(edits[fileIndex].source, gotreesitter.WithOldTree(tree))
			if err != nil {
				if incrementalResult.Tree != nil && incrementalResult.Tree != tree {
					incrementalResult.Tree.Release()
				}
				tree.Release()
				b.Fatalf("%s: incremental parse: %v", file.path, err)
			}
			incrementalTree := incrementalResult.Tree
			requireCompleteGoTree(b, incrementalTree, edits[fileIndex].source, file.path, javaParseModeDFA, timeoutMicros)
			incrementalParses++
			if incrementalTree != nil && incrementalTree != tree {
				incrementalTree.Release()
			}
			tree.Release()
		}
	}
	javaCorpusBenchmarkSink = incrementalParses
	if b.N > 0 {
		b.ReportMetric(float64(incrementalParses)/float64(b.N), "incremental_parses/op")
	}
}

type javaCorpusSingleByteEdit struct {
	source []byte
	edit   gotreesitter.InputEdit
}

func prepareJavaCorpusSingleByteEdit(tb testing.TB, file javaCorpusFile) javaCorpusSingleByteEdit {
	tb.Helper()
	offset := findJavaCorpusDigitEditOffset(file.source)
	if offset <= 0 || offset >= len(file.source) {
		tb.Fatalf("%s: could not find interior digit edit site", file.path)
	}
	edited := append([]byte(nil), file.source...)
	edited[offset] = toggleJavaCorpusDigit(edited[offset])
	start := pointAtOffset(file.source, offset)
	end := pointAtOffset(file.source, offset+1)
	return javaCorpusSingleByteEdit{
		source: edited,
		edit: gotreesitter.InputEdit{
			StartByte:   uint32(offset),
			OldEndByte:  uint32(offset + 1),
			NewEndByte:  uint32(offset + 1),
			StartPoint:  start,
			OldEndPoint: end,
			NewEndPoint: end,
		},
	}
}

func findJavaCorpusDigitEditOffset(src []byte) int {
	if len(src) < 2 {
		return -1
	}
	mid := len(src) / 2
	if pos := findJavaCorpusDigitEditOffsetInRange(src, mid, len(src)); pos >= 0 {
		return pos
	}
	return findJavaCorpusDigitEditOffsetInRange(src, 1, mid)
}

func findJavaCorpusDigitEditOffsetInRange(src []byte, start, end int) int {
	if start < 1 {
		start = 1
	}
	if end > len(src) {
		end = len(src)
	}
	for i := start; i < end; i++ {
		if isJavaCorpusDigit(src[i]) {
			return i
		}
	}
	return -1
}

func isJavaCorpusDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func toggleJavaCorpusDigit(b byte) byte {
	if b == '0' {
		return '1'
	}
	return '0'
}

func countNamedJavaCorpusNodes(root *gotreesitter.Node, scratch *[]*gotreesitter.Node) int64 {
	if root == nil {
		return 0
	}
	stack := append((*scratch)[:0], root)
	var count int64
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node == nil {
			continue
		}
		if node.IsNamed() {
			count++
		}
		for i := node.ChildCount() - 1; i >= 0; i-- {
			if child := node.Child(i); child != nil {
				stack = append(stack, child)
			}
		}
	}
	*scratch = stack[:0]
	return count
}

func countJavaCorpusParentChecks(b *testing.B, path string, root *gotreesitter.Node, scratch *[]*gotreesitter.Node) int64 {
	b.Helper()
	if root == nil {
		return 0
	}
	stack := append((*scratch)[:0], root)
	var count int64
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node == nil {
			continue
		}
		for i := node.ChildCount() - 1; i >= 0; i-- {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if child.Parent() != node {
				b.Fatalf("%s: parent mismatch for child %d", path, i)
			}
			count++
			if child.IsNamed() {
				stack = append(stack, child)
			}
		}
	}
	*scratch = stack[:0]
	return count
}

func countJavaCorpusSiblingSteps(b *testing.B, path string, root *gotreesitter.Node, scratch *[]*gotreesitter.Node) int64 {
	b.Helper()
	if root == nil {
		return 0
	}
	stack := append((*scratch)[:0], root)
	var count int64
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node == nil {
			continue
		}
		var prev *gotreesitter.Node
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if prev != nil {
				if got := prev.NextSibling(); got != child {
					b.Fatalf("%s: next sibling mismatch at child %d", path, i)
				}
				if got := child.PrevSibling(); got != prev {
					b.Fatalf("%s: previous sibling mismatch at child %d", path, i)
				}
				count += 2
			}
			prev = child
			if child.IsNamed() {
				stack = append(stack, child)
			}
		}
	}
	*scratch = stack[:0]
	return count
}

func BenchmarkJavaCorpusCTreeSitterParseFull(b *testing.B) {
	files := loadJavaCorpus(b)
	totalBytes := totalJavaCorpusBytes(files)
	parser := newCTreeSitterJavaParser(b)
	defer parser.Close()

	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, file := range files {
			tree := parseCTree(b, parser, nil, file.source, file.path)
			root := requireCompleteCTree(b, tree, file.source, file.path)
			if root.HasError() {
				tree.Close()
				b.Fatalf("%s: cgo parse has errors", file.path)
			}
			tree.Close()
		}
	}
}
