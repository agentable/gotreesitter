package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agentable/gotreesitter/cgo_harness/internal/realcorpus"
)

func TestBuildInventoryUsesGrammarBlobsAsLanguageUniverse(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")
	mustWrite(t, filepath.Join(grammarDir, "python.bin"), "python")
	mustWrite(t, filepath.Join(grammarDir, "orphan.bin"), "orphan")

	lockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, lockPath, strings.Join([]string{
		"# name repo_url commit [subdir] [ext1,ext2,...]",
		"go https://example.invalid/go abc123 src .go",
		"python https://example.invalid/python def456 src .py,.pyw",
		"",
	}, "\n"))

	manifestPath := filepath.Join(dir, "manifest.json")
	writeJSONFixture(t, manifestPath, corpusManifest{
		Languages: []string{"go", "python"},
		Entries: []corpusManifestEntry{
			{Language: "go", Bucket: "small", Bytes: 512, SourcePath: "small.go", OutputPath: "corpus/go/small.go"},
			{Language: "go", Bucket: "medium", Bytes: 4096, SourcePath: "medium.go", OutputPath: "corpus/go/medium.go"},
			{Language: "go", Bucket: "large", Bytes: 65536, SourcePath: "large.go", OutputPath: "corpus/go/large.go"},
		},
		Missing: []string{"python"},
	})

	benchPath := filepath.Join(dir, "bench.json")
	writeJSONFixture(t, benchPath, benchReport{
		Languages: []benchLanguageReport{
			{
				Language:      "go",
				Readiness:     "meets-current-targets",
				FullRatio:     1.2,
				EditRatio:     0.5,
				NoEditRatio:   0.1,
				FullGoNanos:   120,
				FullCNanos:    100,
				EditGoNanos:   50,
				EditCNanos:    100,
				NoEditGoNanos: 10,
				NoEditCNanos:  100,
				TopAttribution: []attributionBucket{
					{Suite: "Full", Name: "parse_wall_ns/op", Value: 100},
				},
				GoSuiteMetrics: map[string]map[string]float64{
					"Full": {"ns/op": 120, "parse_wall_ns/op": 100},
				},
			},
		},
	})

	inv, err := buildInventory(grammarDir, lockPath, []string{manifestPath}, benchPath)
	if err != nil {
		t.Fatalf("buildInventory: %v", err)
	}
	if inv.Summary.TotalLanguages != 3 {
		t.Fatalf("total languages=%d", inv.Summary.TotalLanguages)
	}
	if inv.Summary.WithLockMetadata != 2 || inv.Summary.MissingLockMetadata != 1 {
		t.Fatalf("lock summary=%+v", inv.Summary)
	}
	if inv.Summary.WithCorpus != 1 || inv.Summary.MissingCorpus != 2 {
		t.Fatalf("corpus summary=%+v", inv.Summary)
	}
	if inv.Summary.WithL3MediumCorpus != 1 || inv.Summary.WithL4LargeCorpus != 1 {
		t.Fatalf("tier summary=%+v", inv.Summary)
	}
	if inv.Summary.Benchmarked != 1 || inv.Summary.Instrumented != 1 || inv.Summary.Profiled != 1 || inv.Summary.MeetsCurrentTargets != 1 {
		t.Fatalf("bench summary=%+v", inv.Summary)
	}
	if got := inv.Summary.ReadinessCounts["meets-current-targets"]; got != 1 {
		t.Fatalf("meets-current-targets readiness count=%d", got)
	}
	if got := inv.Summary.ReadinessCounts[readinessUnmeasured]; got != 2 {
		t.Fatalf("unmeasured readiness count=%d", got)
	}

	byLang := languageStatusesByName(inv.Languages)
	goStatus := byLang["go"]
	if !goStatus.Lock.Present || len(goStatus.Lock.Exts) != 1 || goStatus.Lock.Exts[0] != ".go" {
		t.Fatalf("go lock=%+v", goStatus.Lock)
	}
	if !goStatus.Corpus.HasSmall || !goStatus.Corpus.HasMedium || !goStatus.Corpus.HasLarge {
		t.Fatalf("go corpus=%+v", goStatus.Corpus)
	}
	if !goStatus.Benchmark.Present || !goStatus.Benchmark.Instrumented || !goStatus.Benchmark.Profiled {
		t.Fatalf("go benchmark=%+v", goStatus.Benchmark)
	}

	pythonStatus := byLang["python"]
	if !pythonStatus.Lock.Present {
		t.Fatalf("python lock missing: %+v", pythonStatus.Lock)
	}
	if pythonStatus.Corpus.Present || !pythonStatus.Corpus.ManifestMissing {
		t.Fatalf("python corpus=%+v", pythonStatus.Corpus)
	}
	if pythonStatus.Benchmark.Readiness != readinessUnmeasured {
		t.Fatalf("python readiness=%q", pythonStatus.Benchmark.Readiness)
	}

	orphanStatus := byLang["orphan"]
	if orphanStatus.Lock.Present || orphanStatus.Corpus.Present || orphanStatus.Benchmark.Present {
		t.Fatalf("orphan status=%+v", orphanStatus)
	}
}

