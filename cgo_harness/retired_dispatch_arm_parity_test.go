//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestRetiredDispatchArmCOracleParity(t *testing.T) {
	tests := []struct {
		name     string
		language string
		source   string
		diverges string
	}{
		{
			name:     "html_mismatched_custom_close",
			language: "html",
			source:   "<my-a><my-b><my-c>text</my-x>\n",
			diverges: `root: ChildCount go=3 c=2`,
		},
		{
			name:     "html_autoclosing_custom_chain",
			language: "html",
			source:   "<div><xyz><xyz><xyz>text</div>\n",
		},
		{name: "ocaml_boolean_true", language: "ocaml", source: "let x = true\n"},
		{name: "ocaml_boolean_false", language: "ocaml", source: "let x = false\n"},
		{name: "ocaml_and_ampersand", language: "ocaml", source: "let x = a & b\n"},
		{name: "ocaml_and_double_ampersand", language: "ocaml", source: "let x = a && b\n"},
		{name: "ocaml_or_keyword", language: "ocaml", source: "let x = a or b\n"},
		{name: "ocaml_or_double_bar", language: "ocaml", source: "let x = a || b\n"},
		{
			name:     "ruby_module_bounds",
			language: "ruby",
			source:   "# comment\nmodule Foo\nend\n",
		},
		{
			name:     "lua_local_declarations",
			language: "lua",
			source:   "local function foo() end\nlocal x = 1\n",
		},
		{
			name:     "make_conditional_consequence",
			language: "make",
			source:   "ifneq (a,b)\n\t@echo x\nelse\n\t@echo y\nendif\n",
		},
		{
			name:     "zig_initializer_list",
			language: "zig",
			source:   "const x = .{};\nconst y = .{\"x\"};\n",
		},
		{
			name:     "caddy_sole_child_line_break",
			language: "caddy",
			source:   ":8080 {\n}\n",
		},
		{
			name:     "comment_hidden_trailing_extra",
			language: "comment",
			source:   "one line\n",
		},
		{
			name:     "fortran_statement_line_breaks",
			language: "fortran",
			source:   "program hello\n  implicit none\nend program hello\n",
		},
		{
			name:     "just_assignment_line_break",
			language: "just",
			source:   "name := \"value\"\n",
		},
		{
			name:     "nginx_attribute_line_breaks",
			language: "nginx",
			source:   "http {\n  server {\n    listen 80;\n    server_name example.com;\n  }\n}\n",
		},
		{
			name:     "nim_child_excludes_trailing_line_break",
			language: "nim",
			source:   "echo \"hello\"\n",
		},
		{
			name:     "pascal_program_excludes_trailing_line_break",
			language: "pascal",
			source:   "program hello;\nbegin\nend.\n",
		},
		{
			name:     "pug_sole_child_line_break",
			language: "pug",
			source:   "p hello\n",
		},
		{
			name:     "rst_section_excludes_trailing_line_break",
			language: "rst",
			source:   "Title\n=====\n",
		},
		{
			name:     "bibtex_leading_trivia_root",
			language: "bibtex",
			source:   "\n@article{key, author={A}}\n",
		},
		{
			name:     "cpon_leading_trivia_root",
			language: "cpon",
			source:   "\n{\"a\":1}\n",
		},
		{
			name:     "cue_leading_trivia_root",
			language: "cue",
			source:   "\nx: 1\n",
		},
		{
			name:     "d_leading_trivia_root",
			language: "d",
			source:   "\nmodule m;\nint main() { return 0; }\n",
		},
		{
			name:     "d_module_bounds",
			language: "d",
			source:   "// license\n\nmodule test;\nimport std.stdio;\n",
		},
		{
			name:     "forth_leading_trivia_root",
			language: "forth",
			source:   "\n: square dup * ;\n",
		},
		{
			name:     "kotlin_leading_trivia_root",
			language: "kotlin",
			source:   "\nfun main() {}\n",
		},
		{
			name:     "squirrel_leading_trivia_root",
			language: "squirrel",
			source:   "\nlocal x = 1;\n",
		},
		{
			name:     "cue_alias_map_value_child",
			language: "cue",
			source:   "x: sub\n",
		},
		{
			name:     "gitcommit_alias_map_message_child",
			language: "gitcommit",
			source:   "subject\n\nbody line\n",
		},
		{
			name:     "r_alias_map_string_content_child",
			language: "r",
			source:   "\"\\\\\"\n",
		},
		{
			name:     "haskell_section_spans",
			language: "haskell",
			source:   "module M where\nimport A\nimport B\n\nvalue = 1\n",
		},
		{
			name:     "hcl_hidden_root_trivia",
			language: "hcl",
			source:   "# first\n\n# second\n\nvalue = true\n",
		},
		{
			name:     "erlang_function_clause_replacement",
			language: "erlang",
			source:   "-define(X, foo() -> ok; foo() -> not_ok).\n",
		},
		{
			name:     "erlang_case_clause_replacement",
			language: "erlang",
			source:   "-define(X, [] -> ok; [_ | _] -> not_ok).\n",
		},
		{
			name:     "erlang_top_level_form_bounds",
			language: "erlang",
			source:   "% lead\n-module(sample).\nfoo() -> ok.\n",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			if test.diverges != "" {
				divergences := retiredDispatchArmCOracleDivergences(
					t,
					parityCase{name: test.language, source: test.source},
					[]byte(test.source),
				)
				if len(divergences) != 1 || !strings.Contains(divergences[0], test.diverges) {
					t.Fatalf("C-oracle divergences = %v, want only %q", divergences, test.diverges)
				}
				return
			}
			runParityCase(
				t,
				parityCase{name: test.language, source: test.source},
				"retired-dispatch-arm",
				[]byte(test.source),
			)
		})
	}
}

