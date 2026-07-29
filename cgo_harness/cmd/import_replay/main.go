package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/smacker/go-tree-sitter"
	sittergo "github.com/smacker/go-tree-sitter/golang"
	sitterjava "github.com/smacker/go-tree-sitter/java"
	sitterpython "github.com/smacker/go-tree-sitter/python"
)

const (
	modeCGOTreeParse          = "cgo_tree_parse"
	modeCGOTreeExtract        = "cgo_tree_extract"
	modeGoTreeExtract         = "go_tree_extract"
	modeGoSourceExtractHybrid = "go_source_extract_hybrid"
)

type replayFile struct {
	Path     string `json:"path"`
	RelPath  string `json:"rel_path"`
	Language string `json:"language"`
	Bytes    int    `json:"bytes"`
	Source   []byte `json:"-"`
}

type replayReport struct {
	Root         string                 `json:"root"`
	Files        int                    `json:"files"`
	Bytes        int64                  `json:"bytes"`
	Modes        map[string]*modeReport `json:"modes"`
	OutputDiff   *outputDiffReport      `json:"output_diff,omitempty"`
	OutputDiffs  []*outputDiffReport    `json:"output_diffs,omitempty"`
	Unsupported  map[string]int         `json:"unsupported,omitempty"`
	Errors       []string               `json:"errors,omitempty"`
	Options      map[string]string      `json:"options,omitempty"`
	StartedAt    string                 `json:"started_at"`
	FinishedAt   string                 `json:"finished_at"`
	ElapsedNanos int64                  `json:"elapsed_nanos"`
}

type modeReport struct {
	Files                  int                       `json:"files"`
	Bytes                  int64                     `json:"bytes"`
	Imports                int                       `json:"imports"`
	Fallbacks              int                       `json:"fallbacks"`
	SourceStatuses         map[string]int            `json:"source_statuses,omitempty"`
	FallbackReasons        map[string]int            `json:"fallback_reasons,omitempty"`
	WallNanos              int64                     `json:"wall_nanos"`
	ParseNanos             int64                     `json:"parse_nanos"`
	ExtractNanos           int64                     `json:"extract_nanos"`
	FallbackTreeParseNanos int64                     `json:"fallback_tree_parse_nanos"`
	Languages              map[string]*languageStats `json:"languages"`
	FilesWithErrors        int                       `json:"files_with_errors"`
	Errors                 []string                  `json:"errors,omitempty"`
}

type languageStats struct {
	Files                  int            `json:"files"`
	Bytes                  int64          `json:"bytes"`
	Imports                int            `json:"imports"`
	Fallbacks              int            `json:"fallbacks"`
	SourceStatuses         map[string]int `json:"source_statuses,omitempty"`
	FallbackReasons        map[string]int `json:"fallback_reasons,omitempty"`
	WallNanos              int64          `json:"wall_nanos"`
	ParseNanos             int64          `json:"parse_nanos"`
	ExtractNanos           int64          `json:"extract_nanos"`
	FallbackTreeParseNanos int64          `json:"fallback_tree_parse_nanos"`
	FilesWithErrors        int            `json:"files_with_errors"`
}

type outputDiffReport struct {
	BaseMode     string   `json:"base_mode"`
	CompareMode  string   `json:"compare_mode"`
	Equal        bool     `json:"equal"`
	MissingLines int      `json:"missing_lines"`
	ExtraLines   int      `json:"extra_lines"`
	Sample       []string `json:"sample,omitempty"`
}

type modeRun struct {
	report *modeReport
	output map[string][]gotreesitter.ImportRef
}

