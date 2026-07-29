//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/cgo_harness/internal/realcorpus"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type realCorpusBenchmarkCase struct {
	name   string
	path   string
	source []byte
	entry  grammars.LangEntry
	report grammars.ParseSupport
	goLang *gotreesitter.Language
	cLang  *sitter.Language
}

type realCorpusIncrementalCase struct {
	realCorpusBenchmarkCase
	label       string
	offset      int
	original    byte
	replacement byte
	goEdit      gotreesitter.InputEdit
	cEdit       sitter.InputEdit
}

type realCorpusRuntimeTotals struct {
	arenaBytes                        int64
	scratchBytes                      int64
	nodes                             uint64
	iterations                        uint64
	tokens                            uint64
	compactLeafMaterialized           uint64
	pendingParentMaterialized         uint64
	transientParents                  uint64
	transientChildPointers            uint64
	finalChildRefMaterializedChildren uint64
	finalChildRefSingleAccesses       uint64
	finalChildRefSingleMaterialized   uint64
	normalizationRewrites             uint64
	parseWallNanos                    int64
	parserLoopNanos                   int64
	tokenNextNanos                    int64
	actionDispatchNanos               int64
	actionLookupNanos                 int64
	glrMergeNanos                     int64
	glrCullNanos                      int64
	resultSelectionNanos              int64
	transientParentMaterializeNanos   int64
	resultTreeBuildNanos              int64
	transientChildMaterializeNanos    int64
	resultPythonKeywordRepairNanos    int64
	resultPythonRootRepairNanos       int64
	resultFinalizeRootNanos           int64
	resultExtendTrailingNanos         int64
	resultNormalizeRootStartNanos     int64
	resultCompatibilityNanos          int64
	resultParentLinkNanos             int64
	normalizationNanos                int64
	reduceRangeNanos                  int64
	reducePendingParentNanos          int64
	reduceChildBuildNanos             int64
	reduceParentBuildNanos            int64
	reduceSpanNanos                   int64
	reduceStackPushNanos              int64
	reduceNoTreeBuildNanos            int64
	actionExtraShiftNanos             int64
	actionNoActionNanos               int64
	actionNoActionRelexNanos          int64
	actionNoActionMissingNanos        int64
	actionNoActionRecoverNanos        int64
	actionNoActionErrorNanos          int64
	actionConflictChoiceNanos         int64
	actionConflictForkNanos           int64
	actionSingleShiftNanos            int64
	actionSingleReduceNanos           int64
	actionSingleAcceptNanos           int64
	actionSingleRecoverNanos          int64
	actionSingleOtherNanos            int64
}

type realCorpusIncrementalProfileTotals struct {
	editNanos                          int64
	parseWallNanos                     int64
	reuseNanos                         int64
	reparseNanos                       int64
	reusedSubtrees                     uint64
	reusedBytes                        uint64
	newNodes                           uint64
	reuseUnsupported                   uint64
	reuseRejectDirty                   uint64
	reuseRejectAncestorDirtyBeforeEdit uint64
	reuseRejectHasError                uint64
	reuseRejectInvalidSpan             uint64
	reuseRejectOutOfBounds             uint64
	reuseRejectRootNonLeafChanged      uint64
	reuseRejectLargeNonLeaf            uint64
	recoverSearches                    uint64
	recoverStateChecks                 uint64
	recoverStateSkips                  uint64
	recoverSymbolSkips                 uint64
	recoverLookups                     uint64
	recoverHits                        uint64
	maxStacksSeen                      int
	entryScratchPeak                   uint64
	tokensConsumed                     uint64
	singleStackIterations              int
	multiStackIterations               int
	singleStackTokens                  uint64
	multiStackTokens                   uint64
	singleStackGSSNodes                uint64
	multiStackGSSNodes                 uint64
	gssNodesAllocated                  uint64
	gssNodesRetained                   uint64
	gssNodesDroppedSameToken           uint64
	parentNodesAllocated               uint64
	parentNodesRetained                uint64
	parentNodesDroppedSameToken        uint64
	leafNodesAllocated                 uint64
	leafNodesRetained                  uint64
	leafNodesDroppedSameToken          uint64
	mergeStacksIn                      uint64
	mergeStacksOut                     uint64
	mergeSlotsUsed                     uint64
	globalCullStacksIn                 uint64
	globalCullStacksOut                uint64
	parserLoopNanos                    int64
	tokenNextNanos                     int64
	actionDispatchNanos                int64
	actionLookupNanos                  int64
	glrMergeNanos                      int64
	glrCullNanos                       int64
	resultSelectionNanos               int64
	transientParentMaterializeNanos    int64
	resultTreeBuildNanos               int64
	transientChildMaterializeNanos     int64
	resultPythonKeywordRepairNanos     int64
	resultPythonRootRepairNanos        int64
	resultFinalizeRootNanos            int64
	resultExtendTrailingNanos          int64
	resultNormalizeRootStartNanos      int64
	resultCompatibilityNanos           int64
	resultParentLinkNanos              int64
	normalizationNanos                 int64
}

type realCorpusGoIncrementalState struct {
	tc   realCorpusIncrementalCase
	src  []byte
	tree *gotreesitter.Tree
}

type realCorpusCIncrementalState struct {
	tc   realCorpusIncrementalCase
	src  []byte
	tree *sitter.Tree
}

type realCorpusGoNoEditState struct {
	tc   realCorpusBenchmarkCase
	tree *gotreesitter.Tree
}

type realCorpusCNoEditState struct {
	tc   realCorpusBenchmarkCase
	tree *sitter.Tree
}

func BenchmarkParityRealCorpusParseFull(b *testing.B) {
	for _, name := range realCorpusBenchmarkLanguages(b) {
		name := name
		b.Run(name, func(b *testing.B) {
			cases := prepareRealCorpusBenchmarkCases(b, name)
			if !realCorpusBenchmarkAllowMismatch() {
				verifyRealCorpusBenchmarkFreshParity(b, cases)
			}

			b.Run("gotreesitter", func(b *testing.B) {
				benchmarkRealCorpusGoParseFull(b, cases)
			})
			b.Run("tree-sitter-c", func(b *testing.B) {
				benchmarkRealCorpusCParseFull(b, cases)
			})
		})
	}
}

func BenchmarkParityRealCorpusParseIncrementalSingleByteEdit(b *testing.B) {
	for _, name := range realCorpusBenchmarkLanguages(b) {
		name := name
		b.Run(name, func(b *testing.B) {
			cases := prepareRealCorpusBenchmarkCases(b, name)
			if !realCorpusBenchmarkAllowMismatch() {
				verifyRealCorpusBenchmarkFreshParity(b, cases)
			}
			incrementalCases := prepareRealCorpusIncrementalCases(b, cases)

			b.Run("gotreesitter", func(b *testing.B) {
				benchmarkRealCorpusGoParseIncrementalSingleByteEdit(b, incrementalCases)
			})
			b.Run("tree-sitter-c", func(b *testing.B) {
				benchmarkRealCorpusCParseIncrementalSingleByteEdit(b, incrementalCases)
			})
		})
	}
}

func BenchmarkParityRealCorpusParseIncrementalNoEdit(b *testing.B) {
	for _, name := range realCorpusBenchmarkLanguages(b) {
		name := name
		b.Run(name, func(b *testing.B) {
			cases := prepareRealCorpusBenchmarkCases(b, name)
			if !realCorpusBenchmarkAllowMismatch() {
				verifyRealCorpusBenchmarkFreshParity(b, cases)
			}

			b.Run("gotreesitter", func(b *testing.B) {
				benchmarkRealCorpusGoParseIncrementalNoEdit(b, cases)
			})
			b.Run("tree-sitter-c", func(b *testing.B) {
				benchmarkRealCorpusCParseIncrementalNoEdit(b, cases)
			})
		})
	}
}

func realCorpusBenchmarkLanguages(b *testing.B) []string {
	b.Helper()
	raw := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_LANGS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("GTS_PARITY_BENCH_LANGS"))
	}
	if raw == "" || strings.EqualFold(raw, "top50") {
		b.Skip("set GTS_REAL_CORPUS_BENCH_LANGS to run real-corpus parity benchmarks")
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
		b.Fatalf("GTS_REAL_CORPUS_BENCH_LANGS=%q did not name any languages", raw)
	}
	return out
}

func prepareRealCorpusBenchmarkCases(b *testing.B, name string) []realCorpusBenchmarkCase {
	b.Helper()
	if parityLanguageExcluded(name) {
		b.Skipf("language excluded by GTS_PARITY_SKIP_LANGS: %s", name)
	}
	if reason := paritySkipReason(name); reason != "" {
		b.Skipf("known mismatch: %s", reason)
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

	root := realCorpusBenchmarkRoot(b)
	langRoot := realCorpusBenchmarkLanguageRoot(b, root, name)
	stat, err := os.Stat(langRoot)
	if err != nil || !stat.IsDir() {
		b.Skipf("no real corpus directory for %s under %s", name, root)
	}

	files := loadRealCorpusBenchmarkFiles(b, langRoot, realCorpusBenchmarkFileFiltersFor(b, name, root))
	cases := make([]realCorpusBenchmarkCase, 0, len(files))
	for _, file := range files {
		cases = append(cases, realCorpusBenchmarkCase{
			name:   name,
			path:   file.path,
			source: file.source,
			entry:  entry,
			report: report,
			goLang: entry.Language(),
			cLang:  cLang,
		})
	}
	b.Logf(
		"real corpus %s: files=%d bytes=%d root=%s order=%s cases=%s",
		name,
		len(cases),
		totalRealCorpusBenchmarkBytes(cases),
		langRoot,
		realCorpusBenchmarkOrder(),
		formatRealCorpusCaseList(cases),
	)
	if realCorpusBenchmarkSkipMismatch() && !realCorpusBenchmarkAllowMismatch() {
		cases = filterRealCorpusBenchmarkFreshParity(b, cases)
	}
	if realCorpusBenchmarkAllowMismatch() {
		b.Log("GTS_REAL_CORPUS_BENCH_ALLOW_MISMATCH=1: timing selected files without structural parity precheck")
	}
	return cases
}

type realCorpusBenchmarkFile struct {
	path   string
	source []byte
}

type realCorpusBenchmarkFileFilters = realcorpus.FileMatcher

func realCorpusBenchmarkRoot(b *testing.B) string {
	b.Helper()
	if root := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_ROOT")); root != "" {
		return root
	}
	for _, candidate := range []string{
		"corpus_real",
		filepath.Join("cgo_harness", "corpus_real"),
		filepath.Join("..", "cgo_harness", "corpus_real"),
		"corpus_sources",
		"corpus-sources",
		filepath.Join("cgo_harness", "corpus_sources"),
		filepath.Join("..", "cgo_harness", "corpus_sources"),
	} {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
	}
	b.Fatal("set GTS_REAL_CORPUS_BENCH_ROOT or run from the repository/cgo_harness root")
	return ""
}

