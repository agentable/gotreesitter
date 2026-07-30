package gotreesitter_test

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestGroovyUppercaseMethodCompatibilityRealParser(t *testing.T) {
	lang := grammars.GroovyLanguage()
	source := []byte(`class Main { static String Run() { Helper.Leaf() } }`)
	tree, err := gotreesitter.NewParser(lang).Parse(source)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		t.Fatal("Parse returned a nil tree")
	}
	defer tree.Release()

	method := findGroovyNodeByText(tree.RootNode(), lang, source, "function_definition", `static String Run() { Helper.Leaf() }`)
	if method == nil {
		t.Fatalf("function_definition not found:\n%s", tree.RootNode().SExpr(lang))
	}
	name := method.ChildByFieldName("function", lang)
	if name == nil || string(source[name.StartByte():name.EndByte()]) != "Run" {
		t.Fatalf("function field does not contain Run:\n%s", tree.RootNode().SExpr(lang))
	}
	parameters := method.ChildByFieldName("parameters", lang)
	if parameters == nil || parameters.Type(lang) != "parameter_list" ||
		string(source[parameters.StartByte():parameters.EndByte()]) != "()" {
		t.Fatalf("parameters field is not an empty parameter_list:\n%s", tree.RootNode().SExpr(lang))
	}
	body := method.ChildByFieldName("body", lang)
	if body == nil || body.Type(lang) != "closure" {
		t.Fatalf("body field is not a closure:\n%s", tree.RootNode().SExpr(lang))
	}
	call := findGroovyNodeByText(method, lang, source, "function_call", "Helper.Leaf()")
	if call == nil {
		t.Fatalf("nested function_call not found:\n%s", tree.RootNode().SExpr(lang))
	}
}

func findGroovyNodeByText(
	node *gotreesitter.Node,
	lang *gotreesitter.Language,
	source []byte,
	kind string,
	text string,
) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.Type(lang) == kind && string(source[node.StartByte():node.EndByte()]) == text {
		return node
	}
	for index := 0; index < node.ChildCount(); index++ {
		if found := findGroovyNodeByText(node.Child(index), lang, source, kind, text); found != nil {
			return found
		}
	}
	return nil
}
