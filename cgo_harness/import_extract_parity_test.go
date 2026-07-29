//go:build cgo && treesitter_c_bench_legacy

package cgoharness

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestGoCorpusImportExtractionParity(t *testing.T) {
	files := loadGoCorpus(t)
	lang := grammars.GoLanguage()
	pool := gotreesitter.NewParserPool(lang)
	for _, file := range files {
		tree, err := pool.Parse(file.source)
		if err != nil {
			if tree != nil {
				tree.Release()
			}
			t.Fatalf("%s: %v", file.path, err)
		}
		requireCompleteGoCorpusTree(t, tree, file.source, file.path)
		assertImportExtractionParity(t, lang, file.path, file.source, tree)
		tree.Release()
	}
}

func TestJavaCorpusImportExtractionParity(t *testing.T) {
	files := loadJavaCorpus(t)
	lang := grammars.JavaLanguage()
	timeoutMicros := javaBenchTimeout(t)
	pool := gotreesitter.NewParserPool(lang, gotreesitter.WithParserPoolTimeoutMicros(timeoutMicros))
	for _, file := range files {
		result, err := parseJavaWithMode(pool, lang, javaParseModeDFA, file.source)
		if err != nil {
			if result.tree != nil {
				result.tree.Release()
			}
			t.Fatalf("%s: %v", file.path, err)
		}
		tree := result.tree
		requireCompleteGoTree(t, tree, file.source, file.path, javaParseModeDFA, timeoutMicros)
		assertImportExtractionParity(t, lang, file.path, file.source, tree)
		tree.Release()
	}
}

func TestPythonCorpusImportExtractionParity(t *testing.T) {
	file := loadPythonCorpusFile(t)
	lang := grammars.PythonLanguage()
	pool := gotreesitter.NewParserPool(
		lang,
		gotreesitter.WithParserPoolTimeoutMicros(pythonCorpusBenchTimeoutMicros(t)),
	)
	tree, err := pool.Parse(file.source)
	if err != nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatalf("%s: %v", file.path, err)
	}
	defer tree.Release()
	requireCompletePythonCorpusTree(t, lang, file, tree, "import_extract_parity")
	assertImportExtractionParity(t, lang, file.path, file.source, tree)
}

func TestStarlarkCorpusImportExtractionParity(t *testing.T) {
	files := loadStarlarkCorpus(t)
	lang := grammars.StarlarkLanguage()
	pool := gotreesitter.NewParserPool(lang)
	for _, file := range files {
		tree, err := pool.Parse(file.source)
		if err != nil {
			if tree != nil {
				tree.Release()
			}
			t.Fatalf("%s: %v", file.path, err)
		}
		requireCompleteStarlarkCorpusTree(t, lang, file, tree)
		assertImportExtractionParity(t, lang, file.path, file.source, tree)
		tree.Release()
	}
}

func assertImportExtractionParity(t *testing.T, lang *gotreesitter.Language, path string, source []byte, tree *gotreesitter.Tree) {
	t.Helper()
	treeRefs := gotreesitter.ExtractImports(tree)
	sourceReport := gotreesitter.ExtractImportsFromSourceWithReport(lang, source)
	if sourceReport.Status != gotreesitter.ImportExtractOK || sourceReport.FallbackRecommended {
		t.Fatalf("%s: source import extraction report = %#v, want ok without fallback", path, sourceReport)
	}
	sourceRefs := sourceReport.Imports
	if len(sourceRefs) != len(treeRefs) {
		t.Fatalf("%s: source import refs len = %d, tree refs len = %d\nsource=%#v\ntree=%#v", path, len(sourceRefs), len(treeRefs), sourceRefs, treeRefs)
	}
	for i := range sourceRefs {
		if !importRefsSameShape(sourceRefs[i], treeRefs[i]) {
			t.Fatalf("%s: source import ref[%d] = %#v, tree ref = %#v", path, i, sourceRefs[i], treeRefs[i])
		}
	}
}

func importRefsSameShape(a, b gotreesitter.ImportRef) bool {
	return a.Lang == b.Lang &&
		a.Kind == b.Kind &&
		a.Path == b.Path &&
		a.From == b.From &&
		a.Name == b.Name &&
		a.Alias == b.Alias &&
		a.Static == b.Static &&
		a.Wildcard == b.Wildcard &&
		a.Relative == b.Relative
}

type starlarkCorpusFile struct {
	path   string
	source []byte
}

func loadStarlarkCorpus(tb testing.TB) []starlarkCorpusFile {
	tb.Helper()

	root := strings.TrimSpace(os.Getenv("GOT_STARLARK_CORPUS_ROOT"))
	if root == "" {
		tb.Skip("set GOT_STARLARK_CORPUS_ROOT")
	}
	maxFiles := starlarkCorpusEnvInt(tb, "GOT_STARLARK_CORPUS_MAX_FILES", 0)
	var files []starlarkCorpusFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bazel-bin", "bazel-out", "bazel-testlogs", "node_modules":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !looksStarlarkSourcePath(path) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, starlarkCorpusFile{path: path, source: src})
		return nil
	})
	if err != nil {
		tb.Fatalf("load starlark corpus %s: %v", root, err)
	}
	if len(files) == 0 {
		tb.Fatalf("no Starlark files under %s", root)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})
	if maxFiles > 0 && len(files) > maxFiles {
		files = files[:maxFiles]
	}
	tb.Logf("starlark corpus: root=%s files=%d", root, len(files))
	return files
}

func looksStarlarkSourcePath(path string) bool {
	name := filepath.Base(path)
	switch name {
	case "BUILD", "BUILD.bazel", "WORKSPACE", "WORKSPACE.bazel", "MODULE.bazel":
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".bzl" || ext == ".star" || ext == ".sky"
}

func requireCompleteStarlarkCorpusTree(tb testing.TB, lang *gotreesitter.Language, file starlarkCorpusFile, tree *gotreesitter.Tree) {
	tb.Helper()
	if tree == nil || tree.RootNode() == nil {
		tb.Fatalf("%s: parse returned nil root", file.path)
	}
	root := tree.RootNode()
	rt := tree.ParseRuntime()
	if root.HasError() || tree.ParseStoppedEarly() || root.EndByte() != uint32(len(file.source)) || rt.Truncated {
		tb.Fatalf("%s: incomplete parse type=%q has_error=%v runtime=%s", file.path, root.Type(lang), root.HasError(), rt.Summary())
	}
}

func starlarkCorpusEnvInt(tb testing.TB, name string, fallback int) int {
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
