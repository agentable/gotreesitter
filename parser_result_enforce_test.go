package gotreesitter_test

import (
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestEnforceConstIntFormalParameterCompatibility(t *testing.T) {
	lang := grammars.EnforceLanguage()
	if lang == nil {
		t.Fatal("EnforceLanguage returned nil")
	}
	parser := gotreesitter.NewParser(lang)
	source := []byte("class A { void m(const int x = 69) {} }")

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	defer tree.Release()

	sexpr := tree.RootNode().SExpr(lang)
	want := "(formal_parameter (formal_parameter_modifier) (type_int) (identifier) (literal_int))"
	if !strings.Contains(sexpr, want) {
		t.Fatalf("missing normalized formal_parameter\nwant fragment: %s\ngot: %s", want, sexpr)
	}
	if strings.Contains(sexpr, "(formal_parameter (type_identifier (identifier)) (identifier) (literal_int))") {
		t.Fatalf("found unnormalized formal_parameter: %s", sexpr)
	}
}
