//go:build cgo && treesitter_c_parity

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	gotreesitter "github.com/agentable/gotreesitter"
	cgoharness "github.com/agentable/gotreesitter/cgo_harness"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

var topParityLanguages = []string{
	"go",
	"javascript",
	"typescript",
	"python",
	"rust",
	"c",
	"cpp",
	"java",
	"json",
	"yaml",
	"html",
	"css",
	"markdown",
}

type parityResult struct {
	Language       string                       `json:"language"`
	FileID         string                       `json:"file_id"`
	FilePath       string                       `json:"file_path"`
	SourceSHA256   string                       `json:"source_sha256"`
	DumpVersion    string                       `json:"dump_version"`
	GoRootType     string                       `json:"go_root_type,omitempty"`
	GoRootHasError bool                         `json:"go_root_has_error,omitempty"`
	CRootType      string                       `json:"c_root_type,omitempty"`
	CRootHasError  bool                         `json:"c_root_has_error,omitempty"`
	Pass           bool                         `json:"pass"`
	Category       string                       `json:"category,omitempty"`
	FirstMismatch  *cgoharness.DumpV1Divergence `json:"first_divergence,omitempty"`
	GoDumpPath     string                       `json:"go_dump_path,omitempty"`
	CDumpPath      string                       `json:"c_dump_path,omitempty"`
	Error          string                       `json:"error,omitempty"`
}

type languageRunner struct {
	name                string
	entry               grammars.LangEntry
	support             grammars.ParseSupport
	goLang              *gotreesitter.Language
	goParser            *gotreesitter.Parser
	cParser             *sitter.Parser
	oracleTimeoutMicros uint64
}

type score struct {
	total int
	pass  int
}

