//go:build !grammar_subset || grammar_subset_sql

package grammars

import (
	"strings"
	"testing"
	_ "unsafe"

	gotreesitter "github.com/agentable/gotreesitter"
)

func TestSQLScannerCheckpointRoundTrip(t *testing.T) {
	scanner := SqlExternalScanner{}
	buf := make([]byte, 4096)

	for _, tag := range []string{"", "$$", "$named_42$", "$" + strings.Repeat("x", sqlMaxDollarTagBytes-2) + "$"} {
		t.Run(checkpointTagName(tag), func(t *testing.T) {
			original := &sqlScannerState{tag: tag}
			n := scanner.Serialize(original, buf)
			if n == 0 {
				t.Fatalf("reachable scanner state %q was not checkpointable", tag)
			}
			restored := &sqlScannerState{tag: "$stale$"}
			scanner.Deserialize(restored, buf[:n])
			if restored.tag != tag {
				t.Fatalf("checkpoint round trip tag = %q, want %q", restored.tag, tag)
			}
		})
	}
}

func TestSQLScannerAcceptsUncheckpointableDollarTagForFullParse(t *testing.T) {
	maxTag := "$" + strings.Repeat("x", sqlMaxDollarTagBytes-2) + "$"
	tooLongTag := "$" + strings.Repeat("x", sqlMaxDollarTagBytes-1) + "$"

	if tag, ok := scanSqlDollarTag(newSQLExternalLexer([]byte(maxTag), 0, 0, 0), false); !ok || tag != maxTag {
		t.Fatalf("maximum checkpointable tag was rejected: len=%d ok=%v", len(tag), ok)
	}
	if tag, ok := scanSqlDollarTag(newSQLExternalLexer([]byte(tooLongTag), 0, 0, 0), false); !ok || tag != tooLongTag {
		t.Fatalf("valid uncheckpointable tag was rejected: len=%d ok=%v", len(tag), ok)
	}
	buf := make([]byte, 4096)
	if n := (SqlExternalScanner{}).Serialize(&sqlScannerState{tag: tooLongTag}, buf); n != 0 {
		t.Fatalf("uncheckpointable scanner state serialized as %d bytes, want absent checkpoint", n)
	}
}

func TestSQLScannerFailedClosePreservesCheckpointState(t *testing.T) {
	scanner := SqlExternalScanner{}
	state := &sqlScannerState{tag: "$wanted$"}
	valid := make([]bool, sqlTokDollarTagEnd+1)
	valid[sqlTokDollarTagEnd] = true
	if scanner.Scan(state, newSQLExternalLexer([]byte("$other$"), 0, 0, 0), valid) {
		t.Fatal("mismatched close tag scanned successfully")
	}
	if state.tag != "$wanted$" {
		t.Fatalf("failed scan mutated tag to %q", state.tag)
	}
}

func checkpointTagName(tag string) string {
	if tag == "" {
		return "empty"
	}
	if len(tag) == sqlMaxDollarTagBytes {
		return "maximum"
	}
	return tag
}

//go:linkname newSQLExternalLexer github.com/agentable/gotreesitter.newExternalLexer
func newSQLExternalLexer(source []byte, pos int, row, col uint32) *gotreesitter.ExternalLexer