func realCorpusBenchmarkLanguageRoot(tb testing.TB, root string, language string) string {
	tb.Helper()
	langRoot := filepath.Join(root, language)
	if !realCorpusBenchmarkUseLockFilter(root) {
		return langRoot
	}
	entry, ok := realCorpusBenchmarkLockEntryFor(tb, language)
	if !ok || strings.TrimSpace(entry.Subdir) == "" || strings.TrimSpace(entry.Subdir) == "." {
		return langRoot
	}
	subdir, ok := realcorpus.CleanSubdir(entry.Subdir)
	if !ok {
		tb.Fatalf("invalid real corpus lock subdir for %s: %q", language, entry.Subdir)
	}
	return filepath.Join(langRoot, subdir)
}

func loadRealCorpusBenchmarkFiles(tb testing.TB, root string, filters realCorpusBenchmarkFileFilters) []realCorpusBenchmarkFile {
	tb.Helper()
	minBytes := realCorpusBenchmarkEnvInt(tb, "GTS_REAL_CORPUS_BENCH_MIN_BYTES", 0)
	maxFileBytes := realCorpusBenchmarkEnvInt(tb, "GTS_REAL_CORPUS_BENCH_MAX_FILE_BYTES", 0)
	var files []realCorpusBenchmarkFile
	err := realcorpus.WalkMatchingFiles(root, func(relPath string) bool {
		return realCorpusBenchmarkFileAllowed(relPath, filters)
	}, func(file realcorpus.File) error {
		src, err := os.ReadFile(file.Path)
		if err != nil {
			return err
		}
		if len(src) < minBytes {
			return nil
		}
		if maxFileBytes > 0 && len(src) > maxFileBytes {
			return nil
		}
		files = append(files, realCorpusBenchmarkFile{path: file.Path, source: src})
		return nil
	})
	if err != nil {
		tb.Fatalf("load real corpus %s: %v", root, err)
	}
	if len(files) == 0 {
		tb.Fatalf("real corpus filters selected no files under %s", root)
	}
	sortRealCorpusBenchmarkFiles(tb, files)
	files = selectRealCorpusBenchmarkShard(tb, files)

	maxFiles := realCorpusBenchmarkEnvInt(tb, "GTS_REAL_CORPUS_BENCH_MAX_FILES", 0)
	maxBytes := realCorpusBenchmarkEnvInt(tb, "GTS_REAL_CORPUS_BENCH_MAX_BYTES", 0)
	selected := make([]realCorpusBenchmarkFile, 0, len(files))
	selectedBytes := 0
	for _, file := range files {
		if maxFiles > 0 && len(selected) >= maxFiles {
			break
		}
		if maxBytes > 0 && selectedBytes+len(file.source) > maxBytes {
			continue
		}
		selected = append(selected, file)
		selectedBytes += len(file.source)
	}
	if len(selected) == 0 {
		tb.Fatalf("real corpus filters selected no files under %s", root)
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].path < selected[j].path
	})
	return selected
}

func selectRealCorpusBenchmarkShard(tb testing.TB, files []realCorpusBenchmarkFile) []realCorpusBenchmarkFile {
	tb.Helper()
	shard, shards, ok := realCorpusBenchmarkShardSelection(tb)
	if !ok {
		return files
	}
	start := (len(files) * (shard - 1)) / shards
	end := (len(files) * shard) / shards
	if start == end {
		tb.Fatalf("GTS_REAL_CORPUS_BENCH_SHARD=%d/%d selected no files from %d file(s)", shard, shards, len(files))
	}
	selected := files[start:end]
	tb.Logf(
		"GTS_REAL_CORPUS_BENCH_SHARD selected shard %d/%d: files=%d/%d bytes=%d/%d",
		shard,
		shards,
		len(selected),
		len(files),
		totalRealCorpusBenchmarkFileBytes(selected),
		totalRealCorpusBenchmarkFileBytes(files),
	)
	return selected
}

func realCorpusBenchmarkShardSelection(tb testing.TB) (int, int, bool) {
	tb.Helper()
	rawShards := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_SHARDS"))
	rawShard := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_SHARD"))
	if rawShards == "" && rawShard == "" {
		return 0, 0, false
	}
	if rawShards == "" {
		tb.Fatal("GTS_REAL_CORPUS_BENCH_SHARD requires GTS_REAL_CORPUS_BENCH_SHARDS")
	}
	shards := realCorpusBenchmarkEnvInt(tb, "GTS_REAL_CORPUS_BENCH_SHARDS", 0)
	if shards < 1 {
		tb.Fatalf("invalid GTS_REAL_CORPUS_BENCH_SHARDS=%q; want >= 1", rawShards)
	}
	shard := 1
	if rawShard != "" {
		shard = realCorpusBenchmarkEnvInt(tb, "GTS_REAL_CORPUS_BENCH_SHARD", 0)
	}
	if shard < 1 || shard > shards {
		tb.Fatalf("invalid GTS_REAL_CORPUS_BENCH_SHARD=%q; want 1..%d", rawShard, shards)
	}
	if shards == 1 {
		return 0, 0, false
	}
	return shard, shards, true
}

func realCorpusBenchmarkFileAllowed(path string, filters realCorpusBenchmarkFileFilters) bool {
	return filters.Matches(path)
}

func realCorpusBenchmarkFileFiltersFor(tb testing.TB, language string, root string) realCorpusBenchmarkFileFilters {
	tb.Helper()
	if !realCorpusBenchmarkUseLockFilter(root) {
		return realcorpus.AllowAllFiles()
	}
	entry, _ := realCorpusBenchmarkLockEntryFor(tb, language)
	fallback := realCorpusBenchmarkRegistryExtensions(language)
	if len(fallback) == 0 {
		if entry, ok := parityEntriesByName[language]; ok {
			fallback = append([]string(nil), entry.Extensions...)
		}
	}
	return realcorpus.NewFileMatcher(language, entry.Exts, fallback)
}

func TestRealCorpusBenchmarkMatcherAddsCanonicalNames(t *testing.T) {
	tests := []struct {
		language   string
		allowed    []string
		disallowed string
	}{
		{language: "dockerfile", allowed: []string{"examples/1", "Dockerfile", "service.dockerfile"}, disallowed: "test/corpus/from.txt"},
		{language: "earthfile", allowed: []string{"examples/go/Earthfile", "build.earth"}, disallowed: "test/corpus/from.txt"},
		{language: "git_rebase", allowed: []string{"test/highlight/rebase-merges.git-rebase-todo", "git-rebase-todo"}, disallowed: "test/corpus/corpus.txt"},
		{language: "gomod", allowed: []string{"go.mod", "internal/foo/go.mod"}, disallowed: "go.sum"},
		{language: "kconfig", allowed: []string{"arch/Kconfig", "arch/Kconfig.nxp"}, disallowed: "docs/kconfig.txt"},
		{language: "meson", allowed: []string{"examples/meson.build", "meson_options.txt"}, disallowed: "test/corpus/commands.txt"},
		{language: "nginx", allowed: []string{"test/nginx.conf", "test/conf.nginx"}, disallowed: "test/corpus/base.txt"},
		{language: "requirements", allowed: []string{"requirements.txt"}, disallowed: "test/corpus/example.txt"},
		{language: "ssh_config", allowed: []string{"test/highlight/ssh_config", "known_hosts"}, disallowed: "test/corpus/examples.txt"},
		{language: "todotxt", allowed: []string{"examples/todo.txt"}, disallowed: "corpus/mix.txt"},
	}
	for _, tc := range tests {
		filters := realcorpus.NewFileMatcher(tc.language, nil, nil)
		for _, path := range tc.allowed {
			if !realCorpusBenchmarkFileAllowed(path, filters) {
				t.Fatalf("%s should allow %q: %#v", tc.language, path, filters)
			}
		}
		if realCorpusBenchmarkFileAllowed(tc.disallowed, filters) {
			t.Fatalf("%s should not allow path %q: %#v", tc.language, tc.disallowed, filters)
		}
	}
}

