//go:build cgo && treesitter_c_parity

package cgoharness

// THROWAWAY diagnostic: parse ONE file with the production parser and the C
// oracle, walk both trees in lockstep, and dump the first structural
// difference (type, span, or child count) with full sibling context.
//
//	REPRO_LANG=jq REPRO_FILE=/path/to/builtin.jq \
//	  go test . -tags 'cgo treesitter_c_parity' -run TestFirstDiffDiag -v

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type fddSignature struct {
	File           string
	Path           string
	DiffKind       string
	GoRootType     string
	GoRootStart    uint32
	GoRootEnd      uint32
	GoRootChildren int
	GoRootHasError bool
	CRootKind      string
	CRootStart     uint32
	CRootEnd       uint32
	CRootChildren  int
	CRootHasError  bool
	GoErrorCount   int
	CErrorCount    int
	GoMissingCount int
	CMissingCount  int
	OracleRelation string
	GoStopReason   string
	GoRuntime      string
	GoType         string
	GoStart        uint32
	GoEnd          uint32
	GoChildren     int
	CType          string
	CStart         uint32
	CEnd           uint32
	CChildren      int
	GoText         string
	CText          string
}

func fddCountGoIntegrity(n *gts.Node, lang *gts.Language) (errors, missing int) {
	if n == nil {
		return 0, 0
	}
	if n.Type(lang) == "ERROR" {
		errors++
	}
	if n.IsMissing() {
		missing++
	}
	for i := 0; i < n.ChildCount(); i++ {
		childErrors, childMissing := fddCountGoIntegrity(n.Child(i), lang)
		errors += childErrors
		missing += childMissing
	}
	return errors, missing
}

func fddCountCIntegrity(n *sitter.Node) (errors, missing int) {
	if n == nil {
		return 0, 0
	}
	if n.Kind() == "ERROR" {
		errors++
	}
	if n.IsMissing() {
		missing++
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		childErrors, childMissing := fddCountCIntegrity(n.Child(uint(i)))
		errors += childErrors
		missing += childMissing
	}
	return errors, missing
}

func fddOracleRelation(goErrors, goMissing, cErrors, cMissing int) string {
	if goErrors == 0 && goMissing == 0 && cErrors+cMissing > 0 {
		return "go_clean_c_error"
	}
	if goErrors+goMissing > 0 && cErrors == 0 && cMissing == 0 {
		return "go_error_c_clean"
	}
	return ""
}

func fddTxt(src []byte, s, e uint32) string {
	if int(e) > len(src) {
		e = uint32(len(src))
	}
	if s > e {
		s = e
	}
	r := src[s:e]
	if len(r) > 80 {
		r = r[:80]
	}
	return fmt.Sprintf("%q", string(r))
}

func fddExcerpt(src []byte, s, e uint32) string {
	if int(e) > len(src) {
		e = uint32(len(src))
	}
	if s > e {
		s = e
	}
	const context = 24
	start, end := s, e
	if start > context {
		start -= context
	} else {
		start = 0
	}
	if maxEnd := uint32(len(src)); end+context < maxEnd {
		end += context
	} else {
		end = maxEnd
	}
	r := src[start:end]
	if len(r) > 120 {
		r = r[:120]
	}
	return string(r)
}

func fddDiffKind(g *gts.Node, lang *gts.Language, c *sitter.Node) string {
	switch {
	case g.Type(lang) != c.Kind():
		return "type"
	case g.StartByte() != uint32(c.StartByte()) || g.EndByte() != uint32(c.EndByte()):
		return "span"
	case g.ChildCount() != int(c.ChildCount()):
		return "child-count"
	case g.IsNamed() != c.IsNamed():
		return "named"
	case g.IsMissing() != c.IsMissing():
		return "missing"
	default:
		return ""
	}
}

