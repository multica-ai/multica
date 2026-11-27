// Package js is an ECMAScript5.1 lexer following the specifications at http://www.ecma-international.org/ecma-262/5.1/.
package js

import (
	"unicode"
	"unicode/utf8"

	"github.com/tdewolff/parse/v2"
)

var identifierStart = []*unicode.RangeTable{unicode.Lu, unicode.Ll, unicode.Lt, unicode.Lm, unicode.Lo, unicode.Nl, unicode.Other_ID_Start}
var identifierContinue = []*unicode.RangeTable{unicode.Lu, unicode.Ll, unicode.Lt, unicode.Lm, unicode.Lo, unicode.Nl, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue}

// IsIdentifierStart returns true if the byte-slice start is the start of an identifier
func IsIdentifierStart(b []byte) bool {
	r, _ := utf8.DecodeRune(b)
	return r == '$' || r == '\\' || r == '_' || unicode.IsOneOf(identifierStart, r)
}

// IsIdentifierContinue returns true if the byte-slice start is a continuation of an identifier
func IsIdentifierContinue(b []byte) bool {
	r, _ := utf8.DecodeRune(b)
	return r == '$' || r == '\\' || r == '\u200C' || r == '\u200D' || unicode.IsOneOf(identifierContinue, r)
}

// IsIdentifierEnd returns true if the byte-slice end is a start or continuation of an identifier
func IsIdentifierEnd(b []byte) bool {
	r, _ := utf8.DecodeLastRune(b)
	return r == '$' || r == '\\' || r == '\u200C' || r == '\u200D' || unicode.IsOneOf(identifierContinue, r)
}

////////////////////////////////////////////////////////////////

// Lexer is the state for the lexer.
type Lexer struct {
	r                  *parse.Input
	err                error
	prevLineTerminator bool
	prevNumericLiteral bool
	level              int
	templateLevels     []int
}

// NewLexer returns a new Lexer for a given io.Reader.
func NewLexer(r *parse.Input) *Lexer {
	return &Lexer{
		r:                  r,
		prevLineTerminator: true,
		level:              0,
		templateLevels:     []int{},
	}
}

// Err returns the error encountered during lexing, this is often io.EOF but also other errors can be returned.
func (l *Lexer) Err() error {
	if l.err != nil {
		return l.err
	}
	return l.r.Err()
}

// RegExp reparses the input stream for a regular expression. It is assumed that we just received DivToken or DivEqToken with Next(). This function will go back and read that as a regular expression.
func (l *Lexer) RegExp() (TokenType, []byte) {
	if 0 < l.r.Offset() && l.r.Peek(-1) == '/' {
		l.r.Move(-1)
	} else if 1 < l.r.Offset() && l.r.Peek(-1) == '=' && l.r.Peek(-2) == '/' {
		l.r.Move(-2)
	} else {
		l.err = parse.NewErrorLexer(l.r, "expected / or /=")
		return ErrorToken, nil
	}
	l.r.Skip() // trick to set start = pos

	if l.consumeRegExpToken() {
		return RegExpToken, l.r.Shift()
	}
	l.err = parse.NewErrorLexer(l.r, "unexpected EOF or newline")
	return ErrorToken, nil
}

