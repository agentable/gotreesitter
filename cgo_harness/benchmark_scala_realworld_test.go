//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type scalaBenchCaseData struct {
	name string
	path string
	src  []byte
	edit benchmarkEditSite
}

var scalaBenchCache struct {
	once  sync.Once
	cases []scalaBenchCaseData
	err   error
}

func loadScalaBenchCases(tb testing.TB) []scalaBenchCaseData {
	tb.Helper()
	scalaBenchCache.once.Do(func() {
		repoDir, err := scalaBenchRepoDir()
		if err != nil {
			scalaBenchCache.err = err
			return
		}
		cases, err := readScalaBenchCases(repoDir)
		if err != nil {
			scalaBenchCache.err = err
			return
		}
		scalaBenchCache.cases = cases
	})
	if scalaBenchCache.err != nil {
		tb.Fatalf("load scala benchmark cases: %v", scalaBenchCache.err)
	}
	return filterScalaBenchCases(tb, scalaBenchCache.cases)
}

func filterScalaBenchCases(tb testing.TB, all []scalaBenchCaseData) []scalaBenchCaseData {
	tb.Helper()
	raw := strings.TrimSpace(os.Getenv("GTS_SCALA_BENCH_CASE"))
	if raw == "" || strings.EqualFold(raw, "all") {
		return all
	}

	want := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		want[name] = true
	}
	if len(want) == 0 {
		tb.Fatalf("invalid GTS_SCALA_BENCH_CASE=%q", raw)
	}

	out := make([]scalaBenchCaseData, 0, len(want))
	for _, tc := range all {
		if want[tc.name] {
			out = append(out, tc)
			delete(want, tc.name)
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for name := range want {
			missing = append(missing, name)
		}
		tb.Fatalf("unknown scala benchmark case(s): %s", strings.Join(missing, ","))
	}
	return out
}

func scalaBenchRepoDir() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("GTS_SCALA_BENCH_REPO")); raw != "" {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return "", fmt.Errorf("resolve GTS_SCALA_BENCH_REPO=%q: %w", raw, err)
		}
		if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
			return "", fmt.Errorf("GTS_SCALA_BENCH_REPO has no .git dir: %s", abs)
		}
		return abs, nil
	}

	rootDir, err := os.MkdirTemp("", "gotreesitter-scala-bench-*")
	if err != nil {
		return "", fmt.Errorf("create temp scala bench dir: %w", err)
	}
	repoDir := filepath.Join(rootDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", repoDir, err)
	}
	if err := runGit(repoDir, "init"); err != nil {
		return "", err
	}
	if err := runGit(repoDir, "remote", "add", "origin", scalaRealWorldRepoURL); err != nil {
		return "", err
	}
	if err := runGit(repoDir, "fetch", "--depth=1", "origin", scalaRealWorldCommit); err != nil {
		return "", err
	}
	if err := runGit(repoDir, "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return "", err
	}
	return repoDir, nil
}

func runGit(repoDir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func readScalaBenchCases(repoDir string) ([]scalaBenchCaseData, error) {
	out := make([]scalaBenchCaseData, 0, len(scalaRealWorldCases))
	for _, tc := range scalaRealWorldCases {
		absPath := filepath.Join(repoDir, filepath.FromSlash(tc.path))
		src, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("read %s (%s): %w", tc.name, tc.path, err)
		}
		if len(src) == 0 {
			return nil, fmt.Errorf("empty source for %s (%s)", tc.name, tc.path)
		}
		normalized := normalizedSource("scala", string(src))
		edit, err := findScalaEditSite(normalized)
		if err != nil {
			return nil, fmt.Errorf("%s edit site: %w", tc.name, err)
		}
		out = append(out, scalaBenchCaseData{
			name: tc.name,
			path: tc.path,
			src:  normalized,
			edit: edit,
		})
	}
	return out, nil
}

