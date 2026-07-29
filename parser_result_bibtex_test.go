package gotreesitter_test

import (
	"testing"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestBibtexRecoveredMissingEntryKeyMatchesCShape(t *testing.T) {
	lang := grammars.BibtexLanguage()
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "missing comma after key",
			src:  "@article{test\n    title     = {title-from-embedded-bibtex-file},\n    author    = {author-from-embedded-bibtex-file},\n}\n",
		},
		{
			name: "empty key",
			src:  "@techreport{\n  journal = {Wirtschaftsinformatik},\n  author = {Sam and jason},\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parser := gts.NewParser(lang)
			tree, err := parser.Parse([]byte(tc.src))
			if err != nil || tree == nil || tree.RootNode() == nil {
				t.Fatalf("parse err=%v tree=%v", err, tree)
			}
			defer tree.Release()

			root := tree.RootNode()
			if got := root.Type(lang); got != "document" {
				t.Fatalf("root type = %q, want document; tree=%s", got, root.SExpr(lang))
			}
			entry := firstBibtexEntry(root, lang)
			if entry == nil || entry.ChildCount() < 5 {
				t.Fatalf("missing recovered entry; tree=%s", root.SExpr(lang))
			}
			recovery := entry.Child(1)
			if recovery == nil || recovery.Type(lang) != "ERROR" || !recovery.IsExtra() || !recovery.IsNamed() {
				t.Fatalf("entry child 1 = %v, want named extra ERROR; entry=%s", recovery, entry.SExpr(lang))
			}
			if first := recovery.Child(0); first == nil || first.Type(lang) != "{" {
				t.Fatalf("recovery first child = %v, want opening brace; entry=%s", first, entry.SExpr(lang))
			}
			if got := entry.Child(2); got == nil || got.Type(lang) != "{" {
				t.Fatalf("entry child 2 = %v, want value opening brace; entry=%s", got, entry.SExpr(lang))
			}
		})
	}
}

func firstBibtexEntry(root *gts.Node, lang *gts.Language) *gts.Node {
	if root == nil {
		return nil
	}
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child != nil && child.Type(lang) == "entry" {
			return child
		}
	}
	return nil
}