// Next returns the next Token. It returns ErrorToken when an error was encountered. Using Err() one can retrieve the error message.
func (l *Lexer) Next() (TokenType, []byte) {
	l.err = nil // clear error from previous ErrorToken
	prevLineTerminator := l.prevLineTerminator
	l.prevLineTerminator = false

	prevNumericLiteral := l.prevNumericLiteral
	l.prevNumericLiteral = false

	// study on 50x jQuery shows:
	// spaces: 20k
	// alpha: 16k
	// newlines: 14.4k
	// operators: 4k
	// numbers and dot: 3.6k
	// (): 3.4k
	// {}: 1.8k
	// []: 0.9k
	// "': 1k
	// semicolon: 2.4k
	// colon: 0.8k
	// comma: 2.4k
	// slash: 1.4k
	// `~: almost 0

	c := l.r.Peek(0)
	switch c {
	case ' ', '\t', '\v', '\f':
		l.r.Move(1)
		for l.consumeWhitespace() {
		}
		l.prevLineTerminator = prevLineTerminator
		return WhitespaceToken, l.r.Shift()
	case '\n', '\r':
		l.r.Move(1)
		for l.consumeLineTerminator() {
		}
		l.prevLineTerminator = true
		return LineTerminatorToken, l.r.Shift()
	case '>', '=', '!', '+', '*', '%', '&', '|', '^', '~', '?':
		if tt := l.consumeOperatorToken(); tt != ErrorToken {
			return tt, l.r.Shift()
		}
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.':
		if tt := l.consumeNumericToken(); tt != ErrorToken || l.r.Pos() != 0 {
			l.prevNumericLiteral = true
			return tt, l.r.Shift()
		} else if c == '.' {
			l.r.Move(1)
			if l.r.Peek(0) == '.' && l.r.Peek(1) == '.' {
				l.r.Move(2)
				return EllipsisToken, l.r.Shift()
			}
			return DotToken, l.r.Shift()
		}
	case ',':
		l.r.Move(1)
		return CommaToken, l.r.Shift()
	case ';':
		l.r.Move(1)
		return SemicolonToken, l.r.Shift()
	case '(':
		l.level++
		l.r.Move(1)
		return OpenParenToken, l.r.Shift()
	case ')':
		l.level--
		l.r.Move(1)
		return CloseParenToken, l.r.Shift()
	case '/':
		if tt := l.consumeCommentToken(); tt != ErrorToken || l.err != nil {
			if l.err != nil {
				return ErrorToken, nil
			}
			return tt, l.r.Shift()
		} else if tt := l.consumeOperatorToken(); tt != ErrorToken {
			return tt, l.r.Shift()
		}
	case '{':
		l.level++
		l.r.Move(1)
		return OpenBraceToken, l.r.Shift()
	case '}':
		l.level--
		if len(l.templateLevels) != 0 && l.level == l.templateLevels[len(l.templateLevels)-1] {
			return l.consumeTemplateToken(), l.r.Shift()
		}
		l.r.Move(1)
		return CloseBraceToken, l.r.Shift()
	case ':':
		l.r.Move(1)
		return ColonToken, l.r.Shift()
	case '\'', '"':
		return l.consumeStringToken(), l.r.Shift()
	case ']':
		l.r.Move(1)
		return CloseBracketToken, l.r.Shift()
	case '[':
		l.r.Move(1)
		return OpenBracketToken, l.r.Shift()
	case '<', '-':
		if l.consumeHTMLLikeCommentToken(prevLineTerminator) {
			return CommentToken, l.r.Shift()
		} else if tt := l.consumeOperatorToken(); tt != ErrorToken {
			return tt, l.r.Shift()
		}
	case '`':
		l.templateLevels = append(l.templateLevels, l.level)
		return l.consumeTemplateToken(), l.r.Shift()
	case '#':
		l.r.Move(1)
		if l.consumeIdentifierToken() {
			return PrivateIdentifierToken, l.r.Shift()
		}
	default:
		if l.consumeIdentifierToken() {
			if prevNumericLiteral {
				l.err = parse.NewErrorLexer(l.r, "unexpected identifier after number")
				return ErrorToken, nil
			} else if keyword, ok := Keywords[string(l.r.Lexeme())]; ok {
				return keyword, l.r.Shift()
			}
			return IdentifierToken, l.r.Shift()
		}
		if 0xC0 <= c {
			if l.consumeWhitespace() {
				for l.consumeWhitespace() {
				}
				l.prevLineTerminator = prevLineTerminator
				return WhitespaceToken, l.r.Shift()
			} else if l.consumeLineTerminator() {
				for l.consumeLineTerminator() {
				}
				l.prevLineTerminator = true
				return LineTerminatorToken, l.r.Shift()
			}
		} else if c == 0 && l.r.Err() != nil {
			return ErrorToken, nil
		}
	}

	r, _ := l.r.PeekRune(0)
	l.err = parse.NewErrorLexer(l.r, "unexpected %s", parse.Printable(r))
	l.r.MoveRune() // allow to continue after error
	return ErrorToken, l.r.Shift()
}