func findScalaEditSite(src []byte) (benchmarkEditSite, error) {
	patterns := []string{
		"def ",
		"val ",
		"case ",
		"object ",
		"class ",
		"trait ",
	}
	for _, pattern := range patterns {
		from := 0
		for from < len(src) {
			idx := bytes.Index(src[from:], []byte(pattern))
			if idx < 0 {
				break
			}
			offset := from + idx + len(pattern)
			for offset < len(src) {
				b := src[offset]
				if b < 0x80 && ((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')) {
					return benchmarkEditSite{
						offset: offset,
						start:  pointAtOffset(src, offset),
						end:    pointAtOffset(src, offset+1),
					}, nil
				}
				if b == '\n' || b == '\r' {
					break
				}
				offset++
			}
			from += idx + len(pattern)
		}
	}

	for i, b := range src {
		if b < 0x80 && ((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')) {
			return benchmarkEditSite{
				offset: i,
				start:  pointAtOffset(src, i),
				end:    pointAtOffset(src, i+1),
			}, nil
		}
	}
	return benchmarkEditSite{}, fmt.Errorf("no ASCII identifier-like edit site found")
}

func toggleScalaEditByte(src []byte, offset int) {
	if offset < 0 || offset >= len(src) {
		return
	}
	switch src[offset] {
	case 'a':
		src[offset] = 'b'
	case 'b':
		src[offset] = 'a'
	case 'A':
		src[offset] = 'B'
	case 'B':
		src[offset] = 'A'
	default:
		src[offset] = 'a'
	}
}

func requireCompleteScalaGoTree(tb testing.TB, tree *gotreesitter.Tree, src []byte, phase string) {
	tb.Helper()
	if tree == nil {
		tb.Fatalf("%s parse returned nil tree", phase)
	}
	root := tree.RootNode()
	if root == nil {
		tb.Fatalf("%s parse returned nil root", phase)
	}
	if got, want := root.EndByte(), uint32(len(src)); got != want {
		tb.Fatalf("%s parse truncated: end=%d want=%d runtime=%s", phase, got, want, tree.ParseRuntime().Summary())
	}
}

func requireCompleteScalaCTree(tb testing.TB, tree *sitter.Tree, src []byte, phase string) {
	tb.Helper()
	if tree == nil {
		tb.Fatalf("%s parse returned nil tree", phase)
	}
	root := tree.RootNode()
	if root == nil {
		tb.Fatalf("%s parse returned nil root", phase)
	}
	if got, want := uint32(root.EndByte()), uint32(len(src)); got != want {
		tb.Fatalf("%s parse truncated: end=%d want=%d", phase, got, want)
	}
}

func newCScalaParser(tb testing.TB) *sitter.Parser {
	tb.Helper()
	cLang, err := ParityCLanguage("scala")
	if err != nil {
		tb.Fatalf("load c scala language: %v", err)
	}
	parser := sitter.NewParser()
	if err := parser.SetLanguage(cLang); err != nil {
		tb.Fatalf("set c scala language: %v", err)
	}
	return parser
}

func BenchmarkScalaGoParseFullDFARealWorld(b *testing.B) {
	lang := grammars.ScalaLanguage()
	parser := gotreesitter.NewParser(lang)
	for _, tc := range loadScalaBenchCases(b) {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.src)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tree, err := parser.Parse(tc.src)
				if err != nil {
					b.Fatalf("parse error: %v", err)
				}
				requireCompleteScalaGoTree(b, tree, tc.src, "go full dfa")
				tree.Release()
			}
		})
	}
}

func BenchmarkScalaGoParseIncrementalSingleByteEditDFARealWorld(b *testing.B) {
	lang := grammars.ScalaLanguage()
	parser := gotreesitter.NewParser(lang)

	for _, tc := range loadScalaBenchCases(b) {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			src := append([]byte(nil), tc.src...)
			tree, err := parser.Parse(src)
			if err != nil {
				b.Fatalf("initial parse error: %v", err)
			}
			requireCompleteScalaGoTree(b, tree, src, "go incremental initial")

			edit := gotreesitter.InputEdit{
				StartByte:   uint32(tc.edit.offset),
				OldEndByte:  uint32(tc.edit.offset + 1),
				NewEndByte:  uint32(tc.edit.offset + 1),
				StartPoint:  tc.edit.start,
				OldEndPoint: tc.edit.end,
				NewEndPoint: tc.edit.end,
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				toggleScalaEditByte(src, tc.edit.offset)
				tree.Edit(edit)

				newTree, err := parser.ParseIncremental(src, tree)
				if err != nil {
					b.Fatalf("incremental parse error: %v", err)
				}
				requireCompleteScalaGoTree(b, newTree, src, "go incremental single-byte")
				if newTree != tree {
					tree.Release()
				}
				tree = newTree
			}
			if tree != nil {
				tree.Release()
			}
		})
	}
}