func TestBuildInventoryOverlaysCorpusManifests(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "gleam.bin"), "gleam")
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")
	mustWrite(t, filepath.Join(grammarDir, "python.bin"), "python")

	baseManifestPath := filepath.Join(dir, "base-manifest.json")
	writeJSONFixture(t, baseManifestPath, corpusManifest{
		Entries: []corpusManifestEntry{
			{
				Language:     "go",
				Bucket:       "small",
				Bytes:        512,
				SHA256:       "go-small",
				SourceRepo:   "https://example.invalid/go-corpus",
				SourceCommit: "abc",
				SourcePath:   "small.go",
				OutputPath:   "corpus/go/small.go",
			},
		},
		Missing: []string{"gleam"},
	})

	overlayManifestPath := filepath.Join(dir, "overlay-manifest.json")
	writeJSONFixture(t, overlayManifestPath, corpusManifest{
		Entries: []corpusManifestEntry{
			{
				Language:   "gleam",
				Bucket:     "medium",
				Bytes:      4096,
				SHA256:     "gleam-medium",
				SourcePath: "medium.gleam",
				OutputPath: "corpus/gleam/medium.gleam",
			},
			{
				Language:   "gleam",
				Bucket:     "large",
				Bytes:      65536,
				SHA256:     "gleam-large",
				SourcePath: "large.gleam",
				OutputPath: "corpus/gleam/large.gleam",
			},
			{
				Language:   "go",
				Bucket:     "small",
				Bytes:      512,
				SHA256:     "go-small",
				SourcePath: "small.go",
				OutputPath: "corpus/go/small.go",
			},
		},
		Missing: []string{"go", "python"},
	})

	inv, err := buildInventory(grammarDir, "", []string{baseManifestPath, overlayManifestPath}, "")
	if err != nil {
		t.Fatalf("buildInventory: %v", err)
	}
	if inv.CorpusManifestPath != baseManifestPath+","+overlayManifestPath {
		t.Fatalf("corpus manifest path=%q", inv.CorpusManifestPath)
	}
	if strings.Join(inv.CorpusManifestPaths, ",") != baseManifestPath+","+overlayManifestPath {
		t.Fatalf("corpus manifest paths=%#v", inv.CorpusManifestPaths)
	}
	if inv.Summary.WithCorpus != 2 || inv.Summary.MissingCorpus != 1 {
		t.Fatalf("corpus summary=%+v", inv.Summary)
	}
	if inv.Summary.WithL3MediumCorpus != 1 || inv.Summary.WithL4LargeCorpus != 1 {
		t.Fatalf("tier summary=%+v", inv.Summary)
	}

	byLang := languageStatusesByName(inv.Languages)
	gleamStatus := byLang["gleam"]
	if !gleamStatus.Corpus.Present || !gleamStatus.Corpus.HasMedium || !gleamStatus.Corpus.HasLarge {
		t.Fatalf("gleam corpus=%+v", gleamStatus.Corpus)
	}
	if gleamStatus.Corpus.ManifestMissing {
		t.Fatalf("gleam stale missing marker was not cleared: %+v", gleamStatus.Corpus)
	}
	if gleamStatus.Corpus.Entries != 2 || gleamStatus.Corpus.TotalBytes != 69632 {
		t.Fatalf("gleam corpus counts=%+v", gleamStatus.Corpus)
	}

	goStatus := byLang["go"]
	if !goStatus.Corpus.Present || goStatus.Corpus.ManifestMissing {
		t.Fatalf("go present corpus should win over later missing marker: %+v", goStatus.Corpus)
	}
	if goStatus.Corpus.Entries != 1 || goStatus.Corpus.TotalBytes != 512 {
		t.Fatalf("duplicate go entry should not be double-counted: %+v", goStatus.Corpus)
	}
	if strings.Join(goStatus.Corpus.SourceRepos, ",") != "https://example.invalid/go-corpus" || strings.Join(goStatus.Corpus.SourceCommits, ",") != "abc" {
		t.Fatalf("go source provenance=%+v", goStatus.Corpus)
	}

	pythonStatus := byLang["python"]
	if pythonStatus.Corpus.Present || !pythonStatus.Corpus.ManifestMissing {
		t.Fatalf("python corpus=%+v", pythonStatus.Corpus)
	}
}