////////////////////////////////////////////////////////////////

/*
The following functions follow the specifications at http://www.ecma-international.org/ecma-262/5.1/
*/

func (l *Lexer) consumeWhitespace() bool {
	c := l.r.Peek(0)
	if c == ' ' || c == '\t' || c == '\v' || c == '\f' {
		l.r.Move(1)
		return true
	} else if 0xC0 <= c {
		if r, n := l.r.PeekRune(0); r == '\u00A0' || r == '\uFEFF' || unicode.Is(unicode.Zs, r) {
			l.r.Move(n)
			return true
		}
	}
	return false
}

func (l *Lexer) isLineTerminator() bool {
	c := l.r.Peek(0)
	if c == '\n' || c == '\r' {
		return true
	} else if c == 0xE2 && l.r.Peek(1) == 0x80 && (l.r.Peek(2) == 0xA8 || l.r.Peek(2) == 0xA9) {
		return true
	}
	return false
}

func (l *Lexer) consumeLineTerminator() bool {
	c := l.r.Peek(0)
	if c == '\n' {
		l.r.Move(1)
		return true
	} else if c == '\r' {
		if l.r.Peek(1) == '\n' {
			l.r.Move(2)
		} else {
			l.r.Move(1)
		}
		return true
	} else if c == 0xE2 && l.r.Peek(1) == 0x80 && (l.r.Peek(2) == 0xA8 || l.r.Peek(2) == 0xA9) {
		l.r.Move(3)
		return true
	}
	return false
}

func (l *Lexer) consumeDigit() bool {
	if c := l.r.Peek(0); c >= '0' && c <= '9' {
		l.r.Move(1)
		return true
	}
	return false
}

func (l *Lexer) consumeHexDigit() bool {
	if c := l.r.Peek(0); (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
		l.r.Move(1)
		return true
	}
	return false
}

func (l *Lexer) consumeBinaryDigit() bool {
	if c := l.r.Peek(0); c == '0' || c == '1' {
		l.r.Move(1)
		return true
	}
	return false
}

func (l *Lexer) consumeOctalDigit() bool {
	if c := l.r.Peek(0); c >= '0' && c <= '7' {
		l.r.Move(1)
		return true
	}
	return false
}

func (l *Lexer) consumeUnicodeEscape() bool {
	if l.r.Peek(0) != '\\' || l.r.Peek(1) != 'u' {
		return false
	}
	mark := l.r.Pos()
	l.r.Move(2)
	if c := l.r.Peek(0); c == '{' {
		l.r.Move(1)
		if l.consumeHexDigit() {
			for l.consumeHexDigit() {
			}
			if c := l.r.Peek(0); c == '}' {
				l.r.Move(1)
				return true
			}
		}
		l.r.Rewind(mark)
		return false
	} else if !l.consumeHexDigit() || !l.consumeHexDigit() || !l.consumeHexDigit() || !l.consumeHexDigit() {
		l.r.Rewind(mark)
		return false
	}
	return true
}

func (l *Lexer) consumeSingleLineComment() {
	for {
		c := l.r.Peek(0)
		if c == '\r' || c == '\n' || c == 0 && l.r.Err() != nil {
			break
		} else if 0xC0 <= c {
			if r, _ := l.r.PeekRune(0); r == '\u2028' || r == '\u2029' {
				break
			}
		}
		l.r.Move(1)
	}
}

////////////////////////////////////////////////////////////////

func (l *Lexer) consumeHTMLLikeCommentToken(prevLineTerminator bool) bool {
	c := l.r.Peek(0)
	if c == '<' && l.r.Peek(1) == '!' && l.r.Peek(2) == '-' && l.r.Peek(3) == '-' {
		// opening HTML-style single line comment
		l.r.Move(4)
		l.consumeSingleLineComment()
		return true
	} else if prevLineTerminator && c == '-' && l.r.Peek(1) == '-' && l.r.Peek(2) == '>' {
		// closing HTML-style single line comment
		// (only if current line didn't contain any meaningful tokens)
		l.r.Move(3)
		l.consumeSingleLineComment()
		return true
	}
	return false
}

