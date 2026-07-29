package gotreesitter_test

import (
	"bytes"
	"testing"

	"github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

// benchTagsQuery is a tags query suitable for Go source.
// It matches function/method definitions and call references.
const benchTagsQuery = `
(function_declaration (identifier) @name) @definition.function
(method_declaration (field_identifier) @name) @definition.method
(call_expression (identifier) @name) @reference.call
(call_expression (selector_expression (field_identifier) @name)) @reference.call
(type_declaration (type_spec (type_identifier) @name)) @definition.type
`

var taggerBenchSink int
var codeUnderstandingBenchSink int

// BenchmarkTaggerTag measures tagging a 500-function Go file from scratch.
func BenchmarkTaggerTag(b *testing.B) {
	entry := grammars.DetectLanguage("main.go")
	if entry == nil {
		b.Skip("Go grammar not available")
	}

	lang := entry.Language()
	src := makeGoBenchmarkSource(benchmarkFuncCount(b))

	var opts []gotreesitter.TaggerOption
	if entry.TokenSourceFactory != nil {
		factory := entry.TokenSourceFactory
		opts = append(opts, gotreesitter.WithTaggerTokenSourceFactory(func(s []byte) gotreesitter.TokenSource {
			return factory(s, lang)
		}))
	}

	tagger, err := gotreesitter.NewTagger(lang, benchTagsQuery, opts...)
	if err != nil {
		b.Fatalf("NewTagger failed: %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tags := tagger.Tag(src)
		if len(tags) == 0 {
			b.Fatal("tagger returned no tags")
		}
	}
}

// BenchmarkExtractCodeUnderstandingGo measures parse plus the one-pass
// code-understanding helpers. It is the low-overhead alternative to tags-query
// fanout when callers only need common definitions and call references.
func BenchmarkExtractCodeUnderstandingGo(b *testing.B) {
	entry := grammars.DetectLanguage("main.go")
	if entry == nil {
		b.Skip("Go grammar not available")
	}

	lang := entry.Language()
	src := makeGoBenchmarkSource(benchmarkFuncCount(b))
	parser := gotreesitter.NewParser(lang)

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var (
			tree *gotreesitter.Tree
			err  error
		)
		if entry.TokenSourceFactory != nil {
			tree, err = parser.ParseWithTokenSource(src, entry.TokenSourceFactory(src, lang))
		} else {
			tree, err = parser.Parse(src)
		}
		if err != nil {
			b.Fatalf("parse failed: %v", err)
		}
		defs := gotreesitter.ExtractDefinitionSpans(tree)
		calls := gotreesitter.ExtractCalls(tree)
		if len(defs) == 0 {
			b.Fatalf("understanding helpers returned defs=%d calls=%d", len(defs), len(calls))
		}
		codeUnderstandingBenchSink += len(defs) + len(calls)
		tree.Release()
	}
}

// BenchmarkTaggerTagTreeGo measures tags-query execution over an existing tree.
func BenchmarkTaggerTagTreeGo(b *testing.B) {
	entry := grammars.DetectLanguage("main.go")
	if entry == nil {
		b.Skip("Go grammar not available")
	}

	lang := entry.Language()
	src := makeGoBenchmarkSource(benchmarkFuncCount(b))
	tree := parseBenchmarkTree(b, entry, lang, src)
	defer tree.Release()

	tagger, err := gotreesitter.NewTagger(lang, benchTagsQuery)
	if err != nil {
		b.Fatalf("NewTagger failed: %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tags := tagger.TagTree(tree)
		if len(tags) == 0 {
			b.Fatal("tagger returned no tags")
		}
		taggerBenchSink += len(tags)
	}
}

// BenchmarkExtractCodeUnderstandingTreeGo measures the one-pass helpers over an
// existing tree, isolating inspection overhead from parser cost.
func BenchmarkExtractCodeUnderstandingTreeGo(b *testing.B) {
	entry := grammars.DetectLanguage("main.go")
	if entry == nil {
		b.Skip("Go grammar not available")
	}

	lang := entry.Language()
	src := makeGoBenchmarkSource(benchmarkFuncCount(b))
	tree := parseBenchmarkTree(b, entry, lang, src)
	defer tree.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		defs := gotreesitter.ExtractDefinitionSpans(tree)
		calls := gotreesitter.ExtractCalls(tree)
		if len(defs) == 0 {
			b.Fatalf("understanding helpers returned defs=%d calls=%d", len(defs), len(calls))
		}
		codeUnderstandingBenchSink += len(defs) + len(calls)
	}
}

func parseBenchmarkTree(b *testing.B, entry *grammars.LangEntry, lang *gotreesitter.Language, src []byte) *gotreesitter.Tree {
	b.Helper()
	parser := gotreesitter.NewParser(lang)
	if entry.TokenSourceFactory != nil {
		tree, err := parser.ParseWithTokenSource(src, entry.TokenSourceFactory(src, lang))
		if err != nil {
			b.Fatalf("parse failed: %v", err)
		}
		return tree
	}
	tree, err := parser.Parse(src)
	if err != nil {
		b.Fatalf("parse failed: %v", err)
	}
	return tree
}

// BenchmarkTaggerTagIncremental measures re-tagging after a single-byte edit.
func BenchmarkTaggerTagIncremental(b *testing.B) {
	entry := grammars.DetectLanguage("main.go")
	if entry == nil {
		b.Skip("Go grammar not available")
	}

	lang := entry.Language()
	src := makeGoBenchmarkSource(benchmarkFuncCount(b))

	// Locate the edit site: "v := 0" -> toggle the '0'.
	editAt := bytes.Index(src, []byte("v := 0"))
	if editAt < 0 {
		b.Fatal("could not find edit marker")
	}
	editAt += len("v := ")
	start := pointAtOffset(src, editAt)
	end := pointAtOffset(src, editAt+1)

	// Build the initial tree.
	parser := gotreesitter.NewParser(lang)
	ts := mustGoTokenSource(b, src, lang)
	tree, err := parser.ParseWithTokenSource(src, ts)
	if err != nil {
		b.Fatalf("initial parse failed: %v", err)
	}
	if tree.RootNode() == nil {
		b.Fatal("initial parse returned nil root")
	}

	var opts []gotreesitter.TaggerOption
	if entry.TokenSourceFactory != nil {
		factory := entry.TokenSourceFactory
		opts = append(opts, gotreesitter.WithTaggerTokenSourceFactory(func(s []byte) gotreesitter.TokenSource {
			return factory(s, lang)
		}))
	}

	tagger, err := gotreesitter.NewTagger(lang, benchTagsQuery, opts...)
	if err != nil {
		b.Fatalf("NewTagger failed: %v", err)
	}

	edit := gotreesitter.InputEdit{
		StartByte:   uint32(editAt),
		OldEndByte:  uint32(editAt + 1),
		NewEndByte:  uint32(editAt + 1),
		StartPoint:  start,
		OldEndPoint: end,
		NewEndPoint: end,
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Toggle one ASCII digit in place.
		if src[editAt] == '0' {
			src[editAt] = '1'
		} else {
			src[editAt] = '0'
		}

		tree.Edit(edit)
		tags, newTree := tagger.TagIncremental(src, tree)
		if len(tags) == 0 {
			b.Fatal("incremental tagger returned no tags")
		}
		if newTree != tree {
			tree.Release()
		}
		tree = newTree
	}
	tree.Release()
}
