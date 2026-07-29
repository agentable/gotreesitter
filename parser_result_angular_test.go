package gotreesitter_test

import (
	"bytes"
	"testing"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestAngularLetNonNullAssertionMatchesCRecoveryShape(t *testing.T) {
	src := []byte("<div>@let itemLabel = item.label!;</div>")
	lang := grammars.AngularLanguage()
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()

	root := tree.RootNode()
	let := firstNodeByType(root, lang, "let_statement")
	if let == nil {
		t.Fatalf("missing let_statement: %s", root.SExpr(lang))
	}
	if got, want := let.ChildCount(), 5; got != want {
		t.Fatalf("let_statement child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	errNode := let.Child(3)
	if got := errNode.Type(lang); got != "ERROR" {
		t.Fatalf("let child[3] type = %q, want ERROR; tree=%s", got, root.SExpr(lang))
	}
	if !errNode.IsExtra() {
		t.Fatalf("let ERROR IsExtra = false, want true")
	}
	if got, want := errNode.StartByte(), uint32(32); got != want {
		t.Fatalf("ERROR start = %d, want %d", got, want)
	}
	if got, want := errNode.EndByte(), uint32(33); got != want {
		t.Fatalf("ERROR end = %d, want %d", got, want)
	}
	if got, want := errNode.ChildCount(), 1; got != want {
		t.Fatalf("ERROR child count = %d, want %d", got, want)
	}
	if got := errNode.Child(0).Type(lang); got != "unary_operator" {
		t.Fatalf("ERROR child type = %q, want unary_operator", got)
	}
	if !root.HasError() {
		t.Fatalf("root HasError = false, want true")
	}
}

func TestAngularBinaryNonNullAssertionMatchesCRecoveryShape(t *testing.T) {
	src := []byte("<div>@if (item.level! > 1) { ok }</div>")
	lang := grammars.AngularLanguage()
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()

	root := tree.RootNode()
	binary := firstNodeByType(root, lang, "binary_expression")
	if binary == nil {
		t.Fatalf("missing binary_expression: %s", root.SExpr(lang))
	}
	if got, want := binary.ChildCount(), 4; got != want {
		t.Fatalf("binary_expression child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	errNode := binary.Child(1)
	if got := errNode.Type(lang); got != "ERROR" {
		t.Fatalf("binary child[1] type = %q, want ERROR; tree=%s", got, root.SExpr(lang))
	}
	if !errNode.IsExtra() {
		t.Fatalf("binary ERROR IsExtra = false, want true")
	}
	bang := uint32(bytes.IndexByte(src, '!'))
	if got := errNode.StartByte(); got != bang {
		t.Fatalf("ERROR start = %d, want %d", got, bang)
	}
	if got := errNode.EndByte(); got != bang+1 {
		t.Fatalf("ERROR end = %d, want %d", got, bang+1)
	}
	if got, want := errNode.ChildCount(), 1; got != want {
		t.Fatalf("ERROR child count = %d, want %d", got, want)
	}
	if got := errNode.Child(0).Type(lang); got != "unary_operator" {
		t.Fatalf("ERROR child type = %q, want unary_operator", got)
	}
	if !root.HasError() {
		t.Fatalf("root HasError = false, want true")
	}
}

func TestAngularStrongAmpersandTextMatchesCRecoveryShape(t *testing.T) {
	src := []byte("<strong>Opinionated & versatile,</strong>")
	lang := grammars.AngularLanguage()
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()

	root := tree.RootNode()
	strong := firstNodeByType(root, lang, "element")
	if strong == nil {
		t.Fatalf("missing strong element: %s", root.SExpr(lang))
	}
	if got, want := strong.ChildCount(), 4; got != want {
		t.Fatalf("strong child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	errNode := strong.Child(2)
	if got := errNode.Type(lang); got != "ERROR" {
		t.Fatalf("strong child[2] type = %q, want ERROR; tree=%s", got, root.SExpr(lang))
	}
	if !errNode.IsExtra() {
		t.Fatalf("strong ERROR IsExtra = false, want true")
	}
	if got, want := errNode.StartByte(), uint32(bytes.Index(src, []byte("& versatile,"))); got != want {
		t.Fatalf("ERROR start = %d, want %d", got, want)
	}
	if got, want := errNode.EndByte(), uint32(bytes.Index(src, []byte("</strong>"))); got != want {
		t.Fatalf("ERROR end = %d, want %d", got, want)
	}
	wantTypes := []string{"ERROR", "regular_expression_flags", "ERROR", "s", "ERROR", "regular_expression_flags", "ERROR", ","}
	if got, want := errNode.ChildCount(), len(wantTypes); got != want {
		t.Fatalf("ERROR child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	for i, want := range wantTypes {
		if got := errNode.Child(i).Type(lang); got != want {
			t.Fatalf("ERROR child[%d] type = %q, want %q; tree=%s", i, got, want, root.SExpr(lang))
		}
	}
	if !root.HasError() {
		t.Fatalf("root HasError = false, want true")
	}
}

func firstNodeByType(root *gts.Node, lang *gts.Language, typ string) *gts.Node {
	if root == nil {
		return nil
	}
	if root.Type(lang) == typ {
		return root
	}
	for i := 0; i < root.ChildCount(); i++ {
		if found := firstNodeByType(root.Child(i), lang, typ); found != nil {
			return found
		}
	}
	return nil
}
