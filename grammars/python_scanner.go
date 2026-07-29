//go:build !grammar_subset || grammar_subset_python

package grammars

import (
	gotreesitter "github.com/agentable/gotreesitter"
)

// External token indexes must match the generated Python grammar ExternalSymbols order.
const (
	pyTokNewline = iota
	pyTokIndent
	pyTokDedent
	pyTokStringStart
	pyTokStringContent
	pyTokEscapeInterpolation
	pyTokStringEnd
	pyTokComment
	pyTokCloseBracket
	pyTokCloseParen
	pyTokCloseBrace
	pyTokExcept
	pyTokenCount
)

// Concrete symbol IDs from the checked-in Python grammar ExternalSymbols.
const (
	pySymNewline             gotreesitter.Symbol = 101
	pySymIndent              gotreesitter.Symbol = 102
	pySymDedent              gotreesitter.Symbol = 103
	pySymStringStart         gotreesitter.Symbol = 104
	pySymStringContent       gotreesitter.Symbol = 105
	pySymEscapeInterpolation gotreesitter.Symbol = 106
	pySymStringEnd           gotreesitter.Symbol = 107
	pySymComment             gotreesitter.Symbol = 99
	pySymCloseBracket        gotreesitter.Symbol = 47
	pySymCloseParen          gotreesitter.Symbol = 8
	pySymCloseBrace          gotreesitter.Symbol = 53
	pySymExcept              gotreesitter.Symbol = 33
)

var pyDefaultSymTable = [pyTokenCount]gotreesitter.Symbol{
	pySymNewline,
	pySymIndent,
	pySymDedent,
	pySymStringStart,
	pySymStringContent,
	pySymEscapeInterpolation,
	pySymStringEnd,
	pySymComment,
	pySymCloseBracket,
	pySymCloseParen,
	pySymCloseBrace,
	pySymExcept,
}

var pythonExternalScannerSpec = ExternalScannerSpec{
	Language:     "python",
	UpstreamRepo: "https://github.com/tree-sitter/tree-sitter-python",
	Externals: []string{
		"_newline",
		"_indent",
		"_dedent",
		"string_start",
		"_string_content",
		"escape_interpolation",
		"string_end",
		"comment",
		"]",
		")",
		"}",
		"except",
	},
}

func init() {
	RegisterExternalScannerSpec(pythonExternalScannerSpec)
}

type pyDelimiter byte

const (
	pyDelimSingleQuote pyDelimiter = 1 << 0
	pyDelimDoubleQuote pyDelimiter = 1 << 1
	pyDelimBackQuote   pyDelimiter = 1 << 2
	pyDelimRaw         pyDelimiter = 1 << 3
	pyDelimFormat      pyDelimiter = 1 << 4
	pyDelimTriple      pyDelimiter = 1 << 5
	pyDelimBytes       pyDelimiter = 1 << 6
)

func (d pyDelimiter) isFormat() bool { return d&pyDelimFormat != 0 }
func (d pyDelimiter) isRaw() bool    { return d&pyDelimRaw != 0 }
func (d pyDelimiter) isTriple() bool { return d&pyDelimTriple != 0 }
func (d pyDelimiter) isBytes() bool  { return d&pyDelimBytes != 0 }

func (d pyDelimiter) endChar() rune {
	switch {
	case d&pyDelimSingleQuote != 0:
		return '\''
	case d&pyDelimDoubleQuote != 0:
		return '"'
	case d&pyDelimBackQuote != 0:
		return '`'
	default:
		return 0
	}
}

type pythonScannerState struct {
	indents                  []uint16
	delimiters               []pyDelimiter
	insideInterpolatedString bool
}

func (s *pythonScannerState) syncInsideInterpolatedString() {
	s.insideInterpolatedString = false
	for _, d := range s.delimiters {
		if d.isFormat() {
			s.insideInterpolatedString = true
			return
		}
	}
}

type PythonExternalScanner struct {
	symbols         [pyTokenCount]gotreesitter.Symbol
	externalToToken []int
}

func (PythonExternalScanner) ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner {
	s := PythonExternalScanner{symbols: pyDefaultSymTable}
	s.externalToToken = bindExternalScannerSpec(lang, pythonExternalScannerSpec, func(tokenIdx int, sym gotreesitter.Symbol) {
		s.symbols[tokenIdx] = sym
	})
	return s
}

