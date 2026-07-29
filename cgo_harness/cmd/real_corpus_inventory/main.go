package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentable/gotreesitter/cgo_harness/internal/realcorpus"
	"github.com/agentable/gotreesitter/grammars"
)

const readinessUnmeasured = "unmeasured"

type lockEntry struct {
	Name    string   `json:"name"`
	RepoURL string   `json:"repo_url,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Subdir  string   `json:"subdir,omitempty"`
	Exts    []string `json:"extensions,omitempty"`
}

type corpusManifest struct {
	Languages []string              `json:"languages"`
	Entries   []corpusManifestEntry `json:"entries"`
	Missing   []string              `json:"missing_languages,omitempty"`
}

type corpusManifestEntry struct {
	Language     string `json:"language"`
	Bucket       string `json:"bucket"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	SourceRepo   string `json:"source_repo,omitempty"`
	SourceCommit string `json:"source_commit,omitempty"`
	SourcePath   string `json:"source_path"`
	OutputPath   string `json:"output_path"`
}

type benchReport struct {
	Languages []benchLanguageReport `json:"languages"`
}

type benchLanguageReport struct {
	Language       string                        `json:"language"`
	Readiness      string                        `json:"readiness,omitempty"`
	Expectation    string                        `json:"expectation,omitempty"`
	Failure        string                        `json:"failure,omitempty"`
	FailureSource  string                        `json:"failure_source,omitempty"`
	FullRatio      float64                       `json:"full_ratio,omitempty"`
	EditRatio      float64                       `json:"edit_ratio,omitempty"`
	NoEditRatio    float64                       `json:"noedit_ratio,omitempty"`
	FullGoNanos    float64                       `json:"full_go_ns,omitempty"`
	FullCNanos     float64                       `json:"full_c_ns,omitempty"`
	EditGoNanos    float64                       `json:"edit_go_ns,omitempty"`
	EditCNanos     float64                       `json:"edit_c_ns,omitempty"`
	NoEditGoNanos  float64                       `json:"noedit_go_ns,omitempty"`
	NoEditCNanos   float64                       `json:"noedit_c_ns,omitempty"`
	TopAttribution []attributionBucket           `json:"top_attribution,omitempty"`
	GoSuiteMetrics map[string]map[string]float64 `json:"go_suite_metrics,omitempty"`
	CSuiteMetrics  map[string]map[string]float64 `json:"c_suite_metrics,omitempty"`
}

type attributionBucket struct {
	Suite string  `json:"suite"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type inventory struct {
	GeneratedAt          string           `json:"generated_at"`
	GrammarDir           string           `json:"grammar_dir"`
	LockPath             string           `json:"lock_path,omitempty"`
	CorpusManifestPath   string           `json:"corpus_manifest_path,omitempty"`
	CorpusManifestPaths  []string         `json:"corpus_manifest_paths,omitempty"`
	CorpusSourceRoot     string           `json:"corpus_source_root,omitempty"`
	CorpusSourceLockPath string           `json:"corpus_source_lock_path,omitempty"`
	BenchReportPath      string           `json:"bench_report_path,omitempty"`
	Summary              inventorySummary `json:"summary"`
	Languages            []languageStatus `json:"languages"`
}

type inventorySummary struct {
	TotalLanguages             int            `json:"total_languages"`
	WithLockMetadata           int            `json:"with_lock_metadata"`
	WithCorpus                 int            `json:"with_corpus"`
	WithL3MediumCorpus         int            `json:"with_l3_medium_corpus"`
	WithL4LargeCorpus          int            `json:"with_l4_large_corpus"`
	WithCorpusSource           int            `json:"with_corpus_source"`
	WithCheckedOutCorpusSource int            `json:"with_checked_out_corpus_source"`
	WithPinnedCorpusSource     int            `json:"with_pinned_corpus_source"`
	WithCorpusSourceFiles      int            `json:"with_corpus_source_files"`
	MissingCorpusSourceFiles   int            `json:"missing_corpus_source_files"`
	MissingCorpusSource        int            `json:"missing_corpus_source"`
	Benchmarked                int            `json:"benchmarked"`
	Instrumented               int            `json:"instrumented"`
	Profiled                   int            `json:"profiled"`
	MeetsCurrentTargets        int            `json:"meets_current_targets"`
	MissingCorpus              int            `json:"missing_corpus"`
	MissingBenchmark           int            `json:"missing_benchmark"`
	MissingLockMetadata        int            `json:"missing_lock_metadata"`
	ReadinessCounts            map[string]int `json:"readiness_counts,omitempty"`
}

type languageStatus struct {
	Language  string          `json:"language"`
	BlobPath  string          `json:"blob_path"`
	Lock      lockStatus      `json:"lock"`
	Corpus    corpusStatus    `json:"corpus"`
	Benchmark benchmarkStatus `json:"benchmark"`
}

type lockStatus struct {
	Present bool     `json:"present"`
	RepoURL string   `json:"repo_url,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Subdir  string   `json:"subdir,omitempty"`
	Exts    []string `json:"extensions,omitempty"`
}