func TestSourceMatchersForLanguageAddsCanonicalNames(t *testing.T) {
	tests := []struct {
		language  string
		wantExts  []string
		wantNames []string
	}{
		{language: "dockerfile", wantExts: []string{".dockerfile"}, wantNames: []string{"dockerfile", "containerfile", "1", "9"}},
		{language: "earthfile", wantExts: []string{".earth"}, wantNames: []string{"earthfile"}},
		{language: "git_rebase", wantExts: []string{".git-rebase-todo"}, wantNames: []string{"git-rebase-todo", "rebase-todo"}},
		{language: "kconfig", wantNames: []string{"kconfig", "kconfig.*"}},
		{language: "meson", wantNames: []string{"meson.build", "meson_options.txt"}},
		{language: "nginx", wantExts: []string{".nginx"}, wantNames: []string{"nginx.conf", "conf.nginx"}},
		{language: "requirements", wantNames: []string{"requirements.txt"}},
		{language: "ssh_config", wantNames: []string{"ssh_config", "sshd_config", "known_hosts", "authorized_keys"}},
		{language: "tmux", wantNames: []string{"tmux.conf", ".tmux.conf"}},
		{language: "todotxt", wantNames: []string{"todo.txt"}},
	}
	for _, tc := range tests {
		gotExts, gotNames, _ := sourceMatchersForLanguage(tc.language, nil)
		for _, want := range tc.wantExts {
			if !slices.Contains(gotExts, want) {
				t.Fatalf("%s extension matchers missing %q: %#v", tc.language, want, gotExts)
			}
		}
		for _, want := range tc.wantNames {
			if !slices.Contains(gotNames, want) {
				t.Fatalf("%s basename matchers missing %q: %#v", tc.language, want, gotNames)
			}
		}
	}
}

func TestSourceMatchersForLanguageHonorsExplicitLockMatchers(t *testing.T) {
	gotExts, gotNames, gotPaths := sourceMatchersForLanguage("kconfig", []string{".config", "Kconfig"})
	if !slices.Contains(gotExts, ".config") {
		t.Fatalf("extension matchers missing .config: %#v", gotExts)
	}
	if !slices.Contains(gotNames, "kconfig") {
		t.Fatalf("basename matchers missing kconfig: %#v", gotNames)
	}
	if slices.Contains(gotNames, "kconfig.*") {
		t.Fatalf("explicit lock matchers should not add canonical kconfig.*: %#v", gotNames)
	}
	if len(gotPaths) != 0 {
		t.Fatalf("unexpected path matchers: %#v", gotPaths)
	}
}

func TestSourceMatchersForLanguageKeepsBasenameOnlyLockMatchers(t *testing.T) {
	gotExts, gotNames, gotPaths := sourceMatchersForLanguage("foam", []string{"controlDict", "fvSchemes"})
	if len(gotExts) != 0 {
		t.Fatalf("expected no extension matchers, got %#v", gotExts)
	}
	if len(gotPaths) != 0 {
		t.Fatalf("expected no path matchers, got %#v", gotPaths)
	}
	for _, want := range []string{"controldict", "fvschemes"} {
		if !slices.Contains(gotNames, want) {
			t.Fatalf("basename matchers missing %q: %#v", want, gotNames)
		}
	}
}

func TestSourceMatchersForLanguageHonorsExplicitPathMatchers(t *testing.T) {
	gotExts, gotNames, gotPaths := sourceMatchersForLanguage("ini", []string{"Lib/tomllib/mypy.ini", "Tools\\jit\\mypy.ini", "./root.ini"})
	if len(gotExts) != 0 {
		t.Fatalf("expected no extension matchers, got %#v", gotExts)
	}
	if len(gotNames) != 0 {
		t.Fatalf("expected no basename matchers, got %#v", gotNames)
	}
	for _, want := range []string{"lib/tomllib/mypy.ini", "tools/jit/mypy.ini", "root.ini"} {
		if !slices.Contains(gotPaths, want) {
			t.Fatalf("path matchers missing %q: %#v", want, gotPaths)
		}
	}
	matcher := realcorpus.NewFileMatcherFromParts(gotExts, gotNames, gotPaths)
	if !matcher.Matches("Lib/tomllib/mypy.ini") {
		t.Fatalf("expected exact relative path match: %#v", gotPaths)
	}
	if matcher.Matches("Lib/_pyrepl/mypy.ini") {
		t.Fatalf("unexpected duplicate basename path match: %#v", gotPaths)
	}
	if !matcher.Matches("root.ini") {
		t.Fatalf("expected exact root path match: %#v", gotPaths)
	}
	if matcher.Matches("nested/root.ini") {
		t.Fatalf("unexpected duplicate root basename path match: %#v", gotPaths)
	}
}

