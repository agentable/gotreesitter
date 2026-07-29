package gotreesitter_test

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestEDSRecoverySplitsEmbeddedSectionHeader(t *testing.T) {
	src := []byte("[A]\nEmpty=\n\n[B]\nX=1\n")
	lang := grammars.EdsLanguage()
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()
	root := tree.RootNode()
	if got, want := root.ChildCount(), 2; got != want {
		t.Fatalf("root child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	first, second := root.Child(0), root.Child(1)
	if got, want := first.Type(lang), "section"; got != want {
		t.Fatalf("first child type = %q, want %q", got, want)
	}
	if got, want := first.EndByte(), uint32(10); got != want {
		t.Fatalf("first section EndByte = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	if got, want := second.Type(lang), "section"; got != want {
		t.Fatalf("second child type = %q, want %q", got, want)
	}
	if got, want := second.StartByte(), uint32(12); got != want {
		t.Fatalf("second section StartByte = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
}

func TestEDSRecoveryExpandsTopLevelErrorLines(t *testing.T) {
	src := []byte("[1003sub0]\nParameterName=Number of errors\nObjectType=0x7\n;StorageLocation=RAM\nDataType=0x0005\nAccessType=rw\nDefaultValue=\nPDOMapping=0\n")
	lang := grammars.EdsLanguage()
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()
	root := tree.RootNode()
	errNode := root.Child(2)
	if errNode == nil || errNode.Type(lang) != "ERROR" {
		t.Fatalf("root child 2 = %v, want ERROR; tree=%s", errNode, root.SExpr(lang))
	}
	if got, want := errNode.StartByte(), uint32(78); got != want {
		t.Fatalf("ERROR StartByte = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	if got, want := errNode.ChildCount(), 9; got != want {
		t.Fatalf("ERROR child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	if got, want := errNode.Child(6).StartByte(), uint32(122); got != want {
		t.Fatalf("empty-value recovery child StartByte = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
}
