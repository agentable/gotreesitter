package gotreesitter_test

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestParseIniMypyDeferredCompatibilityParentLinks(t *testing.T) {
	source := []byte("[mypy]\nfiles = Tools/check-c-api-docs/\npretty = True\n\n# We need `_colorize` import:\nmypy_path = $MYPY_CONFIG_FILE_DIR/../../Misc/mypy\n\n# Make sure Python can still be built\n# using Python 3.13 for `PYTHON_FOR_REGEN`...\npython_version = 3.13\n\n# ...And be strict:\nstrict = True\nextra_checks = True\nenable_error_code = \n    ignore-without-code,\n    redundant-expr,\n    truthy-bool,\n    possibly-undefined,\n")
	lang := grammars.IniLanguage()
	tree, err := gotreesitter.NewParser(lang).Parse(source)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	t.Cleanup(tree.Release)

	root := tree.RootNode()
	if got := tree.ParseStopReason(); got != gotreesitter.ParseStopAccepted {
		t.Fatalf("ParseStopReason = %q, want %q; root=%s", got, gotreesitter.ParseStopAccepted, root.SExpr(lang))
	}
	if rt := tree.ParseRuntime(); rt.Truncated {
		t.Fatalf("ParseRuntime.Truncated = true; runtime=%s", rt.Summary())
	}
	if got := root.Type(lang); got != "document" {
		t.Fatalf("root type = %q, want document; root=%s", got, root.SExpr(lang))
	}
	if got := root.ChildCount(); got != 2 {
		t.Fatalf("root child count = %d, want 2; root=%s", got, root.SExpr(lang))
	}
	assertParentLinks(t, lang, root)
}

func assertParentLinks(t *testing.T, lang *gotreesitter.Language, parent *gotreesitter.Node) {
	t.Helper()
	for i := 0; i < parent.ChildCount(); i++ {
		child := parent.Child(i)
		if child == nil {
			continue
		}
		if got := child.Parent(); got != parent {
			t.Fatalf("child[%d] %s parent = %p, want %p", i, child.Type(lang), got, parent)
		}
		assertParentLinks(t, lang, child)
	}
}