func TestSourceMatcherAllowsKconfigPrefixBasenames(t *testing.T) {
	gotExts, gotNames, gotPaths := sourceMatchersForLanguage("kconfig", nil)
	matcher := realcorpus.NewFileMatcherFromParts(gotExts, gotNames, gotPaths)
	if !matcher.Matches("arch/Kconfig") {
		t.Fatalf("expected Kconfig basename match: %#v", gotNames)
	}
	if !matcher.Matches("arch/Kconfig.nxp") {
		t.Fatalf("expected Kconfig.* basename match: %#v", gotNames)
	}
	if matcher.Matches("arch/not-kconfig.txt") {
		t.Fatalf("unexpected generic config match: %#v", gotNames)
	}
}

func TestBuildInventoryTracksCorpusSourceCheckouts(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")
	mustWrite(t, filepath.Join(grammarDir, "python.bin"), "python")

	root := filepath.Join(dir, "corpus_sources")
	goCheckout := filepath.Join(root, "go")
	initGitRepo(t, goCheckout, "https://example.invalid/go-corpus")
	mustMkdir(t, filepath.Join(goCheckout, "cmd"))
	mustWrite(t, filepath.Join(goCheckout, "cmd", "main.go"), "package main\nfunc main() {}\n")
	runGitTest(t, goCheckout, "add", "cmd/main.go")
	runGitTest(t, goCheckout, "-c", "user.name=gotreesitter-test", "-c", "user.email=test@example.invalid", "commit", "-m", "add go source")
	commit := strings.TrimSpace(runGitTest(t, goCheckout, "rev-parse", "HEAD"))
	lockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, lockPath, strings.Join([]string{
		"go https://example.invalid/go-corpus " + commit + " src .go",
		"python https://example.invalid/python-corpus def456 . .py",
		"",
	}, "\n"))

	inv, err := buildInventoryWithOptions(grammarDir, lockPath, nil, "", inventoryOptions{
		CorpusSourceRoot: root,
	})
	if err != nil {
		t.Fatalf("buildInventoryWithOptions: %v", err)
	}
	if inv.Summary.WithCorpusSource != 1 || inv.Summary.WithCheckedOutCorpusSource != 1 || inv.Summary.WithPinnedCorpusSource != 1 || inv.Summary.MissingCorpusSource != 1 {
		t.Fatalf("source summary=%+v", inv.Summary)
	}
	if inv.Summary.WithCorpusSourceFiles != 1 || inv.Summary.MissingCorpusSourceFiles != 0 {
		t.Fatalf("source file summary=%+v", inv.Summary)
	}

	byLang := languageStatusesByName(inv.Languages)
	goStatus := byLang["go"].Corpus.Source
	if !goStatus.CheckedOut || !goStatus.Pinned || goStatus.RepoURL != "https://example.invalid/go-corpus" || goStatus.ActualCommit != commit {
		t.Fatalf("go source=%+v", goStatus)
	}
	if goStatus.MatchingFiles != 1 || goStatus.MatchingBytes == 0 || strings.Join(goStatus.MatchExtensions, ",") != ".go" {
		t.Fatalf("go source matching=%+v", goStatus)
	}
	pythonStatus := byLang["python"].Corpus.Source
	if pythonStatus.CheckedOut || pythonStatus.Pinned || !strings.HasSuffix(pythonStatus.ExpectedPath, filepath.Join("corpus_sources", "python")) {
		t.Fatalf("python source=%+v", pythonStatus)
	}
}