type corpusStatus struct {
	Present         bool         `json:"present"`
	Entries         int          `json:"entries"`
	Buckets         []string     `json:"buckets,omitempty"`
	SourceRepos     []string     `json:"source_repos,omitempty"`
	SourceCommits   []string     `json:"source_commits,omitempty"`
	TotalBytes      int64        `json:"total_bytes,omitempty"`
	HasSmall        bool         `json:"has_small"`
	HasMedium       bool         `json:"has_medium"`
	HasLarge        bool         `json:"has_large"`
	ManifestMissing bool         `json:"manifest_missing,omitempty"`
	Source          sourceStatus `json:"source"`
}

type sourceStatus struct {
	ExpectedPath    string   `json:"expected_path,omitempty"`
	Subdir          string   `json:"subdir,omitempty"`
	ScanPath        string   `json:"scan_path,omitempty"`
	CheckedOut      bool     `json:"checked_out"`
	RepoURL         string   `json:"repo_url,omitempty"`
	ExpectedCommit  string   `json:"expected_commit,omitempty"`
	RemoteURL       string   `json:"remote_url,omitempty"`
	RemoteMatches   bool     `json:"remote_matches,omitempty"`
	ActualCommit    string   `json:"actual_commit,omitempty"`
	Pinned          bool     `json:"pinned"`
	MatchExtensions []string `json:"match_extensions,omitempty"`
	MatchBasenames  []string `json:"match_basenames,omitempty"`
	MatchPaths      []string `json:"match_paths,omitempty"`
	MatchingFiles   int      `json:"matching_files,omitempty"`
	MatchingBytes   int64    `json:"matching_bytes,omitempty"`
}

type benchmarkStatus struct {
	Present       bool    `json:"present"`
	Readiness     string  `json:"readiness"`
	Expectation   string  `json:"expectation,omitempty"`
	Failure       string  `json:"failure,omitempty"`
	FailureSource string  `json:"failure_source,omitempty"`
	FullRatio     float64 `json:"full_ratio,omitempty"`
	EditRatio     float64 `json:"edit_ratio,omitempty"`
	NoEditRatio   float64 `json:"noedit_ratio,omitempty"`
	HasFull       bool    `json:"has_full"`
	HasEdit       bool    `json:"has_edit"`
	HasNoEdit     bool    `json:"has_noedit"`
	Instrumented  bool    `json:"instrumented"`
	Profiled      bool    `json:"profiled"`
}