func fddFirst(g *gts.Node, lang *gts.Language, c *sitter.Node, path string) *fddSignature {
	if kind := fddDiffKind(g, lang, c); kind != "" {
		return &fddSignature{
			Path:       path,
			DiffKind:   kind,
			GoType:     g.Type(lang),
			GoStart:    g.StartByte(),
			GoEnd:      g.EndByte(),
			GoChildren: g.ChildCount(),
			CType:      c.Kind(),
			CStart:     uint32(c.StartByte()),
			CEnd:       uint32(c.EndByte()),
			CChildren:  int(c.ChildCount()),
		}
	}
	for i := 0; i < g.ChildCount(); i++ {
		childPath := fmt.Sprintf("%s[%d]", path, i)
		gChild := g.Child(i)
		cChild := c.Child(uint(i))
		if parityCompareFields {
			goField := g.FieldNameForChild(i, lang)
			cField := c.FieldNameForChild(uint32(i))
			if goField != cField {
				return &fddSignature{
					Path:       childPath,
					DiffKind:   "field-name",
					GoType:     gChild.Type(lang),
					GoStart:    gChild.StartByte(),
					GoEnd:      gChild.EndByte(),
					GoChildren: gChild.ChildCount(),
					CType:      cChild.Kind(),
					CStart:     uint32(cChild.StartByte()),
					CEnd:       uint32(cChild.EndByte()),
					CChildren:  int(cChild.ChildCount()),
				}
			}
		}
		if sig := fddFirst(gChild, lang, cChild, childPath); sig != nil {
			return sig
		}
	}
	return nil
}

func fddBuildSignature(file string, goRoot *gts.Node, lang *gts.Language, cRoot *sitter.Node, src []byte, stopReason, goRuntime string) *fddSignature {
	sig := fddFirst(goRoot, lang, cRoot, "root")
	if sig == nil {
		return nil
	}
	sig.File = file
	sig.GoRootType = goRoot.Type(lang)
	sig.GoRootStart = goRoot.StartByte()
	sig.GoRootEnd = goRoot.EndByte()
	sig.GoRootChildren = goRoot.ChildCount()
	sig.GoRootHasError = goRoot.HasError()
	sig.CRootKind = cRoot.Kind()
	sig.CRootStart = uint32(cRoot.StartByte())
	sig.CRootEnd = uint32(cRoot.EndByte())
	sig.CRootChildren = int(cRoot.ChildCount())
	sig.CRootHasError = cRoot.HasError()
	sig.GoErrorCount, sig.GoMissingCount = fddCountGoIntegrity(goRoot, lang)
	sig.CErrorCount, sig.CMissingCount = fddCountCIntegrity(cRoot)
	sig.OracleRelation = fddOracleRelation(sig.GoErrorCount, sig.GoMissingCount, sig.CErrorCount, sig.CMissingCount)
	sig.GoStopReason = stopReason
	sig.GoRuntime = goRuntime
	sig.GoText = fddExcerpt(src, sig.GoStart, sig.GoEnd)
	sig.CText = fddExcerpt(src, sig.CStart, sig.CEnd)
	return sig
}

func fddQuote(s string) string {
	return strconv.Quote(s)
}

func fddPrintSignature(lang string, sig *fddSignature) {
	fmt.Printf("DIVERGE-SIG lang=%s file=%s base=%s goRoot=%s goRootSpan=%d:%d goRootCC=%d goRootErr=%v cRoot=%s cRootSpan=%d:%d cRootCC=%d cRootErr=%v oracle=%s goStop=%s goRuntime=%s path=%s diff=%s goType=%s cType=%s goSpan=%d:%d cSpan=%d:%d goCC=%d cCC=%d goText=%s cText=%s\n",
		fddQuote(lang),
		fddQuote(sig.File),
		fddQuote(filepath.Base(sig.File)),
		fddQuote(sig.GoRootType),
		sig.GoRootStart,
		sig.GoRootEnd,
		sig.GoRootChildren,
		sig.GoRootHasError,
		fddQuote(sig.CRootKind),
		sig.CRootStart,
		sig.CRootEnd,
		sig.CRootChildren,
		sig.CRootHasError,
		fddQuote(sig.OracleRelation),
		fddQuote(sig.GoStopReason),
		fddQuote(sig.GoRuntime),
		fddQuote(sig.Path),
		fddQuote(sig.DiffKind),
		fddQuote(sig.GoType),
		fddQuote(sig.CType),
		sig.GoStart,
		sig.GoEnd,
		sig.CStart,
		sig.CEnd,
		sig.GoChildren,
		sig.CChildren,
		fddQuote(sig.GoText),
		fddQuote(sig.CText),
	)
}