func main() {
	var (
		langFlag                string
		corpusFlag              string
		outFlag                 string
		artifactFlag            string
		artifactMode            string
		scoreboardMD            string
		oracleFlag              string
		oracleTimeoutMS         int
		gcAfterFile             bool
		skipGoOnOracleRootError bool
		failOnMismatch          bool
		workers                 int
	)

	flag.StringVar(&langFlag, "lang", "top10", "language name, comma list, or top10")
	flag.StringVar(&corpusFlag, "corpus", "", "corpus root directory")
	flag.StringVar(&outFlag, "out", "parity_out/results.jsonl", "JSONL output path")
	flag.StringVar(&artifactFlag, "artifact-dir", "parity_out/dump_v1", "directory for dump.v1 artifacts; empty disables dump emission")
	flag.StringVar(&artifactMode, "artifact-mode", "all", "artifact emission mode: all|failures")
	flag.StringVar(&scoreboardMD, "scoreboard", "PARITY.md", "scoreboard markdown output path; empty disables scoreboard emission")
	flag.StringVar(&oracleFlag, "oracle", "c", "oracle runtime (only 'c' is supported)")
	flag.IntVar(&oracleTimeoutMS, "oracle-timeout-ms", 0, "if >0, set a timeout for pinned C oracle parses and emit oracle_timeout rows when they abort")
	flag.BoolVar(&gcAfterFile, "gc-after-file", false, "when true, force GC and return free heap pages to the OS after each file; intended for bounded corpus sweeps")
	flag.BoolVar(&skipGoOnOracleRootError, "skip-go-on-oracle-error", false, "when true, emit a result row and skip gotreesitter parsing if the C oracle root already has parse errors")
	flag.BoolVar(&failOnMismatch, "fail-on-mismatch", false, "when true, exit non-zero after writing outputs if any result row has pass=false")
	flag.IntVar(&workers, "workers", 1, "parallel parity workers per language; each worker owns separate Go and C parsers")
	flag.Parse()

	if corpusFlag == "" {
		fatalf("--corpus is required")
	}
	if oracleFlag != "c" {
		fatalf("--oracle=%s is not supported; use --oracle c", oracleFlag)
	}
	if oracleTimeoutMS < 0 {
		fatalf("--oracle-timeout-ms must be >= 0")
	}
	if workers < 1 {
		fatalf("--workers must be >= 1")
	}
	var err error
	artifactMode, err = normalizeArtifactMode(artifactMode)
	if err != nil {
		fatalf("%v", err)
	}

	langs := parseLangs(langFlag)
	if len(langs) == 0 {
		fatalf("no languages selected")
	}

	entriesByName := make(map[string]grammars.LangEntry)
	for _, entry := range grammars.AllLanguages() {
		entriesByName[entry.Name] = entry
	}
	supportByName := make(map[string]grammars.ParseSupport, len(langs))
	for _, name := range langs {
		if report, ok := grammars.ParseSupportFor(name); ok {
			supportByName[report.Name] = report
		}
	}

	if err := os.MkdirAll(filepath.Dir(outFlag), 0o755); err != nil {
		fatalf("create out dir: %v", err)
	}
	if strings.TrimSpace(artifactFlag) != "" {
		if err := os.MkdirAll(artifactFlag, 0o755); err != nil {
			fatalf("create artifact dir: %v", err)
		}
	}

	outFile, err := os.Create(outFlag)
	if err != nil {
		fatalf("create %s: %v", outFlag, err)
	}
	defer outFile.Close()
	writer := bufio.NewWriter(outFile)
	defer writer.Flush()

	enc := json.NewEncoder(writer)
	enc.SetEscapeHTML(false)

	scores := make(map[string]score, len(langs))
	seenFiles := 0
	failedFiles := 0

	for _, lang := range langs {
		files, root, err := collectFilesForLanguage(corpusFlag, lang, len(langs) == 1)
		if err != nil {
			fatalf("%s corpus: %v", lang, err)
		}
		if len(files) == 0 {
			fmt.Fprintf(os.Stderr, "[%s] no files under %s\n", lang, root)
			continue
		}

		langArtifacts := ""
		if strings.TrimSpace(artifactFlag) != "" {
			langArtifacts = filepath.Join(artifactFlag, lang)
			if err := os.MkdirAll(langArtifacts, 0o755); err != nil {
				fatalf("create artifact lang dir %s: %v", langArtifacts, err)
			}
		}

		results, err := runLanguageParity(
			lang,
			files,
			root,
			langArtifacts,
			artifactMode,
			skipGoOnOracleRootError,
			entriesByName,
			supportByName,
			oracleTimeoutMS,
			workers,
			gcAfterFile,
		)
		if err != nil {
			fatalf("%s parity: %v", lang, err)
		}
		for _, result := range results {
			if err := enc.Encode(result); err != nil {
				fatalf("write jsonl for %s: %v", result.FilePath, err)
			}
			updateScore(scores, lang, result.Pass)
			seenFiles++
			if !result.Pass {
				failedFiles++
			}
		}
	}

	if err := writer.Flush(); err != nil {
		fatalf("flush %s: %v", outFlag, err)
	}
	if strings.TrimSpace(scoreboardMD) != "" {
		if err := writeScoreboard(scoreboardMD, scores); err != nil {
			fatalf("write scoreboard: %v", err)
		}
	}

	fmt.Printf("wrote %d results to %s\n", seenFiles, outFlag)
	if strings.TrimSpace(scoreboardMD) != "" {
		fmt.Printf("updated scoreboard: %s\n", scoreboardMD)
	}
	if code := mismatchGateExitCode(failOnMismatch, failedFiles); code != 0 {
		fmt.Fprintf(os.Stderr, "parity mismatches: %d/%d result rows failed\n", failedFiles, seenFiles)
		os.Exit(code)
	}
}

func mismatchGateExitCode(enabled bool, failedRows int) int {
	if enabled && failedRows > 0 {
		return 2
	}
	return 0
}

type fileParityTask struct {
	index int
	abs   string
	rel   string
}

type fileParityOutput struct {
	index  int
	result parityResult
}

