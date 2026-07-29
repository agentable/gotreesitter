//go:build cgo && treesitter_c_parity

package cgoharness

// THROWAWAY diagnostic: parse ONE file with the C oracle and the production
// Go parser and dump BOTH full trees (every node, byte spans, named flag).
//
//	REPRO_LANG=requirements REPRO_FILE=/path/to/input \
//	  go test . -tags treesitter_c_parity -run TestCTreeDumpDiag -v

import (
	"fmt"
	"os"
	"strings"
	"testing"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func ctdDumpC(n *sitter.Node, src []byte, depth int, t *testing.T) {
	txt := ""
	s, e := n.StartByte(), n.EndByte()
	if int(e) <= len(src) {
		raw := string(src[s:e])
		if len(raw) > 60 {
			raw = raw[:60]
		}
		txt = fmt.Sprintf("%q", raw)
	}
	t.Logf("C %s%s [%d:%d] named=%v missing=%v extra=%v cc=%d %s",
		strings.Repeat("  ", depth), n.Kind(), s, e, n.IsNamed(), n.IsMissing(), n.IsExtra(), int(n.ChildCount()), txt)
	for i := 0; i < int(n.ChildCount()); i++ {
		ctdDumpC(n.Child(uint(i)), src, depth+1, t)
	}
}

func ctdDumpGo(n *gts.Node, lang *gts.Language, src []byte, depth int, t *testing.T) {
	txt := ""
	s, e := n.StartByte(), n.EndByte()
	if int(e) <= len(src) {
		raw := string(src[s:e])
		if len(raw) > 60 {
			raw = raw[:60]
		}
		txt = fmt.Sprintf("%q", raw)
	}
	t.Logf("G %s%s [%d:%d] named=%v missing=%v extra=%v cc=%d %s",
		strings.Repeat("  ", depth), n.Type(lang), s, e, n.IsNamed(), n.IsMissing(), n.IsExtra(), n.ChildCount(), txt)
	for i := 0; i < n.ChildCount(); i++ {
		ctdDumpGo(n.Child(i), lang, src, depth+1, t)
	}
}

func TestCTreeDumpDiag(t *testing.T) {
	name := os.Getenv("REPRO_LANG")
	file := os.Getenv("REPRO_FILE")
	if name == "" || file == "" {
		t.Skip("set REPRO_LANG and REPRO_FILE")
	}
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cLang, err := ParityCLanguage(name)
	if err != nil {
		t.Fatalf("%s: no C reference: %v", name, err)
	}
	cp := sitter.NewParser()
	defer cp.Close()
	_ = cp.SetLanguage(cLang)
	if os.Getenv("REPRO_CLOG") != "" {
		cp.SetLogger(func(logType sitter.LogType, msg string) {
			if logType == sitter.LogTypeParse {
				t.Logf("CLOG %s", msg)
			}
		})
	}
	ct := cp.Parse(src, nil)
	defer ct.Close()
	t.Logf("=== C oracle tree (%d bytes) ===", len(src))
	ctdDumpC(ct.RootNode(), src, 0, t)

	var goLang *gts.Language
	for _, e := range grammars.AllLanguages() {
		if e.Name == name {
			goLang = e.Language()
			break
		}
	}
	if goLang == nil {
		t.Fatalf("%s: not in grammars.AllLanguages", name)
	}
	var gt *gts.Tree
	if os.Getenv("REPRO_GO_BACKEND") == "registry" {
		var parseErr error
		gt, _, parseErr = parseWithGo(parityCase{name: name, source: string(src)}, src, nil)
		if parseErr != nil {
			t.Fatalf("go registry parse failed: %v", parseErr)
		}
	} else {
		gp := gts.NewParser(goLang)
		gt, _ = gp.Parse(src)
	}
	if gt == nil || gt.RootNode() == nil {
		t.Fatalf("go parse failed")
	}
	defer gt.Release()
	t.Logf("=== Go tree ===")
	ctdDumpGo(gt.RootNode(), goLang, src, 0, t)
}
