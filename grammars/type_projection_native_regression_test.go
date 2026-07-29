//go:build !grammar_subset

package grammars

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/internal/benchfixtures"
)

type typeProjectionRetirementCase struct {
	name                   string
	source                 []byte
	language               *gotreesitter.Language
	assert                 func(*testing.T, *gotreesitter.Node, *gotreesitter.Language, []byte)
	reuseUnsupportedReason string
	compactDecline         bool
}

func TestTypeProjectionNeedsNoResultCompatibility(t *testing.T) {
	for _, test := range typeProjectionRetirementCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			tree, err := gotreesitter.NewParser(test.language).
				ParseNoResultCompatibilityBenchmarkOnly(test.source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(tree.Release)
			test.assert(t, tree.RootNode(), test.language, test.source)
		})
	}
}

func TestTypeProjectionRoutes(t *testing.T) {
	gotreesitter.SetGLRForestEnabled(false)
	t.Cleanup(func() { gotreesitter.SetGLRForestEnabled(true) })

	for _, test := range typeProjectionRetirementCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			source := append(append([]byte(nil), test.source...), '\n')

			productionParser := gotreesitter.NewParser(test.language)
			productionParser.SetAdmissionCandidateRoute(false)
			production, err := productionParser.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(production.Release)
			test.assert(t, production.RootNode(), test.language, source)

			routedBefore, fallbackBefore := gotreesitter.AdmissionCandidateCounters()
			compactParser := gotreesitter.NewParser(test.language)
			compactParser.SetAdmissionCandidateRoute(true)
			compact, err := compactParser.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(compact.Release)
			test.assert(t, compact.RootNode(), test.language, source)
			routedAfter, fallbackAfter := gotreesitter.AdmissionCandidateCounters()
			if test.compactDecline {
				if routedAfter != routedBefore || fallbackAfter != fallbackBefore+1 {
					t.Fatalf(
						"compact decline counters routed=%d/%d fallback=%d/%d",
						routedBefore,
						routedAfter,
						fallbackBefore,
						fallbackAfter,
					)
				}
			} else if routedAfter != routedBefore+1 || fallbackAfter != fallbackBefore {
				t.Fatalf(
					"compact route counters routed=%d/%d fallback=%d/%d",
					routedBefore,
					routedAfter,
					fallbackBefore,
					fallbackAfter,
				)
			}

			gotreesitter.SetGLRForestEnabled(true)
			forestParser := gotreesitter.NewParser(test.language)
			forest, ok := forestParser.ParseForestExperimental(source)
			gotreesitter.SetGLRForestEnabled(false)
			if !ok || forest == nil {
				offset, symbol, reason, _ := forestParser.ForestDeclineInfo()
				t.Fatalf("forest declined at %d symbol=%d reason=%s", offset, symbol, reason)
			}
			t.Cleanup(forest.Release)
			test.assert(t, forest.RootNode(), test.language, source)
			if !forest.ParseRuntime().ForestFastPath {
				t.Fatal("forest parse did not report the forest route")
			}

			incrementalParser := gotreesitter.NewParser(test.language)
			incrementalParser.SetAdmissionCandidateRoute(false)
			oldTree, err := incrementalParser.Parse(test.source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(oldTree.Release)
			endPoint := retiredDispatchPointAtByte(test.source, len(test.source))
			oldTree.Edit(gotreesitter.InputEdit{
				StartByte:   uint32(len(test.source)),
				OldEndByte:  uint32(len(test.source)),
				NewEndByte:  uint32(len(source)),
				StartPoint:  endPoint,
				OldEndPoint: endPoint,
				NewEndPoint: gotreesitter.Point{Row: endPoint.Row + 1},
			})
			incremental, profile, err := incrementalParser.ParseIncrementalProfiled(source, oldTree)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(incremental.Release)
			test.assert(t, incremental.RootNode(), test.language, source)
			if test.reuseUnsupportedReason != "" {
				if !profile.ReuseUnsupported ||
					profile.ReuseUnsupportedReason != test.reuseUnsupportedReason {
					t.Fatalf("incremental reuse status = %+v", profile)
				}
			} else if !profile.OldTreeReuseRoute || profile.ReusedSubtrees == 0 ||
				profile.ReusedBytes == 0 {
				t.Fatalf("incremental route did not reuse the old tree: %+v", profile)
			}

			want := typeProjectionTreeDigest(t, production, test.language)
			for route, tree := range map[string]*gotreesitter.Tree{
				"compact":     compact,
				"forest":      forest,
				"incremental": incremental,
			} {
				if got := typeProjectionTreeDigest(t, tree, test.language); got != want {
					t.Fatalf("%s digest = %s, want %s", route, got, want)
				}
			}
		})
	}
}

func typeProjectionRetirementCases() []typeProjectionRetirementCase {
	return []typeProjectionRetirementCase{
		{
			name:                   "arduino_builtin_primitive_types",
			source:                 []byte("int f(void) { char c = 0; return c; }"),
			language:               ArduinoLanguage(),
			assert:                 assertArduinoBuiltinPrimitiveTypes,
			reuseUnsupportedReason: "external_scanner_unsupported",
		},
		{
			name:           "objc_protocol_type_identifier",
			source:         []byte("@interface CallbackClient : NSObject <ClientProtocol>\n@end"),
			language:       ObjcLanguage(),
			assert:         assertObjcProtocolTypeIdentifier,
			compactDecline: true,
		},
	}
}

func typeProjectionTreeDigest(
	t *testing.T,
	tree *gotreesitter.Tree,
	language *gotreesitter.Language,
) string {
	t.Helper()
	receipt, err := benchfixtures.InspectGoTree(tree.RootNode(), language)
	if err != nil {
		t.Fatal(err)
	}
	return receipt.SHA256
}

func assertArduinoBuiltinPrimitiveTypes(
	t *testing.T,
	root *gotreesitter.Node,
	language *gotreesitter.Language,
	source []byte,
) {
	t.Helper()
	for _, text := range []string{"int", "void", "char"} {
		node := findTypeProjectionNode(root, language, source, "primitive_type", text)
		if node == nil || !node.IsNamed() || node.ChildCount() != 0 {
			t.Fatalf("Arduino primitive %q = %v: %s", text, node, root.SExpr(language))
		}
	}
}

func assertObjcProtocolTypeIdentifier(
	t *testing.T,
	root *gotreesitter.Node,
	language *gotreesitter.Language,
	source []byte,
) {
	t.Helper()
	assertTypeProjectionLeaf(t, root, language, source, "type_identifier", "ClientProtocol")
}

func assertTypeProjectionLeaf(
	t *testing.T,
	root *gotreesitter.Node,
	language *gotreesitter.Language,
	source []byte,
	nodeType string,
	text string,
) {
	t.Helper()
	node := findTypeProjectionNode(root, language, source, nodeType, text)
	if node == nil || !node.IsNamed() || node.ChildCount() != 0 {
		t.Fatalf("%s %q = %v: %s", nodeType, text, node, root.SExpr(language))
	}
}

func findTypeProjectionNode(
	node *gotreesitter.Node,
	language *gotreesitter.Language,
	source []byte,
	nodeType string,
	text string,
) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.Type(language) == nodeType && node.Text(source) == text {
		return node
	}
	for index := 0; index < node.ChildCount(); index++ {
		if found := findTypeProjectionNode(
			node.Child(index),
			language,
			source,
			nodeType,
			text,
		); found != nil {
			return found
		}
	}
	return nil
}
