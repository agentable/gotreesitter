//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"regexp"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

// stripFieldLabels removes "name: ", "object: ", etc. from C S-expressions
// so we can compare structural tree shape without field label differences.
var fieldLabelRE = regexp.MustCompile(`\w+: `)

func stripFieldLabels(s string) string {
	return fieldLabelRE.ReplaceAllString(s, "")
}

func TestJavaMethodCallParity(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"obj.method()", `public class Foo { void f() { obj.method(); } }`},
		{"a.b.c()", `public class Foo { void f() { a.b.c(); } }`},
		{"System.out.println", `public class Foo { void f() { System.out.println("hello"); } }`},
		{"items.add", `public class Foo { void f() { items.add("hello"); } }`},
		{"simple call", `public class Foo { void f() { println("hello"); } }`},
		{"new + call", `public class Foo { void f() { new Foo().bar(); } }`},
	}

	// Load Go language
	entry := grammars.DetectLanguage("Test.java")
	goLang := entry.Language()

	// Load C reference language
	cLang, err := ParityCLanguage("java")
	if err != nil {
		t.Fatalf("load C java parser: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)

			// Go parse
			goParser := gotreesitter.NewParser(goLang)
			goTree, err := goParser.Parse(src)
			if err != nil {
				t.Fatalf("Go parse error: %v", err)
			}
			goRoot := goTree.RootNode()
			goSexp := goRoot.SExpr(goLang)
			goErr := goRoot.HasError()

			// C parse
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatalf("C SetLanguage error: %v", err)
			}
			cTree := cParser.Parse(src, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatalf("C parser returned nil tree")
			}
			defer cTree.Close()
			cRoot := cTree.RootNode()
			cSexp := cRoot.ToSexp()
			cErr := cRoot.HasError()

			t.Logf("Go hasError=%v  C hasError=%v", goErr, cErr)
			t.Logf("Go: %s", goSexp)
			t.Logf("C:  %s", cSexp)

			if goErr && !cErr {
				t.Errorf("Go parse has errors but C parse does not — gotreesitter parser bug")
			}
			// Compare structural tree shape (strip field labels from C output)
			if goSexp != stripFieldLabels(cSexp) {
				t.Errorf("S-expression MISMATCH (after stripping field labels)")
			}
		})
	}
}
