package cgoharness

import gotreesitter "github.com/agentable/gotreesitter"

// IsAcceptedFullSpanCleanGoTree reports whether a successful Go parse produced
// the corpus-policy clean result shared by perf_scan and parse_gap_report: the
// parse accepted without stopping early, its root covers exactly
// [0, sourceBytes), and neither the root nor any descendant is an ERROR node.
// Parse-call errors are handled by callers before this tree-only predicate.
func IsAcceptedFullSpanCleanGoTree(tree *gotreesitter.Tree, sourceBytes int) bool {
	if tree == nil || sourceBytes < 0 || uint64(sourceBytes) > uint64(^uint32(0)) || tree.ParseStoppedEarly() {
		return false
	}
	root := tree.RootNode()
	if root == nil {
		return false
	}
	return root.StartByte() == 0 &&
		root.EndByte() == uint32(sourceBytes) &&
		!root.HasError() &&
		!root.IsError()
}
