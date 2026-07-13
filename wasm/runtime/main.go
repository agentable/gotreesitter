//go:build js && wasm

package main

import (
	"syscall/js"
	"unicode/utf16"

	"github.com/odvcencio/gotreesitter"
)

type runtimeLanguage struct {
	language    *gotreesitter.Language
	highlighter *gotreesitter.Highlighter
	tagger      *gotreesitter.Tagger
}

type runtimeDocument struct {
	languageName string
	source       []uint16
	parser       *gotreesitter.Parser
	tree         *gotreesitter.Tree
	revision     uint64
}

var (
	languages = map[string]*runtimeLanguage{}
	documents = map[string]*runtimeDocument{}
)

func main() {
	runtime := js.Global().Get("Object").New()
	runtime.Set("loadBlob", js.FuncOf(loadBlob))
	runtime.Set("parse", js.FuncOf(parse))
	runtime.Set("highlight", js.FuncOf(highlight))
	runtime.Set("open", js.FuncOf(openDocument))
	runtime.Set("update", js.FuncOf(updateDocument))
	runtime.Set("close", js.FuncOf(closeDocument))
	runtime.Set("query", js.FuncOf(queryDocument))
	runtime.Set("version", "0.2.0-runtime")
	runtime.Set("mode", "runtime")
	js.Global().Set("gotreesitter", runtime)
	select {}
}

func loadBlob(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return errResult("usage: loadBlob(name, blobUint8Array, highlightQuery, [tagsQuery])")
	}
	name := args[0].String()
	jsArr := args[1]
	highlightQuery := args[2].String()
	tagsQuery := ""
	if len(args) >= 4 && args[3].Type() == js.TypeString {
		tagsQuery = args[3].String()
	}

	blob := make([]byte, jsArr.Get("length").Int())
	js.CopyBytesToGo(blob, jsArr)
	language, loadErr := gotreesitter.LoadLanguage(blob)
	if loadErr != nil {
		return errResult("load blob: " + loadErr.Error())
	}

	runtime := &runtimeLanguage{language: language}
	if highlightQuery != "" {
		runtime.highlighter, loadErr = gotreesitter.NewHighlighter(language, highlightQuery)
		if loadErr != nil {
			return errResult("highlighter: " + loadErr.Error())
		}
	}
	if tagsQuery != "" {
		runtime.tagger, loadErr = gotreesitter.NewTagger(language, tagsQuery)
		if loadErr != nil {
			return errResult("tagger: " + loadErr.Error())
		}
	}
	languages[name] = runtime
	return okResult(map[string]interface{}{
		"name":         name,
		"highlighting": runtime.highlighter != nil,
		"tags":         runtime.tagger != nil,
	})
}

func parse(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("usage: parse(name, source)")
	}
	runtime, has := languages[args[0].String()]
	if !has {
		return errResult("language not loaded: " + args[0].String())
	}
	tree, parseErr := gotreesitter.NewParser(runtime.language).ParseUTF16(toUTF16(args[1].String()))
	if parseErr != nil {
		return errResult(parseErr.Error())
	}
	defer tree.Release()
	return okResult(map[string]interface{}{
		"sexp":     tree.RootNode().SExpr(runtime.language),
		"hasError": tree.RootNode().HasError(),
	})
}

func highlight(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("usage: highlight(name, source)")
	}
	runtime, has := languages[args[0].String()]
	if !has || runtime.highlighter == nil {
		return errResult("no highlighter for: " + args[0].String())
	}
	return okResult(map[string]interface{}{
		"ranges": highlightRanges(runtime.highlighter.HighlightUTF16(toUTF16(args[1].String()))),
	})
}

func openDocument(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return errResult("usage: open(name, documentID, source)")
	}
	name, documentID := args[0].String(), args[1].String()
	if documentID == "" {
		return errResult("document id is required")
	}
	runtime, has := languages[name]
	if !has {
		return errResult("language not loaded: " + name)
	}
	if previous := documents[documentID]; previous != nil {
		previous.release()
	}
	source := toUTF16(args[2].String())
	document := &runtimeDocument{
		languageName: name,
		source:       source,
		parser:       gotreesitter.NewParser(runtime.language),
		revision:     1,
	}
	var parseErr error
	document.tree, parseErr = document.parser.ParseUTF16(source)
	if parseErr != nil {
		return errResult(parseErr.Error())
	}
	documents[documentID] = document
	return document.analysis(runtime, false, nil)
}

func updateDocument(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("usage: update(documentID, source)")
	}
	documentID := args[0].String()
	document, has := documents[documentID]
	if !has {
		return errResult("document not open: " + documentID)
	}
	runtime := languages[document.languageName]
	newSource := toUTF16(args[1].String())
	edit, changed := utf16EditBetween(document.source, newSource)
	if !changed {
		return document.analysis(runtime, true, &edit)
	}
	if !document.tree.EditUTF16(edit, newSource) {
		return errResult("edit does not align to a UTF-16 boundary")
	}
	oldTree := document.tree
	newTree, parseErr := document.parser.ParseIncrementalUTF16(newSource, oldTree)
	if parseErr != nil {
		return errResult(parseErr.Error())
	}
	document.tree = newTree
	document.source = newSource
	document.revision++
	if oldTree != newTree {
		oldTree.Release()
	}
	return document.analysis(runtime, true, &edit)
}

