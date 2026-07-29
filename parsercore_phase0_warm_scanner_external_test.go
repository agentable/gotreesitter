//go:build gts_parsercorephase0

package gotreesitter_test

import (
	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
)

func init() {
	gotreesitter.SetDiagnosticParserCoreWarmGoScannerForTest(grammars.GoExternalScanner{})
}