func retiredDispatchArmCOracleDivergences(t *testing.T, test parityCase, source []byte) []string {
	t.Helper()
	cLanguage, err := ParityCLanguage(test.name)
	if err != nil {
		t.Fatal(err)
	}
	goTree, goLanguage, err := parseWithGo(test, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGoTree(goTree)

	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLanguage); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C reference parser returned a nil tree")
	}
	defer cTree.Close()

	var divergences []string
	compareNodes(goTree.RootNode(), goLanguage, cTree.RootNode(), "root", &divergences)
	return divergences
}

func TestHTMLRecoveredNestedCustomTagRangeCOracleParity(t *testing.T) {
	scheduleParityMemoryScavenge(t)

	const sourceText = "<div><xyz><xyz>text</div>\n"
	source := []byte(sourceText)
	test := parityCase{name: "html", source: sourceText}
	goTree, goLanguage, err := parseWithGo(test, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGoTree(goTree)

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
		t.Fatal("C reference parser returned a nil tree")
	}
	defer cTree.Close()

	var divergences []string
	compareNodes(goTree.RootNode(), goLanguage, cTree.RootNode(), "root", &divergences)
	if len(divergences) != 0 {
		t.Fatalf("C-oracle divergences = %v", divergences)
	}

	firstStart := strings.Index(sourceText, "<xyz>")
	secondStart := strings.LastIndex(sourceText, "<xyz>")
	closeStart := strings.Index(sourceText, "</div>")
	if firstStart < 0 || secondStart < 0 || closeStart < 0 {
		t.Fatal("test source lacks a required HTML marker")
	}
	wantEndPoint := pointAtOffset(source, closeStart)
	for _, start := range []int{firstStart, secondStart} {
		goElement := findGoNodeByTypeAndStart(goTree.RootNode(), goLanguage, "element", uint32(start))
		if goElement == nil {
			t.Fatalf("Go tree is missing the element at %d", start)
		}
		cElement := findCNodeByKindAndStart(cTree.RootNode(), "element", uint(start))
		if cElement == nil {
			t.Fatalf("C tree is missing the element at %d", start)
		}
		if got := goElement.EndByte(); got != uint32(closeStart) {
			t.Fatalf("Go element at %d ends at %d, want %d", start, got, closeStart)
		}
		if got := cElement.EndByte(); got != uint(closeStart) {
			t.Fatalf("C element at %d ends at %d, want %d", start, got, closeStart)
		}
		if got := goElement.EndPoint(); got != wantEndPoint {
			t.Fatalf("Go element at %d ends at %#v, want %#v", start, got, wantEndPoint)
		}
		cEndPoint := cElement.EndPosition()
		if uint32(cEndPoint.Row) != wantEndPoint.Row || uint32(cEndPoint.Column) != wantEndPoint.Column {
			t.Fatalf("C element at %d ends at %#v, want %#v", start, cEndPoint, wantEndPoint)
		}
	}
}

func findGoNodeByTypeAndStart(
	node *gotreesitter.Node,
	lang *gotreesitter.Language,
	typ string,
	start uint32,
) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.Type(lang) == typ && node.StartByte() == start {
		return node
	}
	for index := 0; index < node.ChildCount(); index++ {
		if found := findGoNodeByTypeAndStart(node.Child(index), lang, typ, start); found != nil {
			return found
		}
	}
	return nil
}

func findCNodeByKindAndStart(node *sitter.Node, kind string, start uint) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.Kind() == kind && node.StartByte() == start {
		return node
	}
	for index := uint(0); index < node.ChildCount(); index++ {
		if found := findCNodeByKindAndStart(node.Child(index), kind, start); found != nil {
			return found
		}
	}
	return nil
}
