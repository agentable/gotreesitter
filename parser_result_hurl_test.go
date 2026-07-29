package gotreesitter_test

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestNormalizeHurlTrailingFileDelimiterErrorRoot(t *testing.T) {
	lang := grammars.HurlLanguage()
	src := []byte("POST http://localhost:8000/data\nfile,")
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()
	root := tree.RootNode()
	if got := root.Type(lang); got != "hurl_file" {
		t.Fatalf("root type = %q, want hurl_file; tree=%s", got, root.SExpr(lang))
	}
	if !root.HasError() {
		t.Fatalf("root HasError = false, want true; tree=%s", root.SExpr(lang))
	}
}

func TestNormalizeHurlDoesNotRetagNonFileDelimiterErrorRoot(t *testing.T) {
	lang := grammars.HurlLanguage()
	src := []byte("GET http://localhost:8000/hello\n\nxxx")
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()
	root := tree.RootNode()
	if got := root.Type(lang); got != "ERROR" {
		t.Fatalf("root type = %q, want ERROR; tree=%s", got, root.SExpr(lang))
	}
}
