//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"

	"github.com/agentable/gotreesitter"
)

func firstGoNamedChildByType(root *gotreesitter.Node, lang *gotreesitter.Language, typ string) *gotreesitter.Node {
	if root == nil {
		return nil
	}
	for i := 0; i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child != nil && child.Type(lang) == typ {
			return child
		}
	}
	return nil
}

func assertGoIssue3FieldLookups(t *testing.T, src []byte) {
	t.Helper()
	tc := parityCase{name: "go", source: string(src)}
	tree, lang, err := parseWithGo(tc, src, nil)
	if err != nil {
		t.Fatalf("go parse failed: %v", err)
	}
	defer releaseGoTree(tree)

	fn := firstGoNamedChildByType(tree.RootNode(), lang, "function_declaration")
	if fn == nil {
		t.Fatalf("function_declaration not found in root")
	}

	checks := map[string]string{
		"name":       "identifier",
		"parameters": "parameter_list",
		"result":     "type_identifier",
		"body":       "block",
	}
	for field, wantType := range checks {
		child := fn.ChildByFieldName(field, lang)
		if child == nil {
			t.Fatalf("ChildByFieldName(%q) returned nil", field)
		}
		if got := child.Type(lang); got != wantType {
			t.Fatalf("field %q type = %q, want %q", field, got, wantType)
		}
	}
}

func TestParityIssue3Repros(t *testing.T) {
	assertGoIssue3FieldLookups(t, []byte("package main\nfunc Hello() string { return \"hello\" }\n"))

	cases := []parityCase{
		{
			name:   "go",
			source: "package main\nfunc Hello() string { return \"hello\" }\n",
		},
		{
			name:   "python",
			source: "def greet(name):\n    return \"hello\"\n\nclass MyClass:\n    def method(self):\n        pass\n",
		},
		{
			name:   "tsx",
			source: "function App() { return <div/> }\n",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if reason := paritySkipReason(tc.name); reason != "" {
				t.Skipf("known mismatch: %s", reason)
			}
			runParityCase(t, tc, "issue3", normalizedSource(tc.name, tc.source))
		})
	}
}