func TestValidateRequiredCorpusSources(t *testing.T) {
	tests := []struct {
		name    string
		inv     inventory
		wantErr string
	}{
		{
			name: "valid",
			inv: inventory{
				Summary: inventorySummary{TotalLanguages: 2, WithPinnedCorpusSource: 2, WithCorpusSourceFiles: 2},
				Languages: []languageStatus{
					{Language: "git_rebase", Corpus: corpusStatus{Source: sourceStatus{Pinned: true, MatchingFiles: 1}}},
					{Language: "go", Corpus: corpusStatus{Source: sourceStatus{Pinned: true, MatchingFiles: 2}}},
				},
			},
		},
		{
			name: "missing checkout preserves pinning failure",
			inv: inventory{
				Summary: inventorySummary{TotalLanguages: 2, WithPinnedCorpusSource: 1, WithCorpusSourceFiles: 1, MissingCorpusSource: 1},
				Languages: []languageStatus{
					{Language: "git_rebase"},
					{Language: "go", Corpus: corpusStatus{Source: sourceStatus{Pinned: true, MatchingFiles: 2}}},
				},
			},
			wantErr: "missing pinned corpus source checkouts for 1/2 languages",
		},
		{
			name: "pinned checkout has no policy matches",
			inv: inventory{
				Summary: inventorySummary{TotalLanguages: 2, WithPinnedCorpusSource: 2, WithCorpusSourceFiles: 1, MissingCorpusSourceFiles: 1},
				Languages: []languageStatus{
					{Language: "git_rebase", Corpus: corpusStatus{Source: sourceStatus{CheckedOut: true, Pinned: true}}},
					{Language: "go", Corpus: corpusStatus{Source: sourceStatus{CheckedOut: true, Pinned: true, MatchingFiles: 2}}},
				},
			},
			wantErr: "pinned corpus source checkouts with no matching files for 1/2 languages: git_rebase",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequiredCorpusSources(tt.inv)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateRequiredCorpusSources: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("validateRequiredCorpusSources error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRequiredCorpusSourcesRejectsPinnedCheckoutWithoutMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "git_rebase.bin"), "git_rebase")

	root := filepath.Join(dir, "corpus_sources")
	checkout := filepath.Join(root, "git_rebase")
	commit := initGitRepo(t, checkout, "https://example.invalid/git-rebase-corpus")
	lockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, lockPath, "git_rebase https://example.invalid/git-rebase-corpus "+commit+" . .git-rebase-todo\n")

	inv, err := buildInventoryWithOptions(grammarDir, lockPath, nil, "", inventoryOptions{CorpusSourceRoot: root})
	if err != nil {
		t.Fatalf("buildInventoryWithOptions: %v", err)
	}
	source := languageStatusesByName(inv.Languages)["git_rebase"].Corpus.Source
	if !source.Pinned || source.MatchingFiles != 0 {
		t.Fatalf("expected pinned source with no policy matches: %+v", source)
	}
	if err := validateRequiredCorpusSources(inv); err == nil || !strings.Contains(err.Error(), "git_rebase") {
		t.Fatalf("validateRequiredCorpusSources error = %v, want git_rebase empty-source failure", err)
	}
}

func TestBuildInventoryDoesNotCountSymlinkOnlyCorpusMatches(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")

	root := filepath.Join(dir, "corpus_sources")
	checkout := filepath.Join(root, "go")
	initGitRepo(t, checkout, "https://example.invalid/go-corpus")
	if err := os.Symlink("sample.txt", filepath.Join(checkout, "main.go")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	runGitTest(t, checkout, "add", "main.go")
	runGitTest(t, checkout, "-c", "user.name=gotreesitter-test", "-c", "user.email=test@example.invalid", "commit", "-m", "add symlink")
	commit := strings.TrimSpace(runGitTest(t, checkout, "rev-parse", "HEAD"))
	lockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, lockPath, "go https://example.invalid/go-corpus "+commit+" . .go\n")

	inv, err := buildInventoryWithOptions(grammarDir, lockPath, nil, "", inventoryOptions{CorpusSourceRoot: root})
	if err != nil {
		t.Fatalf("buildInventoryWithOptions: %v", err)
	}
	source := languageStatusesByName(inv.Languages)["go"].Corpus.Source
	if !source.Pinned || source.MatchingFiles != 0 {
		t.Fatalf("symlink must not count as a benchmark-eligible regular file: %+v", source)
	}
	if err := validateRequiredCorpusSources(inv); err == nil {
		t.Fatal("expected symlink-only corpus source to fail required-source validation")
	}
}

func TestBuildInventoryIncludesVendorAndDistCorpusMatches(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")

	root := filepath.Join(dir, "corpus_sources")
	checkout := filepath.Join(root, "go")
	initGitRepo(t, checkout, "https://example.invalid/go-corpus")
	mustMkdir(t, filepath.Join(checkout, "vendor"))
	mustMkdir(t, filepath.Join(checkout, "dist"))
	mustWrite(t, filepath.Join(checkout, "vendor", "dependency.go"), "package dependency\n")
	mustWrite(t, filepath.Join(checkout, "dist", "generated.go"), "package generated\n")
	runGitTest(t, checkout, "add", "vendor/dependency.go", "dist/generated.go")
	runGitTest(t, checkout, "-c", "user.name=gotreesitter-test", "-c", "user.email=test@example.invalid", "commit", "-m", "add generated sources")
	commit := strings.TrimSpace(runGitTest(t, checkout, "rev-parse", "HEAD"))
	lockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, lockPath, "go https://example.invalid/go-corpus "+commit+" . .go\n")

	inv, err := buildInventoryWithOptions(grammarDir, lockPath, nil, "", inventoryOptions{CorpusSourceRoot: root})
	if err != nil {
		t.Fatalf("buildInventoryWithOptions: %v", err)
	}
	source := languageStatusesByName(inv.Languages)["go"].Corpus.Source
	if source.MatchingFiles != 2 || source.MatchingBytes == 0 {
		t.Fatalf("vendor and dist files should follow benchmark eligibility: %+v", source)
	}
}

func TestBuildInventoryRejectsInvalidCorpusSourceSubdir(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")
	grammarLockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, grammarLockPath, "go https://example.invalid/go-grammar grammarcommit src .go\n")
	sourceLockPath := filepath.Join(dir, "corpus_sources.lock")
	mustWrite(t, sourceLockPath, "go https://example.invalid/go-corpus corpuscommit ../escape .go\n")

	_, err := buildInventoryWithOptions(grammarDir, grammarLockPath, nil, "", inventoryOptions{
		CorpusSourceRoot:     filepath.Join(dir, "corpus_sources"),
		CorpusSourceLockPath: sourceLockPath,
	})
	if err == nil || !strings.Contains(err.Error(), `resolve corpus source for go: invalid corpus source lock subdir "../escape"`) {
		t.Fatalf("buildInventoryWithOptions error = %v, want invalid subdir", err)
	}
}