func main() {
	var (
		grammarDir           string
		lockPath             string
		manifestPath         string
		sourceRoot           string
		sourceLockPath       string
		benchPath            string
		outJSON              string
		outMD                string
		outLangs             string
		selector             string
		printLangs           bool
		requireCorpusSources bool
	)
	flag.StringVar(&grammarDir, "grammar-dir", "", "path to grammars/grammar_blobs")
	flag.StringVar(&lockPath, "lock", "", "path to grammars/languages.lock")
	flag.StringVar(&manifestPath, "corpus-manifest", "", "optional comma-separated path(s) to real corpus manifest JSON")
	flag.StringVar(&sourceRoot, "corpus-source-root", "", "root containing external per-language corpus checkouts; default is ../gotreesitter-corpora/corpus_sources")
	flag.StringVar(&sourceLockPath, "corpus-source-lock", "", "optional source lock for external corpus checkouts; falls back to -lock")
	flag.StringVar(&benchPath, "bench-report", "", "optional path to real_corpus_bench_report.json")
	flag.StringVar(&outJSON, "out-json", "", "optional JSON output path; stdout when empty and -out-md is empty")
	flag.StringVar(&outMD, "out-md", "", "optional Markdown output path")
	flag.StringVar(&outLangs, "out-langs", "", "optional newline-separated selected language output path")
	flag.StringVar(&selector, "select", "all", "language selector for -print-langs/-out-langs")
	flag.BoolVar(&printLangs, "print-langs", false, "print selected languages and suppress default JSON stdout")
	flag.BoolVar(&requireCorpusSources, "require-corpus-sources", false, "fail if any language lacks a pinned external corpus source checkout with matching files")
	flag.Parse()

	resolvedGrammarDir, err := resolvePath(grammarDir, []string{
		"grammars/grammar_blobs",
		filepath.Join("..", "grammars", "grammar_blobs"),
	})
	if err != nil {
		fatalf("resolve grammar dir: %v", err)
	}
	resolvedLockPath, err := resolveOptionalPath(lockPath, []string{
		"grammars/languages.lock",
		filepath.Join("..", "grammars", "languages.lock"),
	})
	if err != nil {
		fatalf("resolve lock path: %v", err)
	}
	resolvedManifestPaths, err := resolveOptionalPaths(manifestPath, []string{
		filepath.Join("cgo_harness", "corpus_real", "manifest.json"),
		filepath.Join("corpus_real", "manifest.json"),
	})
	if err != nil {
		fatalf("resolve corpus manifest: %v", err)
	}
	resolvedBenchPath, err := resolveOptionalPath(benchPath, nil)
	if err != nil {
		fatalf("resolve bench report: %v", err)
	}
	resolvedSourceLockPath, err := resolveOptionalPath(sourceLockPath, nil)
	if err != nil {
		fatalf("resolve corpus source lock: %v", err)
	}
	resolvedSourceRoot := resolveCorpusSourceRoot(sourceRoot)

	inv, err := buildInventoryWithOptions(resolvedGrammarDir, resolvedLockPath, resolvedManifestPaths, resolvedBenchPath, inventoryOptions{
		CorpusSourceRoot:     resolvedSourceRoot,
		CorpusSourceLockPath: resolvedSourceLockPath,
	})
	if err != nil {
		fatalf("build inventory: %v", err)
	}
	if requireCorpusSources {
		if err := validateRequiredCorpusSources(inv); err != nil {
			fatalf("%v", err)
		}
	}
	selectedLangs, err := selectLanguageNames(inv.Languages, selector)
	if err != nil {
		fatalf("select languages: %v", err)
	}
	if outLangs != "" {
		if err := writeLanguageList(outLangs, selectedLangs); err != nil {
			fatalf("write %s: %v", outLangs, err)
		}
	}
	if outJSON != "" {
		if err := writeJSON(outJSON, inv); err != nil {
			fatalf("write %s: %v", outJSON, err)
		}
	}
	if outMD != "" {
		if err := writeMarkdown(outMD, inv); err != nil {
			fatalf("write %s: %v", outMD, err)
		}
	}
	if printLangs {
		if err := writeLanguageList("-", selectedLangs); err != nil {
			fatalf("write selected languages: %v", err)
		}
	}
	if outJSON == "" && outMD == "" && outLangs == "" && !printLangs {
		if err := writeJSON("-", inv); err != nil {
			fatalf("write stdout: %v", err)
		}
	}
}

func validateRequiredCorpusSources(inv inventory) error {
	if inv.Summary.WithPinnedCorpusSource < inv.Summary.TotalLanguages {
		missingPinned := inv.Summary.TotalLanguages - inv.Summary.WithPinnedCorpusSource
		return fmt.Errorf("missing pinned corpus source checkouts for %d/%d languages", missingPinned, inv.Summary.TotalLanguages)
	}

	empty := make([]string, 0, inv.Summary.MissingCorpusSourceFiles)
	for _, status := range inv.Languages {
		if status.Corpus.Source.Pinned && status.Corpus.Source.MatchingFiles == 0 {
			empty = append(empty, status.Language)
		}
	}
	if len(empty) > 0 {
		return fmt.Errorf("pinned corpus source checkouts with no matching files for %d/%d languages: %s", len(empty), inv.Summary.TotalLanguages, strings.Join(empty, ", "))
	}
	return nil
}

type inventoryOptions struct {
	CorpusSourceRoot     string
	CorpusSourceLockPath string
}

func buildInventory(grammarDir, lockPath string, manifestPaths []string, benchPath string) (inventory, error) {
	return buildInventoryWithOptions(grammarDir, lockPath, manifestPaths, benchPath, inventoryOptions{})
}