func (PythonExternalScanner) Create() any {
	return &pythonScannerState{
		indents: []uint16{0},
	}
}

func (PythonExternalScanner) Destroy(payload any) {}

func (PythonExternalScanner) Serialize(payload any, buf []byte) int {
	s := payload.(*pythonScannerState)
	if len(buf) == 0 {
		return 0
	}
	s.syncInsideInterpolatedString()

	size := 0
	if s.insideInterpolatedString {
		buf[size] = 1
	}
	size++
	if size >= len(buf) {
		return size
	}

	delimCount := len(s.delimiters)
	if delimCount > 255 {
		delimCount = 255
	}
	buf[size] = byte(delimCount)
	size++
	if size >= len(buf) {
		return size
	}

	for i := 0; i < delimCount && size < len(buf); i++ {
		buf[size] = byte(s.delimiters[i])
		size++
	}

	// Skip indents[0] (sentinel), serialize from index 1.
	for i := 1; i < len(s.indents) && size+1 < len(buf); i++ {
		v := s.indents[i]
		buf[size] = byte(v & 0xFF)
		buf[size+1] = byte((v >> 8) & 0xFF)
		size += 2
	}

	return size
}

func (PythonExternalScanner) Deserialize(payload any, buf []byte) {
	s := payload.(*pythonScannerState)
	s.delimiters = s.delimiters[:0]
	s.indents = s.indents[:0]
	s.indents = append(s.indents, 0)
	s.insideInterpolatedString = false

	if len(buf) == 0 {
		return
	}

	size := 0
	s.insideInterpolatedString = buf[size] != 0
	size++
	if size >= len(buf) {
		return
	}

	delimCount := int(buf[size])
	size++
	for i := 0; i < delimCount && size < len(buf); i++ {
		s.delimiters = append(s.delimiters, pyDelimiter(buf[size]))
		size++
	}
	s.syncInsideInterpolatedString()

	for size+1 < len(buf) {
		v := uint16(buf[size]) | uint16(buf[size+1])<<8
		s.indents = append(s.indents, v)
		size += 2
	}
}

// SupportsIncrementalReuse remains disabled until checkpoint restoration can
// prove that indentation stacks preserve every class-closing DEDENT. The
// token-invariant leaf fast path is validated before this capability check and
// remains available for edits that do not depend on scanner state.
func (PythonExternalScanner) SupportsIncrementalReuse() bool { return false }

func (PythonExternalScanner) UsesExternalScannerCheckpoints() bool { return true }

func (PythonExternalScanner) PreservesStateOnScanFailure() bool { return true }

func (p PythonExternalScanner) symbolTable() *[pyTokenCount]gotreesitter.Symbol {
	if p.symbols == ([pyTokenCount]gotreesitter.Symbol{}) {
		return &pyDefaultSymTable
	}
	return &p.symbols
}

func (p PythonExternalScanner) remapValidSymbols(validSymbols []bool, semanticValid *[pyTokenCount]bool) []bool {
	if len(p.externalToToken) == 0 {
		return validSymbols
	}
	*semanticValid = [pyTokenCount]bool{}
	for externalIdx, valid := range validSymbols {
		if !valid || externalIdx >= len(p.externalToToken) {
			continue
		}
		tokenIdx := p.externalToToken[externalIdx]
		if tokenIdx >= 0 && tokenIdx < pyTokenCount {
			semanticValid[tokenIdx] = true
		}
	}
	return semanticValid[:]
}