func TestBuildInventoryReportsCorpusSourceScanErrors(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")

	root := filepath.Join(dir, "corpus_sources")
	checkout := filepath.Join(root, "go")
	commit := initGitRepo(t, checkout, "https://example.invalid/go-corpus")
	grammarLockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, grammarLockPath, "go https://example.invalid/go-grammar grammarcommit src .go\n")
	sourceLockPath := filepath.Join(dir, "corpus_sources.lock")
	mustWrite(t, sourceLockPath, "go https://example.invalid/go-corpus "+commit+" missing .go\n")

	_, err := buildInventoryWithOptions(grammarDir, grammarLockPath, nil, "", inventoryOptions{
		CorpusSourceRoot:     root,
		CorpusSourceLockPath: sourceLockPath,
	})
	if err == nil || !strings.Contains(err.Error(), "resolve corpus source for go: scan") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("buildInventoryWithOptions error = %v, want actionable missing-subdir scan error", err)
	}
}

func TestBuildInventoryReportsNonDirectoryCorpusSource(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")
	root := filepath.Join(dir, "corpus_sources")
	mustMkdir(t, root)
	mustWrite(t, filepath.Join(root, "go"), "not a checkout\n")
	lockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, lockPath, "go https://example.invalid/go-corpus commit . .go\n")

	_, err := buildInventoryWithOptions(grammarDir, lockPath, nil, "", inventoryOptions{CorpusSourceRoot: root})
	if err == nil || !strings.Contains(err.Error(), "corpus source checkout is not a directory") {
		t.Fatalf("buildInventoryWithOptions error = %v, want non-directory source error", err)
	}
}

func TestBuildInventoryCanUseSeparateCorpusSourceLock(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "go.bin"), "go")

	root := filepath.Join(dir, "corpus_sources")
	goCheckout := filepath.Join(root, "go")
	initGitRepo(t, goCheckout, "https://example.invalid/go-corpus")
	mustWrite(t, filepath.Join(goCheckout, "main.go"), "package main\nfunc main() {}\n")
	runGitTest(t, goCheckout, "add", "main.go")
	runGitTest(t, goCheckout, "-c", "user.name=gotreesitter-test", "-c", "user.email=test@example.invalid", "commit", "-m", "add corpus source")
	corpusCommit := strings.TrimSpace(runGitTest(t, goCheckout, "rev-parse", "HEAD"))

	grammarLockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, grammarLockPath, strings.Join([]string{
		"go https://example.invalid/go-grammar grammarcommit . .go",
		"",
	}, "\n"))
	sourceLockPath := filepath.Join(dir, "corpus_sources.lock")
	mustWrite(t, sourceLockPath, strings.Join([]string{
		"go https://example.invalid/go-corpus " + corpusCommit + " .",
		"",
	}, "\n"))

	inv, err := buildInventoryWithOptions(grammarDir, grammarLockPath, nil, "", inventoryOptions{
		CorpusSourceRoot:     root,
		CorpusSourceLockPath: sourceLockPath,
	})
	if err != nil {
		t.Fatalf("buildInventoryWithOptions: %v", err)
	}
	if inv.CorpusSourceLockPath != sourceLockPath {
		t.Fatalf("corpus source lock path=%q", inv.CorpusSourceLockPath)
	}
	goStatus := languageStatusesByName(inv.Languages)["go"]
	if goStatus.Lock.RepoURL != "https://example.invalid/go-grammar" || goStatus.Lock.Commit != "grammarcommit" {
		t.Fatalf("grammar lock metadata changed: %+v", goStatus.Lock)
	}
	source := goStatus.Corpus.Source
	if !source.Pinned || source.RepoURL != "https://example.invalid/go-corpus" || source.ExpectedCommit != corpusCommit {
		t.Fatalf("source lock metadata not applied: %+v", source)
	}
	if strings.Join(source.MatchExtensions, ",") != ".go" || source.MatchingFiles != 1 {
		t.Fatalf("source lock should inherit grammar matchers: %+v", source)
	}
}