func buildInventoryWithOptions(grammarDir, lockPath string, manifestPaths []string, benchPath string, opts inventoryOptions) (inventory, error) {
	blobs, err := loadGrammarBlobs(grammarDir)
	if err != nil {
		return inventory{}, err
	}
	lockEntries := map[string]lockEntry{}
	if lockPath != "" {
		lockEntries, err = parseLockFile(lockPath)
		if err != nil {
			return inventory{}, fmt.Errorf("parse lock: %w", err)
		}
	}
	sourceLockEntries := lockEntries
	hasCorpusSourceLock := opts.CorpusSourceLockPath != ""
	if opts.CorpusSourceLockPath != "" {
		sourceLockEntries, err = parseLockFile(opts.CorpusSourceLockPath)
		if err != nil {
			return inventory{}, fmt.Errorf("parse corpus source lock: %w", err)
		}
	}
	corpusByLanguage := map[string]corpusStatus{}
	if len(manifestPaths) > 0 {
		corpusByLanguage, err = loadCorpusStatuses(manifestPaths)
		if err != nil {
			return inventory{}, fmt.Errorf("load corpus manifest: %w", err)
		}
	}
	benchByLanguage := map[string]benchmarkStatus{}
	if benchPath != "" {
		benchByLanguage, err = loadBenchmarkStatuses(benchPath)
		if err != nil {
			return inventory{}, fmt.Errorf("load bench report: %w", err)
		}
	}
	names := make([]string, 0, len(blobs))
	for name := range blobs {
		names = append(names, name)
	}
	sort.Strings(names)

	inv := inventory{
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GrammarDir:           grammarDir,
		LockPath:             lockPath,
		CorpusManifestPath:   strings.Join(manifestPaths, ","),
		CorpusManifestPaths:  append([]string(nil), manifestPaths...),
		CorpusSourceRoot:     opts.CorpusSourceRoot,
		CorpusSourceLockPath: opts.CorpusSourceLockPath,
		BenchReportPath:      benchPath,
		Languages:            make([]languageStatus, 0, len(names)),
	}
	for _, name := range names {
		status := languageStatus{
			Language:  name,
			BlobPath:  blobs[name],
			Benchmark: benchmarkStatus{Readiness: readinessUnmeasured},
		}
		if entry, ok := lockEntries[name]; ok {
			status.Lock = lockStatus{
				Present: true,
				RepoURL: entry.RepoURL,
				Commit:  entry.Commit,
				Subdir:  entry.Subdir,
				Exts:    append([]string(nil), entry.Exts...),
			}
		}
		if corpus, ok := corpusByLanguage[name]; ok {
			status.Corpus = corpus
		}
		status.Corpus.Source, err = resolveSourceStatus(opts.CorpusSourceRoot, name, sourceLockForLanguage(name, status.Lock, sourceLockEntries, hasCorpusSourceLock))
		if err != nil {
			return inventory{}, fmt.Errorf("resolve corpus source for %s: %w", name, err)
		}
		if bench, ok := benchByLanguage[name]; ok {
			status.Benchmark = bench
		}
		inv.Languages = append(inv.Languages, status)
		accumulateSummary(&inv.Summary, status)
	}
	inv.Summary.TotalLanguages = len(inv.Languages)
	return inv, nil
}

func sourceLockForLanguage(language string, grammarLock lockStatus, sourceEntries map[string]lockEntry, hasCorpusSourceLock bool) lockStatus {
	if hasCorpusSourceLock {
		if entry, ok := sourceEntries[language]; ok {
			exts := append([]string(nil), entry.Exts...)
			if len(exts) == 0 {
				exts = append([]string(nil), grammarLock.Exts...)
			}
			return lockStatus{
				Present: true,
				RepoURL: entry.RepoURL,
				Commit:  entry.Commit,
				Subdir:  entry.Subdir,
				Exts:    exts,
			}
		}
		fallback := grammarLock
		fallback.Subdir = ""
		return fallback
	}
	if entry, ok := sourceEntries[language]; ok {
		exts := append([]string(nil), entry.Exts...)
		if len(exts) == 0 {
			exts = append([]string(nil), grammarLock.Exts...)
		}
		return lockStatus{
			Present: true,
			RepoURL: entry.RepoURL,
			Commit:  entry.Commit,
			Exts:    exts,
		}
	}
	fallback := grammarLock
	fallback.Subdir = ""
	return fallback
}