func closeDocument(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return errResult("usage: close(documentID)")
	}
	documentID := args[0].String()
	document, has := documents[documentID]
	if has {
		document.release()
		delete(documents, documentID)
	}
	return okResult(map[string]interface{}{"closed": has})
}

func queryDocument(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errResult("usage: query(documentID, querySource)")
	}
	document, has := documents[args[0].String()]
	if !has {
		return errResult("document not open: " + args[0].String())
	}
	runtime := languages[document.languageName]
	query, queryErr := gotreesitter.NewQuery(args[1].String(), runtime.language)
	if queryErr != nil {
		return errResult(queryErr.Error())
	}
	matches := query.Execute(document.tree)
	jsMatches := make([]interface{}, 0, len(matches))
	for _, match := range matches {
		captures := make([]interface{}, 0, len(match.Captures))
		for _, capture := range match.Captures {
			if capture.Node == nil {
				continue
			}
			range16, valid := document.tree.UTF16RangeForRange(capture.Node.Range())
			if !valid {
				continue
			}
			captures = append(captures, map[string]interface{}{
				"name":  capture.Name,
				"text":  capture.Text(document.tree.Source()),
				"range": jsRange(range16),
			})
		}
		jsMatches = append(jsMatches, map[string]interface{}{
			"patternIndex": match.PatternIndex,
			"captures":     captures,
		})
	}
	return okResult(map[string]interface{}{"matches": jsMatches})
}

func (document *runtimeDocument) analysis(runtime *runtimeLanguage, incremental bool, edit *gotreesitter.UTF16Edit) interface{} {
	root := document.tree.RootNode()
	result := map[string]interface{}{
		"revision":    document.revision,
		"incremental": incremental,
		"hasError":    root != nil && root.HasError(),
		"highlights":  []interface{}{},
		"tags":        []interface{}{},
	}
	if runtime.highlighter != nil {
		result["highlights"] = highlightRanges(runtime.highlighter.HighlightTreeUTF16(document.tree))
	}
	if runtime.tagger != nil {
		result["tags"] = tagRanges(runtime.tagger.TagTreeUTF16(document.tree))
	}
	if edit != nil {
		result["edit"] = map[string]interface{}{
			"start16":  edit.StartCodeUnit,
			"oldEnd16": edit.OldEndCodeUnit,
			"newEnd16": edit.NewEndCodeUnit,
		}
	}
	return okResult(result)
}

func (document *runtimeDocument) release() {
	if document != nil && document.tree != nil {
		document.tree.Release()
		document.tree = nil
	}
}

func highlightRanges(ranges []gotreesitter.UTF16HighlightRange) []interface{} {
	result := make([]interface{}, 0, len(ranges))
	for _, highlight := range ranges {
		result = append(result, map[string]interface{}{
			"start16":      highlight.StartCodeUnit,
			"end16":        highlight.EndCodeUnit,
			"startPoint":   jsPoint(highlight.StartPoint),
			"endPoint":     jsPoint(highlight.EndPoint),
			"capture":      highlight.Capture,
			"patternIndex": highlight.PatternIndex,
		})
	}
	return result
}

func tagRanges(tags []gotreesitter.UTF16Tag) []interface{} {
	result := make([]interface{}, 0, len(tags))
	for _, tag := range tags {
		result = append(result, map[string]interface{}{
			"kind":      tag.Kind,
			"name":      tag.Name,
			"range":     jsRange(tag.Range),
			"nameRange": jsRange(tag.NameRange),
		})
	}
	return result
}

func jsRange(sourceRange gotreesitter.UTF16Range) map[string]interface{} {
	return map[string]interface{}{
		"start16":    sourceRange.StartCodeUnit,
		"end16":      sourceRange.EndCodeUnit,
		"startPoint": jsPoint(sourceRange.StartPoint),
		"endPoint":   jsPoint(sourceRange.EndPoint),
	}
}

func jsPoint(point gotreesitter.Point) map[string]interface{} {
	return map[string]interface{}{"row": point.Row, "column": point.Column}
}

func toUTF16(value string) []uint16 {
	return utf16.Encode([]rune(value))
}

func okResult(extra map[string]interface{}) interface{} {
	extra["ok"] = true
	return jsObject(extra)
}

func errResult(message string) interface{} {
	return jsObject(map[string]interface{}{"ok": false, "error": message})
}

// TinyGo's syscall/js.ValueOf intentionally accepts only primitive Go values.
// Build compound values explicitly so the same runtime works under TinyGo and
// the standard Go js/wasm target.
func jsObject(fields map[string]interface{}) js.Value {
	object := js.Global().Get("Object").New()
	for key, value := range fields {
		object.Set(key, jsValue(value))
	}
	return object
}

func jsArray(values []interface{}) js.Value {
	array := js.Global().Get("Array").New(len(values))
	for index, value := range values {
		array.SetIndex(index, jsValue(value))
	}
	return array
}

func jsValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return jsObject(typed)
	case []interface{}:
		return jsArray(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return float64(typed)
	default:
		return value
	}
}