func TestBuildInventoryCountsCorpusSourceFilesUnderSourceLockSubdir(t *testing.T) {
	dir := t.TempDir()
	grammarDir := filepath.Join(dir, "grammar_blobs")
	mustMkdir(t, grammarDir)
	mustWrite(t, filepath.Join(grammarDir, "vimdoc.bin"), "vimdoc")

	root := filepath.Join(dir, "corpus_sources")
	checkout := filepath.Join(root, "vimdoc")
	initGitRepo(t, checkout, "https://example.invalid/vimdoc-corpus")
	mustMkdir(t, filepath.Join(checkout, "runtime", "doc"))
	mustWrite(t, filepath.Join(checkout, "runtime", "doc", "help.txt"), "help text\n")
	mustWrite(t, filepath.Join(checkout, "CMakeLists.txt"), "not vimdoc\n")
	runGitTest(t, checkout, "add", "runtime/doc/help.txt", "CMakeLists.txt")
	runGitTest(t, checkout, "-c", "user.name=gotreesitter-test", "-c", "user.email=test@example.invalid", "commit", "-m", "add docs")
	commit := strings.TrimSpace(runGitTest(t, checkout, "rev-parse", "HEAD"))

	grammarLockPath := filepath.Join(dir, "languages.lock")
	mustWrite(t, grammarLockPath, strings.Join([]string{
		"vimdoc https://example.invalid/vimdoc-grammar grammarcommit src .txt",
		"",
	}, "\n"))
	sourceLockPath := filepath.Join(dir, "corpus_sources.lock")
	mustWrite(t, sourceLockPath, strings.Join([]string{
		"vimdoc https://example.invalid/vimdoc-corpus " + commit + " runtime/doc .txt",
		"",
	}, "\n"))

	inv, err := buildInventoryWithOptions(grammarDir, grammarLockPath, nil, "", inventoryOptions{
		CorpusSourceRoot:     root,
		CorpusSourceLockPath: sourceLockPath,
	})
	if err != nil {
		t.Fatalf("buildInventoryWithOptions: %v", err)
	}
	source := languageStatusesByName(inv.Languages)["vimdoc"].Corpus.Source
	if source.Subdir != "runtime/doc" || !strings.HasSuffix(source.ScanPath, filepath.Join("vimdoc", "runtime", "doc")) {
		t.Fatalf("source subdir/scan path not tracked: %+v", source)
	}
	if source.MatchingFiles != 1 {
		t.Fatalf("source lock subdir should exclude top-level CMakeLists.txt: %+v", source)
	}
	if inv.Summary.WithCorpusSourceFiles != 1 || inv.Summary.MissingCorpusSourceFiles != 0 {
		t.Fatalf("source summary=%+v", inv.Summary)
	}
}

func TestWriteMarkdownIncludesCoverageRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.md")
	inv := inventory{
		GeneratedAt: "2026-06-07T00:00:00Z",
		Summary: inventorySummary{
			TotalLanguages:        1,
			WithCorpus:            1,
			WithCorpusSourceFiles: 1,
			Benchmarked:           1,
			Instrumented:          1,
			Profiled:              1,
			ReadinessCounts:       map[string]int{"meets-current-targets": 1},
		},
		Languages: []languageStatus{
			{
				Language: "go",
				Lock:     lockStatus{Present: true},
				Corpus: corpusStatus{
					Present: true,
					Buckets: []string{"large", "medium", "small"},
					Source:  sourceStatus{ExpectedPath: "corpus_sources/go", CheckedOut: true, Pinned: true, MatchingFiles: 3, MatchingBytes: 4096},
				},
				Benchmark: benchmarkStatus{
					Readiness:    "meets-current-targets",
					Instrumented: true,
					Profiled:     true,
				},
			},
		},
	}
	if err := writeMarkdown(path, inv); err != nil {
		t.Fatalf("writeMarkdown: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"| Languages | 1 |", "| Profiled | 1 |", "| With corpus source files | 1 |", "| meets-current-targets | 1 |", "| go | yes | large/medium/small | pinned | 3 | 4096 | meets-current-targets | yes | yes |"} {
		if !strings.Contains(text, want) {
			t.Fatalf("markdown missing %q:\n%s", want, text)
		}
	}
}

