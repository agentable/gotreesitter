package gotreesitter_test

import (
	"testing"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestLedgerRecoveredDateSuffixError(t *testing.T) {
	src := []byte("\n15.03.2006 Exxon\n    Expenses:Auto:Gas          10,00 EUR\n    Liabilities:MasterCard    -10,00 EUR\n")
	lang := grammars.LedgerLanguage()
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()

	plain := firstLedgerNodeByType(tree.RootNode(), lang, "plain_xact")
	if plain == nil {
		t.Fatalf("missing plain_xact: %s", tree.RootNode().SExpr(lang))
	}
	if got, want := plain.ChildCount(), 7; got != want {
		t.Fatalf("plain_xact child count = %d, want %d; tree=%s", got, want, tree.RootNode().SExpr(lang))
	}
	errNode := plain.Child(1)
	if got := errNode.Type(lang); got != "ERROR" {
		t.Fatalf("child[1] type = %q, want ERROR; tree=%s", got, tree.RootNode().SExpr(lang))
	}
	if got, want := errNode.StartByte(), uint32(9); got != want {
		t.Fatalf("ERROR start = %d, want %d", got, want)
	}
	if got, want := errNode.EndByte(), uint32(11); got != want {
		t.Fatalf("ERROR end = %d, want %d", got, want)
	}
}

func TestLedgerYearDirectiveRecoversSourceFileRoot(t *testing.T) {
	src := []byte(`
--input-date-format %d.%m

Y2010
03.01 * Foo
    A                 10.00 EUR
    B

05.02 * Bar
    A                 20.00 EUR
    B

test reg A
10-Jan-03 Foo                   A                         10.00 EUR    10.00 EUR
10-Feb-05 Bar                   A                         20.00 EUR    30.00 EUR
end test

`)
	lang := grammars.LedgerLanguage()
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		t.Fatalf("parse failed: tree=%v err=%v", tree, err)
	}
	defer tree.Release()

	root := tree.RootNode()
	if got := root.Type(lang); got != "source_file" {
		t.Fatalf("root type = %q, want source_file; tree=%s", got, root.SExpr(lang))
	}
	if got, want := root.ChildCount(), 12; got != want {
		t.Fatalf("root child count = %d, want %d; tree=%s", got, want, root.SExpr(lang))
	}
	if got := root.Child(3).Type(lang); got != "ERROR" {
		t.Fatalf("child[3] type = %q, want ERROR; tree=%s", got, root.SExpr(lang))
	}
	if got, want := root.Child(3).StartByte(), uint32(28); got != want {
		t.Fatalf("year ERROR start = %d, want %d", got, want)
	}
	if got, want := root.Child(3).EndByte(), uint32(33); got != want {
		t.Fatalf("year ERROR end = %d, want %d", got, want)
	}
	if got := root.Child(5).Type(lang); got != "journal_item" {
		t.Fatalf("child[5] type = %q, want journal_item", got)
	}
	if got := root.Child(7).Type(lang); got != "journal_item" {
		t.Fatalf("child[7] type = %q, want journal_item", got)
	}
	if got := root.Child(9).Type(lang); got != "journal_item" {
		t.Fatalf("child[9] type = %q, want journal_item", got)
	}
}

func firstLedgerNodeByType(root *gts.Node, lang *gts.Language, typ string) *gts.Node {
	if root == nil {
		return nil
	}
	if root.Type(lang) == typ {
		return root
	}
	for i := 0; i < root.ChildCount(); i++ {
		if got := firstLedgerNodeByType(root.Child(i), lang, typ); got != nil {
			return got
		}
	}
	return nil
}
