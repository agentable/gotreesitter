package gotreesitter_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestExtractDefinitionSpansAndCallsGo(t *testing.T) {
	source := []byte(`package main

type Service struct{}

func (s Service) Run() {
	helper()
}

func helper() {}
`)
	tree := parseUnderstandingTree(t, "main.go", source)
	defer tree.Release()

	defs := gotreesitter.ExtractDefinitionSpans(tree)
	assertDefinitionNames(t, defs, "Service", "Run", "helper")

	calls := gotreesitter.ExtractCalls(tree)
	assertCallNames(t, calls, "helper")

	offset := uint32(strings.Index(string(source), "helper()"))
	enclosing, ok := gotreesitter.EnclosingDefinition(tree, offset)
	if !ok || enclosing.Name != "Run" || enclosing.Kind != "method" {
		t.Fatalf("EnclosingDefinition = %#v, %v; want method Run", enclosing, ok)
	}
}

func TestExtractDefinitionSpansCallsAndHeritageJavaScript(t *testing.T) {
	source := []byte(`class Child extends Base {
  method() {
    this.work();
    helper();
  }
}

function helper() {}
`)
	tree := parseUnderstandingTree(t, "main.js", source)
	defer tree.Release()

	defs := gotreesitter.ExtractDefinitionSpans(tree)
	assertDefinitionNames(t, defs, "Child", "method", "helper")

	calls := gotreesitter.ExtractCalls(tree)
	assertCallNames(t, calls, "work", "helper")

	heritage := gotreesitter.ExtractHeritage(tree)
	if !hasHeritage(heritage, "Child", "Base") {
		t.Fatalf("ExtractHeritage = %#v, want Child extends Base", heritage)
	}
}

func TestExtractDefinitionSpansCallsAndHeritagePython(t *testing.T) {
	source := []byte(`class Child(Base, mixins.Helper):
    def method(self):
        self.work()
        helper()

def helper():
    pass
`)
	tree := parseUnderstandingTree(t, "script.py", source)
	defer tree.Release()

	defs := gotreesitter.ExtractDefinitionSpans(tree)
	assertDefinitionNames(t, defs, "Child", "method", "helper")

	calls := gotreesitter.ExtractCalls(tree)
	assertCallNames(t, calls, "work", "helper")

	heritage := gotreesitter.ExtractHeritage(tree)
	if !hasHeritage(heritage, "Child", "Base") || !hasHeritage(heritage, "Child", "mixins.Helper") {
		t.Fatalf("ExtractHeritage = %#v, want Child bases", heritage)
	}
}

func TestExtractDefinitionSpansCallsAndHeritageJava(t *testing.T) {
	source := []byte(`package example;

class Child extends Base implements Runnable {
  void method() {
    helper();
  }
  void helper() {}
}
`)
	tree := parseUnderstandingTree(t, "Child.java", source)
	defer tree.Release()

	defs := gotreesitter.ExtractDefinitionSpans(tree)
	assertDefinitionNames(t, defs, "Child", "method", "helper")

	calls := gotreesitter.ExtractCalls(tree)
	assertCallNames(t, calls, "helper")

	heritage := gotreesitter.ExtractHeritage(tree)
	if !hasHeritage(heritage, "Child", "Base") || !hasHeritage(heritage, "Child", "Runnable") {
		t.Fatalf("ExtractHeritage = %#v, want Child extends Base implements Runnable", heritage)
	}
}

func TestExtractDefinitionSpansTypeScript(t *testing.T) {
	source := []byte(`class Child extends Base {
  method(): void {
    helper();
  }
}

function helper(): void {}
`)
	tree := parseUnderstandingTree(t, "main.ts", source)
	defer tree.Release()

	defs := gotreesitter.ExtractDefinitionSpans(tree)
	assertDefinitionNames(t, defs, "Child", "method", "helper")

	calls := gotreesitter.ExtractCalls(tree)
	assertCallNames(t, calls, "helper")
}

func TestExtractExplicitInstantiations(t *testing.T) {
	tests := []struct {
		filename string
		source   string
		want     string
	}{
		{
			filename: "main.go",
			source:   "package main\ntype Worker struct{}\nvar _ = Worker{}\n",
			want:     "Worker",
		},
		{
			filename: "Main.java",
			source:   "class Worker {}\nclass Main { Object run() { return new Worker(); } }\n",
			want:     "Worker",
		},
		{
			filename: "main.js",
			source:   "class Worker {}\nconst worker = new Worker();\n",
			want:     "Worker",
		},
		{
			filename: "main.ts",
			source:   "class Worker {}\nconst worker: Worker = new Worker();\n",
			want:     "Worker",
		},
	}
	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			source := []byte(test.source)
			tree := parseUnderstandingTree(t, test.filename, source)
			defer tree.Release()
			refs := gotreesitter.ExtractInstantiations(tree)
			if len(refs) != 1 ||
				refs[0].Name != test.want ||
				string(source[refs[0].NameStartByte:refs[0].NameEndByte]) != test.want ||
				refs[0].StartByte >= refs[0].EndByte {
				t.Fatalf("ExtractInstantiations = %#v, want %q", refs, test.want)
			}
		})
	}
}

func parseUnderstandingTree(t *testing.T, filename string, source []byte) *gotreesitter.Tree {
	t.Helper()
	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		t.Fatalf("DetectLanguage(%q) returned nil", filename)
	}
	lang := entry.Language()
	parser := gotreesitter.NewParser(lang)
	var (
		tree *gotreesitter.Tree
		err  error
	)
	if entry.TokenSourceFactory != nil {
		tree, err = parser.ParseWithTokenSource(source, entry.TokenSourceFactory(source, lang))
	} else {
		tree, err = parser.Parse(source)
	}
	if err != nil {
		t.Fatalf("parse %s failed: %v", filename, err)
	}
	if tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse %s returned nil tree/root", filename)
	}
	if tree.RootNode().HasError() {
		t.Fatalf("parse %s has errors: %s", filename, tree.RootNode().SExpr(lang))
	}
	return tree
}

func assertDefinitionNames(t *testing.T, defs []gotreesitter.DefinitionSpan, names ...string) {
	t.Helper()
	var got []string
	for _, def := range defs {
		got = append(got, def.Name)
	}
	for _, name := range names {
		if !slices.Contains(got, name) {
			t.Fatalf("definition names = %v, want %q in %#v", got, name, defs)
		}
	}
}

func assertCallNames(t *testing.T, calls []gotreesitter.CallRef, names ...string) {
	t.Helper()
	var got []string
	for _, call := range calls {
		got = append(got, call.Name)
	}
	for _, name := range names {
		if !slices.Contains(got, name) {
			t.Fatalf("call names = %v, want %q in %#v", got, name, calls)
		}
	}
}

func hasHeritage(refs []gotreesitter.HeritageRef, name, parent string) bool {
	for _, ref := range refs {
		if ref.Name == name && ref.Parent == parent {
			return true
		}
	}
	return false
}