func loadGrammarBlobs(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".bin" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".bin")
		out[name] = filepath.Join(dir, entry.Name())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no .bin grammar blobs found in %s", dir)
	}
	return out, nil
}

func parseLockFile(path string) (map[string]lockEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]lockEntry{}
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil, fmt.Errorf("line %d: expected at least name repo commit", lineNo+1)
		}
		entry := lockEntry{Name: fields[0], RepoURL: fields[1], Commit: fields[2]}
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
		out[entry.Name] = entry
	}
	return out, nil
}

func loadCorpusStatuses(paths []string) (map[string]corpusStatus, error) {
	out := map[string]corpusStatus{}
	seenEntries := map[string]bool{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var manifest corpusManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, entry := range manifest.Entries {
			status := out[entry.Language]
			status.SourceRepos = appendUnique(status.SourceRepos, entry.SourceRepo)
			status.SourceCommits = appendUnique(status.SourceCommits, entry.SourceCommit)
			entryKey := corpusEntryKey(entry)
			if seenEntries[entryKey] {
				out[entry.Language] = status
				continue
			}
			seenEntries[entryKey] = true

			status.Present = true
			status.ManifestMissing = false
			status.Entries++
			status.TotalBytes += entry.Bytes
			switch entry.Bucket {
			case "small":
				status.HasSmall = true
			case "medium":
				status.HasMedium = true
			case "large":
				status.HasLarge = true
			}
			status.Buckets = appendUnique(status.Buckets, entry.Bucket)
			out[entry.Language] = status
		}
		for _, name := range manifest.Missing {
			status := out[name]
			if !status.Present {
				status.ManifestMissing = true
			}
			out[name] = status
		}
	}
	for name, status := range out {
		sort.Strings(status.Buckets)
		sort.Strings(status.SourceRepos)
		sort.Strings(status.SourceCommits)
		out[name] = status
	}
	return out, nil
}

func resolveSourceStatus(root, language string, lock lockStatus) (sourceStatus, error) {
	if strings.TrimSpace(root) == "" {
		return sourceStatus{}, nil
	}
	expectedPath := filepath.Clean(filepath.Join(root, language))
	status := sourceStatus{
		ExpectedPath:   expectedPath,
		RepoURL:        lock.RepoURL,
		ExpectedCommit: lock.Commit,
	}
	subdir, ok := realcorpus.CleanSubdir(lock.Subdir)
	if !ok {
		return status, fmt.Errorf("invalid corpus source lock subdir %q", lock.Subdir)
	}
	st, err := os.Stat(expectedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, fmt.Errorf("stat %s: %w", expectedPath, err)
	}
	if !st.IsDir() {
		return status, fmt.Errorf("corpus source checkout is not a directory: %s", expectedPath)
	}
	status.CheckedOut = true
	status.ActualCommit = gitHead(expectedPath)
	status.RemoteURL = gitRemoteURL(expectedPath)
	status.RemoteMatches = sameGitRemote(status.RemoteURL, lock.RepoURL)
	status.Pinned = lock.Commit != "" && status.ActualCommit == lock.Commit && status.RemoteMatches
	status.MatchExtensions, status.MatchBasenames, status.MatchPaths = sourceMatchersForLanguage(language, lock.Exts)
	scanPath := expectedPath
	if subdir != "" {
		status.Subdir = filepath.ToSlash(subdir)
		scanPath = filepath.Join(expectedPath, subdir)
		status.ScanPath = scanPath
	}
	status.MatchingFiles, status.MatchingBytes, err = countMatchingSourceFiles(scanPath, status.MatchExtensions, status.MatchBasenames, status.MatchPaths)
	if err != nil {
		return status, fmt.Errorf("scan %s: %w", scanPath, err)
	}
	return status, nil
}

func sameGitRemote(actual, expected string) bool {
	actual = strings.TrimSuffix(strings.TrimSpace(actual), ".git")
	expected = strings.TrimSuffix(strings.TrimSpace(expected), ".git")
	return actual != "" && actual == expected
}

func sourceMatchersForLanguage(language string, exts []string) ([]string, []string, []string) {
	return realcorpus.NewFileMatcher(language, exts, registryExtensionsForLanguage(language)).Matchers()
}

