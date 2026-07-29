package cgoharness

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
)

func TestIsAcceptedFullSpanCleanGoTreeRejectsIncompleteAndErrorRoots(t *testing.T) {
	src := []byte("abcdef")
	tests := []struct {
		name  string
		root  *gotreesitter.Node
		clean bool
	}{
		{
			name:  "accepted-full-span",
			root:  gotreesitter.NewLeafNode(1, true, 0, 6, gotreesitter.Point{}, gotreesitter.Point{Column: 6}),
			clean: true,
		},
		{
			name: "prefix-start",
			root: gotreesitter.NewLeafNode(1, true, 1, 6,
				gotreesitter.Point{Column: 1}, gotreesitter.Point{Column: 6}),
		},
		{
			name: "truncated-end",
			root: gotreesitter.NewLeafNode(1, true, 0, 5, gotreesitter.Point{}, gotreesitter.Point{Column: 5}),
		},
		{
			name: "full-span-error-root",
			// 65535 is Tree-sitter's stable ERROR symbol.
			root: gotreesitter.NewLeafNode(gotreesitter.Symbol(65535), true, 0, 6,
				gotreesitter.Point{}, gotreesitter.Point{Column: 6}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tree := gotreesitter.NewTree(tc.root, src, nil)
			defer tree.Release()
			if got := IsAcceptedFullSpanCleanGoTree(tree, len(src)); got != tc.clean {
				t.Fatalf("IsAcceptedFullSpanCleanGoTree()=%v, want %v", got, tc.clean)
			}
		})
	}
}