func runLanguageParity(
	lang string,
	files []string,
	root string,
	artifactDir string,
	artifactMode string,
	skipGoOnOracleRootError bool,
	entriesByName map[string]grammars.LangEntry,
	supportByName map[string]grammars.ParseSupport,
	oracleTimeoutMS int,
	workers int,
	gcAfterFile bool,
) ([]parityResult, error) {
	workerCount := effectiveCorpusParityWorkers(workers, len(files))
	if workerCount == 0 {
		return nil, nil
	}
	runners := make([]*languageRunner, 0, workerCount)
	for i := 0; i < workerCount; i++ {
		runner, err := buildRunner(lang, entriesByName, supportByName, oracleTimeoutMS)
		if err != nil {
			for _, existing := range runners {
				existing.Close()
			}
			return nil, fmt.Errorf("init worker %d runner: %w", i, err)
		}
		runners = append(runners, runner)
	}
	defer func() {
		for _, runner := range runners {
			runner.Close()
		}
	}()

	tasks := make(chan fileParityTask)
	outputs := make(chan fileParityOutput, len(files))
	var wg sync.WaitGroup
	for _, runner := range runners {
		wg.Add(1)
		go func(runner *languageRunner) {
			defer wg.Done()
			for task := range tasks {
				result := runPathParity(runner, artifactDir, artifactMode, skipGoOnOracleRootError, task.abs, task.rel)
				outputs <- fileParityOutput{index: task.index, result: result}
				if gcAfterFile {
					runtime.GC()
					debug.FreeOSMemory()
				}
			}
		}(runner)
	}
	go func() {
		for i, abs := range files {
			rel, err := filepath.Rel(root, abs)
			if err != nil {
				rel = filepath.Base(abs)
			}
			tasks <- fileParityTask{index: i, abs: abs, rel: rel}
		}
		close(tasks)
		wg.Wait()
		close(outputs)
	}()

	results := make([]parityResult, len(files))
	for output := range outputs {
		results[output.index] = output.result
	}
	return results, nil
}

func effectiveCorpusParityWorkers(requested, fileCount int) int {
	if fileCount <= 0 {
		return 0
	}
	if requested < 1 {
		return 1
	}
	if requested > fileCount {
		return fileCount
	}
	return requested
}