func (l *Lexer) consumeCommentToken() TokenType {
	c := l.r.Peek(1)
	if c == '/' {
		// single line comment
		l.r.Move(2)
		l.consumeSingleLineComment()
		return CommentToken
	} else if c == '*' {
		l.r.Move(2)
		tt := CommentToken
		for {
			c := l.r.Peek(0)
			if c == '*' && l.r.Peek(1) == '/' {
				l.r.Move(2)
				break
			} else if c == 0 && l.r.Err() != nil {
				l.err = parse.NewErrorLexer(l.r, "unexpected EOF in comment")
				return ErrorToken
			} else if l.consumeLineTerminator() {
				l.prevLineTerminator = true
				tt = CommentLineTerminatorToken
			} else {
				l.r.Move(1)
			}
		}
		return tt
	}
	return ErrorToken
}

var opTokens = map[byte]TokenType{
	'=': EqToken,
	'!': NotToken,
	'<': LtToken,
	'>': GtToken,
	'+': AddToken,
	'-': SubToken,
	'*': MulToken,
	'/': DivToken,
	'%': ModToken,
	'&': BitAndToken,
	'|': BitOrToken,
	'^': BitXorToken,
	'~': BitNotToken,
	'?': QuestionToken,
}

var opEqTokens = map[byte]TokenType{
	'=': EqEqToken,
	'!': NotEqToken,
	'<': LtEqToken,
	'>': GtEqToken,
	'+': AddEqToken,
	'-': SubEqToken,
	'*': MulEqToken,
	'/': DivEqToken,
	'%': ModEqToken,
	'&': BitAndEqToken,
	'|': BitOrEqToken,
	'^': BitXorEqToken,
}

var opOpTokens = map[byte]TokenType{
	'<': LtLtToken,
	'+': IncrToken,
	'-': DecrToken,
	'*': ExpToken,
	'&': AndToken,
	'|': OrToken,
	'?': NullishToken,
}

var opOpEqTokens = map[byte]TokenType{
	'<': LtLtEqToken,
	'*': ExpEqToken,
	'&': AndEqToken,
	'|': OrEqToken,
	'?': NullishEqToken,
}

func (l *Lexer) consumeOperatorToken() TokenType {
	c := l.r.Peek(0)
	l.r.Move(1)
	if l.r.Peek(0) == '=' {
		l.r.Move(1)
		if l.r.Peek(0) == '=' && (c == '!' || c == '=') {
			l.r.Move(1)
			if c == '!' {
				return NotEqEqToken
			}
			return EqEqEqToken
		}
		return opEqTokens[c]
	} else if l.r.Peek(0) == c && (c == '+' || c == '-' || c == '*' || c == '&' || c == '|' || c == '?' || c == '<') {
		l.r.Move(1)
		if l.r.Peek(0) == '=' && c != '+' && c != '-' {
			l.r.Move(1)
			return opOpEqTokens[c]
		}
		return opOpTokens[c]
	} else if c == '?' && l.r.Peek(0) == '.' && (l.r.Peek(1) < '0' || l.r.Peek(1) > '9') {
		l.r.Move(1)
		return OptChainToken
	} else if c == '=' && l.r.Peek(0) == '>' {
		l.r.Move(1)
		return ArrowToken
	} else if c == '>' && l.r.Peek(0) == '>' {
		l.r.Move(1)
		if l.r.Peek(0) == '>' {
			l.r.Move(1)
			if l.r.Peek(0) == '=' {
				l.r.Move(1)
				return GtGtGtEqToken
			}
			return GtGtGtToken
		} else if l.r.Peek(0) == '=' {
			l.r.Move(1)
			return GtGtEqToken
		}
		return GtGtToken
	}
	return opTokens[c]
}

func (l *Lexer) consumeIdentifierToken() bool {
	c := l.r.Peek(0)
	if identifierStartTable[c] {
		l.r.Move(1)
	} else if 0xC0 <= c {
		if r, n := l.r.PeekRune(0); unicode.IsOneOf(identifierStart, r) {
			l.r.Move(n)
		} else {
			return false
		}
	} else if !l.consumeUnicodeEscape() {
		return false
	}
	for {
		c := l.r.Peek(0)
		if identifierTable[c] {
			l.r.Move(1)
		} else if 0xC0 <= c {
			if r, n := l.r.PeekRune(0); r == '\u200C' || r == '\u200D' || unicode.IsOneOf(identifierContinue, r) {
				l.r.Move(n)
			} else {
				break
			}
		} else if !l.consumeUnicodeEscape() {
			break
		}
	}
	return true
}