func registryExtensionsForLanguage(language string) []string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return nil
	}
	for _, entry := range grammars.AllLanguages() {
		if strings.ToLower(strings.TrimSpace(entry.Name)) != language {
			continue
		}
		return append([]string(nil), entry.Extensions...)
	}
	return nil
}

func countMatchingSourceFiles(root string, exts []string, basenames []string, paths []string) (int, int64, error) {
	matcher := realcorpus.NewFileMatcherFromParts(exts, basenames, paths)
	var count int
	var bytes int64
	err := realcorpus.WalkMatchingFiles(root, func(relPath string) bool {
		return matcher.Matches(relPath)
	}, func(file realcorpus.File) error {
		count++
		bytes += file.Size
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return count, bytes, nil
}

func gitHead(path string) string {
	cmd := exec.Command("git", "-C", path, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitRemoteURL(path string) string {
	cmd := exec.Command("git", "-C", path, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func corpusEntryKey(entry corpusManifestEntry) string {
	parts := []string{
		entry.Language,
		entry.Bucket,
		entry.OutputPath,
		entry.SHA256,
	}
	return strings.Join(parts, "\x00")
}

func loadBenchmarkStatuses(path string) (map[string]benchmarkStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report benchReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	out := map[string]benchmarkStatus{}
	for _, lang := range report.Languages {
		instrumented := hasMetrics(lang.GoSuiteMetrics)
		profiled := len(lang.TopAttribution) > 0 || hasProfileMetrics(lang.GoSuiteMetrics)
		status := benchmarkStatus{
			Present:       true,
			Readiness:     lang.Readiness,
			Expectation:   lang.Expectation,
			Failure:       lang.Failure,
			FailureSource: lang.FailureSource,
			FullRatio:     lang.FullRatio,
			EditRatio:     lang.EditRatio,
			NoEditRatio:   lang.NoEditRatio,
			HasFull:       lang.FullGoNanos > 0 && lang.FullCNanos > 0,
			HasEdit:       lang.EditGoNanos > 0 && lang.EditCNanos > 0,
			HasNoEdit:     lang.NoEditGoNanos > 0 && lang.NoEditCNanos > 0,
			Instrumented:  instrumented,
			Profiled:      profiled,
		}
		if status.Readiness == "" {
			status.Readiness = readinessUnmeasured
		}
		out[lang.Language] = status
	}
	return out, nil
}

func hasProfileMetrics(metrics map[string]map[string]float64) bool {
	standard := map[string]bool{
		"ns/op":     true,
		"B/op":      true,
		"allocs/op": true,
		"MB/s":      true,
		"files/op":  true,
	}
	for _, suite := range metrics {
		for name := range suite {
			if !standard[name] {
				return true
			}
		}
	}
	return false
}

func hasMetrics(metrics map[string]map[string]float64) bool {
	for _, suite := range metrics {
		if len(suite) > 0 {
			return true
		}
	}
	return false
}

func accumulateSummary(summary *inventorySummary, status languageStatus) {
	readiness := status.Benchmark.Readiness
	if readiness == "" {
		readiness = readinessUnmeasured
	}
	if summary.ReadinessCounts == nil {
		summary.ReadinessCounts = map[string]int{}
	}
	summary.ReadinessCounts[readiness]++
	if status.Lock.Present {
		summary.WithLockMetadata++
	} else {
		summary.MissingLockMetadata++
	}
	if status.Corpus.Present {
		summary.WithCorpus++
	} else {
		summary.MissingCorpus++
	}
	if status.Corpus.HasMedium {
		summary.WithL3MediumCorpus++
	}
	if status.Corpus.HasLarge {
		summary.WithL4LargeCorpus++
	}
	if status.Corpus.Source.CheckedOut {
		summary.WithCorpusSource++
		summary.WithCheckedOutCorpusSource++
	} else {
		summary.MissingCorpusSource++
	}
	if status.Corpus.Source.Pinned {
		summary.WithPinnedCorpusSource++
	}
	if status.Corpus.Source.MatchingFiles > 0 {
		summary.WithCorpusSourceFiles++
	} else if status.Corpus.Source.CheckedOut {
		summary.MissingCorpusSourceFiles++
	}
	if status.Benchmark.Present && status.Benchmark.HasFull && status.Benchmark.HasEdit && status.Benchmark.HasNoEdit {
		summary.Benchmarked++
	} else {
		summary.MissingBenchmark++
	}
	if status.Benchmark.Instrumented {
		summary.Instrumented++
	}
	if status.Benchmark.Profiled {
		summary.Profiled++
	}
	if status.Benchmark.Readiness == "meets-current-targets" {
		summary.MeetsCurrentTargets++
	}
}

func selectLanguageNames(statuses []languageStatus, selector string) ([]string, error) {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if selector == "" {
		selector = "all"
	}
	match := func(status languageStatus) (bool, error) {
		benchmarked := status.Benchmark.Present && status.Benchmark.HasFull && status.Benchmark.HasEdit && status.Benchmark.HasNoEdit
		switch selector {
		case "all":
			return true, nil
		case "locked", "with-lock":
			return status.Lock.Present, nil
		case "missing-lock":
			return !status.Lock.Present, nil
		case "with-corpus":
			return status.Corpus.Present, nil
		case "missing-corpus":
			return !status.Corpus.Present, nil
		case "with-corpus-source", "with-corpus-checkout", "separated-corpus":
			return status.Corpus.Source.CheckedOut, nil
		case "checked-out-corpus-source", "checked-out-separated-corpus":
			return status.Corpus.Source.CheckedOut, nil
		case "pinned-corpus-source", "pinned-separated-corpus":
			return status.Corpus.Source.Pinned, nil
		case "missing-corpus-source", "missing-separated-corpus":
			return !status.Corpus.Source.CheckedOut, nil
		case "unpinned-corpus-source":
			return status.Corpus.Source.CheckedOut && !status.Corpus.Source.Pinned, nil
		case "with-corpus-source-files", "usable-corpus-source":
			return status.Corpus.Source.MatchingFiles > 0, nil
		case "missing-corpus-source-files", "empty-corpus-source":
			return status.Corpus.Source.CheckedOut && status.Corpus.Source.MatchingFiles == 0, nil
		case "missing-l3-medium-corpus":
			return !status.Corpus.HasMedium, nil
		case "missing-l4-large-corpus":
			return !status.Corpus.HasLarge, nil
		case "benchmarked":
			return benchmarked, nil
		case "missing-benchmark", "needs-benchmark":
			return !benchmarked, nil
		case "ready-to-benchmark", "corpus-missing-benchmark", "corpus-no-benchmark":
			return status.Corpus.Present && !benchmarked, nil
		case "corpus-unmeasured", "unattempted-corpus", "corpus-first-benchmark":
			return status.Corpus.Present &&
				(!status.Benchmark.Present || status.Benchmark.Readiness == readinessUnmeasured), nil
		case "l4-corpus-unmeasured", "large-corpus-unmeasured":
			return status.Corpus.Present &&
				status.Corpus.HasLarge &&
				(!status.Benchmark.Present || status.Benchmark.Readiness == readinessUnmeasured), nil
		case "instrumented":
			return status.Benchmark.Instrumented, nil
		case "profiled":
			return status.Benchmark.Profiled, nil
		case "unmeasured":
			return !status.Benchmark.Present || status.Benchmark.Readiness == readinessUnmeasured, nil
		case "needs-work":
			return status.Benchmark.Present &&
				status.Benchmark.Readiness != readinessUnmeasured &&
				status.Benchmark.Readiness != "meets-current-targets", nil
		case "parity-blocked", "blocked-by-parity":
			return status.Benchmark.Readiness == "parity-blocked", nil
		case "meets-current-targets":
			return status.Benchmark.Readiness == "meets-current-targets", nil
		default:
			return false, fmt.Errorf("unknown selector %q", selector)
		}
	}

	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		ok, err := match(status)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, status.Language)
		}
	}
	sort.Strings(out)
	return out, nil
}

func writeLanguageList(path string, languages []string) error {
	data := []byte(strings.Join(languages, "\n"))
	if len(data) > 0 {
		data = append(data, '\n')
	}
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeJSON(path string, inv inventory) error {
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeMarkdown(path string, inv inventory) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Real Corpus Inventory\n\n")
	fmt.Fprintf(&b, "Generated: `%s`\n\n", inv.GeneratedAt)
	fmt.Fprintf(&b, "| Metric | Count |\n|---|---:|\n")
	fmt.Fprintf(&b, "| Languages | %d |\n", inv.Summary.TotalLanguages)
	fmt.Fprintf(&b, "| With lock metadata | %d |\n", inv.Summary.WithLockMetadata)
	fmt.Fprintf(&b, "| With corpus | %d |\n", inv.Summary.WithCorpus)
	fmt.Fprintf(&b, "| With L3 medium corpus | %d |\n", inv.Summary.WithL3MediumCorpus)
	fmt.Fprintf(&b, "| With L4 large corpus | %d |\n", inv.Summary.WithL4LargeCorpus)
	fmt.Fprintf(&b, "| With corpus source checkout | %d |\n", inv.Summary.WithCorpusSource)
	fmt.Fprintf(&b, "| With checked-out corpus source | %d |\n", inv.Summary.WithCheckedOutCorpusSource)
	fmt.Fprintf(&b, "| With pinned corpus source | %d |\n", inv.Summary.WithPinnedCorpusSource)
	fmt.Fprintf(&b, "| With corpus source files | %d |\n", inv.Summary.WithCorpusSourceFiles)
	fmt.Fprintf(&b, "| Checked-out corpus sources with no matching files | %d |\n", inv.Summary.MissingCorpusSourceFiles)
	fmt.Fprintf(&b, "| Missing corpus source checkout | %d |\n", inv.Summary.MissingCorpusSource)
	fmt.Fprintf(&b, "| Benchmarked | %d |\n", inv.Summary.Benchmarked)
	fmt.Fprintf(&b, "| Instrumented | %d |\n", inv.Summary.Instrumented)
	fmt.Fprintf(&b, "| Profiled | %d |\n", inv.Summary.Profiled)
	fmt.Fprintf(&b, "| Meets current targets | %d |\n\n", inv.Summary.MeetsCurrentTargets)
	if len(inv.Summary.ReadinessCounts) > 0 {
		fmt.Fprintf(&b, "## Readiness Counts\n\n")
		fmt.Fprintf(&b, "| Readiness | Count |\n|---|---:|\n")
		readinesses := make([]string, 0, len(inv.Summary.ReadinessCounts))
		for readiness := range inv.Summary.ReadinessCounts {
			readinesses = append(readinesses, readiness)
		}
		sort.Strings(readinesses)
		for _, readiness := range readinesses {
			fmt.Fprintf(&b, "| %s | %d |\n", readiness, inv.Summary.ReadinessCounts[readiness])
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "| Language | Lock | Corpus buckets | Corpus source | Source files | Source bytes | Bench readiness | Instrumented | Profiled |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---:|---:|---|---|---|\n")
	for _, lang := range inv.Languages {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d | %s | %s | %s |\n",
			lang.Language,
			formatBool(lang.Lock.Present),
			formatBuckets(lang.Corpus),
			formatSource(lang.Corpus.Source),
			lang.Corpus.Source.MatchingFiles,
			lang.Corpus.Source.MatchingBytes,
			lang.Benchmark.Readiness,
			formatBool(lang.Benchmark.Instrumented),
			formatBool(lang.Benchmark.Profiled),
		)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func formatBool(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func formatBuckets(status corpusStatus) string {
	if !status.Present {
		return ""
	}
	return strings.Join(status.Buckets, "/")
}

func formatSource(status sourceStatus) string {
	switch {
	case status.Pinned:
		return "pinned"
	case status.CheckedOut:
		return "checked-out"
	case status.ExpectedPath != "":
		return "missing"
	default:
		return ""
	}
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func resolvePath(explicit string, candidates []string) (string, error) {
	path, err := resolveOptionalPath(explicit, candidates)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("path not found")
	}
	return path, nil
}

func resolveOptionalPath(explicit string, candidates []string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

func resolveOptionalPaths(explicit string, candidates []string) ([]string, error) {
	if strings.TrimSpace(explicit) != "" {
		paths := parsePathList(explicit)
		if len(paths) == 0 {
			return nil, nil
		}
		out := make([]string, 0, len(paths))
		seen := map[string]bool{}
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				return nil, err
			}
			if !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
		}
		return out, nil
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return []string{candidate}, nil
		}
	}
	return nil, nil
}

func resolveCorpusSourceRoot(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return filepath.Clean(explicit)
	}
	for _, candidate := range []string{
		filepath.Join("..", "gotreesitter-corpora", "corpus_sources"),
		filepath.Join("..", "..", "gotreesitter-corpora", "corpus_sources"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate)
		}
	}
	return filepath.Clean(filepath.Join("..", "gotreesitter-corpora", "corpus_sources"))
}

func parsePathList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "real_corpus_inventory: "+format+"\n", args...)
	os.Exit(1)
}