func parseLangs(raw string) []string {
	value := strings.TrimSpace(raw)
	switch value {
	case "", "top10", "top":
		out := make([]string, len(topParityLanguages))
		copy(out, topParityLanguages)
		return out
	case "top50":
		if langs, err := loadLangsFromListFile([]string{
			filepath.Join("..", "grammars", "update_tier1_top50.txt"),
			filepath.Join("grammars", "update_tier1_top50.txt"),
		}); err == nil && len(langs) > 0 {
			return langs
		}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func loadLangsFromListFile(candidates []string) ([]string, error) {
	var path string
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		st, err := os.Stat(candidate)
		if err == nil && !st.IsDir() {
			path = candidate
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("list file not found")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out, nil
}

func buildRunner(lang string, entries map[string]grammars.LangEntry, support map[string]grammars.ParseSupport, oracleTimeoutMS int) (*languageRunner, error) {
	entry, ok := entries[lang]
	if !ok {
		return nil, fmt.Errorf("language %q is not in grammars registry", lang)
	}
	report, ok := support[lang]
	if !ok {
		return nil, fmt.Errorf("language %q has no parse support report", lang)
	}
	if report.Backend == grammars.ParseBackendUnsupported {
		return nil, fmt.Errorf("language %q parse backend is unsupported", lang)
	}
	goLang := entry.Language()
	goParser := gotreesitter.NewParser(goLang)
	cLang, err := cgoharness.ParityCLanguage(lang)
	if err != nil {
		return nil, fmt.Errorf("load C oracle language: %w", err)
	}
	cParser := sitter.NewParser()
	if err := cParser.SetLanguage(cLang); err != nil {
		cParser.Close()
		return nil, fmt.Errorf("set C language: %w", err)
	}
	var oracleTimeoutMicros uint64
	if oracleTimeoutMS > 0 {
		oracleTimeoutMicros = uint64(oracleTimeoutMS) * 1000
		cParser.SetTimeoutMicros(oracleTimeoutMicros)
	}
	return &languageRunner{
		name:                lang,
		entry:               entry,
		support:             report,
		goLang:              goLang,
		goParser:            goParser,
		cParser:             cParser,
		oracleTimeoutMicros: oracleTimeoutMicros,
	}, nil
}

func (r *languageRunner) Close() {
	if r == nil || r.cParser == nil {
		return
	}
	r.cParser.Close()
}

func collectFilesForLanguage(corpusRoot, lang string, allowRawRoot bool) ([]string, string, error) {
	root := filepath.Join(corpusRoot, lang)
	if info, err := os.Stat(root); err == nil && info.IsDir() {
		return collectFiles(root)
	}
	if allowRawRoot {
		return collectFiles(corpusRoot)
	}
	return nil, "", fmt.Errorf("missing language directory %s", root)
}

func collectFiles(root string) ([]string, string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return []string{root}, filepath.Dir(root), nil
	}

	files := make([]string, 0, 128)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Strings(files)
	return files, root, nil
}

func runPathParity(runner *languageRunner, artifactDir, artifactMode string, skipGoOnOracleRootError bool, absPath, fileID string) parityResult {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return parityResult{
			Language:    runner.name,
			FileID:      fileID,
			FilePath:    absPath,
			DumpVersion: cgoharness.DumpV1Version,
			Pass:        false,
			Category:    "io",
			Error:       err.Error(),
		}
	}
	return runFileParity(runner, artifactDir, artifactMode, skipGoOnOracleRootError, absPath, fileID, src)
}

func runFileParity(runner *languageRunner, artifactDir, artifactMode string, skipGoOnOracleRootError bool, absPath, fileID string, src []byte) parityResult {
	hash := sha256.Sum256(src)
	sourceHash := hex.EncodeToString(hash[:])

	res := parityResult{
		Language:     runner.name,
		FileID:       fileID,
		FilePath:     absPath,
		SourceSHA256: sourceHash,
		DumpVersion:  cgoharness.DumpV1Version,
	}

	cTree := runner.cParser.Parse(src, nil)
	if cTree == nil || cTree.RootNode() == nil {
		res.Pass = false
		res.Category, res.Error = oracleParseFailure(runner.oracleTimeoutMicros)
		return res
	}
	defer cTree.Close()
	cRoot := cTree.RootNode()
	res.CRootType = cRoot.Kind()
	res.CRootHasError = cRoot.HasError()
	if skipGoOnOracleRootError && res.CRootHasError {
		res.Pass = false
		res.Category = "oracle_error"
		res.Error = "skipped gotreesitter parse because C oracle root has errors"
		return res
	}

	goTree, goLang, err := parseWithGoRunner(runner, src)
	if err != nil {
		res.Pass = false
		res.Category = "go_parse"
		res.Error = err.Error()
		return res
	}
	if goTree == nil || goTree.RootNode() == nil {
		res.Pass = false
		res.Category = "go_parse"
		res.Error = "gotreesitter returned nil tree"
		return res
	}
	defer goTree.Release()
	goRoot := goTree.RootNode()
	res.GoRootType = goRoot.Type(goLang)
	res.GoRootHasError = goRoot.HasError()

	diff := cgoharness.FirstDivergenceDumpV1(goRoot, goLang, cRoot)
	if diff != nil {
		res.Pass = false
		res.Category = diff.Category
		res.FirstMismatch = diff
	} else {
		res.Pass = true
	}

	if shouldEmitArtifacts(artifactDir, artifactMode, res.Pass) {
		goDump := cgoharness.DumpV1FromGo(goRoot, goLang)
		cDump := cgoharness.DumpV1FromC(cRoot)
		safeID := safeArtifactID(fileID)
		goDumpPath := filepath.Join(artifactDir, safeID+".go.dump.v1.json")
		cDumpPath := filepath.Join(artifactDir, safeID+".c.dump.v1.json")

		if err := writeJSON(goDumpPath, goDump); err != nil {
			res.Pass = false
			res.Category = "artifact"
			res.Error = fmt.Sprintf("write %s: %v", goDumpPath, err)
			return res
		}
		if err := writeJSON(cDumpPath, cDump); err != nil {
			res.Pass = false
			res.Category = "artifact"
			res.Error = fmt.Sprintf("write %s: %v", cDumpPath, err)
			return res
		}
		res.GoDumpPath = goDumpPath
		res.CDumpPath = cDumpPath
	}
	return res
}

func oracleParseFailure(timeoutMicros uint64) (string, string) {
	if timeoutMicros > 0 {
		return "oracle_timeout", fmt.Sprintf("C oracle parse aborted after %dms timeout", timeoutMicros/1000)
	}
	return "c_parse", "C oracle returned nil tree"
}

func normalizeArtifactMode(raw string) (string, error) {
	switch mode := strings.ToLower(strings.TrimSpace(raw)); mode {
	case "", "all":
		return "all", nil
	case "failures":
		return "failures", nil
	default:
		return "", fmt.Errorf("invalid --artifact-mode %q (want all|failures)", raw)
	}
}

func shouldEmitArtifacts(artifactDir, artifactMode string, pass bool) bool {
	if strings.TrimSpace(artifactDir) == "" {
		return false
	}
	switch artifactMode {
	case "failures":
		return !pass
	case "all":
		return true
	default:
		return false
	}
}

func parseWithGoRunner(runner *languageRunner, src []byte) (*gotreesitter.Tree, *gotreesitter.Language, error) {
	switch runner.support.Backend {
	case grammars.ParseBackendTokenSource:
		if runner.entry.TokenSourceFactory == nil {
			return nil, nil, fmt.Errorf("token_source backend without TokenSourceFactory")
		}
		tree, err := runner.goParser.ParseWithTokenSource(src, runner.entry.TokenSourceFactory(src, runner.goLang))
		return tree, runner.goLang, err
	case grammars.ParseBackendDFA, grammars.ParseBackendDFAPartial:
		tree, err := runner.goParser.Parse(src)
		return tree, runner.goLang, err
	default:
		return nil, nil, fmt.Errorf("unsupported backend %q", runner.support.Backend)
	}
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func safeArtifactID(fileID string) string {
	s := strings.ReplaceAll(fileID, string(os.PathSeparator), "__")
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ":", "_")
	return s
}

func updateScore(scores map[string]score, lang string, pass bool) {
	s := scores[lang]
	s.total++
	if pass {
		s.pass++
	}
	scores[lang] = s
}

func writeScoreboard(path string, scores map[string]score) error {
	langs := make([]string, 0, len(scores))
	for lang := range scores {
		langs = append(langs, lang)
	}
	sort.Strings(langs)

	totalPass := 0
	total := 0
	var b strings.Builder
	b.WriteString("# PARITY\n\n")
	b.WriteString(fmt.Sprintf("_Generated: %s_\n\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString("| Language | Fresh Parse Parity |\n")
	b.WriteString("| --- | --- |\n")
	for _, lang := range langs {
		s := scores[lang]
		totalPass += s.pass
		total += s.total
		b.WriteString(fmt.Sprintf("| %s | %d/%d |\n", lang, s.pass, s.total))
	}
	b.WriteString(fmt.Sprintf("| **TOTAL** | **%d/%d** |\n", totalPass, total))
	b.WriteString("\n")
	b.WriteString("- Dump artifact: `dump.v1`\n")
	b.WriteString("- Incremental parity scoreboard: pending integration in this command\n")
	b.WriteString("- Query parity scoreboard: pending integration in this command\n")

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