func (l *Lexer) consumeNumericSeparator(f func() bool) bool {
	if l.r.Peek(0) != '_' {
		return false
	}
	l.r.Move(1)
	if !f() {
		l.r.Move(-1)
		return false
	}
	return true
}

func (l *Lexer) consumeNumericToken() TokenType {
	// assume to be on 0 1 2 3 4 5 6 7 8 9 .
	first := l.r.Peek(0)
	if first == '0' {
		l.r.Move(1)
		if l.r.Peek(0) == 'x' || l.r.Peek(0) == 'X' {
			l.r.Move(1)
			if l.consumeHexDigit() {
				for l.consumeHexDigit() || l.consumeNumericSeparator(l.consumeHexDigit) {
				}
				if l.r.Peek(0) == 'n' {
					l.r.Move(1)
				}
				return HexadecimalToken
			}
			l.r.Move(-1)
			return IntegerToken
		} else if l.r.Peek(0) == 'b' || l.r.Peek(0) == 'B' {
			l.r.Move(1)
			if l.consumeBinaryDigit() {
				for l.consumeBinaryDigit() || l.consumeNumericSeparator(l.consumeBinaryDigit) {
				}
				if l.r.Peek(0) == 'n' {
					l.r.Move(1)
				}
				return BinaryToken
			}
			l.r.Move(-1)
			return IntegerToken
		} else if l.r.Peek(0) == 'o' || l.r.Peek(0) == 'O' {
			l.r.Move(1)
			if l.consumeOctalDigit() {
				for l.consumeOctalDigit() || l.consumeNumericSeparator(l.consumeOctalDigit) {
				}
				if l.r.Peek(0) == 'n' {
					l.r.Move(1)
				}
				return OctalToken
			}
			l.r.Move(-1)
			return IntegerToken
		} else if l.r.Peek(0) == 'n' {
			l.r.Move(1)
			return IntegerToken
		} else if '0' <= l.r.Peek(0) && l.r.Peek(0) <= '9' {
			l.err = parse.NewErrorLexer(l.r, "legacy octal numbers are not supported")
			return ErrorToken
		}
	} else if first != '.' {
		for l.consumeDigit() || l.consumeNumericSeparator(l.consumeDigit) {
		}
	}
	// we have parsed a 0 or an integer number
	c := l.r.Peek(0)
	if c == '.' {
		l.r.Move(1)
		if l.consumeDigit() {
			for l.consumeDigit() || l.consumeNumericSeparator(l.consumeDigit) {
			}
			c = l.r.Peek(0)
		} else if first == '.' {
			// number starts with a dot and must be followed by digits
			l.r.Move(-1)
			return ErrorToken // may be dot or ellipsis
		} else {
			c = l.r.Peek(0)
		}
	} else if c == 'n' {
		l.r.Move(1)
		return IntegerToken
	} else if c != 'e' && c != 'E' {
		return IntegerToken
	}
	if c == 'e' || c == 'E' {
		l.r.Move(1)
		c = l.r.Peek(0)
		if c == '+' || c == '-' {
			l.r.Move(1)
		}
		if !l.consumeDigit() {
			l.err = parse.NewErrorLexer(l.r, "invalid number")
			return ErrorToken
		}
		for l.consumeDigit() || l.consumeNumericSeparator(l.consumeDigit) {
		}
	}
	return DecimalToken
}

func (l *Lexer) consumeStringToken() TokenType {
	// assume to be on ' or "
	delim := l.r.Peek(0)
	l.r.Move(1)
	for {
		c := l.r.Peek(0)
		if c == delim {
			l.r.Move(1)
			break
		} else if c == '\\' {
			l.r.Move(1)
			if !l.consumeLineTerminator() {
				if c := l.r.Peek(0); c == delim || c == '\\' {
					l.r.Move(1)
				}
			}
			continue
		} else if c == '\n' || c == '\r' || c == 0 && l.r.Err() != nil {
			l.err = parse.NewErrorLexer(l.r, "unterminated string literal")
			return ErrorToken
		}
		l.r.Move(1)
	}
	return StringToken
}