func (p PythonExternalScanner) Scan(payload any, lexer *gotreesitter.ExternalLexer, validSymbols []bool) bool {
	s := payload.(*pythonScannerState)
	if len(s.indents) == 0 {
		s.indents = append(s.indents, 0)
	}
	s.syncInsideInterpolatedString()
	symbols := p.symbolTable()
	if len(p.externalToToken) > 0 {
		var semanticValid [pyTokenCount]bool
		validSymbols = p.remapValidSymbols(validSymbols, &semanticValid)
	}

	isValid := func(idx int) bool {
		return idx >= 0 && idx < len(validSymbols) && validSymbols[idx]
	}

	errorRecoveryMode := isValid(pyTokStringContent) && isValid(pyTokIndent)
	withinBrackets := isValid(pyTokCloseBrace) || isValid(pyTokCloseParen) || isValid(pyTokCloseBracket)

	advancedOnce := false
	if isValid(pyTokEscapeInterpolation) && len(s.delimiters) > 0 &&
		(lexer.Lookahead() == '{' || lexer.Lookahead() == '}') && !errorRecoveryMode {
		delimiter := s.delimiters[len(s.delimiters)-1]
		if delimiter.isFormat() {
			lexer.MarkEnd()
			isLeftBrace := lexer.Lookahead() == '{'
			lexer.Advance(false)
			advancedOnce = true
			if (lexer.Lookahead() == '{' && isLeftBrace) || (lexer.Lookahead() == '}' && !isLeftBrace) {
				lexer.Advance(false)
				lexer.MarkEnd()
				lexer.SetResultSymbol(symbols[pyTokEscapeInterpolation])
				return true
			}
			return false
		}
	}

	if isValid(pyTokStringContent) && len(s.delimiters) > 0 && !errorRecoveryMode {
		delimiter := s.delimiters[len(s.delimiters)-1]
		endChar := delimiter.endChar()
		hasContent := advancedOnce

		for lexer.Lookahead() != 0 {
			if (advancedOnce || lexer.Lookahead() == '{' || lexer.Lookahead() == '}') && delimiter.isFormat() {
				lexer.MarkEnd()
				lexer.SetResultSymbol(symbols[pyTokStringContent])
				return hasContent
			}

			if lexer.Lookahead() == '\\' {
				if delimiter.isRaw() {
					lexer.Advance(false)
					if lexer.Lookahead() == endChar || lexer.Lookahead() == '\\' {
						lexer.Advance(false)
					}
					if lexer.Lookahead() == '\r' {
						lexer.Advance(false)
						if lexer.Lookahead() == '\n' {
							lexer.Advance(false)
						}
					} else if lexer.Lookahead() == '\n' {
						lexer.Advance(false)
					}
					continue
				}

				if delimiter.isBytes() {
					lexer.MarkEnd()
					lexer.Advance(false)
					if lexer.Lookahead() == 'N' || lexer.Lookahead() == 'u' || lexer.Lookahead() == 'U' {
						lexer.Advance(false)
					} else {
						lexer.SetResultSymbol(symbols[pyTokStringContent])
						return hasContent
					}
				} else {
					lexer.MarkEnd()
					lexer.SetResultSymbol(symbols[pyTokStringContent])
					return hasContent
				}
			} else if lexer.Lookahead() == endChar {
				if delimiter.isTriple() {
					lexer.MarkEnd()
					lexer.Advance(false)
					if lexer.Lookahead() == endChar {
						lexer.Advance(false)
						if lexer.Lookahead() == endChar {
							if hasContent {
								lexer.SetResultSymbol(symbols[pyTokStringContent])
							} else {
								lexer.Advance(false)
								lexer.MarkEnd()
								s.delimiters = s.delimiters[:len(s.delimiters)-1]
								lexer.SetResultSymbol(symbols[pyTokStringEnd])
								s.insideInterpolatedString = false
							}
							return true
						}
						lexer.MarkEnd()
						lexer.SetResultSymbol(symbols[pyTokStringContent])
						return true
					}
					lexer.MarkEnd()
					lexer.SetResultSymbol(symbols[pyTokStringContent])
					return true
				}

				if hasContent {
					lexer.SetResultSymbol(symbols[pyTokStringContent])
				} else {
					lexer.Advance(false)
					s.delimiters = s.delimiters[:len(s.delimiters)-1]
					lexer.SetResultSymbol(symbols[pyTokStringEnd])
					s.insideInterpolatedString = false
				}
				lexer.MarkEnd()
				return true
			} else if lexer.Lookahead() == '\n' && hasContent && !delimiter.isTriple() {
				return false
			}

			lexer.Advance(false)
			hasContent = true
		}
	}

	lexer.MarkEnd()

	foundEndOfLine := false
	var indentLength uint16
	firstCommentIndentLength := int32(-1)

	for {
		switch lexer.Lookahead() {
		case '\n':
			foundEndOfLine = true
			indentLength = 0
			lexer.Advance(true)
		case ' ':
			indentLength += uint16(lexer.AdvanceSpaces(true))
		case '\r', '\f':
			indentLength = 0
			lexer.Advance(true)
		case '\t':
			indentLength += 8
			lexer.Advance(true)
		case '#':
			if isValid(pyTokIndent) || isValid(pyTokDedent) || isValid(pyTokNewline) || isValid(pyTokExcept) {
				if !foundEndOfLine {
					return false
				}
				if firstCommentIndentLength == -1 {
					firstCommentIndentLength = int32(indentLength)
				}
				lexer.AdvanceUntilNewline(true)
				lexer.Advance(true)
				indentLength = 0
				continue
			}
			goto afterIndentLoop
		case '\\':
			lexer.Advance(true)
			if lexer.Lookahead() == '\r' {
				lexer.Advance(true)
			}
			if lexer.Lookahead() == '\n' || lexer.Lookahead() == 0 {
				lexer.Advance(true)
			} else {
				return false
			}
		case 0:
			indentLength = 0
			foundEndOfLine = true
			goto afterIndentLoop
		default:
			goto afterIndentLoop
		}
	}

afterIndentLoop:
	if foundEndOfLine {
		currentIndent := s.indents[len(s.indents)-1]

		if isValid(pyTokIndent) && indentLength > currentIndent {
			s.indents = append(s.indents, indentLength)
			lexer.SetResultSymbol(symbols[pyTokIndent])
			return true
		}

		nextTokIsStringStart := lexer.Lookahead() == '"' || lexer.Lookahead() == '\'' || lexer.Lookahead() == '`'
		if (isValid(pyTokDedent) ||
			(!isValid(pyTokNewline) && !(isValid(pyTokStringStart) && nextTokIsStringStart) && !withinBrackets)) &&
			indentLength < currentIndent &&
			!s.insideInterpolatedString &&
			firstCommentIndentLength < int32(currentIndent) {
			s.indents = s.indents[:len(s.indents)-1]
			lexer.SetResultSymbol(symbols[pyTokDedent])
			return true
		}

		if isValid(pyTokNewline) && !errorRecoveryMode {
			lexer.SetResultSymbol(symbols[pyTokNewline])
			return true
		}
	}

	if firstCommentIndentLength == -1 && isValid(pyTokStringStart) {
		var delimiter pyDelimiter
		hasFlags := false

		for lexer.Lookahead() != 0 {
			switch lexer.Lookahead() {
			case 'f', 'F', 't', 'T':
				delimiter |= pyDelimFormat
			case 'r', 'R':
				delimiter |= pyDelimRaw
			case 'b', 'B':
				delimiter |= pyDelimBytes
			case 'u', 'U':
				// accepted prefix, no scanner flag
			default:
				goto afterFlags
			}
			hasFlags = true
			lexer.Advance(false)
		}

	afterFlags:
		switch lexer.Lookahead() {
		case '`':
			delimiter |= pyDelimBackQuote
			lexer.Advance(false)
			lexer.MarkEnd()
		case '\'':
			delimiter |= pyDelimSingleQuote
			lexer.Advance(false)
			lexer.MarkEnd()
			if lexer.Lookahead() == '\'' {
				lexer.Advance(false)
				if lexer.Lookahead() == '\'' {
					lexer.Advance(false)
					lexer.MarkEnd()
					delimiter |= pyDelimTriple
				}
			}
		case '"':
			delimiter |= pyDelimDoubleQuote
			lexer.Advance(false)
			lexer.MarkEnd()
			if lexer.Lookahead() == '"' {
				lexer.Advance(false)
				if lexer.Lookahead() == '"' {
					lexer.Advance(false)
					lexer.MarkEnd()
					delimiter |= pyDelimTriple
				}
			}
		}

		if delimiter.endChar() != 0 {
			s.delimiters = append(s.delimiters, delimiter)
			lexer.SetResultSymbol(symbols[pyTokStringStart])
			s.insideInterpolatedString = delimiter.isFormat()
			return true
		}
		if hasFlags {
			return false
		}
	}

	return false
}
