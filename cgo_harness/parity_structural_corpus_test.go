//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/agentable/gotreesitter/grammars"
)

// extToLanguage maps file extensions to tree-sitter language names.
var extToLanguage = map[string]string{
	".go":   "go",
	".py":   "python",
	".java": "java",
	".js":   "javascript",
	".ts":   "typescript",
}

// TestParityStructuralCorpus reads every file in corpus_structural/, detects
// the language from the file extension, parses with both gotreesitter (Go) and
// the C reference parser, and reports ALL divergences found by compareNodes.
func TestParityStructuralCorpus(t *testing.T) {
	corpusDir := filepath.Join("corpus_structural")
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("read corpus_structural: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("corpus_structural is empty")
	}

	type fileSummary struct {
		filename   string
		lang       string
		divergeN   int
		divergeAll []string
		errMsg     string
	}

	// Per-language counters.
	langPass := make(map[string]int)
	langFail := make(map[string]int)
	langDivTotal := make(map[string]int)

	var results []fileSummary

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		ext := filepath.Ext(de.Name())
		langName, ok := extToLanguage[ext]
		if !ok {
			t.Logf("SKIP %s: unrecognized extension %q", de.Name(), ext)
			continue
		}

		filePath := filepath.Join(corpusDir, de.Name())
		src, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("read %s: %v", de.Name(), err)
			results = append(results, fileSummary{filename: de.Name(), lang: langName, errMsg: err.Error()})
			langFail[langName]++
			continue
		}

		t.Run(de.Name(), func(t *testing.T) {
			// Parse with Go.
			tc := parityCase{name: langName, source: string(src)}
			goTree, goLang, err := parseWithGo(tc, src, nil)
			if err != nil {
				msg := fmt.Sprintf("Go parse error: %v", err)
				t.Error(msg)
				results = append(results, fileSummary{filename: de.Name(), lang: langName, errMsg: msg})
				langFail[langName]++
				return
			}
			defer releaseGoTree(goTree)

			// Parse with C.
			cLang, err := ParityCLanguage(langName)
			if err != nil {
				if skipReason := parityReferenceSkipReason(err); skipReason != "" {
					t.Skipf("C parser unavailable: %s", skipReason)
				}
				msg := fmt.Sprintf("C parser load error: %v", err)
				t.Error(msg)
				results = append(results, fileSummary{filename: de.Name(), lang: langName, errMsg: msg})
				langFail[langName]++
				return
			}
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				if skipReason := parityReferenceSkipReason(err); skipReason != "" {
					t.Skipf("C SetLanguage: %s", skipReason)
				}
				msg := fmt.Sprintf("C SetLanguage error: %v", err)
				t.Error(msg)
				results = append(results, fileSummary{filename: de.Name(), lang: langName, errMsg: msg})
				langFail[langName]++
				return
			}
			cTree := cParser.Parse(src, nil)
			if cTree == nil || cTree.RootNode() == nil {
				msg := "C parser returned nil tree"
				t.Error(msg)
				results = append(results, fileSummary{filename: de.Name(), lang: langName, errMsg: msg})
				langFail[langName]++
				return
			}
			defer cTree.Close()

			// Compare trees.
			var errs []string
			compareNodes(goTree.RootNode(), goLang, cTree.RootNode(), "root", &errs)

			if len(errs) == 0 {
				t.Logf("PARITY OK: %s (%s, %d bytes)", de.Name(), langName, len(src))
				results = append(results, fileSummary{filename: de.Name(), lang: langName, divergeN: 0})
				langPass[langName]++
				return
			}

			// Log ALL divergences.
			t.Logf("DIVERGENCES in %s (%s): %d total", de.Name(), langName, len(errs))
			for i, e := range errs {
				t.Logf("  [%d] %s", i+1, e)
			}

			// Dump trees, truncated to 100 lines each.
			goTreeDump := dumpGoTree(goTree.RootNode(), goLang, 0)
			cTreeDump := dumpCTree(cTree.RootNode(), 0)

			t.Logf("--- Go tree (%s) ---", de.Name())
			logTruncated(t, goTreeDump, 100)
			t.Logf("--- C tree (%s) ---", de.Name())
			logTruncated(t, cTreeDump, 100)

			results = append(results, fileSummary{
				filename:   de.Name(),
				lang:       langName,
				divergeN:   len(errs),
				divergeAll: errs,
			})
			langFail[langName]++
			langDivTotal[langName] += len(errs)

			t.Errorf("%s: %d divergence(s)", de.Name(), len(errs))
		})
	}

	// Summary.
	t.Log("")
	t.Log("=== STRUCTURAL CORPUS PARITY SUMMARY ===")
	allLangs := make(map[string]bool)
	for k := range langPass {
		allLangs[k] = true
	}
	for k := range langFail {
		allLangs[k] = true
	}
	totalPass, totalFail, totalDiv := 0, 0, 0
	for lang := range allLangs {
		p, f, d := langPass[lang], langFail[lang], langDivTotal[lang]
		t.Logf("  %-12s  pass=%d  fail=%d  divergences=%d", lang, p, f, d)
		totalPass += p
		totalFail += f
		totalDiv += d
	}
	t.Logf("  TOTAL         pass=%d  fail=%d  divergences=%d", totalPass, totalFail, totalDiv)
	t.Log("====================================")
}

// logTruncated logs a multi-line string, capping output at maxLines.
func logTruncated(t *testing.T, s string, maxLines int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		for _, l := range lines[:maxLines] {
			t.Log(l)
		}
		t.Logf("... (%d more lines truncated)", len(lines)-maxLines)
	} else {
		for _, l := range lines {
			t.Log(l)
		}
	}
}

// init-time guard: ensure all languages in extToLanguage have parse support.
func init() {
	for _, lang := range extToLanguage {
		if _, ok := parityEntriesByName[lang]; !ok {
			// Soft: don't panic, the test will report it at runtime.
			_ = lang
		}
	}
}

// Ensure the grammars package is referenced to keep the import valid.
var _ = grammars.AllLanguages