func fddPrintProgress(lang string, sig *fddSignature, fileIndex, fileTotal, bytes int, elapsedMS int64) {
	fmt.Printf("MEASURE-PROGRESS lang=%s file=%d/%d base=%s path=%s bytes=%d phase=comparison_diag result=diverge diff=%s firstDiffPath=%s goType=%s cType=%s goSpan=%d:%d cSpan=%d:%d goCC=%d cCC=%d goRoot=%s goRootSpan=%d:%d goRootCC=%d goRootErr=%v cRoot=%s cRootSpan=%d:%d cRootCC=%d cRootErr=%v goErrors=%d cErrors=%d goMissing=%d cMissing=%d oracle=%s goStop=%s runtime=%s goRuntime=%s elapsed_ms=%d\n",
		fddQuote(lang),
		fileIndex,
		fileTotal,
		fddQuote(filepath.Base(sig.File)),
		fddQuote(sig.File),
		bytes,
		fddQuote(sig.DiffKind),
		fddQuote(sig.Path),
		fddQuote(sig.GoType),
		fddQuote(sig.CType),
		sig.GoStart,
		sig.GoEnd,
		sig.CStart,
		sig.CEnd,
		sig.GoChildren,
		sig.CChildren,
		fddQuote(sig.GoRootType),
		sig.GoRootStart,
		sig.GoRootEnd,
		sig.GoRootChildren,
		sig.GoRootHasError,
		fddQuote(sig.CRootKind),
		sig.CRootStart,
		sig.CRootEnd,
		sig.CRootChildren,
		sig.CRootHasError,
		sig.GoErrorCount,
		sig.CErrorCount,
		sig.GoMissingCount,
		sig.CMissingCount,
		fddQuote(sig.OracleRelation),
		fddQuote(sig.GoStopReason),
		fddQuote(sig.GoRuntime),
		fddQuote(sig.GoRuntime),
		elapsedMS,
	)
}

func fddDumpBoth(g *gts.Node, lang *gts.Language, c *sitter.Node, src []byte, path string, t *testing.T) {
	t.Logf("  FIRST-DIFF @%s", path)
	t.Logf("    go: type=%q [%d:%d] cc=%d", g.Type(lang), g.StartByte(), g.EndByte(), g.ChildCount())
	t.Logf("    c : kind=%q [%d:%d] cc=%d", c.Kind(), c.StartByte(), c.EndByte(), int(c.ChildCount()))
	for i := 0; i < g.ChildCount(); i++ {
		ch := g.Child(i)
		t.Logf("      go.child[%d]: type=%q [%d:%d] named=%v %s", i, ch.Type(lang), ch.StartByte(), ch.EndByte(), ch.IsNamed(), fddTxt(src, ch.StartByte(), ch.EndByte()))
	}
	for i := 0; i < int(c.ChildCount()); i++ {
		ch := c.Child(uint(i))
		t.Logf("      c.child[%d]: kind=%q [%d:%d] named=%v %s", i, ch.Kind(), ch.StartByte(), ch.EndByte(), ch.IsNamed(), fddTxt(src, uint32(ch.StartByte()), uint32(ch.EndByte())))
	}
}

func fddGoSymbolMetadata(lang *gts.Language, sym gts.Symbol) (gts.SymbolMetadata, bool) {
	if lang == nil {
		return gts.SymbolMetadata{}, false
	}
	idx := int(sym)
	if idx < 0 || idx >= len(lang.SymbolMetadata) {
		return gts.SymbolMetadata{}, false
	}
	return lang.SymbolMetadata[idx], true
}

