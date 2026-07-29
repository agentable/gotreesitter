//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"fmt"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestTypeProjectionRetirementCOracleParity(t *testing.T) {
	tests := []parityCase{
		{
			name:   "arduino",
			source: "int f(void) { char c = 0; return c; }\n",
		},
		{
			name:   "objc",
			source: "@interface CallbackClient : NSObject <ClientProtocol>\n@end\n",
		},
	}
	for index, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%s_%d", test.name, index), func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			source := []byte(test.source)
			entry, ok := parityEntriesByName[test.name]
			if !ok {
				t.Fatalf("missing grammar entry for %s", test.name)
			}
			language := entry.Language()
			goTree, err := gotreesitter.NewParser(language).
				ParseNoResultCompatibilityBenchmarkOnly(source)
			if err != nil {
				t.Fatal(err)
			}
			defer goTree.Release()

			cLanguage, err := ParityCLanguage(test.name)
			if err != nil {
				t.Fatal(err)
			}
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLanguage); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C oracle returned a nil tree")
			}
			defer cTree.Close()

			var divergences []string
			compareNodes(
				goTree.RootNode(),
				language,
				cTree.RootNode(),
				"root",
				&divergences,
			)
			if len(divergences) != 0 {
				t.Fatalf("C-oracle divergences: %v", divergences)
			}
		})
	}
}