func (l *Lexer) consumeRegExpToken() bool {
	// assume to be on /
	l.r.Move(1)
	inClass := false
	for {
		c := l.r.Peek(0)
		if !inClass && c == '/' {
			l.r.Move(1)
			break
		} else if c == '[' {
			inClass = true
		} else if c == ']' {
			inClass = false
		} else if c == '\\' {
			l.r.Move(1)
			if l.isLineTerminator() || l.r.Peek(0) == 0 && l.r.Err() != nil {
				return false
			}
		} else if l.isLineTerminator() || c == 0 && l.r.Err() != nil {
			return false
		}
		l.r.Move(1)
	}
	// flags
	for {
		c := l.r.Peek(0)
		if identifierTable[c] {
			l.r.Move(1)
		} else if 0xC0 <= c {
			if r, n := l.r.PeekRune(0); r == '\u200C' || r == '\u200D' || unicode.IsOneOf(identifierContinue, r) {
				l.r.Move(n)
			} else {
				break
			}
		} else {
			break
		}
	}
	return true
}

func (l *Lexer) consumeTemplateToken() TokenType {
	// assume to be on ` or } when already within template
	continuation := l.r.Peek(0) == '}'
	l.r.Move(1)
	for {
		c := l.r.Peek(0)
		if c == '`' {
			l.templateLevels = l.templateLevels[:len(l.templateLevels)-1]
			l.r.Move(1)
			if continuation {
				return TemplateEndToken
			}
			return TemplateToken
		} else if c == '$' && l.r.Peek(1) == '{' {
			l.level++
			l.r.Move(2)
			if continuation {
				return TemplateMiddleToken
			}
			return TemplateStartToken
		} else if c == '\\' {
			l.r.Move(1)
			if c := l.r.Peek(0); c != 0 {
				l.r.Move(1)
			}
			continue
		} else if c == 0 && l.r.Err() != nil {
			l.err = parse.NewErrorLexer(l.r, "unterminated template literal")
			return ErrorToken
		}
		l.r.Move(1)
	}
}

var identifierStartTable = [256]bool{
	// ASCII
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, true, false, false, false, // $
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, true, true, true, true, true, true, true, // A, B, C, D, E, F, G
	true, true, true, true, true, true, true, true, // H, I, J, K, L, M, N, O
	true, true, true, true, true, true, true, true, // P, Q, R, S, T, U, V, W
	true, true, true, false, false, false, false, true, // X, Y, Z, _

	false, true, true, true, true, true, true, true, // a, b, c, d, e, f, g
	true, true, true, true, true, true, true, true, // h, i, j, k, l, m, n, o
	true, true, true, true, true, true, true, true, // p, q, r, s, t, u, v, w
	true, true, true, false, false, false, false, false, // x, y, z

	// non-ASCII
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
}

var identifierTable = [256]bool{
	// ASCII
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, true, false, false, false, // $
	false, false, false, false, false, false, false, false,
	true, true, true, true, true, true, true, true, // 0, 1, 2, 3, 4, 5, 6, 7
	true, true, false, false, false, false, false, false, // 8, 9

	false, true, true, true, true, true, true, true, // A, B, C, D, E, F, G
	true, true, true, true, true, true, true, true, // H, I, J, K, L, M, N, O
	true, true, true, true, true, true, true, true, // P, Q, R, S, T, U, V, W
	true, true, true, false, false, false, false, true, // X, Y, Z, _

	false, true, true, true, true, true, true, true, // a, b, c, d, e, f, g
	true, true, true, true, true, true, true, true, // h, i, j, k, l, m, n, o
	true, true, true, true, true, true, true, true, // p, q, r, s, t, u, v, w
	true, true, true, false, false, false, false, false, // x, y, z

	// non-ASCII
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,

	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
	false, false, false, false, false, false, false, false,
}
