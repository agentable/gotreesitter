//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/agentable/gotreesitter"
)

func TestSQLGrammargenCGORegressionCases(t *testing.T) {
	var grammar grammargenCGOGrammar
	found := false
	for _, g := range grammargenCGOGrammars {
		if g.name == "sql" {
			grammar = g
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing sql grammargen CGO config")
	}

	gram, err := importGrammargenSource(grammar)
	if err != nil {
		t.Skipf("import unavailable: %v", err)
	}
	timeout := grammar.genTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	genLang, err := grammargenGenerate(gram, timeout)
	if err != nil {
		t.Fatalf("generate grammar: %v", err)
	}
	refLang := grammar.blobFunc()
	if refLang != nil && refLang.ExternalScanner != nil && len(genLang.ExternalSymbols) > 0 {
		if scanner, ok := gotreesitter.AdaptExternalScannerByExternalOrder(refLang, genLang); ok {
			genLang.ExternalScanner = scanner
		}
	}

	cLang, err := ParityCLanguage("sql")
	if err != nil {
		t.Skipf("C parser unavailable: %v", err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("C SetLanguage: %v", err)
	}

	genParser := gotreesitter.NewParser(genLang)
	blobParser := gotreesitter.NewParser(refLang)

	cases := []struct {
		name string
		src  string
	}{
		{name: "select_identifier_list", src: "SELECT a, b;\n"},
		{name: "select_parenthesized_boolean", src: "SELECT (TRUE);\n"},
		{name: "select_dollar_quoted_string", src: "SELECT $$hey$$;\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)

			cTree := cParser.Parse(src, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C nil tree")
			}
			defer cTree.Close()
			cRoot := cTree.RootNode()
			if cRoot.HasError() {
				t.Fatalf("C root has error:\n%s", dumpCTree(cRoot, 0))
			}

			genTree, err := genParser.Parse(src)
			if err != nil {
				t.Fatalf("generated parse error: %v", err)
			}
			genRoot := genTree.RootNode()

			blobTree, err := blobParser.Parse(src)
			if err != nil {
				t.Fatalf("blob parse error: %v", err)
			}
			blobRoot := blobTree.RootNode()

			var genVsCErrs []string
			compareNodes(genRoot, genLang, cRoot, "root", &genVsCErrs)
			if len(genVsCErrs) == 0 {
				return
			}

			var blobVsCErrs []string
			compareNodes(blobRoot, refLang, cRoot, "root", &blobVsCErrs)
			if len(blobVsCErrs) > 0 {
				t.Logf(
					"generated and blob both diverge from C; not treating as grammargen-specific regression\nGEN-vs-C:\n%s\n\nblob-vs-C:\n%s",
					joinTopErrors(genVsCErrs),
					joinTopErrors(blobVsCErrs),
				)
				return
			}

			var genVsBlobErrs []string
			compareGoTreesForLangs(genRoot, genLang, blobRoot, refLang, "root", &genVsBlobErrs)

			t.Fatalf(
				"generated-vs-C divergences:\n%s\n\ngenerated-vs-blob:\n%s\n\nblob-vs-C:\n%s\n\ngenerated:\n%s\n\nblob:\n%s\n\nc:\n%s",
				joinTopErrors(genVsCErrs),
				joinTopErrors(genVsBlobErrs),
				joinTopErrors(blobVsCErrs),
				genRoot.SExpr(genLang),
				blobRoot.SExpr(refLang),
				dumpCTree(cRoot, 0),
			)
		})
	}
}
