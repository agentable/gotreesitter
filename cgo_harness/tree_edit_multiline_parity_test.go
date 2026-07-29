//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestTreeEditMultilineCoordinatesMatchC(t *testing.T) {
	source := []byte("package p\n\nfunc f() {\n\tx := 1\n\ty := 2\n\tprintln(x, y)\n}\n")
	cLang := loadCanonicalGoCLanguage(t)

	tests := []struct {
		name        string
		startNeedle string
		endNeedle   string
		newText     string
	}{
		{name: "multiline insertion", startNeedle: "\tx := 1", endNeedle: "\tx := 1", newText: "// one\n// two\n"},
		{name: "whole-line deletion", startNeedle: "\tx := 1\n", endNeedle: "\ty := 2", newText: ""},
		{name: "cross-line replacement", startNeedle: "x := 1", endNeedle: " := 2", newText: "replacement\n\tcontinued"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := strings.Index(string(source), tc.startNeedle)
			oldEnd := strings.Index(string(source), tc.endNeedle)
			if start < 0 || oldEnd < 0 {
				t.Fatal("missing test needle")
			}
			if tc.startNeedle == tc.endNeedle {
				oldEnd = start
			} else {
				oldEnd += len(tc.endNeedle)
			}
			edited := append([]byte(nil), source[:start]...)
			edited = append(edited, tc.newText...)
			edited = append(edited, source[oldEnd:]...)
			newEnd := start + len(tc.newText)

			goTree, goLang, err := parseWithGo(parityCase{name: "go", source: string(source)}, source, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseGoTree(goTree)

			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil {
				t.Fatal("C parse returned nil")
			}
			defer cTree.Close()
			if diff := FirstDivergenceDumpV1(goTree.RootNode(), goLang, cTree.RootNode()); diff != nil {
				t.Fatalf("pre-edit tree differs from C: %+v", diff)
			}

			goEdit := gotreesitter.InputEdit{
				StartByte:   uint32(start),
				OldEndByte:  uint32(oldEnd),
				NewEndByte:  uint32(newEnd),
				StartPoint:  pointAtOffset(source, start),
				OldEndPoint: pointAtOffset(source, oldEnd),
				NewEndPoint: pointAtOffset(edited, newEnd),
			}
			goTree.Edit(goEdit)
			cTree.Edit(&sitter.InputEdit{
				StartByte:      uint(start),
				OldEndByte:     uint(oldEnd),
				NewEndByte:     uint(newEnd),
				StartPosition:  sitter.Point{Row: uint(goEdit.StartPoint.Row), Column: uint(goEdit.StartPoint.Column)},
				OldEndPosition: sitter.Point{Row: uint(goEdit.OldEndPoint.Row), Column: uint(goEdit.OldEndPoint.Column)},
				NewEndPosition: sitter.Point{Row: uint(goEdit.NewEndPoint.Row), Column: uint(goEdit.NewEndPoint.Column)},
			})

			if diff := FirstDivergenceDumpV1(goTree.RootNode(), goLang, cTree.RootNode()); diff != nil {
				t.Fatalf("post-edit tree differs from C: %+v", diff)
			}

			goIncremental, _, err := parseWithGo(parityCase{name: "go", source: string(edited)}, edited, goTree)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseGoTree(goIncremental)
			cFresh := cParser.Parse(edited, nil)
			if cFresh == nil {
				t.Fatal("fresh C parse returned nil")
			}
			defer cFresh.Close()
			if diff := FirstDivergenceDumpV1(goIncremental.RootNode(), goLang, cFresh.RootNode()); diff != nil {
				t.Fatalf("incremental Go tree differs from fresh C: %+v", diff)
			}
		})
	}
}
