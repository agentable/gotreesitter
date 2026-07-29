package grammars

import (
	"fmt"
	"sync"

	"github.com/agentable/gotreesitter"
)

var (
	poolsMu sync.RWMutex
	pools   = map[string]*gotreesitter.ParserPool{}
)

func getOrCreatePool(name string, lang *gotreesitter.Language) *gotreesitter.ParserPool {
	poolsMu.RLock()
	pp, ok := pools[name]
	poolsMu.RUnlock()
	if ok && pp.Language() == lang {
		return pp
	}

	poolsMu.Lock()
	defer poolsMu.Unlock()
	if pp, ok = pools[name]; ok && pp.Language() == lang {
		return pp
	}
	pp = gotreesitter.NewParserPool(lang)
	pools[name] = pp
	return pp
}

// ParseFile detects the language from filename, parses source, and returns
// a BoundTree. The caller must call Release() on the returned BoundTree.
func ParseFile(filename string, source []byte) (*gotreesitter.BoundTree, error) {
	entry := DetectLanguage(filename)
	if entry == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filename)
	}

	lang := entry.Language()
	parser := gotreesitter.NewParser(lang)

	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		tree, err = parser.ParseWithTokenSource(source, ts)
	} else {
		tree, err = parser.Parse(source)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	return gotreesitter.Bind(tree), nil
}

// ParseFilePooled is like ParseFile but reuses a per-language ParserPool
// to avoid allocating a new parser on every call. It is safe for concurrent use.
// The caller must call Release() on the returned BoundTree.
func ParseFilePooled(filename string, source []byte) (*gotreesitter.BoundTree, error) {
	entry := DetectLanguage(filename)
	if entry == nil {
		return nil, fmt.Errorf("unsupported file type: %s", filename)
	}

	lang := entry.Language()
	pp := getOrCreatePool(entry.Name, lang)

	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(source, lang)
		tree, err = pp.ParseWithTokenSource(source, ts)
	} else {
		tree, err = pp.Parse(source)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	return gotreesitter.Bind(tree), nil
}