func main() {
	var (
		root       = flag.String("root", ".", "repository root to scan")
		outPath    = flag.String("out", "", "optional JSON report path")
		outputDir  = flag.String("output-dir", "", "optional directory for normalized dependency outputs and diff")
		modesFlag  = flag.String("modes", strings.Join([]string{modeCGOTreeParse, modeCGOTreeExtract, modeGoTreeExtract, modeGoSourceExtractHybrid}, ","), "comma-separated replay modes")
		langsFlag  = flag.String("langs", strings.Join(supportedReplayLanguages(), ","), "comma-separated languages to scan")
		maxFiles   = flag.Int("max-files", 0, "optional maximum files to scan")
		maxBytes   = flag.Int64("max-bytes", 0, "optional maximum total bytes to scan")
		sampleDiff = flag.Int("sample-diff", 80, "maximum normalized diff sample lines")
	)
	flag.Parse()

	started := time.Now()
	langs := parseLanguages(*langsFlag)
	files, unsupported, err := collectReplayFiles(*root, langs, *maxFiles, *maxBytes)
	if err != nil {
		fatalf("%v", err)
	}
	modes := parseModes(*modesFlag)
	report := replayReport{
		Root:        *root,
		Files:       len(files),
		Bytes:       totalReplayBytes(files),
		Modes:       make(map[string]*modeReport, len(modes)),
		Unsupported: unsupported,
		Options: map[string]string{
			"modes":       *modesFlag,
			"langs":       strings.Join(sortedLanguageNames(langs), ","),
			"max_files":   fmt.Sprint(*maxFiles),
			"max_bytes":   fmt.Sprint(*maxBytes),
			"output_dir":  *outputDir,
			"sample_diff": fmt.Sprint(*sampleDiff),
		},
		StartedAt: started.UTC().Format(time.RFC3339Nano),
	}

	runs := make(map[string]modeRun, len(modes))
	for _, mode := range modes {
		run := runMode(mode, files)
		report.Modes[mode] = run.report
		runs[mode] = run
	}

	if compare, ok := runs[modeGoSourceExtractHybrid]; ok {
		for _, baseMode := range []string{modeCGOTreeExtract, modeGoTreeExtract} {
			base, ok := runs[baseMode]
			if !ok {
				continue
			}
			diff := compareOutputs(baseMode, base.output, modeGoSourceExtractHybrid, compare.output, *sampleDiff)
			report.OutputDiffs = append(report.OutputDiffs, diff)
			if baseMode == modeGoTreeExtract {
				report.OutputDiff = diff
			}
		}
	}
	if *outputDir != "" {
		if err := writeReplayOutputs(*outputDir, runs, report.OutputDiffs); err != nil {
			report.Errors = append(report.Errors, err.Error())
		}
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	report.ElapsedNanos = time.Since(started).Nanoseconds()

	printSummary(report)
	if *outPath != "" {
		if err := writeJSON(*outPath, report); err != nil {
			fatalf("write report: %v", err)
		}
	}
}

func collectReplayFiles(root string, langs map[string]bool, maxFiles int, maxBytes int64) ([]replayFile, map[string]int, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	var files []replayFile
	unsupported := make(map[string]int)
	var selectedBytes int64
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != absRoot {
			switch d.Name() {
			case ".git", ".hg", ".svn", ".gradle", "bazel-bin", "bazel-out", "bazel-testlogs", "node_modules", "target", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if maxFiles > 0 && len(files) >= maxFiles {
			return nil
		}
		entry := grammars.DetectLanguage(path)
		if entry == nil {
			return nil
		}
		lang := entry.Name
		switch lang {
		case "go", "java", "python", "starlark":
		default:
			unsupported[lang]++
			return nil
		}
		if !langs[lang] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if maxBytes > 0 && selectedBytes+int64(len(src)) > maxBytes {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			rel = path
		}
		files = append(files, replayFile{
			Path:     path,
			RelPath:  filepath.ToSlash(rel),
			Language: lang,
			Bytes:    len(src),
			Source:   src,
		})
		selectedBytes += int64(len(src))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, unsupported, nil
}

func parseModes(raw string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		mode := strings.TrimSpace(part)
		if mode == "" {
			continue
		}
		switch mode {
		case modeCGOTreeParse, modeCGOTreeExtract, modeGoTreeExtract, modeGoSourceExtractHybrid:
		default:
			fatalf("unknown mode %q", mode)
		}
		if _, ok := seen[mode]; ok {
			continue
		}
		seen[mode] = struct{}{}
		out = append(out, mode)
	}
	if len(out) == 0 {
		fatalf("no modes selected")
	}
	return out
}

func supportedReplayLanguages() []string {
	return []string{"go", "java", "python", "starlark"}
}

func parseLanguages(raw string) map[string]bool {
	out := make(map[string]bool)
	supported := make(map[string]bool)
	for _, lang := range supportedReplayLanguages() {
		supported[lang] = true
	}
	for _, part := range strings.Split(raw, ",") {
		lang := strings.TrimSpace(part)
		if lang == "" {
			continue
		}
		if !supported[lang] {
			fatalf("unknown language %q", lang)
		}
		out[lang] = true
	}
	if len(out) == 0 {
		fatalf("no languages selected")
	}
	return out
}

func sortedLanguageNames(langs map[string]bool) []string {
	out := make([]string, 0, len(langs))
	for lang := range langs {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

func runMode(mode string, files []replayFile) modeRun {
	mr := &modeReport{
		SourceStatuses:  make(map[string]int),
		FallbackReasons: make(map[string]int),
		Languages:       make(map[string]*languageStats),
	}
	output := make(map[string][]gotreesitter.ImportRef)
	start := time.Now()
	for _, file := range files {
		fileStart := time.Now()
		ls := mr.language(file.Language)
		mr.Files++
		mr.Bytes += int64(file.Bytes)
		ls.Files++
		ls.Bytes += int64(file.Bytes)
		switch mode {
		case modeCGOTreeParse:
			parseStart := time.Now()
			err := parseWithCGo(file)
			parseNanos := time.Since(parseStart).Nanoseconds()
			mr.ParseNanos += parseNanos
			ls.ParseNanos += parseNanos
			if err != nil {
				recordModeError(mr, ls, file, err)
			}
		case modeCGOTreeExtract:
			result := parseAndExtractWithCGo(file)
			mr.ParseNanos += result.parseNanos
			ls.ParseNanos += result.parseNanos
			mr.ExtractNanos += result.extractNanos
			ls.ExtractNanos += result.extractNanos
			if result.err != nil {
				recordModeError(mr, ls, file, result.err)
				break
			}
			mr.Imports += len(result.refs)
			ls.Imports += len(result.refs)
			output[file.RelPath] = result.refs
		case modeGoTreeExtract:
			parseStart := time.Now()
			refs, err := parseWithGoTree(file)
			parseNanos := time.Since(parseStart).Nanoseconds()
			mr.ParseNanos += parseNanos
			ls.ParseNanos += parseNanos
			if err != nil {
				recordModeError(mr, ls, file, err)
				break
			}
			extractNanos := int64(0)
			mr.ExtractNanos += extractNanos
			ls.ExtractNanos += extractNanos
			mr.Imports += len(refs)
			ls.Imports += len(refs)
			output[file.RelPath] = refs
		case modeGoSourceExtractHybrid:
			extractStart := time.Now()
			report := gotreesitter.ExtractImportsFromSourceWithReport(languageForName(file.Language), file.Source)
			extractNanos := time.Since(extractStart).Nanoseconds()
			mr.ExtractNanos += extractNanos
			ls.ExtractNanos += extractNanos
			status := string(report.Status)
			if status == "" {
				status = "unknown"
			}
			mr.SourceStatuses[status]++
			if ls.SourceStatuses == nil {
				ls.SourceStatuses = make(map[string]int)
			}
			ls.SourceStatuses[status]++
			refs := report.Imports
			if report.FallbackRecommended {
				mr.Fallbacks++
				ls.Fallbacks++
				reason := report.Reason
				if reason == "" {
					reason = string(report.Status)
				}
				mr.FallbackReasons[reason]++
				if ls.FallbackReasons == nil {
					ls.FallbackReasons = make(map[string]int)
				}
				ls.FallbackReasons[reason]++
				parseStart := time.Now()
				fallbackRefs, err := parseWithGoTree(file)
				parseNanos := time.Since(parseStart).Nanoseconds()
				mr.FallbackTreeParseNanos += parseNanos
				ls.FallbackTreeParseNanos += parseNanos
				if err != nil {
					recordModeError(mr, ls, file, err)
					break
				}
				refs = fallbackRefs
			}
			mr.Imports += len(refs)
			ls.Imports += len(refs)
			output[file.RelPath] = refs
		}
		wallNanos := time.Since(fileStart).Nanoseconds()
		mr.WallNanos += wallNanos
		ls.WallNanos += wallNanos
	}
	mr.WallNanos = time.Since(start).Nanoseconds()
	if len(mr.SourceStatuses) == 0 {
		mr.SourceStatuses = nil
	}
	if len(mr.FallbackReasons) == 0 {
		mr.FallbackReasons = nil
	}
	for _, ls := range mr.Languages {
		if len(ls.SourceStatuses) == 0 {
			ls.SourceStatuses = nil
		}
		if len(ls.FallbackReasons) == 0 {
			ls.FallbackReasons = nil
		}
	}
	return modeRun{report: mr, output: output}
}

func (m *modeReport) language(name string) *languageStats {
	if ls := m.Languages[name]; ls != nil {
		return ls
	}
	ls := &languageStats{}
	m.Languages[name] = ls
	return ls
}

func recordModeError(mr *modeReport, ls *languageStats, file replayFile, err error) {
	mr.FilesWithErrors++
	ls.FilesWithErrors++
	if len(mr.Errors) < 20 {
		mr.Errors = append(mr.Errors, fmt.Sprintf("%s: %v", file.RelPath, err))
	}
}

type cgoExtractResult struct {
	refs         []gotreesitter.ImportRef
	parseNanos   int64
	extractNanos int64
	err          error
}

func parseWithGoTree(file replayFile) ([]gotreesitter.ImportRef, error) {
	lang := languageForName(file.Language)
	if lang == nil {
		return nil, fmt.Errorf("unsupported gotreesitter language %q", file.Language)
	}
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(file.Source)
	if err != nil {
		if tree != nil {
			tree.Release()
		}
		return nil, err
	}
	defer tree.Release()
	root := tree.RootNode()
	rt := tree.ParseRuntime()
	if root == nil || root.HasError() || tree.ParseStoppedEarly() || root.EndByte() != uint32(len(file.Source)) || rt.Truncated {
		return nil, fmt.Errorf("incomplete parse has_root=%v has_error=%v runtime=%s", root != nil, root != nil && root.HasError(), rt.Summary())
	}
	return gotreesitter.ExtractImports(tree), nil
}

func parseWithCGo(file replayFile) error {
	parser, err := newCGoParser(file.Language)
	if err != nil {
		return err
	}
	defer parser.Close()
	tree, err := parser.ParseCtx(context.Background(), nil, file.Source)
	if err != nil {
		return fmt.Errorf("cgo parse error: %w", err)
	}
	if tree == nil {
		return fmt.Errorf("cgo parse returned nil tree")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() || root.EndByte() != uint32(len(file.Source)) {
		return fmt.Errorf("cgo incomplete parse has_root=%v has_error=%v root_end=%d want=%d", root != nil, root != nil && root.HasError(), rootEndByte(root), len(file.Source))
	}
	return nil
}

func parseAndExtractWithCGo(file replayFile) cgoExtractResult {
	parser, err := newCGoParser(file.Language)
	if err != nil {
		return cgoExtractResult{err: err}
	}
	defer parser.Close()
	parseStart := time.Now()
	tree, err := parser.ParseCtx(context.Background(), nil, file.Source)
	parseNanos := time.Since(parseStart).Nanoseconds()
	if err != nil {
		return cgoExtractResult{parseNanos: parseNanos, err: fmt.Errorf("cgo parse error: %w", err)}
	}
	if tree == nil {
		return cgoExtractResult{parseNanos: parseNanos, err: fmt.Errorf("cgo parse returned nil tree")}
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() || root.EndByte() != uint32(len(file.Source)) {
		return cgoExtractResult{
			parseNanos: parseNanos,
			err:        fmt.Errorf("cgo incomplete parse has_root=%v has_error=%v root_end=%d want=%d", root != nil, root != nil && root.HasError(), rootEndByte(root), len(file.Source)),
		}
	}
	extractStart := time.Now()
	refs := extractCGoImports(root, file.Language, file.Source)
	extractNanos := time.Since(extractStart).Nanoseconds()
	return cgoExtractResult{refs: refs, parseNanos: parseNanos, extractNanos: extractNanos}
}

func newCGoParser(lang string) (*sitter.Parser, error) {
	parser := sitter.NewParser()
	switch lang {
	case "go":
		parser.SetLanguage(sittergo.GetLanguage())
	case "java":
		parser.SetLanguage(sitterjava.GetLanguage())
	case "python":
		parser.SetLanguage(sitterpython.GetLanguage())
	default:
		parser.Close()
		return nil, fmt.Errorf("cgo_tree_parse unsupported for %s", lang)
	}
	return parser, nil
}

func rootEndByte(root *sitter.Node) uint32 {
	if root == nil {
		return 0
	}
	return root.EndByte()
}

func extractCGoImports(root *sitter.Node, langName string, source []byte) []gotreesitter.ImportRef {
	lang := languageForName(langName)
	if root == nil || lang == nil {
		return nil
	}
	var refs []gotreesitter.ImportRef
	walkCGoImportTree(root, func(n *sitter.Node) bool {
		switch langName {
		case "go":
			return extractCGoGoImportNode(n, source, &refs)
		case "java":
			return extractCGoJavaImportNode(n, source, &refs)
		case "python":
			return extractCGoPythonImportNode(n, lang, source, &refs)
		default:
			return true
		}
	})
	return refs
}

func walkCGoImportTree(n *sitter.Node, visit func(*sitter.Node) bool) {
	if n == nil {
		return
	}
	if !visit(n) {
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkCGoImportTree(n.Child(i), visit)
	}
}

func extractCGoGoImportNode(n *sitter.Node, source []byte, refs *[]gotreesitter.ImportRef) bool {
	switch n.Type() {
	case "package_clause":
		if name := firstCGoDescendantText(n, source, "package_identifier", "identifier"); name != "" {
			*refs = append(*refs, gotreesitter.ImportRef{
				Lang:      "go",
				Kind:      "package",
				Name:      name,
				StartByte: n.StartByte(),
				EndByte:   n.EndByte(),
			})
		}
		return false
	case "import_declaration":
		specs := collectCGoDescendantsByType(n, "import_spec")
		if len(specs) == 0 {
			specs = []*sitter.Node{n}
		}
		for _, spec := range specs {
			pathNode := firstCGoDescendantByType(spec, "interpreted_string_literal", "raw_string_literal")
			if pathNode == nil {
				continue
			}
			path := importStringLiteralText(pathNode.Content(source))
			if path == "" {
				continue
			}
			*refs = append(*refs, gotreesitter.ImportRef{
				Lang:      "go",
				Kind:      "import",
				Path:      path,
				Name:      lastDottedName(path),
				Alias:     cgoGoImportAlias(spec, pathNode, source),
				StartByte: spec.StartByte(),
				EndByte:   spec.EndByte(),
			})
		}
		return false
	}
	return true
}

func cgoGoImportAlias(spec, pathNode *sitter.Node, source []byte) string {
	for i := 0; i < int(spec.ChildCount()); i++ {
		child := spec.Child(i)
		if child == nil || child.StartByte() >= pathNode.StartByte() {
			break
		}
		switch child.Type() {
		case "package_identifier", "identifier":
			return child.Content(source)
		}
		text := strings.TrimSpace(child.Content(source))
		if text == "." || text == "_" {
			return text
		}
	}
	return ""
}

func extractCGoJavaImportNode(n *sitter.Node, source []byte, refs *[]gotreesitter.ImportRef) bool {
	switch n.Type() {
	case "package_declaration":
		text := strings.TrimSpace(n.Content(source))
		text = strings.TrimPrefix(text, "package")
		text = strings.TrimSuffix(strings.TrimSpace(text), ";")
		if text != "" {
			*refs = append(*refs, gotreesitter.ImportRef{
				Lang:      "java",
				Kind:      "package",
				Path:      text,
				Name:      lastDottedName(text),
				StartByte: n.StartByte(),
				EndByte:   n.EndByte(),
			})
		}
		return false
	case "import_declaration":
		text := strings.TrimSpace(n.Content(source))
		text = strings.TrimPrefix(text, "import")
		text = strings.TrimSuffix(strings.TrimSpace(text), ";")
		ref := gotreesitter.ImportRef{
			Lang:      "java",
			Kind:      "import",
			StartByte: n.StartByte(),
			EndByte:   n.EndByte(),
		}
		if strings.HasPrefix(text, "static ") {
			ref.Static = true
			text = strings.TrimSpace(strings.TrimPrefix(text, "static"))
		}
		if strings.HasSuffix(text, ".*") {
			ref.Wildcard = true
			text = strings.TrimSuffix(text, ".*")
		}
		ref.Path = text
		ref.Name = lastDottedName(text)
		*refs = append(*refs, ref)
		return false
	}
	return true
}

func extractCGoPythonImportNode(n *sitter.Node, lang *gotreesitter.Language, source []byte, refs *[]gotreesitter.ImportRef) bool {
	switch n.Type() {
	case "import_statement", "import_from_statement", "future_import_statement":
		start := n.StartByte()
		result := gotreesitter.ExtractImportsFromSourceWithReport(lang, []byte(n.Content(source)))
		for _, ref := range result.Imports {
			ref.StartByte += start
			ref.EndByte += start
			*refs = append(*refs, ref)
		}
		return false
	}
	return true
}

func firstCGoDescendantText(n *sitter.Node, source []byte, types ...string) string {
	if child := firstCGoDescendantByType(n, types...); child != nil {
		return child.Content(source)
	}
	return ""
}

func firstCGoDescendantByType(n *sitter.Node, types ...string) *sitter.Node {
	if n == nil {
		return nil
	}
	typ := n.Type()
	for _, want := range types {
		if typ == want {
			return n
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if found := firstCGoDescendantByType(n.Child(i), types...); found != nil {
			return found
		}
	}
	return nil
}

func collectCGoDescendantsByType(n *sitter.Node, typ string) []*sitter.Node {
	var out []*sitter.Node
	var walk func(*sitter.Node)
	walk = func(cur *sitter.Node) {
		if cur == nil {
			return
		}
		if cur.Type() == typ {
			out = append(out, cur)
			return
		}
		for i := 0; i < int(cur.ChildCount()); i++ {
			walk(cur.Child(i))
		}
	}
	walk(n)
	return out
}

func importStringLiteralText(text string) string {
	value, ok := importStringLiteralValue(text)
	if ok {
		return value
	}
	return strings.Trim(strings.TrimSpace(text), "`\"'")
}

func importStringLiteralValue(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if unquoted, err := strconv.Unquote(text); err == nil {
		return unquoted, true
	}
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
		var b strings.Builder
		for s := text[1 : len(text)-1]; len(s) > 0; {
			r, _, tail, err := strconv.UnquoteChar(s, '\'')
			if err != nil {
				return "", false
			}
			b.WriteRune(r)
			s = tail
		}
		return b.String(), true
	}
	return "", false
}

func lastDottedName(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimSuffix(path, ".*")
	if path == "" {
		return ""
	}
	if idx := strings.LastIndex(path, "/"); idx >= 0 && idx+1 < len(path) {
		path = path[idx+1:]
	}
	if idx := strings.LastIndex(path, "."); idx >= 0 && idx+1 < len(path) {
		return path[idx+1:]
	}
	return path
}

func languageForName(name string) *gotreesitter.Language {
	switch name {
	case "go":
		return grammars.GoLanguage()
	case "java":
		return grammars.JavaLanguage()
	case "python":
		return grammars.PythonLanguage()
	case "starlark":
		return grammars.StarlarkLanguage()
	default:
		return nil
	}
}

func compareOutputs(baseMode string, base map[string][]gotreesitter.ImportRef, compareMode string, compare map[string][]gotreesitter.ImportRef, sampleLimit int) *outputDiffReport {
	baseLines := normalizedOutputLines(base)
	compareLines := normalizedOutputLines(compare)
	baseSet := make(map[string]struct{}, len(baseLines))
	compareSet := make(map[string]struct{}, len(compareLines))
	for _, line := range baseLines {
		baseSet[line] = struct{}{}
	}
	for _, line := range compareLines {
		compareSet[line] = struct{}{}
	}
	diff := &outputDiffReport{BaseMode: baseMode, CompareMode: compareMode}
	for _, line := range baseLines {
		if _, ok := compareSet[line]; !ok {
			diff.MissingLines++
			if len(diff.Sample) < sampleLimit {
				diff.Sample = append(diff.Sample, "- "+line)
			}
		}
	}
	for _, line := range compareLines {
		if _, ok := baseSet[line]; !ok {
			diff.ExtraLines++
			if len(diff.Sample) < sampleLimit {
				diff.Sample = append(diff.Sample, "+ "+line)
			}
		}
	}
	diff.Equal = diff.MissingLines == 0 && diff.ExtraLines == 0
	return diff
}

func normalizedOutputLines(output map[string][]gotreesitter.ImportRef) []string {
	files := make([]string, 0, len(output))
	for path := range output {
		files = append(files, path)
	}
	sort.Strings(files)
	lines := make([]string, 0)
	for _, path := range files {
		refs := append([]gotreesitter.ImportRef(nil), output[path]...)
		sort.SliceStable(refs, func(i, j int) bool {
			return importRefKey(refs[i]) < importRefKey(refs[j])
		})
		if len(refs) == 0 {
			lines = append(lines, fmt.Sprintf("file %q", path))
			continue
		}
		for _, ref := range refs {
			lines = append(lines, fmt.Sprintf("file %q import %s", path, importRefKey(ref)))
		}
	}
	return lines
}

func importRefKey(ref gotreesitter.ImportRef) string {
	return fmt.Sprintf("lang=%s kind=%s path=%s from=%s name=%s alias=%s static=%t wildcard=%t relative=%d",
		ref.Lang, ref.Kind, ref.Path, ref.From, ref.Name, ref.Alias, ref.Static, ref.Wildcard, ref.Relative)
}

func writeReplayOutputs(dir string, runs map[string]modeRun, diffs []*outputDiffReport) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for mode, run := range runs {
		if len(run.output) == 0 {
			continue
		}
		lines := normalizedOutputLines(run.output)
		var buf bytes.Buffer
		for _, line := range lines {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, mode+".BUILD.imports"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	for _, diff := range diffs {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "base=%s compare=%s equal=%t missing=%d extra=%d\n", diff.BaseMode, diff.CompareMode, diff.Equal, diff.MissingLines, diff.ExtraLines)
		for _, line := range diff.Sample {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
		name := diff.BaseMode + "_vs_" + diff.CompareMode + ".BUILD.imports.diff"
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	if len(diffs) == 1 {
		diff := diffs[0]
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "base=%s compare=%s equal=%t missing=%d extra=%d\n", diff.BaseMode, diff.CompareMode, diff.Equal, diff.MissingLines, diff.ExtraLines)
		for _, line := range diff.Sample {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, "BUILD.imports.diff"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func printSummary(report replayReport) {
	fmt.Printf("import replay root=%s files=%d bytes=%d elapsed=%s\n", report.Root, report.Files, report.Bytes, time.Duration(report.ElapsedNanos))
	fmt.Printf("%-30s %8s %12s %10s %10s %12s %12s %12s\n", "mode", "files", "bytes", "imports", "fallbacks", "wall", "parse", "extract")
	modes := make([]string, 0, len(report.Modes))
	for mode := range report.Modes {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	for _, mode := range modes {
		mr := report.Modes[mode]
		fmt.Printf("%-30s %8d %12d %10d %10d %12s %12s %12s\n",
			mode,
			mr.Files,
			mr.Bytes,
			mr.Imports,
			mr.Fallbacks,
			time.Duration(mr.WallNanos),
			time.Duration(mr.ParseNanos),
			time.Duration(mr.ExtractNanos),
		)
	}
	for _, diff := range report.OutputDiffs {
		fmt.Printf("output diff %s vs %s: equal=%t missing=%d extra=%d\n", diff.BaseMode, diff.CompareMode, diff.Equal, diff.MissingLines, diff.ExtraLines)
		for _, line := range diff.Sample {
			fmt.Println(line)
		}
	}
}

func totalReplayBytes(files []replayFile) int64 {
	var total int64
	for _, file := range files {
		total += int64(file.Bytes)
	}
	return total
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