func fddHiddenFlattenStructuralApprox(lang *gts.Language, sym gts.Symbol) bool {
	meta, ok := fddGoSymbolMetadata(lang, sym)
	if !ok {
		return true
	}
	return meta.Visible
}

func fddSymbolAuditNode(t *testing.T, label string, n *gts.Node, lang *gts.Language, src []byte) {
	if n == nil {
		t.Logf("      SYMBOL-AUDIT go.%s <nil>", label)
		return
	}
	sym := n.Symbol()
	meta, ok := fddGoSymbolMetadata(lang, sym)
	metaName := ""
	metaVisible := false
	metaNamed := false
	metaSupertype := false
	metaGeneratedRepeatAux := false
	if ok {
		metaName = meta.Name
		metaVisible = meta.Visible
		metaNamed = meta.Named
		metaSupertype = meta.Supertype
		metaGeneratedRepeatAux = meta.GeneratedRepeatAux
	}
	t.Logf("      SYMBOL-AUDIT go.%s sym=%d type=%q metaOK=%v metaName=%q nodeNamed=%v metaNamed=%v visible=%v supertype=%v extra=%v generatedRepeatAux=%v flattenStructuralApprox=%v childCount=%d span=%d:%d hasError=%v text=%s",
		label,
		sym,
		n.Type(lang),
		ok,
		metaName,
		n.IsNamed(),
		metaNamed,
		metaVisible,
		metaSupertype,
		n.IsExtra(),
		metaGeneratedRepeatAux,
		fddHiddenFlattenStructuralApprox(lang, sym),
		n.ChildCount(),
		n.StartByte(),
		n.EndByte(),
		n.HasError(),
		fddTxt(src, n.StartByte(), n.EndByte()),
	)
}

func fddDumpSymbolAudit(g *gts.Node, lang *gts.Language, c *sitter.Node, src []byte, path string, t *testing.T) {
	t.Logf("    SYMBOL-AUDIT path=%s note=%q", path, "flattenStructuralApprox mirrors symbolStructuralForHiddenFlattening only for preservedHidden=nil; parser-internal preservedHidden overrides are not exported to cgo_harness")
	fddSymbolAuditNode(t, "diff", g, lang, src)
	for i := 0; g != nil && i < g.ChildCount(); i++ {
		fddSymbolAuditNode(t, fmt.Sprintf("child[%d]", i), g.Child(i), lang, src)
	}
	for i := 0; c != nil && i < int(c.ChildCount()); i++ {
		ch := c.Child(uint(i))
		t.Logf("      SYMBOL-AUDIT c.child[%d] kind=%q named=%v extra=%v childCount=%d span=%d:%d hasError=%v text=%s",
			i,
			ch.Kind(),
			ch.IsNamed(),
			ch.IsExtra(),
			int(ch.ChildCount()),
			ch.StartByte(),
			ch.EndByte(),
			ch.HasError(),
			fddTxt(src, uint32(ch.StartByte()), uint32(ch.EndByte())),
		)
	}
	fddDumpMismatchedChildSymbolAudit(g, lang, c, src, t)
}

