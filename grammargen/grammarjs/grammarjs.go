// Package grammarjs wires grammargen's grammar.js importer to gotreesitter's
// embedded JavaScript grammar. Blank-import it to enable
// grammargen.ImportGrammarJS:
//
//	import _ "github.com/agentable/gotreesitter/grammargen/grammarjs"
//
// It is a separate package on purpose: importing grammargen alone no longer
// pulls in the gotreesitter/grammars registry (~200 grammars, ~22MB). Only code
// that actually imports tree-sitter grammar.js files pays for the JS grammar.
package grammarjs

import (
	"github.com/agentable/gotreesitter/grammargen"
	"github.com/agentable/gotreesitter/grammars"
)

func init() {
	grammargen.SetJSGrammarProvider(grammars.JavascriptLanguage)
}
