//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func TestTypeScriptImportTypeGenericCallCParity(t *testing.T) {
	cases := []struct {
		name string
		lang string
		src  string
	}{
		{name: "typescript_typeof_import_query", lang: "typescript", src: "const x = foo<typeof import(\"some-module\")>();\n"},
		{name: "typescript_import_type_member", lang: "typescript", src: "const x = foo<import(\"some-module\").Bar>();\n"},
		{name: "tsx_typeof_import_query", lang: "tsx", src: "const x = foo<typeof import(\"some-module\")>();\n"},
		{name: "tsx_import_type_member", lang: "tsx", src: "const x = foo<import(\"some-module\").Bar>();\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := grammars.DetectLanguageByName(tc.lang)
			if entry == nil || entry.Language == nil {
				t.Fatalf("missing Go %s grammar", tc.lang)
			}
			goLang := entry.Language()
			goTree, err := gotreesitter.NewParser(goLang).Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Go parse: %v", err)
			}
			goRoot := goTree.RootNode()
			if goRoot == nil || goRoot.HasError() {
				t.Fatalf("Go parse has error: %s", goRoot.SExpr(goLang))
			}

			langName := tc.lang
			lang, err := COracleLanguage(langName)
			if err != nil {
				t.Fatal(err)
			}
			parser := sitter.NewParser()
			defer parser.Close()
			if err := parser.SetLanguage(lang); err != nil {
				t.Fatal(err)
			}
			cTree := parser.Parse([]byte(tc.src), nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("nil C tree")
			}
			defer cTree.Close()
			cRoot := cTree.RootNode()
			if cRoot.HasError() {
				t.Fatalf("C parse has error: %s", dumpCTree(cRoot, 0))
			}

			var errs []string
			compareNodes(goRoot, goLang, cRoot, "root", &errs)
			if len(errs) > 0 {
				t.Fatalf("Go/C tree mismatch:\n%s", strings.Join(errs, "\n"))
			}
		})
	}
}
