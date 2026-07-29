package grammars

import (
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
)

func TestFieldProjectionNativeRetiredDispatchArms(t *testing.T) {
	tests := fieldProjectionRetirementCases()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			tree, err := gotreesitter.NewParser(test.language).ParseNoResultCompatibilityBenchmarkOnly(test.source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(tree.Release)
			test.assert(t, tree.RootNode(), test.language)
		})
	}
}

func TestFieldProjectionRetiredDispatchArmRoutes(t *testing.T) {
	tests := fieldProjectionRetirementCases()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if test.name == "haskell_root_sections" || test.name == "erlang_root_forms" {
				assertFieldProjectionProductionIncrementalRoutes(t, test)
				return
			}
			for _, receipt := range retiredDispatchRouteReceipts(t, test.language, test.source) {
				t.Run(receipt.name, func(t *testing.T) {
					test.assert(t, receipt.tree.RootNode(), test.language)
					if receipt.name == "incremental" {
						profile := receipt.incrementalProfile
						if test.reuseUnsupportedReason != "" {
							if !profile.ReuseUnsupported || profile.ReuseUnsupportedReason != test.reuseUnsupportedReason {
								t.Fatalf("incremental reuse status = %+v", profile)
							}
						} else if !profile.OldTreeReuseRoute || profile.ReusedSubtrees == 0 || profile.ReusedBytes == 0 {
							t.Fatalf("incremental route did not reuse the old tree: %+v", profile)
						}
					}
				})
			}
		})
	}
}

func assertFieldProjectionProductionIncrementalRoutes(
	t *testing.T,
	test fieldProjectionRetirementCase,
) {
	t.Helper()
	source := append(append([]byte(nil), test.source...), '\n')
	parser := gotreesitter.NewParser(test.language)
	parser.SetAdmissionCandidateRoute(false)
	production, err := parser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(production.Release)
	test.assert(t, production.RootNode(), test.language)

	oldTree, err := parser.Parse(test.source)
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
	incremental, profile, err := parser.ParseIncrementalProfiled(source, oldTree)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(incremental.Release)
	test.assert(t, incremental.RootNode(), test.language)
	if test.reuseUnsupportedReason != "" {
		if !profile.ReuseUnsupported || profile.ReuseUnsupportedReason != test.reuseUnsupportedReason {
			t.Fatalf("incremental reuse status = %+v", profile)
		}
		return
	}
	if !profile.OldTreeReuseRoute || profile.ReusedSubtrees == 0 || profile.ReusedBytes == 0 {
		t.Fatalf("incremental route did not reuse the old tree: %+v", profile)
	}
}

type fieldProjectionRetirementCase struct {
	name     string
	source   []byte
	language *gotreesitter.Language
	assert   func(*testing.T, *gotreesitter.Node, *gotreesitter.Language)
	// reuseUnsupportedReason records a locked grammar limitation.
	reuseUnsupportedReason string
}

func fieldProjectionRetirementCases() []fieldProjectionRetirementCase {
	return []fieldProjectionRetirementCase{
		{
			name:                   "lua_local_declarations",
			source:                 []byte("local function foo() end\nlocal x = 1"),
			language:               LuaLanguage(),
			assert:                 assertLuaLocalDeclarationFields,
			reuseUnsupportedReason: "external_scanner_unsupported",
		},
		{
			name:     "make_conditional_consequence",
			source:   []byte("ifneq (a,b)\n\t@echo x\nelse\n\t@echo y\nendif"),
			language: MakeLanguage(),
			assert:   assertMakeConsequenceFields,
		},
		{
			name:     "zig_initializer_list",
			source:   []byte("const x = .{};\nconst y = .{\"x\"};"),
			language: ZigLanguage(),
			assert:   assertZigInitializerListFields,
		},
		{
			name:                   "haskell_root_sections",
			source:                 []byte("{-# LANGUAGE OverloadedStrings #-}\n-- | module docs\nmodule Main where\nimport Data.List\n\nx = 1"),
			language:               HaskellLanguage(),
			assert:                 assertHaskellRootSectionFields,
			reuseUnsupportedReason: "external_scanner_unsupported",
		},
		{
			name:     "erlang_root_forms",
			source:   []byte("% file comment\n-module(test).\n-export([main/0]).\nmain() ->\n  ok % inner comment\n  ."),
			language: ErlangLanguage(),
			assert:   assertErlangRootFormFields,
		},
	}
}

func assertLuaLocalDeclarationFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	if root == nil || root.Type(lang) != "chunk" || root.ChildCount() != 2 {
		t.Fatalf("unexpected Lua root: %v", root)
	}
	for index := 0; index < root.ChildCount(); index++ {
		if got := root.FieldNameForChild(index, lang); got != "local_declaration" {
			t.Fatalf("Lua child %d field = %q, want local_declaration", index, got)
		}
	}
}

func assertMakeConsequenceFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	conditional := findFirstNamedDescendantWhere(root, lang, "conditional", func(*gotreesitter.Node) bool { return true })
	if conditional == nil || conditional.ChildCount() < 4 {
		t.Fatalf("missing Make conditional: %s", root.SExpr(lang))
	}
	assertFieldName(t, conditional, lang, 1, "\t", "consequence")
	assertFieldName(t, conditional, lang, 2, "recipe_line", "consequence")

	elseDirective := conditional.Child(3)
	if elseDirective == nil || elseDirective.Type(lang) != "else_directive" || elseDirective.ChildCount() < 3 {
		t.Fatalf("missing Make else directive: %s", root.SExpr(lang))
	}
	assertFieldName(t, elseDirective, lang, 1, "\t", "consequence")
	assertFieldName(t, elseDirective, lang, 2, "recipe_line", "consequence")
}

func assertZigInitializerListFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	count := 0
	var visit func(*gotreesitter.Node)
	visit = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if node.Type(lang) == "anonymous_struct_initializer" {
			count++
			assertFieldName(t, node, lang, 1, "initializer_list", "")
		}
		for index := 0; index < node.ChildCount(); index++ {
			visit(node.Child(index))
		}
	}
	visit(root)
	if count != 2 {
		t.Fatalf("Zig initializer count = %d, want 2: %s", count, root.SExpr(lang))
	}
}

func assertHaskellRootSectionFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	if root == nil || root.HasError() || root.Type(lang) != "haskell" {
		t.Fatalf("unexpected Haskell root: %v", root)
	}
	wantFields := map[string]string{
		"imports":      "imports",
		"declarations": "declarations",
	}
	for index := 0; index < root.ChildCount(); index++ {
		want := wantFields[root.Child(index).Type(lang)]
		if got := root.FieldNameForChild(index, lang); got != want {
			t.Fatalf("Haskell child %d field = %q, want %q", index, got, want)
		}
	}
}

func assertErlangRootFormFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	if root == nil || root.HasError() || root.Type(lang) != "source_file" {
		t.Fatalf("unexpected Erlang root: %v", root)
	}
	for index := 0; index < root.ChildCount(); index++ {
		want := "forms_only"
		if root.Child(index).IsExtra() {
			want = ""
		}
		if got := root.FieldNameForChild(index, lang); got != want {
			t.Fatalf("Erlang child %d field = %q, want %q", index, got, want)
		}
	}
}

func assertFieldName(
	t *testing.T,
	parent *gotreesitter.Node,
	lang *gotreesitter.Language,
	index int,
	wantType string,
	wantField string,
) {
	t.Helper()
	child := parent.Child(index)
	if child == nil || child.Type(lang) != wantType {
		t.Fatalf("child %d type = %v, want %s", index, child, wantType)
	}
	if got := parent.FieldNameForChild(index, lang); got != wantField {
		t.Fatalf("child %d field = %q, want %q", index, got, wantField)
	}
}