func fddDumpMismatchedChildSymbolAudit(g *gts.Node, lang *gts.Language, c *sitter.Node, src []byte, t *testing.T) {
	if g == nil || c == nil {
		return
	}
	limit := g.ChildCount()
	if cc := int(c.ChildCount()); cc < limit {
		limit = cc
	}
	detailed := 0
	for i := 0; i < limit; i++ {
		gChild := g.Child(i)
		cChild := c.Child(uint(i))
		if fddDiffKind(gChild, lang, cChild) == "" {
			continue
		}
		sameStart := gChild.StartByte() == uint32(cChild.StartByte())
		if detailed >= 6 && !sameStart {
			continue
		}
		detailed++
		t.Logf("      SYMBOL-AUDIT detail child[%d] sameStart=%v", i, sameStart)
		for j := 0; j < gChild.ChildCount(); j++ {
			if !fddAuditChildIndexIncluded(j, gChild.ChildCount()) {
				if j == 16 {
					t.Logf("      SYMBOL-AUDIT go.child[%d].children.truncated total=%d", i, gChild.ChildCount())
				}
				continue
			}
			fddSymbolAuditNode(t, fmt.Sprintf("child[%d].child[%d]", i, j), gChild.Child(j), lang, src)
		}
		for j := 0; j < int(cChild.ChildCount()); j++ {
			if !fddAuditChildIndexIncluded(j, int(cChild.ChildCount())) {
				if j == 16 {
					t.Logf("      SYMBOL-AUDIT c.child[%d].children.truncated total=%d", i, int(cChild.ChildCount()))
				}
				continue
			}
			ch := cChild.Child(uint(j))
			t.Logf("      SYMBOL-AUDIT c.child[%d].child[%d] kind=%q named=%v extra=%v childCount=%d span=%d:%d hasError=%v text=%s",
				i,
				j,
				ch.Kind(),
				ch.IsNamed(),
				ch.IsExtra(),
				int(ch.ChildCount()),
				ch.StartByte(),
				ch.EndByte(),
				ch.HasError(),
				fddTxt(src, uint32(ch.StartByte()), uint32(ch.EndByte())),
			)
		}
	}
}

func fddAuditChildIndexIncluded(idx, total int) bool {
	return total <= 24 || idx < 16 || idx >= total-4
}

func fddWalk(g *gts.Node, lang *gts.Language, c *sitter.Node, src []byte, path string, t *testing.T) bool {
	if fddDiffKind(g, lang, c) != "" {
		fddDumpBoth(g, lang, c, src, path, t)
		if os.Getenv("REPRO_SYMBOL_AUDIT") == "1" {
			fddDumpSymbolAudit(g, lang, c, src, path, t)
		}
		return true
	}
	for i := 0; i < g.ChildCount(); i++ {
		if fddWalk(g.Child(i), lang, c.Child(uint(i)), src, fmt.Sprintf("%s[%d]", path, i), t) {
			return true
		}
	}
	return false
}

func TestFirstDiffDiag(t *testing.T) {
	name := os.Getenv("REPRO_LANG")
	file := os.Getenv("REPRO_FILE")
	if name == "" || file == "" {
		t.Skip("set REPRO_LANG and REPRO_FILE")
	}
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
	cLang, err := ParityCLanguage(name)
	if err != nil {
		t.Fatalf("%s: no C reference: %v", name, err)
	}
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if os.Getenv("REPRO_DEBUG_DFA") == "1" {
		gts.DebugDFA.Store(true)
		defer gts.DebugDFA.Store(false)
	}
	gp := gts.NewParser(goLang)
	if os.Getenv("REPRO_GLR_TRACE") == "1" {
		gp.SetGLRTrace(true)
	}
	tr, _ := gp.Parse(src)
	if tr == nil || tr.RootNode() == nil {
		t.Fatalf("go parse failed")
	}
	defer tr.Release()
	cp := sitter.NewParser()
	defer cp.Close()
	_ = cp.SetLanguage(cLang)
	ct := cp.Parse(src, nil)
	if ct == nil || ct.RootNode() == nil {
		t.Fatalf("c parse failed")
	}
	defer ct.Close()
	t.Logf("=== %s (%d bytes) ===", file, len(src))
	t.Logf("  go stopReason=%v runtime=%s rootHasError=%v cRootHasError=%v", tr.ParseStopReason(), tr.ParseRuntime().Summary(), tr.RootNode().HasError(), ct.RootNode().HasError())
	if !fddWalk(tr.RootNode(), goLang, ct.RootNode(), src, "root", t) {
		t.Logf("  (no structural divergence)")
	}
}