func TestSelectLanguageNames(t *testing.T) {
	statuses := []languageStatus{
		{
			Language: "go",
			Lock:     lockStatus{Present: true},
			Corpus:   corpusStatus{Present: true, HasMedium: true, HasLarge: true, Source: sourceStatus{CheckedOut: true, Pinned: true, MatchingFiles: 2}},
			Benchmark: benchmarkStatus{
				Present:      true,
				Readiness:    "meets-current-targets",
				HasFull:      true,
				HasEdit:      true,
				HasNoEdit:    true,
				Instrumented: true,
				Profiled:     true,
			},
		},
		{
			Language:  "python",
			Lock:      lockStatus{Present: true},
			Corpus:    corpusStatus{Present: true, HasMedium: true},
			Benchmark: benchmarkStatus{Readiness: readinessUnmeasured},
		},
		{
			Language:  "zig",
			Benchmark: benchmarkStatus{Readiness: readinessUnmeasured},
		},
		{
			Language: "rust",
			Lock:     lockStatus{Present: true},
			Corpus:   corpusStatus{Present: true, HasMedium: true, HasLarge: true, Source: sourceStatus{CheckedOut: true}},
			Benchmark: benchmarkStatus{
				Present:   true,
				Readiness: "needs-full-parse-work",
				HasFull:   true,
				HasEdit:   true,
				HasNoEdit: true,
			},
		},
		{
			Language: "hcl",
			Lock:     lockStatus{Present: true},
			Corpus:   corpusStatus{Present: true, HasMedium: true, HasLarge: true},
			Benchmark: benchmarkStatus{
				Present:       true,
				Readiness:     "parity-blocked",
				Failure:       "large.tf: shape mismatch",
				FailureSource: "container.log:3",
			},
		},
	}

	tests := []struct {
		selector string
		want     []string
	}{
		{selector: "all", want: []string{"go", "hcl", "python", "rust", "zig"}},
		{selector: "missing-corpus", want: []string{"zig"}},
		{selector: "with-corpus-source", want: []string{"go", "rust"}},
		{selector: "checked-out-corpus-source", want: []string{"go", "rust"}},
		{selector: "pinned-corpus-source", want: []string{"go"}},
		{selector: "missing-corpus-source", want: []string{"hcl", "python", "zig"}},
		{selector: "usable-corpus-source", want: []string{"go"}},
		{selector: "empty-corpus-source", want: []string{"rust"}},
		{selector: "ready-to-benchmark", want: []string{"hcl", "python"}},
		{selector: "corpus-unmeasured", want: []string{"python"}},
		{selector: "l4-corpus-unmeasured", want: []string{}},
		{selector: "missing-l4-large-corpus", want: []string{"python", "zig"}},
		{selector: "benchmarked", want: []string{"go", "rust"}},
		{selector: "instrumented", want: []string{"go"}},
		{selector: "profiled", want: []string{"go"}},
		{selector: "needs-work", want: []string{"hcl", "rust"}},
		{selector: "parity-blocked", want: []string{"hcl"}},
		{selector: "missing-lock", want: []string{"zig"}},
	}

	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			got, err := selectLanguageNames(statuses, tt.selector)
			if err != nil {
				t.Fatalf("selectLanguageNames: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("selected = %#v, want %#v", got, tt.want)
			}
		})
	}

	if _, err := selectLanguageNames(statuses, "nope"); err == nil {
		t.Fatal("expected error for unknown selector")
	}
}

func languageStatusesByName(statuses []languageStatus) map[string]languageStatus {
	out := make(map[string]languageStatus, len(statuses))
	for _, status := range statuses {
		out[status.Language] = status
	}
	return out
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, string(data))
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, path string, remote string) string {
	t.Helper()
	mustMkdir(t, path)
	runGitTest(t, path, "init")
	mustWrite(t, filepath.Join(path, "sample.txt"), "sample\n")
	runGitTest(t, path, "add", "sample.txt")
	runGitTest(t, path, "-c", "user.name=gotreesitter-test", "-c", "user.email=test@example.invalid", "commit", "-m", "seed")
	runGitTest(t, path, "remote", "add", "origin", remote)
	out := runGitTest(t, path, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