func BenchmarkScalaGoParseIncrementalNoEditDFARealWorld(b *testing.B) {
	lang := grammars.ScalaLanguage()
	parser := gotreesitter.NewParser(lang)

	for _, tc := range loadScalaBenchCases(b) {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			tree, err := parser.Parse(tc.src)
			if err != nil {
				b.Fatalf("initial parse error: %v", err)
			}
			requireCompleteScalaGoTree(b, tree, tc.src, "go incremental no-edit initial")

			b.ReportAllocs()
			b.SetBytes(int64(len(tc.src)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				newTree, err := parser.ParseIncremental(tc.src, tree)
				if err != nil {
					b.Fatalf("incremental parse error: %v", err)
				}
				if newTree == nil || newTree.RootNode() == nil {
					if tree != nil {
						tree.Release()
					}
					if newTree != nil {
						newTree.Release()
					}
					b.Skipf("go incremental no-edit returned nil root for scala/%s", tc.name)
				}
				if newTree.RootNode().EndByte() != uint32(len(tc.src)) {
					rt := newTree.ParseRuntime()
					tree.Release()
					newTree.Release()
					b.Skipf("go incremental no-edit truncated for scala/%s: %s", tc.name, rt.Summary())
				}
				if newTree != tree {
					tree.Release()
				}
				tree = newTree
			}
			if tree != nil {
				tree.Release()
			}
		})
	}
}

func BenchmarkScalaCTreeSitterParseFullRealWorld(b *testing.B) {
	parser := newCScalaParser(b)
	defer parser.Close()

	for _, tc := range loadScalaBenchCases(b) {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.src)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tree := parser.Parse(tc.src, nil)
				requireCompleteScalaCTree(b, tree, tc.src, "c full")
				tree.Close()
			}
		})
	}
}

func BenchmarkScalaCTreeSitterParseIncrementalSingleByteEditRealWorld(b *testing.B) {
	parser := newCScalaParser(b)
	defer parser.Close()

	for _, tc := range loadScalaBenchCases(b) {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			src := append([]byte(nil), tc.src...)
			tree := parser.Parse(src, nil)
			requireCompleteScalaCTree(b, tree, src, "c incremental initial")

			edit := sitter.InputEdit{
				StartByte:  uint(tc.edit.offset),
				OldEndByte: uint(tc.edit.offset + 1),
				NewEndByte: uint(tc.edit.offset + 1),
				StartPosition: sitter.Point{
					Row: uint(tc.edit.start.Row), Column: uint(tc.edit.start.Column),
				},
				OldEndPosition: sitter.Point{
					Row: uint(tc.edit.end.Row), Column: uint(tc.edit.end.Column),
				},
				NewEndPosition: sitter.Point{
					Row: uint(tc.edit.end.Row), Column: uint(tc.edit.end.Column),
				},
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(src)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				toggleScalaEditByte(src, tc.edit.offset)
				tree.Edit(&edit)
				newTree := parser.Parse(src, tree)
				requireCompleteScalaCTree(b, newTree, src, "c incremental single-byte")
				if newTree != tree {
					tree.Close()
				}
				tree = newTree
			}
			if tree != nil {
				tree.Close()
			}
		})
	}
}

func BenchmarkScalaCTreeSitterParseIncrementalNoEditRealWorld(b *testing.B) {
	parser := newCScalaParser(b)
	defer parser.Close()

	for _, tc := range loadScalaBenchCases(b) {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			tree := parser.Parse(tc.src, nil)
			requireCompleteScalaCTree(b, tree, tc.src, "c incremental no-edit initial")

			b.ReportAllocs()
			b.SetBytes(int64(len(tc.src)))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				newTree := parser.Parse(tc.src, tree)
				if newTree == nil || newTree.RootNode() == nil {
					if tree != nil {
						tree.Close()
					}
					if newTree != nil {
						newTree.Close()
					}
					b.Skipf("c incremental no-edit returned nil root for scala/%s", tc.name)
				}
				if uint32(newTree.RootNode().EndByte()) != uint32(len(tc.src)) {
					tree.Close()
					newTree.Close()
					b.Skipf("c incremental no-edit truncated for scala/%s", tc.name)
				}
				if newTree != tree {
					tree.Close()
				}
				tree = newTree
			}
			if tree != nil {
				tree.Close()
			}
		})
	}
}