func TestRealCorpusBenchmarkLockMatchersAreAuthoritative(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "corpus_sources.lock")
	if err := os.WriteFile(lockPath, []byte(strings.Join([]string{
		"# name repo commit subdir extensions",
		"ssh_config https://example.invalid/openssh abc123 . ssh_config",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK", lockPath)
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK_FILTER", "1")

	filters := realCorpusBenchmarkFileFiltersFor(t, "ssh_config", filepath.Join(dir, "corpus-sources", "ssh_config"))
	if !realCorpusBenchmarkFileAllowed("ssh_config", filters) {
		t.Fatalf("explicit lock matcher should allow ssh_config: %#v", filters)
	}
	for _, path := range []string{"sshd_config", "known_hosts", "authorized_keys"} {
		if realCorpusBenchmarkFileAllowed(path, filters) {
			t.Fatalf("explicit lock matcher should not allow canonical extra %q: %#v", path, filters)
		}
	}
}

func TestRealCorpusBenchmarkLockPathMatchersAreAuthoritative(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "corpus_sources.lock")
	if err := os.WriteFile(lockPath, []byte(strings.Join([]string{
		"# name repo commit subdir extensions",
		"ini https://example.invalid/cpython abc123 . Lib/tomllib/mypy.ini",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK", lockPath)
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK_FILTER", "1")

	filters := realCorpusBenchmarkFileFiltersFor(t, "ini", filepath.Join(dir, "corpus-sources", "ini"))
	if !realCorpusBenchmarkFileAllowed("Lib/tomllib/mypy.ini", filters) {
		t.Fatalf("explicit path matcher should allow exact path: %#v", filters)
	}
	if realCorpusBenchmarkFileAllowed("Lib/_pyrepl/mypy.ini", filters) {
		t.Fatalf("explicit path matcher should not allow duplicate basename: %#v", filters)
	}
	if realCorpusBenchmarkFileAllowed("mypy.ini", filters) {
		t.Fatalf("explicit path matcher should not degrade to basename match: %#v", filters)
	}
}

func TestRealCorpusBenchmarkRootPathMatchersAreAuthoritative(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "corpus_sources.lock")
	if err := os.WriteFile(lockPath, []byte(strings.Join([]string{
		"# name repo commit subdir extensions",
		"requirements https://example.invalid/ansible abc123 . ./requirements.txt",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK", lockPath)
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK_FILTER", "1")

	filters := realCorpusBenchmarkFileFiltersFor(t, "requirements", filepath.Join(dir, "corpus-sources", "requirements"))
	if !realCorpusBenchmarkFileAllowed("requirements.txt", filters) {
		t.Fatalf("root path matcher should allow exact root file: %#v", filters)
	}
	if realCorpusBenchmarkFileAllowed("test/units/requirements.txt", filters) {
		t.Fatalf("root path matcher should not allow duplicate basename: %#v", filters)
	}
}

func TestRealCorpusBenchmarkCasePathPrefersLanguageRelativePath(t *testing.T) {
	got := realCorpusBenchmarkCasePath(realCorpusBenchmarkCase{
		name: "ini",
		path: filepath.Join("/corpus-sources", "ini", "Lib", "tomllib", "mypy.ini"),
	})
	if got != "Lib/tomllib/mypy.ini" {
		t.Fatalf("case path = %q, want language-relative path", got)
	}
}

func TestRealCorpusBenchmarkLanguageRootHonorsLockSubdir(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "corpus_sources.lock")
	if err := os.WriteFile(lockPath, []byte(strings.Join([]string{
		"# name repo commit subdir extensions",
		"vimdoc https://example.invalid/vimdoc abc123 runtime/doc .txt",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "corpus-sources")
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK", lockPath)
	t.Setenv("GTS_REAL_CORPUS_BENCH_LOCK_FILTER", "1")

	got := filepath.ToSlash(realCorpusBenchmarkLanguageRoot(t, root, "vimdoc"))
	want := filepath.ToSlash(filepath.Join(root, "vimdoc", "runtime", "doc"))
	if got != want {
		t.Fatalf("language root=%q, want %q", got, want)
	}
}

func TestSelectRealCorpusBenchmarkShardUsesContiguousOrderedSlices(t *testing.T) {
	files := []realCorpusBenchmarkFile{
		{path: "a.sh", source: []byte("a")},
		{path: "b.sh", source: []byte("bb")},
		{path: "c.sh", source: []byte("ccc")},
		{path: "d.sh", source: []byte("dddd")},
		{path: "e.sh", source: []byte("eeeee")},
	}
	t.Setenv("GTS_REAL_CORPUS_BENCH_SHARDS", "2")
	t.Setenv("GTS_REAL_CORPUS_BENCH_SHARD", "2")

	got := selectRealCorpusBenchmarkShard(t, files)
	gotPaths := make([]string, 0, len(got))
	for _, file := range got {
		gotPaths = append(gotPaths, file.path)
	}
	want := []string{"c.sh", "d.sh", "e.sh"}
	if strings.Join(gotPaths, ",") != strings.Join(want, ",") {
		t.Fatalf("selected shard paths=%v, want %v", gotPaths, want)
	}
}

func TestLoadRealCorpusBenchmarkFilesSkipsNonRegularMatchingPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "schema.prisma"), []byte("model User { id Int @id }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(dir, "fixture-dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, filepath.Join(dir, "linked-dir.prisma")); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	files := loadRealCorpusBenchmarkFiles(t, dir, realcorpus.NewFileMatcher("", []string{".prisma"}, nil))
	if len(files) != 1 {
		t.Fatalf("selected files=%d, want 1: %#v", len(files), files)
	}
	if got := filepath.Base(files[0].path); got != "schema.prisma" {
		t.Fatalf("selected path=%q, want schema.prisma", got)
	}
}

func realCorpusBenchmarkRegistryExtensions(language string) []string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return nil
	}
	for _, entry := range grammars.AllLanguages() {
		if strings.ToLower(strings.TrimSpace(entry.Name)) != language {
			continue
		}
		out := make([]string, 0, len(entry.Extensions))
		for _, ext := range entry.Extensions {
			ext = strings.ToLower(strings.TrimSpace(ext))
			if ext != "" {
				out = append(out, ext)
			}
		}
		return out
	}
	return nil
}

func realCorpusBenchmarkUseLockFilter(root string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_LOCK_FILTER"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(root))
	return strings.HasSuffix(clean, "corpus_sources") || strings.HasSuffix(clean, "corpus-sources")
}

type realCorpusBenchmarkLockEntry struct {
	Subdir string
	Exts   []string
}

func realCorpusBenchmarkLockPath(tb testing.TB) string {
	tb.Helper()
	if path := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_LOCK")); path != "" {
		return path
	}
	for _, candidate := range []string{
		filepath.Join("..", "grammars", "languages.lock"),
		filepath.Join("grammars", "languages.lock"),
		filepath.Join("..", "..", "grammars", "languages.lock"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	tb.Fatal("set GTS_REAL_CORPUS_BENCH_LOCK for lock-filtered full corpus benchmarks")
	return ""
}

func realCorpusBenchmarkLockEntryFor(tb testing.TB, language string) (realCorpusBenchmarkLockEntry, bool) {
	tb.Helper()
	lockPath := realCorpusBenchmarkLockPath(tb)
	entries, err := realCorpusBenchmarkLockEntries(lockPath)
	if err != nil {
		tb.Fatalf("load real corpus benchmark lock filters: %v", err)
	}
	entry, ok := entries[language]
	return entry, ok
}

func realCorpusBenchmarkLockEntries(path string) (map[string]realCorpusBenchmarkLockEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]realCorpusBenchmarkLockEntry{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil, fmt.Errorf("%s:%d: invalid lock row", path, lineNo)
		}
		entry := realCorpusBenchmarkLockEntry{}
		if len(fields) >= 4 {
			entry.Subdir = fields[3]
		}
		if len(fields) >= 5 {
			for _, ext := range strings.Split(fields[4], ",") {
				ext = strings.TrimSpace(ext)
				if ext != "" {
					entry.Exts = append(entry.Exts, ext)
				}
			}
		}
		out[fields[0]] = entry
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func sortRealCorpusBenchmarkFiles(tb testing.TB, files []realCorpusBenchmarkFile) {
	tb.Helper()
	switch realCorpusBenchmarkOrder() {
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
	default:
		tb.Fatalf("invalid GTS_REAL_CORPUS_BENCH_ORDER=%q; want path, largest, or smallest", realCorpusBenchmarkOrder())
	}
}

func realCorpusBenchmarkOrder() string {
	order := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_ORDER"))
	if order == "" {
		return "path"
	}
	return order
}

func realCorpusBenchmarkSkipMismatch() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_SKIP_MISMATCH"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func realCorpusBenchmarkAllowMismatch() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_ALLOW_MISMATCH"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func realCorpusBenchmarkEnvInt(tb testing.TB, name string, fallback int) int {
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

func totalRealCorpusBenchmarkFileBytes(files []realCorpusBenchmarkFile) int64 {
	var total int64
	for _, file := range files {
		total += int64(len(file.source))
	}
	return total
}

func totalRealCorpusBenchmarkBytes(cases []realCorpusBenchmarkCase) int64 {
	var total int64
	for _, tc := range cases {
		total += int64(len(tc.source))
	}
	return total
}

func verifyRealCorpusBenchmarkFreshParity(b *testing.B, cases []realCorpusBenchmarkCase) {
	b.Helper()
	for _, tc := range cases {
		errs, first := realCorpusBenchmarkFreshParityErrors(b, tc)
		if len(errs) == 0 {
			continue
		}
		b.Fatalf(
			"structural parity mismatch before benchmark for %s/%s: first=%s\n  %s",
			tc.name,
			realCorpusBenchmarkCasePath(tc),
			formatRealCorpusDivergence(first),
			firstTop50BenchmarkLines(errs, 12),
		)
	}
}

func filterRealCorpusBenchmarkFreshParity(b *testing.B, cases []realCorpusBenchmarkCase) []realCorpusBenchmarkCase {
	b.Helper()
	out := make([]realCorpusBenchmarkCase, 0, len(cases))
	var skipped []string
	for _, tc := range cases {
		errs, first, failure := tryRealCorpusBenchmarkFreshParity(b, tc)
		if failure != "" {
			skipped = append(skipped, fmt.Sprintf("%s(%s)", realCorpusBenchmarkCasePath(tc), failure))
			continue
		}
		if len(errs) == 0 {
			out = append(out, tc)
			continue
		}
		skipped = append(skipped, fmt.Sprintf("%s(%s)", realCorpusBenchmarkCasePath(tc), formatRealCorpusDivergence(first)))
	}
	if len(out) == 0 {
		b.Fatalf("GTS_REAL_CORPUS_BENCH_SKIP_MISMATCH selected no parity-clean files; skipped=%s", strings.Join(skipped, ", "))
	}
	if len(skipped) > 0 {
		b.Logf("GTS_REAL_CORPUS_BENCH_SKIP_MISMATCH skipped %d/%d file(s): %s", len(skipped), len(cases), strings.Join(skipped, ", "))
	}
	return out
}

func realCorpusBenchmarkFreshParityErrors(b *testing.B, tc realCorpusBenchmarkCase) ([]string, *DumpV1Divergence) {
	b.Helper()
	errs, first, failure := tryRealCorpusBenchmarkFreshParity(b, tc)
	if failure != "" {
		b.Fatal(failure)
	}
	return errs, first
}

func tryRealCorpusBenchmarkFreshParity(b *testing.B, tc realCorpusBenchmarkCase) ([]string, *DumpV1Divergence, string) {
	b.Helper()
	goParser := gotreesitter.NewParser(tc.goLang)
	goTree, failure := tryParseRealCorpusGoFull(tc, goParser, "gotreesitter full")
	if failure != "" {
		return nil, nil, fmt.Sprintf(
			"gotreesitter full %s/%s %s",
			tc.name,
			realCorpusBenchmarkCasePath(tc),
			failure,
		)
	}
	cParser := newRealCorpusCParser(b, tc)
	cTree, failure := tryParseRealCorpusCFull(cParser, tc.source, "C full")
	if failure != "" {
		releaseGoTree(goTree)
		cParser.Close()
		return nil, nil, fmt.Sprintf(
			"C full %s/%s %s",
			tc.name,
			realCorpusBenchmarkCasePath(tc),
			failure,
		)
	}

	var errs []string
	compareNodes(goTree.RootNode(), tc.goLang, cTree.RootNode(), "root", &errs)
	first := FirstDivergenceDumpV1(goTree.RootNode(), tc.goLang, cTree.RootNode())
	releaseGoTree(goTree)
	cTree.Close()
	cParser.Close()
	return errs, first, ""
}

func formatRealCorpusDivergence(diff *DumpV1Divergence) string {
	if diff == nil {
		return "(none)"
	}
	return fmt.Sprintf("%s %s go=%s c=%s", diff.Path, diff.Category, diff.GoValue, diff.CValue)
}

func prepareRealCorpusIncrementalCases(b *testing.B, cases []realCorpusBenchmarkCase) []realCorpusIncrementalCase {
	b.Helper()
	out := make([]realCorpusIncrementalCase, 0, len(cases))
	for _, tc := range cases {
		editCase, ok := prepareRealCorpusIncrementalCase(b, tc)
		if !ok {
			b.Logf("skip %s: no single-byte incremental edit site matched benchmark constraints", tc.path)
			continue
		}
		out = append(out, editCase)
	}
	if len(out) == 0 {
		b.Skip("no selected files had a single-byte incremental edit site matching benchmark constraints")
	}
	return out
}

func prepareRealCorpusIncrementalCase(b *testing.B, tc realCorpusBenchmarkCase) (realCorpusIncrementalCase, bool) {
	b.Helper()
	candidates := incrementalEditCandidates(tc.source)
	maxCandidates := realCorpusBenchmarkEnvInt(b, "GTS_REAL_CORPUS_BENCH_EDIT_CANDIDATES", 128)
	maxRejectLogs := realCorpusBenchmarkEnvInt(b, "GTS_REAL_CORPUS_BENCH_EDIT_REJECTION_LOGS", 4)
	tried := 0
	rejectLogs := 0
	for _, candidate := range candidates {
		if candidate.oldEnd != candidate.start+1 || len(candidate.replacement) != 1 {
			continue
		}
		if maxCandidates > 0 && tried >= maxCandidates {
			break
		}
		tried++
		if realCorpusBenchmarkAllowMismatch() {
			return makeRealCorpusIncrementalCase(tc, candidate), true
		}
		editCase, rejectReason, ok := verifyRealCorpusIncrementalCandidate(b, tc, candidate)
		if ok {
			return editCase, true
		}
		if rejectReason != "" && (maxRejectLogs <= 0 || rejectLogs < maxRejectLogs) {
			b.Logf("skip %s/%s candidate=%s: %s", tc.name, realCorpusBenchmarkCasePath(tc), candidate.label, rejectReason)
			rejectLogs++
		}
	}
	return realCorpusIncrementalCase{}, false
}

func makeRealCorpusIncrementalCase(tc realCorpusBenchmarkCase, candidate incrementalEditCandidate) realCorpusIncrementalCase {
	edited := applyEditCandidate(tc.source, candidate)
	goEdit := gotreesitter.InputEdit{
		StartByte:   uint32(candidate.start),
		OldEndByte:  uint32(candidate.oldEnd),
		NewEndByte:  uint32(candidate.newEnd()),
		StartPoint:  pointAtOffset(tc.source, candidate.start),
		OldEndPoint: pointAtOffset(tc.source, candidate.oldEnd),
		NewEndPoint: pointAtOffset(edited, candidate.newEnd()),
	}
	return realCorpusIncrementalCase{
		realCorpusBenchmarkCase: tc,
		label:                   candidate.label,
		offset:                  candidate.start,
		original:                tc.source[candidate.start],
		replacement:             candidate.replacement[0],
		goEdit:                  goEdit,
		cEdit:                   realCorpusCInputEdit(goEdit),
	}
}

func verifyRealCorpusIncrementalCandidate(b *testing.B, tc realCorpusBenchmarkCase, candidate incrementalEditCandidate) (realCorpusIncrementalCase, string, bool) {
	b.Helper()
	editCase := makeRealCorpusIncrementalCase(tc, candidate)
	edited := applyEditCandidate(tc.source, candidate)

	goParser := gotreesitter.NewParser(tc.goLang)
	goFreshTree, goFreshFailure := tryParseRealCorpusGoFull(realCorpusCaseWithSource(tc, edited), goParser, "gotreesitter fresh edit candidate")
	if goFreshFailure != "" {
		return realCorpusIncrementalCase{}, goFreshFailure, false
	}
	defer releaseGoTree(goFreshTree)

	cParser := newRealCorpusCParser(b, tc)
	defer cParser.Close()
	cFreshTree, cFreshFailure := tryParseRealCorpusCFull(cParser, edited, "C fresh edit candidate")
	if cFreshFailure != "" {
		return realCorpusIncrementalCase{}, cFreshFailure, false
	}
	defer cFreshTree.Close()
	var freshErrs []string
	compareNodes(goFreshTree.RootNode(), tc.goLang, cFreshTree.RootNode(), "root", &freshErrs)
	if len(freshErrs) > 0 {
		return realCorpusIncrementalCase{}, formatRealCorpusCandidateFreshMismatch(goFreshTree, tc.goLang, cFreshTree, freshErrs), false
	}

	goOldTree := parseRealCorpusGoFull(b, tc, goParser)
	goOldTree.Edit(editCase.goEdit)
	verifyPhase := fmt.Sprintf(
		"gotreesitter incremental candidate=%s offset=%d %q->%q",
		editCase.label,
		editCase.offset,
		editCase.original,
		editCase.replacement,
	)
	goIncrTree, ok := tryParseCompleteRealCorpusGoIncrementalWithPhase(b, realCorpusCaseWithSource(tc, edited), goParser, goOldTree, verifyPhase)
	releaseGoTree(goOldTree)
	if !ok {
		return realCorpusIncrementalCase{}, "gotreesitter incremental incomplete parse", false
	}
	defer releaseGoTree(goIncrTree)
	var goIncrErrs []string
	compareGoNodes(goIncrTree.RootNode(), tc.goLang, goFreshTree.RootNode(), "root", &goIncrErrs)
	if len(goIncrErrs) > 0 {
		return realCorpusIncrementalCase{}, "gotreesitter incremental mismatch against fresh: " + firstRealCorpusBenchmarkError(goIncrErrs), false
	}

	cOldTree := parseRealCorpusCFull(b, cParser, tc.source)
	cOldTree.Edit(&editCase.cEdit)
	cIncrTree, ok := tryParseCompleteRealCorpusCIncremental(cParser, edited, cOldTree)
	cOldTree.Close()
	if !ok {
		return realCorpusIncrementalCase{}, "C incremental incomplete parse", false
	}
	defer cIncrTree.Close()
	var cIncrErrs []string
	compareNodes(goFreshTree.RootNode(), tc.goLang, cIncrTree.RootNode(), "root", &cIncrErrs)
	if len(cIncrErrs) > 0 {
		return realCorpusIncrementalCase{}, formatRealCorpusCandidateCIncrementalMismatch(goFreshTree, tc.goLang, cIncrTree, cIncrErrs), false
	}

	return editCase, "", true
}

func formatRealCorpusCandidateFreshMismatch(goTree *gotreesitter.Tree, goLang *gotreesitter.Language, cTree *sitter.Tree, errs []string) string {
	if diff := FirstDivergenceDumpV1(goTree.RootNode(), goLang, cTree.RootNode()); diff != nil {
		return "fresh structural mismatch: first=" + formatRealCorpusDivergence(diff)
	}
	return "fresh structural mismatch: " + firstRealCorpusBenchmarkError(errs)
}

func formatRealCorpusCandidateCIncrementalMismatch(goFreshTree *gotreesitter.Tree, goLang *gotreesitter.Language, cIncrTree *sitter.Tree, errs []string) string {
	if diff := FirstDivergenceDumpV1(goFreshTree.RootNode(), goLang, cIncrTree.RootNode()); diff != nil {
		return "C incremental mismatch against fresh: first=" + formatRealCorpusDivergence(diff)
	}
	return "C incremental mismatch against fresh: " + firstRealCorpusBenchmarkError(errs)
}

func firstRealCorpusBenchmarkError(errs []string) string {
	if len(errs) == 0 {
		return "(unknown)"
	}
	line := strings.TrimSpace(firstTop50BenchmarkLines(errs, 1))
	line = strings.ReplaceAll(line, "\n", "; ")
	if line == "" {
		return "(unknown)"
	}
	return line
}

func realCorpusCaseWithSource(tc realCorpusBenchmarkCase, source []byte) realCorpusBenchmarkCase {
	tc.source = source
	return tc
}

func realCorpusCInputEdit(edit gotreesitter.InputEdit) sitter.InputEdit {
	return sitter.InputEdit{
		StartByte:      uint(edit.StartByte),
		OldEndByte:     uint(edit.OldEndByte),
		NewEndByte:     uint(edit.NewEndByte),
		StartPosition:  sitter.Point{Row: uint(edit.StartPoint.Row), Column: uint(edit.StartPoint.Column)},
		OldEndPosition: sitter.Point{Row: uint(edit.OldEndPoint.Row), Column: uint(edit.OldEndPoint.Column)},
		NewEndPosition: sitter.Point{Row: uint(edit.NewEndPoint.Row), Column: uint(edit.NewEndPoint.Column)},
	}
}

func benchmarkRealCorpusGoParseFull(b *testing.B, cases []realCorpusBenchmarkCase) {
	parser := gotreesitter.NewParser(cases[0].goLang)
	b.ReportAllocs()
	b.SetBytes(totalRealCorpusBenchmarkBytes(cases))
	b.ResetTimer()

	var totals realCorpusRuntimeTotals
	for i := 0; i < b.N; i++ {
		for _, tc := range cases {
			tree := parseRealCorpusGoFull(b, tc, parser)
			totals.add(tree.ParseRuntime())
			releaseGoTree(tree)
		}
	}
	totals.report(b, cases, b.N)
}

func benchmarkRealCorpusCParseFull(b *testing.B, cases []realCorpusBenchmarkCase) {
	parser := newRealCorpusCParser(b, cases[0])
	defer parser.Close()
	b.ReportAllocs()
	b.SetBytes(totalRealCorpusBenchmarkBytes(cases))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, tc := range cases {
			tree := parseRealCorpusCFull(b, parser, tc.source)
			tree.Close()
		}
	}
	reportRealCorpusCaseMetrics(b, cases)
}

func benchmarkRealCorpusGoParseIncrementalSingleByteEdit(b *testing.B, cases []realCorpusIncrementalCase) {
	parser := gotreesitter.NewParser(cases[0].goLang)
	states := make([]realCorpusGoIncrementalState, 0, len(cases))
	for _, tc := range cases {
		tree := parseRealCorpusGoFull(b, tc.realCorpusBenchmarkCase, parser)
		states = append(states, realCorpusGoIncrementalState{
			tc:   tc,
			src:  append([]byte(nil), tc.source...),
			tree: tree,
		})
	}
	defer func() {
		for _, state := range states {
			releaseGoTree(state.tree)
		}
	}()

	b.ReportAllocs()
	b.SetBytes(totalRealCorpusIncrementalBytes(cases))
	b.ResetTimer()

	var totals realCorpusRuntimeTotals
	var profileTotals realCorpusIncrementalProfileTotals
	for i := 0; i < b.N; i++ {
		for stateIndex := range states {
			state := &states[stateIndex]
			toggleRealCorpusEditByte(state.src, state.tc)
			editStart := time.Now()
			state.tree.Edit(state.tc.goEdit)
			profileTotals.addEdit(time.Since(editStart))
			oldTree := state.tree
			parseStart := time.Now()
			phase := fmt.Sprintf(
				"gotreesitter incremental edit=%s offset=%d %q->%q",
				state.tc.label,
				state.tc.offset,
				state.tc.original,
				state.tc.replacement,
			)
			newTree, profile := parseRealCorpusGoIncrementalProfiledWithPhase(b, realCorpusCaseWithSource(state.tc.realCorpusBenchmarkCase, state.src), parser, state.tree, phase)
			profileTotals.addParseWall(time.Since(parseStart))
			profileTotals.add(profile)
			if newTree != oldTree {
				totals.add(newTree.ParseRuntime())
			}
			if newTree != state.tree {
				releaseGoTree(state.tree)
			}
			state.tree = newTree
		}
	}
	totals.report(b, realCorpusCasesFromIncremental(cases), b.N)
	profileTotals.report(b, b.N)
}

func benchmarkRealCorpusCParseIncrementalSingleByteEdit(b *testing.B, cases []realCorpusIncrementalCase) {
	parser := newRealCorpusCParser(b, cases[0].realCorpusBenchmarkCase)
	defer parser.Close()
	states := make([]realCorpusCIncrementalState, 0, len(cases))
	for _, tc := range cases {
		src := append([]byte(nil), tc.source...)
		tree := parseRealCorpusCFull(b, parser, src)
		states = append(states, realCorpusCIncrementalState{
			tc:   tc,
			src:  src,
			tree: tree,
		})
	}
	defer func() {
		for _, state := range states {
			state.tree.Close()
		}
	}()

	b.ReportAllocs()
	b.SetBytes(totalRealCorpusIncrementalBytes(cases))
	b.ResetTimer()

	var editNanos int64
	var parseWallNanos int64
	for i := 0; i < b.N; i++ {
		for stateIndex := range states {
			state := &states[stateIndex]
			toggleRealCorpusEditByte(state.src, state.tc)
			editStart := time.Now()
			state.tree.Edit(&state.tc.cEdit)
			editNanos += time.Since(editStart).Nanoseconds()
			parseStart := time.Now()
			newTree := parseRealCorpusCIncremental(b, parser, state.src, state.tree)
			parseWallNanos += time.Since(parseStart).Nanoseconds()
			if newTree != state.tree {
				state.tree.Close()
			}
			state.tree = newTree
		}
	}
	reportRealCorpusCaseMetrics(b, realCorpusCasesFromIncremental(cases))
	if b.N > 0 {
		b.ReportMetric(float64(editNanos)/float64(b.N), "edit_ns/op")
		b.ReportMetric(float64(parseWallNanos)/float64(b.N), "parse_wall_ns/op")
	}
}

func benchmarkRealCorpusGoParseIncrementalNoEdit(b *testing.B, cases []realCorpusBenchmarkCase) {
	parser := gotreesitter.NewParser(cases[0].goLang)
	states := make([]realCorpusGoNoEditState, 0, len(cases))
	for _, tc := range cases {
		states = append(states, realCorpusGoNoEditState{
			tc:   tc,
			tree: parseRealCorpusGoFull(b, tc, parser),
		})
	}
	defer func() {
		for _, state := range states {
			releaseGoTree(state.tree)
		}
	}()

	b.ReportAllocs()
	b.SetBytes(totalRealCorpusBenchmarkBytes(cases))
	b.ResetTimer()

	var totals realCorpusRuntimeTotals
	var profileTotals realCorpusIncrementalProfileTotals
	for i := 0; i < b.N; i++ {
		for stateIndex := range states {
			state := &states[stateIndex]
			oldTree := state.tree
			parseStart := time.Now()
			newTree, profile := parseRealCorpusGoIncrementalProfiled(b, state.tc, parser, state.tree)
			profileTotals.addParseWall(time.Since(parseStart))
			profileTotals.add(profile)
			if newTree != oldTree {
				totals.add(newTree.ParseRuntime())
			}
			if newTree != state.tree {
				releaseGoTree(state.tree)
			}
			state.tree = newTree
		}
	}
	totals.report(b, cases, b.N)
	profileTotals.report(b, b.N)
}

func benchmarkRealCorpusCParseIncrementalNoEdit(b *testing.B, cases []realCorpusBenchmarkCase) {
	parser := newRealCorpusCParser(b, cases[0])
	defer parser.Close()
	states := make([]realCorpusCNoEditState, 0, len(cases))
	for _, tc := range cases {
		states = append(states, realCorpusCNoEditState{
			tc:   tc,
			tree: parseRealCorpusCFull(b, parser, tc.source),
		})
	}
	defer func() {
		for _, state := range states {
			state.tree.Close()
		}
	}()

	b.ReportAllocs()
	b.SetBytes(totalRealCorpusBenchmarkBytes(cases))
	b.ResetTimer()

	var parseWallNanos int64
	for i := 0; i < b.N; i++ {
		for stateIndex := range states {
			state := &states[stateIndex]
			parseStart := time.Now()
			newTree := parseRealCorpusCIncremental(b, parser, state.tc.source, state.tree)
			parseWallNanos += time.Since(parseStart).Nanoseconds()
			if newTree != state.tree {
				state.tree.Close()
			}
			state.tree = newTree
		}
	}
	reportRealCorpusCaseMetrics(b, cases)
	if b.N > 0 {
		b.ReportMetric(float64(parseWallNanos)/float64(b.N), "parse_wall_ns/op")
	}
}

func parseRealCorpusGoFull(tb testing.TB, tc realCorpusBenchmarkCase, parser *gotreesitter.Parser) *gotreesitter.Tree {
	tb.Helper()
	tree, failure := tryParseRealCorpusGoFull(tc, parser, "gotreesitter full")
	if failure != "" {
		tb.Fatalf("%s %s/%s %s", "gotreesitter full", tc.name, realCorpusBenchmarkCasePath(tc), failure)
	}
	return tree
}

func tryParseRealCorpusGoFull(tc realCorpusBenchmarkCase, parser *gotreesitter.Parser, phase string) (*gotreesitter.Tree, string) {
	var tree *gotreesitter.Tree
	var err error
	switch tc.report.Backend {
	case grammars.ParseBackendTokenSource:
		if tc.entry.TokenSourceFactory == nil {
			return nil, fmt.Sprintf("token source backend without factory for %q", tc.name)
		}
		tree, err = parser.ParseWithTokenSource(tc.source, tc.entry.TokenSourceFactory(tc.source, tc.goLang))
	case grammars.ParseBackendDFA, grammars.ParseBackendDFAPartial:
		tree, err = parser.Parse(tc.source)
	default:
		return nil, fmt.Sprintf("unsupported parse backend %q for %q", tc.report.Backend, tc.name)
	}
	if failure := completeRealCorpusGoTreeFailure(tc, tree, phase, err); failure != "" {
		if tree != nil {
			releaseGoTree(tree)
		}
		return nil, failure
	}
	return tree, ""
}

func tryParseCompleteRealCorpusGoIncrementalWithPhase(tb testing.TB, tc realCorpusBenchmarkCase, parser *gotreesitter.Parser, oldTree *gotreesitter.Tree, phase string) (*gotreesitter.Tree, bool) {
	tb.Helper()
	var tree *gotreesitter.Tree
	var err error
	switch tc.report.Backend {
	case grammars.ParseBackendTokenSource:
		if tc.entry.TokenSourceFactory == nil {
			tb.Fatalf("token source backend without factory for %q", tc.name)
		}
		tree, err = parser.ParseIncrementalWithTokenSource(tc.source, oldTree, tc.entry.TokenSourceFactory(tc.source, tc.goLang))
	case grammars.ParseBackendDFA, grammars.ParseBackendDFAPartial:
		tree, err = parser.ParseIncremental(tc.source, oldTree)
	default:
		tb.Fatalf("unsupported incremental backend %q for %q", tc.report.Backend, tc.name)
	}
	if failure := completeRealCorpusGoTreeFailure(tc, tree, phase, err); failure != "" {
		if tree != nil {
			releaseGoTree(tree)
		}
		tb.Logf("%s %s/%s %s", phase, tc.name, realCorpusBenchmarkCasePath(tc), failure)
		return nil, false
	}
	return tree, true
}

func parseRealCorpusGoIncrementalProfiled(tb testing.TB, tc realCorpusBenchmarkCase, parser *gotreesitter.Parser, oldTree *gotreesitter.Tree) (*gotreesitter.Tree, gotreesitter.IncrementalParseProfile) {
	tb.Helper()
	return parseRealCorpusGoIncrementalProfiledWithPhase(tb, tc, parser, oldTree, "gotreesitter incremental")
}

func parseRealCorpusGoIncrementalProfiledWithPhase(tb testing.TB, tc realCorpusBenchmarkCase, parser *gotreesitter.Parser, oldTree *gotreesitter.Tree, phase string) (*gotreesitter.Tree, gotreesitter.IncrementalParseProfile) {
	tb.Helper()
	var tree *gotreesitter.Tree
	var profile gotreesitter.IncrementalParseProfile
	var err error
	switch tc.report.Backend {
	case grammars.ParseBackendTokenSource:
		if tc.entry.TokenSourceFactory == nil {
			tb.Fatalf("token source backend without factory for %q", tc.name)
		}
		tree, profile, err = parser.ParseIncrementalWithTokenSourceProfiled(tc.source, oldTree, tc.entry.TokenSourceFactory(tc.source, tc.goLang))
	case grammars.ParseBackendDFA, grammars.ParseBackendDFAPartial:
		tree, profile, err = parser.ParseIncrementalProfiled(tc.source, oldTree)
	default:
		tb.Fatalf("unsupported incremental backend %q for %q", tc.report.Backend, tc.name)
	}
	requireCompleteRealCorpusGoTree(tb, tc, tree, phase, err)
	return tree, profile
}

func requireCompleteRealCorpusGoTree(tb testing.TB, tc realCorpusBenchmarkCase, tree *gotreesitter.Tree, phase string, err error) {
	tb.Helper()
	if failure := completeRealCorpusGoTreeFailure(tc, tree, phase, err); failure != "" {
		if tree != nil {
			releaseGoTree(tree)
		}
		tb.Fatalf("%s %s/%s %s", phase, tc.name, realCorpusBenchmarkCasePath(tc), failure)
	}
}

func completeRealCorpusGoTreeFailure(tc realCorpusBenchmarkCase, tree *gotreesitter.Tree, phase string, err error) string {
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		return "returned nil tree"
	}
	if got, want := tree.RootNode().EndByte(), uint32(len(tc.source)); got != want {
		rt := tree.ParseRuntime()
		return fmt.Sprintf("truncated: root.EndByte=%d want=%d %s", got, want, rt.Summary())
	}
	return ""
}

func newRealCorpusCParser(tb testing.TB, tc realCorpusBenchmarkCase) *sitter.Parser {
	tb.Helper()
	parser := sitter.NewParser()
	if err := parser.SetLanguage(tc.cLang); err != nil {
		parser.Close()
		if skipReason := parityReferenceSkipReason(err); skipReason != "" {
			tb.Skipf("skip C reference parser SetLanguage: %s", skipReason)
		}
		tb.Fatalf("C SetLanguage %s: %v", tc.name, err)
	}
	return parser
}

func parseRealCorpusCFull(tb testing.TB, parser *sitter.Parser, source []byte) *sitter.Tree {
	tb.Helper()
	tree, failure := tryParseRealCorpusCFull(parser, source, "C full")
	if failure != "" {
		tb.Fatalf("C full parse %s", failure)
	}
	return tree
}

func parseRealCorpusCIncremental(tb testing.TB, parser *sitter.Parser, source []byte, oldTree *sitter.Tree) *sitter.Tree {
	tb.Helper()
	tree := parser.Parse(source, oldTree)
	requireCompleteRealCorpusCTree(tb, tree, source, "C incremental")
	return tree
}

func tryParseCompleteRealCorpusCIncremental(parser *sitter.Parser, source []byte, oldTree *sitter.Tree) (*sitter.Tree, bool) {
	tree := parser.Parse(source, oldTree)
	if !isCompleteRealCorpusCTree(tree, source) {
		return nil, false
	}
	return tree, true
}

func requireCompleteRealCorpusCTree(tb testing.TB, tree *sitter.Tree, source []byte, phase string) {
	tb.Helper()
	if failure := completeRealCorpusCTreeFailure(tree, source, phase); failure != "" {
		if tree != nil {
			tree.Close()
		}
		tb.Fatalf("%s parse %s", phase, failure)
	}
}

func tryParseRealCorpusCFull(parser *sitter.Parser, source []byte, phase string) (*sitter.Tree, string) {
	tree := parser.Parse(source, nil)
	if failure := completeRealCorpusCTreeFailure(tree, source, phase); failure != "" {
		if tree != nil {
			tree.Close()
		}
		return nil, failure
	}
	return tree, ""
}

func completeRealCorpusCTreeFailure(tree *sitter.Tree, source []byte, phase string) string {
	if tree == nil || tree.RootNode() == nil {
		return "returned nil tree"
	}
	if got, want := uint32(tree.RootNode().EndByte()), uint32(len(source)); got != want {
		return fmt.Sprintf("truncated: root.EndByte=%d want=%d type=%q hasError=%v", got, want, tree.RootNode().Kind(), tree.RootNode().HasError())
	}
	return ""
}

func isCompleteRealCorpusCTree(tree *sitter.Tree, source []byte) bool {
	if tree == nil {
		return false
	}
	if tree.RootNode() == nil {
		tree.Close()
		return false
	}
	if got, want := uint32(tree.RootNode().EndByte()), uint32(len(source)); got != want {
		tree.Close()
		return false
	}
	return true
}

func toggleRealCorpusEditByte(src []byte, tc realCorpusIncrementalCase) {
	if src[tc.offset] == tc.original {
		src[tc.offset] = tc.replacement
	} else {
		src[tc.offset] = tc.original
	}
}

func totalRealCorpusIncrementalBytes(cases []realCorpusIncrementalCase) int64 {
	var total int64
	for _, tc := range cases {
		total += int64(len(tc.source))
	}
	return total
}

func realCorpusCasesFromIncremental(cases []realCorpusIncrementalCase) []realCorpusBenchmarkCase {
	out := make([]realCorpusBenchmarkCase, 0, len(cases))
	for _, tc := range cases {
		out = append(out, tc.realCorpusBenchmarkCase)
	}
	return out
}

func (t *realCorpusRuntimeTotals) add(rt gotreesitter.ParseRuntime) {
	t.arenaBytes += rt.ArenaBytesAllocated
	t.scratchBytes += rt.ScratchBytesAllocated
	t.nodes += uint64(rt.NodesAllocated)
	t.iterations += uint64(rt.Iterations)
	t.tokens += rt.TokensConsumed
	t.compactLeafMaterialized += rt.CompactFullLeafMaterialized
	t.pendingParentMaterialized += rt.PendingParentMaterialized
	t.transientParents += rt.TransientParentNodesMaterialized
	t.transientChildPointers += rt.TransientChildPointersMaterialized
	t.finalChildRefMaterializedChildren += rt.FinalChildRefMaterializedChildren
	t.finalChildRefSingleAccesses += rt.FinalChildRefSingleChildAccesses
	t.finalChildRefSingleMaterialized += rt.FinalChildRefSingleChildMaterializedChildren
	t.normalizationRewrites += rt.NormalizationNodesRewritten
	t.parseWallNanos += rt.ParseWallNanos
	t.parserLoopNanos += rt.ParserLoopNanos
	t.tokenNextNanos += rt.TokenNextNanos
	t.actionDispatchNanos += rt.ActionDispatchNanos
	t.actionLookupNanos += rt.ActionLookupNanos
	t.glrMergeNanos += rt.GLRMergeNanos
	t.glrCullNanos += rt.GLRCullNanos
	t.resultSelectionNanos += rt.ResultSelectionNanos
	t.transientParentMaterializeNanos += rt.TransientParentMaterializationNanos
	t.resultTreeBuildNanos += rt.ResultTreeBuildNanos
	t.transientChildMaterializeNanos += rt.TransientChildMaterializationNanos
	t.resultPythonKeywordRepairNanos += rt.ResultPythonKeywordRepairNanos
	t.resultPythonRootRepairNanos += rt.ResultPythonRootRepairNanos
	t.resultFinalizeRootNanos += rt.ResultFinalizeRootNanos
	t.resultExtendTrailingNanos += rt.ResultExtendTrailingNanos
	t.resultNormalizeRootStartNanos += rt.ResultNormalizeRootStartNanos
	t.resultCompatibilityNanos += rt.ResultCompatibilityNanos
	t.resultParentLinkNanos += rt.ResultParentLinkNanos
	t.normalizationNanos += rt.NormalizationNanos
	if reduceTiming := rt.ReduceTiming; reduceTiming != nil {
		t.reduceRangeNanos += reduceTiming.RangeNanos
		t.reducePendingParentNanos += reduceTiming.PendingParentNanos
		t.reduceChildBuildNanos += reduceTiming.ChildBuildNanos
		t.reduceParentBuildNanos += reduceTiming.ParentBuildNanos
		t.reduceSpanNanos += reduceTiming.SpanNanos
		t.reduceStackPushNanos += reduceTiming.StackPushNanos
		t.reduceNoTreeBuildNanos += reduceTiming.NoTreeBuildNanos
	}
	if actionTiming := rt.ActionTiming; actionTiming != nil {
		t.actionExtraShiftNanos += actionTiming.ExtraShiftNanos
		t.actionNoActionNanos += actionTiming.NoActionNanos
		t.actionNoActionRelexNanos += actionTiming.NoActionRelexNanos
		t.actionNoActionMissingNanos += actionTiming.NoActionMissingNanos
		t.actionNoActionRecoverNanos += actionTiming.NoActionRecoverNanos
		t.actionNoActionErrorNanos += actionTiming.NoActionErrorNanos
		t.actionConflictChoiceNanos += actionTiming.ConflictChoiceNanos
		t.actionConflictForkNanos += actionTiming.ConflictForkNanos
		t.actionSingleShiftNanos += actionTiming.SingleShiftNanos
		t.actionSingleReduceNanos += actionTiming.SingleReduceNanos
		t.actionSingleAcceptNanos += actionTiming.SingleAcceptNanos
		t.actionSingleRecoverNanos += actionTiming.SingleRecoverNanos
		t.actionSingleOtherNanos += actionTiming.SingleOtherNanos
	}
}

func (t *realCorpusIncrementalProfileTotals) addEdit(d time.Duration) {
	t.editNanos += d.Nanoseconds()
}

func (t *realCorpusIncrementalProfileTotals) addParseWall(d time.Duration) {
	t.parseWallNanos += d.Nanoseconds()
}

func (t *realCorpusIncrementalProfileTotals) add(profile gotreesitter.IncrementalParseProfile) {
	t.reuseNanos += profile.ReuseCursorNanos
	t.reparseNanos += profile.ReparseNanos
	t.reusedSubtrees += profile.ReusedSubtrees
	t.reusedBytes += profile.ReusedBytes
	t.newNodes += profile.NewNodesAllocated
	if profile.ReuseUnsupported {
		t.reuseUnsupported++
	}
	t.reuseRejectDirty += profile.ReuseRejectDirty
	t.reuseRejectAncestorDirtyBeforeEdit += profile.ReuseRejectAncestorDirtyBeforeEdit
	t.reuseRejectHasError += profile.ReuseRejectHasError
	t.reuseRejectInvalidSpan += profile.ReuseRejectInvalidSpan
	t.reuseRejectOutOfBounds += profile.ReuseRejectOutOfBounds
	t.reuseRejectRootNonLeafChanged += profile.ReuseRejectRootNonLeafChanged
	t.reuseRejectLargeNonLeaf += profile.ReuseRejectLargeNonLeaf
	t.recoverSearches += profile.RecoverSearches
	t.recoverStateChecks += profile.RecoverStateChecks
	t.recoverStateSkips += profile.RecoverStateSkips
	t.recoverSymbolSkips += profile.RecoverSymbolSkips
	t.recoverLookups += profile.RecoverLookups
	t.recoverHits += profile.RecoverHits
	if profile.MaxStacksSeen > t.maxStacksSeen {
		t.maxStacksSeen = profile.MaxStacksSeen
	}
	if profile.EntryScratchPeak > t.entryScratchPeak {
		t.entryScratchPeak = profile.EntryScratchPeak
	}
	t.tokensConsumed += profile.TokensConsumed
	t.singleStackIterations += profile.SingleStackIterations
	t.multiStackIterations += profile.MultiStackIterations
	t.singleStackTokens += profile.SingleStackTokens
	t.multiStackTokens += profile.MultiStackTokens
	t.singleStackGSSNodes += profile.SingleStackGSSNodes
	t.multiStackGSSNodes += profile.MultiStackGSSNodes
	t.gssNodesAllocated += profile.GSSNodesAllocated
	t.gssNodesRetained += profile.GSSNodesRetained
	t.gssNodesDroppedSameToken += profile.GSSNodesDroppedSameToken
	t.parentNodesAllocated += profile.ParentNodesAllocated
	t.parentNodesRetained += profile.ParentNodesRetained
	t.parentNodesDroppedSameToken += profile.ParentNodesDroppedSameToken
	t.leafNodesAllocated += profile.LeafNodesAllocated
	t.leafNodesRetained += profile.LeafNodesRetained
	t.leafNodesDroppedSameToken += profile.LeafNodesDroppedSameToken
	t.mergeStacksIn += profile.MergeStacksIn
	t.mergeStacksOut += profile.MergeStacksOut
	t.mergeSlotsUsed += profile.MergeSlotsUsed
	t.globalCullStacksIn += profile.GlobalCullStacksIn
	t.globalCullStacksOut += profile.GlobalCullStacksOut
	t.parserLoopNanos += profile.ParserLoopNanos
	t.tokenNextNanos += profile.TokenNextNanos
	t.actionDispatchNanos += profile.ActionDispatchNanos
	t.actionLookupNanos += profile.ActionLookupNanos
	t.glrMergeNanos += profile.GLRMergeNanos
	t.glrCullNanos += profile.GLRCullNanos
	t.resultSelectionNanos += profile.ResultSelectionNanos
	t.transientParentMaterializeNanos += profile.TransientParentMaterializationNanos
	t.resultTreeBuildNanos += profile.ResultTreeBuildNanos
	t.transientChildMaterializeNanos += profile.TransientChildMaterializationNanos
	t.resultPythonKeywordRepairNanos += profile.ResultPythonKeywordRepairNanos
	t.resultPythonRootRepairNanos += profile.ResultPythonRootRepairNanos
	t.resultFinalizeRootNanos += profile.ResultFinalizeRootNanos
	t.resultExtendTrailingNanos += profile.ResultExtendTrailingNanos
	t.resultNormalizeRootStartNanos += profile.ResultNormalizeRootStartNanos
	t.resultCompatibilityNanos += profile.ResultCompatibilityNanos
	t.resultParentLinkNanos += profile.ResultParentLinkNanos
	t.normalizationNanos += profile.NormalizationNanos
}

func (t realCorpusRuntimeTotals) report(b *testing.B, cases []realCorpusBenchmarkCase, benchN int) {
	reportRealCorpusCaseMetrics(b, cases)
	if benchN == 0 {
		return
	}
	n := float64(benchN)
	b.ReportMetric(float64(t.arenaBytes)/n, "arena_B/op")
	b.ReportMetric(float64(t.scratchBytes)/n, "scratch_B/op")
	b.ReportMetric(float64(t.nodes)/n, "nodes/op")
	b.ReportMetric(float64(t.iterations)/n, "iterations/op")
	b.ReportMetric(float64(t.tokens)/n, "tokens/op")
	b.ReportMetric(float64(t.compactLeafMaterialized)/n, "compact_leaf_mat/op")
	b.ReportMetric(float64(t.pendingParentMaterialized)/n, "pending_parent_mat/op")
	b.ReportMetric(float64(t.transientParents)/n, "transient_parent_mat/op")
	b.ReportMetric(float64(t.transientChildPointers)/n, "transient_child_ptr_mat/op")
	b.ReportMetric(float64(t.finalChildRefMaterializedChildren)/n, "final_child_ref_mat_children/op")
	b.ReportMetric(float64(t.finalChildRefSingleAccesses)/n, "final_child_ref_single_accesses/op")
	b.ReportMetric(float64(t.finalChildRefSingleMaterialized)/n, "final_child_ref_single_mat/op")
	b.ReportMetric(float64(t.normalizationRewrites)/n, "normalization_rewrites/op")
	b.ReportMetric(float64(t.parseWallNanos)/n, "parse_wall_ns/op")
	reportRealCorpusParserPhaseMetrics(
		b,
		n,
		t.parserLoopNanos,
		t.tokenNextNanos,
		t.actionDispatchNanos,
		t.actionLookupNanos,
		t.glrMergeNanos,
		t.glrCullNanos,
	)
	reportRealCorpusPhaseMetrics(
		b,
		n,
		t.resultSelectionNanos,
		t.transientParentMaterializeNanos,
		t.resultTreeBuildNanos,
		t.transientChildMaterializeNanos,
		t.resultPythonKeywordRepairNanos,
		t.resultPythonRootRepairNanos,
		t.resultFinalizeRootNanos,
		t.resultExtendTrailingNanos,
		t.resultNormalizeRootStartNanos,
		t.resultCompatibilityNanos,
		t.resultParentLinkNanos,
		t.normalizationNanos,
	)
	reportRealCorpusReduceTimingMetrics(
		b,
		n,
		t.reduceRangeNanos,
		t.reducePendingParentNanos,
		t.reduceChildBuildNanos,
		t.reduceParentBuildNanos,
		t.reduceSpanNanos,
		t.reduceStackPushNanos,
		t.reduceNoTreeBuildNanos,
	)
	reportRealCorpusActionTimingMetrics(
		b,
		n,
		t.actionExtraShiftNanos,
		t.actionNoActionNanos,
		t.actionNoActionRelexNanos,
		t.actionNoActionMissingNanos,
		t.actionNoActionRecoverNanos,
		t.actionNoActionErrorNanos,
		t.actionConflictChoiceNanos,
		t.actionConflictForkNanos,
		t.actionSingleShiftNanos,
		t.actionSingleReduceNanos,
		t.actionSingleAcceptNanos,
		t.actionSingleRecoverNanos,
		t.actionSingleOtherNanos,
	)
}

func (t realCorpusIncrementalProfileTotals) report(b *testing.B, benchN int) {
	if benchN == 0 {
		return
	}
	n := float64(benchN)
	b.ReportMetric(float64(t.editNanos)/n, "edit_ns/op")
	b.ReportMetric(float64(t.parseWallNanos)/n, "parse_wall_ns/op")
	b.ReportMetric(float64(t.reuseNanos)/n, "reuse_ns/op")
	b.ReportMetric(float64(t.reparseNanos)/n, "reparse_ns/op")
	unattributedNanos := t.parseWallNanos - t.reuseNanos - t.reparseNanos
	if unattributedNanos < 0 {
		unattributedNanos = 0
	}
	b.ReportMetric(float64(unattributedNanos)/n, "unattributed_ns/op")
	b.ReportMetric(float64(t.reusedSubtrees)/n, "reused_subtrees/op")
	b.ReportMetric(float64(t.reusedBytes)/n, "reused_B/op")
	b.ReportMetric(float64(t.newNodes)/n, "new_nodes/op")
	b.ReportMetric(float64(t.reuseUnsupported)/n, "reuse_unsupported/op")
	b.ReportMetric(float64(t.reuseRejectDirty)/n, "reuse_reject_dirty/op")
	b.ReportMetric(float64(t.reuseRejectAncestorDirtyBeforeEdit)/n, "reuse_reject_ancestor_dirty/op")
	b.ReportMetric(float64(t.reuseRejectHasError)/n, "reuse_reject_error/op")
	b.ReportMetric(float64(t.reuseRejectInvalidSpan)/n, "reuse_reject_invalid_span/op")
	b.ReportMetric(float64(t.reuseRejectOutOfBounds)/n, "reuse_reject_oob/op")
	b.ReportMetric(float64(t.reuseRejectRootNonLeafChanged)/n, "reuse_reject_root_nonleaf/op")
	b.ReportMetric(float64(t.reuseRejectLargeNonLeaf)/n, "reuse_reject_large_nonleaf/op")
	b.ReportMetric(float64(t.recoverSearches)/n, "recover_searches/op")
	b.ReportMetric(float64(t.recoverStateChecks)/n, "recover_state_checks/op")
	b.ReportMetric(float64(t.recoverStateSkips)/n, "recover_state_skips/op")
	b.ReportMetric(float64(t.recoverSymbolSkips)/n, "recover_symbol_skips/op")
	b.ReportMetric(float64(t.recoverLookups)/n, "recover_lookups/op")
	b.ReportMetric(float64(t.recoverHits)/n, "recover_hits/op")
	b.ReportMetric(float64(t.maxStacksSeen), "max_stacks_peak")
	b.ReportMetric(float64(t.entryScratchPeak), "entry_scratch_peak")
	b.ReportMetric(float64(t.tokensConsumed)/n, "incr_tokens/op")
	b.ReportMetric(float64(t.singleStackIterations)/n, "single_stack_iterations/op")
	b.ReportMetric(float64(t.multiStackIterations)/n, "multi_stack_iterations/op")
	b.ReportMetric(float64(t.singleStackTokens)/n, "single_stack_tokens/op")
	b.ReportMetric(float64(t.multiStackTokens)/n, "multi_stack_tokens/op")
	b.ReportMetric(float64(t.singleStackGSSNodes)/n, "single_stack_gss_nodes/op")
	b.ReportMetric(float64(t.multiStackGSSNodes)/n, "multi_stack_gss_nodes/op")
	b.ReportMetric(float64(t.gssNodesAllocated)/n, "gss_nodes_alloc/op")
	b.ReportMetric(float64(t.gssNodesRetained)/n, "gss_nodes_retained/op")
	b.ReportMetric(float64(t.gssNodesDroppedSameToken)/n, "gss_nodes_dropped/op")
	b.ReportMetric(float64(t.parentNodesAllocated)/n, "parent_nodes_alloc/op")
	b.ReportMetric(float64(t.parentNodesRetained)/n, "parent_nodes_retained/op")
	b.ReportMetric(float64(t.parentNodesDroppedSameToken)/n, "parent_nodes_dropped/op")
	b.ReportMetric(float64(t.leafNodesAllocated)/n, "leaf_nodes_alloc/op")
	b.ReportMetric(float64(t.leafNodesRetained)/n, "leaf_nodes_retained/op")
	b.ReportMetric(float64(t.leafNodesDroppedSameToken)/n, "leaf_nodes_dropped/op")
	b.ReportMetric(float64(t.mergeStacksIn)/n, "merge_stacks_in/op")
	b.ReportMetric(float64(t.mergeStacksOut)/n, "merge_stacks_out/op")
	b.ReportMetric(float64(t.mergeSlotsUsed)/n, "merge_slots_used/op")
	b.ReportMetric(float64(t.globalCullStacksIn)/n, "global_cull_stacks_in/op")
	b.ReportMetric(float64(t.globalCullStacksOut)/n, "global_cull_stacks_out/op")
	reportRealCorpusParserPhaseMetrics(
		b,
		n,
		t.parserLoopNanos,
		t.tokenNextNanos,
		t.actionDispatchNanos,
		t.actionLookupNanos,
		t.glrMergeNanos,
		t.glrCullNanos,
	)
	reportRealCorpusPhaseMetrics(
		b,
		n,
		t.resultSelectionNanos,
		t.transientParentMaterializeNanos,
		t.resultTreeBuildNanos,
		t.transientChildMaterializeNanos,
		t.resultPythonKeywordRepairNanos,
		t.resultPythonRootRepairNanos,
		t.resultFinalizeRootNanos,
		t.resultExtendTrailingNanos,
		t.resultNormalizeRootStartNanos,
		t.resultCompatibilityNanos,
		t.resultParentLinkNanos,
		t.normalizationNanos,
	)
}

func reportRealCorpusParserPhaseMetrics(
	b *testing.B,
	n float64,
	parserLoopNanos int64,
	tokenNextNanos int64,
	actionDispatchNanos int64,
	actionLookupNanos int64,
	glrMergeNanos int64,
	glrCullNanos int64,
) {
	actionApplyNanos := actionDispatchNanos - actionLookupNanos
	if actionApplyNanos < 0 {
		actionApplyNanos = 0
	}
	parserAccountedNanos := tokenNextNanos +
		actionDispatchNanos +
		glrMergeNanos +
		glrCullNanos
	parserUnattributedNanos := parserLoopNanos - parserAccountedNanos
	if parserUnattributedNanos < 0 {
		parserUnattributedNanos = 0
	}
	b.ReportMetric(float64(parserLoopNanos)/n, "parser_loop_ns/op")
	b.ReportMetric(float64(tokenNextNanos)/n, "token_next_ns/op")
	b.ReportMetric(float64(actionDispatchNanos)/n, "action_dispatch_ns/op")
	b.ReportMetric(float64(actionLookupNanos)/n, "action_lookup_ns/op")
	b.ReportMetric(float64(actionApplyNanos)/n, "action_apply_ns/op")
	b.ReportMetric(float64(glrMergeNanos)/n, "glr_merge_ns/op")
	b.ReportMetric(float64(glrCullNanos)/n, "glr_cull_ns/op")
	b.ReportMetric(float64(parserAccountedNanos)/n, "parser_accounted_ns/op")
	b.ReportMetric(float64(parserUnattributedNanos)/n, "parser_unattributed_ns/op")
}

func reportRealCorpusPhaseMetrics(
	b *testing.B,
	n float64,
	resultSelectionNanos int64,
	transientParentMaterializeNanos int64,
	resultTreeBuildNanos int64,
	transientChildMaterializeNanos int64,
	resultPythonKeywordRepairNanos int64,
	resultPythonRootRepairNanos int64,
	resultFinalizeRootNanos int64,
	resultExtendTrailingNanos int64,
	resultNormalizeRootStartNanos int64,
	resultCompatibilityNanos int64,
	resultParentLinkNanos int64,
	normalizationNanos int64,
) {
	resultAccountedNanos := resultSelectionNanos +
		transientParentMaterializeNanos +
		resultTreeBuildNanos +
		transientChildMaterializeNanos +
		resultPythonKeywordRepairNanos +
		resultPythonRootRepairNanos +
		resultFinalizeRootNanos +
		resultExtendTrailingNanos +
		resultNormalizeRootStartNanos +
		resultCompatibilityNanos +
		resultParentLinkNanos +
		normalizationNanos
	b.ReportMetric(float64(resultSelectionNanos)/n, "result_select_ns/op")
	b.ReportMetric(float64(transientParentMaterializeNanos)/n, "transient_parent_materialize_ns/op")
	b.ReportMetric(float64(resultTreeBuildNanos)/n, "result_tree_build_ns/op")
	b.ReportMetric(float64(transientChildMaterializeNanos)/n, "transient_child_materialize_ns/op")
	b.ReportMetric(float64(resultPythonKeywordRepairNanos)/n, "result_python_keyword_repair_ns/op")
	b.ReportMetric(float64(resultPythonRootRepairNanos)/n, "result_python_root_repair_ns/op")
	b.ReportMetric(float64(resultFinalizeRootNanos)/n, "result_finalize_root_ns/op")
	b.ReportMetric(float64(resultExtendTrailingNanos)/n, "result_extend_trailing_ns/op")
	b.ReportMetric(float64(resultNormalizeRootStartNanos)/n, "result_normalize_root_start_ns/op")
	b.ReportMetric(float64(resultCompatibilityNanos)/n, "result_compatibility_ns/op")
	b.ReportMetric(float64(resultParentLinkNanos)/n, "result_parent_link_ns/op")
	b.ReportMetric(float64(normalizationNanos)/n, "normalization_ns/op")
	b.ReportMetric(float64(resultAccountedNanos)/n, "result_accounted_ns/op")
}

func reportRealCorpusReduceTimingMetrics(
	b *testing.B,
	n float64,
	reduceRangeNanos int64,
	reducePendingParentNanos int64,
	reduceChildBuildNanos int64,
	reduceParentBuildNanos int64,
	reduceSpanNanos int64,
	reduceStackPushNanos int64,
	reduceNoTreeBuildNanos int64,
) {
	reduceTimedNanos := reduceRangeNanos +
		reducePendingParentNanos +
		reduceChildBuildNanos +
		reduceParentBuildNanos +
		reduceSpanNanos +
		reduceStackPushNanos +
		reduceNoTreeBuildNanos
	if reduceTimedNanos == 0 {
		return
	}
	b.ReportMetric(float64(reduceRangeNanos)/n, "reduce_range_ns/op")
	b.ReportMetric(float64(reducePendingParentNanos)/n, "reduce_pending_parent_ns/op")
	b.ReportMetric(float64(reduceChildBuildNanos)/n, "reduce_child_build_ns/op")
	b.ReportMetric(float64(reduceParentBuildNanos)/n, "reduce_parent_build_ns/op")
	b.ReportMetric(float64(reduceSpanNanos)/n, "reduce_span_ns/op")
	b.ReportMetric(float64(reduceStackPushNanos)/n, "reduce_stack_push_ns/op")
	b.ReportMetric(float64(reduceNoTreeBuildNanos)/n, "reduce_notree_build_ns/op")
	b.ReportMetric(float64(reduceTimedNanos)/n, "reduce_timed_ns/op")
}

func reportRealCorpusActionTimingMetrics(
	b *testing.B,
	n float64,
	actionExtraShiftNanos int64,
	actionNoActionNanos int64,
	actionNoActionRelexNanos int64,
	actionNoActionMissingNanos int64,
	actionNoActionRecoverNanos int64,
	actionNoActionErrorNanos int64,
	actionConflictChoiceNanos int64,
	actionConflictForkNanos int64,
	actionSingleShiftNanos int64,
	actionSingleReduceNanos int64,
	actionSingleAcceptNanos int64,
	actionSingleRecoverNanos int64,
	actionSingleOtherNanos int64,
) {
	actionTimedNanos := actionExtraShiftNanos +
		actionNoActionNanos +
		actionConflictChoiceNanos +
		actionConflictForkNanos +
		actionSingleShiftNanos +
		actionSingleReduceNanos +
		actionSingleAcceptNanos +
		actionSingleRecoverNanos +
		actionSingleOtherNanos
	if actionTimedNanos == 0 {
		return
	}
	b.ReportMetric(float64(actionExtraShiftNanos)/n, "action_extra_shift_ns/op")
	b.ReportMetric(float64(actionNoActionNanos)/n, "action_no_action_ns/op")
	b.ReportMetric(float64(actionNoActionRelexNanos)/n, "action_no_action_relex_ns/op")
	b.ReportMetric(float64(actionNoActionMissingNanos)/n, "action_no_action_missing_ns/op")
	b.ReportMetric(float64(actionNoActionRecoverNanos)/n, "action_no_action_recover_ns/op")
	b.ReportMetric(float64(actionNoActionErrorNanos)/n, "action_no_action_error_ns/op")
	b.ReportMetric(float64(actionConflictChoiceNanos)/n, "action_conflict_choice_ns/op")
	b.ReportMetric(float64(actionConflictForkNanos)/n, "action_conflict_fork_ns/op")
	b.ReportMetric(float64(actionSingleShiftNanos)/n, "action_single_shift_ns/op")
	b.ReportMetric(float64(actionSingleReduceNanos)/n, "action_single_reduce_ns/op")
	b.ReportMetric(float64(actionSingleAcceptNanos)/n, "action_single_accept_ns/op")
	b.ReportMetric(float64(actionSingleRecoverNanos)/n, "action_single_recover_ns/op")
	b.ReportMetric(float64(actionSingleOtherNanos)/n, "action_single_other_ns/op")
	b.ReportMetric(float64(actionTimedNanos)/n, "action_timed_ns/op")
}

func reportRealCorpusCaseMetrics(b *testing.B, cases []realCorpusBenchmarkCase) {
	b.ReportMetric(float64(len(cases)), "files/op")
}

func formatRealCorpusCaseList(cases []realCorpusBenchmarkCase) string {
	names := make([]string, 0, len(cases))
	for _, tc := range cases {
		names = append(names, fmt.Sprintf("%s:%dB", realCorpusBenchmarkCasePath(tc), len(tc.source)))
	}
	return strings.Join(names, ",")
}

func realCorpusBenchmarkCasePath(tc realCorpusBenchmarkCase) string {
	path := filepath.ToSlash(tc.path)
	parts := strings.Split(path, "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] == tc.name {
			return strings.Join(parts[i+1:], "/")
		}
	}
	return filepath.Base(tc.path)
}
