package js

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/test"
)

type TTs []TokenType

func TestTokens(t *testing.T) {
	var tokenTests = []struct {
		js       string
		expected []TokenType
	}{
		{" \t\v\f\u00A0\uFEFF\u2000", TTs{}}, // WhitespaceToken
		{"\n\r\r\n\u2028\u2029", TTs{LineTerminatorToken}},
		{"5.2 .04 1. 2.e3 0x0F 5e99", TTs{DecimalToken, DecimalToken, DecimalToken, DecimalToken, HexadecimalToken, DecimalToken}},
		{"2_3 5_4.1_2 1_1n 0o2_3 0b1_1 0xF_F", TTs{IntegerToken, DecimalToken, IntegerToken, OctalToken, BinaryToken, HexadecimalToken}},
		{"0o22 0b11", TTs{OctalToken, BinaryToken}},
		{"0n 2345n 0o5n 0b1n 0x5n 435.333n", TTs{IntegerToken, IntegerToken, OctalToken, BinaryToken, HexadecimalToken, DecimalToken, ErrorToken}},
		{"a = 'string'", TTs{IdentifierToken, EqToken, StringToken}},
		{"/*comment*/ //comment", TTs{CommentToken, CommentToken}},
		{"{ } ( ) [ ]", TTs{OpenBraceToken, CloseBraceToken, OpenParenToken, CloseParenToken, OpenBracketToken, CloseBracketToken}},
		{". ; , < > <= ...", TTs{DotToken, SemicolonToken, CommaToken, LtToken, GtToken, LtEqToken, EllipsisToken}},
		{">= == != === !==", TTs{GtEqToken, EqEqToken, NotEqToken, EqEqEqToken, NotEqEqToken}},
		{"+ - * / % ** ++ --", TTs{AddToken, SubToken, MulToken, DivToken, ModToken, ExpToken, IncrToken, DecrToken}},
		{"<< >> >>> & | ^", TTs{LtLtToken, GtGtToken, GtGtGtToken, BitAndToken, BitOrToken, BitXorToken}},
		{"! ~ && || ? : ?? ?.", TTs{NotToken, BitNotToken, AndToken, OrToken, QuestionToken, ColonToken, NullishToken, OptChainToken}},
		{"= += -= *= **= /= %= <<=", TTs{EqToken, AddEqToken, SubEqToken, MulEqToken, ExpEqToken, DivEqToken, ModEqToken, LtLtEqToken}},
		{">>= >>>= &= |= ^= =>", TTs{GtGtEqToken, GtGtGtEqToken, BitAndEqToken, BitOrEqToken, BitXorEqToken, ArrowToken}},
		{"&&= ||= ??=", TTs{AndEqToken, OrEqToken, NullishEqToken}},
		{"?.5", TTs{QuestionToken, DecimalToken}},
		{"?.a", TTs{OptChainToken, IdentifierToken}},
		{"await break case catch class const continue", TTs{AwaitToken, BreakToken, CaseToken, CatchToken, ClassToken, ConstToken, ContinueToken}},
		{"debugger default delete do else enum export extends", TTs{DebuggerToken, DefaultToken, DeleteToken, DoToken, ElseToken, EnumToken, ExportToken, ExtendsToken}},
		{"false finally for function if import in instanceof", TTs{FalseToken, FinallyToken, ForToken, FunctionToken, IfToken, ImportToken, InToken, InstanceofToken}},
		{"new null return super switch this throw true", TTs{NewToken, NullToken, ReturnToken, SuperToken, SwitchToken, ThisToken, ThrowToken, TrueToken}},
		{"try typeof var void while with yield", TTs{TryToken, TypeofToken, VarToken, VoidToken, WhileToken, WithToken, YieldToken}},
		{"implements interface let package private protected public static", TTs{ImplementsToken, InterfaceToken, LetToken, PackageToken, PrivateToken, ProtectedToken, PublicToken, StaticToken}},
		{"as async from get meta of set target", TTs{AsToken, AsyncToken, FromToken, GetToken, MetaToken, OfToken, SetToken, TargetToken}},
		{"#ident", TTs{PrivateIdentifierToken}},

		{"/*co\nm\u2028m/*ent*/ //co//mment\u2029//comment", TTs{CommentLineTerminatorToken, CommentToken, LineTerminatorToken, CommentToken}},
		{"<!-", TTs{LtToken, NotToken, SubToken}},
		{"1<!--2\n", TTs{IntegerToken, CommentToken, LineTerminatorToken}},
		{"x=y-->10\n", TTs{IdentifierToken, EqToken, IdentifierToken, DecrToken, GtToken, IntegerToken, LineTerminatorToken}},
		{"  /*comment*/ -->nothing\n", TTs{CommentToken, DecrToken, GtToken, IdentifierToken, LineTerminatorToken}},
		{"1 /*comment\nmultiline*/ -->nothing\n", TTs{IntegerToken, CommentLineTerminatorToken, CommentToken, LineTerminatorToken}},
		{"$ _\u200C \\u2000 _\\u200C \u200C", TTs{IdentifierToken, IdentifierToken, IdentifierToken, IdentifierToken, ErrorToken}},
		{">>>=>>>>=", TTs{GtGtGtEqToken, GtGtGtToken, GtEqToken}},
		{"1/", TTs{IntegerToken, DivToken}},
		{"1/=", TTs{IntegerToken, DivEqToken}},
		{"'str\\i\\'ng'", TTs{StringToken}},
		{"'str\\\\'abc", TTs{StringToken, IdentifierToken}},
		{"'str\\\ni\\\\u00A0ng'", TTs{StringToken}},
		{"'str\u2028\u2029ing'", TTs{StringToken}},

		{"0b0101 0o0707 0b17", TTs{BinaryToken, OctalToken, BinaryToken, IntegerToken}},
		{"`template`", TTs{TemplateToken}},
		{"`a${x+y}b`", TTs{TemplateStartToken, IdentifierToken, AddToken, IdentifierToken, TemplateEndToken}},
		{"`tmpl${x}tmpl${x}`", TTs{TemplateStartToken, IdentifierToken, TemplateMiddleToken, IdentifierToken, TemplateEndToken}},
		{"`temp\nlate`", TTs{TemplateToken}},
		{"`outer${{x: 10}}bar${ raw`nested${2}endnest` }end`", TTs{TemplateStartToken, OpenBraceToken, IdentifierToken, ColonToken, IntegerToken, CloseBraceToken, TemplateMiddleToken, IdentifierToken, TemplateStartToken, IntegerToken, TemplateEndToken, TemplateEndToken}},
		{"`tmpl ${ a ? '' : `tmpl2 ${b ? 'b' : 'c'}` }`", TTs{TemplateStartToken, IdentifierToken, QuestionToken, StringToken, ColonToken, TemplateStartToken, IdentifierToken, QuestionToken, StringToken, ColonToken, StringToken, TemplateEndToken, TemplateEndToken}},

		// early endings
		{"'string", TTs{ErrorToken}},
		{"'\n", TTs{ErrorToken}},
		{"'\u2028", TTs{ErrorToken}},
		{"'str\\\U00100000ing\\0'", TTs{StringToken}},
		{"'strin\\00g'", TTs{StringToken}},
		{"/*comment", TTs{ErrorToken}},
		{"a=/regexp", TTs{IdentifierToken, EqToken, DivToken, IdentifierToken}},
		{"\\u002", TTs{ErrorToken}},
		{"`template", TTs{ErrorToken}},
		{"`template${x}template", TTs{TemplateStartToken, IdentifierToken, ErrorToken}},
		{"a++=1", TTs{IdentifierToken, IncrToken, EqToken, IntegerToken}},
		{"a++==1", TTs{IdentifierToken, IncrToken, EqEqToken, IntegerToken}},
		{"a++===1", TTs{IdentifierToken, IncrToken, EqEqEqToken, IntegerToken}},

		// null characters
		{"'string\x00'return", TTs{StringToken, ReturnToken}},
		{"//comment\x00comment\nreturn", TTs{CommentToken, LineTerminatorToken, ReturnToken}},
		{"/*comment\x00*/return", TTs{CommentToken, ReturnToken}},
		{"`template\x00`return", TTs{TemplateToken, ReturnToken}},
		{"`template\\\x00`return", TTs{TemplateToken, ReturnToken}},

		// numbers
		{"0xg", TTs{IntegerToken, ErrorToken}},
		{"0bg", TTs{IntegerToken, ErrorToken}},
		{"0og", TTs{IntegerToken, ErrorToken}},
		{"010", TTs{ErrorToken}}, // Decimal(0) Decimal(10) Identifier(xF)
		{"50e+-0", TTs{ErrorToken}},
		{"5.a", TTs{DecimalToken, ErrorToken}},
		{"5..a", TTs{DecimalToken, DotToken, IdentifierToken}},

		// coverage
		{"Ø a〉", TTs{IdentifierToken, IdentifierToken, ErrorToken}},
		{"\u00A0\uFEFF\u2000", TTs{}},
		{"\u2028\u2029", TTs{LineTerminatorToken}},
		{"\\u0029ident", TTs{IdentifierToken}},
		{"\\u{0029FEF}ident", TTs{IdentifierToken}},
		{"\\u{}", TTs{ErrorToken}},
		{"\\ugident", TTs{ErrorToken}},
		{"'str\ring'", TTs{ErrorToken}},
		{"a=/\\\n", TTs{IdentifierToken, EqToken, DivToken, ErrorToken}},
		{"a=/x\n", TTs{IdentifierToken, EqToken, DivToken, IdentifierToken, LineTerminatorToken}},
		{"`\\``", TTs{TemplateToken}},
		{"`\\${ 1 }`", TTs{TemplateToken}},
		{"`\\\r\n`", TTs{TemplateToken}},

		// go fuzz
		{"`", TTs{ErrorToken}},

		// issues
		{"_\u00bare_unicode_escape_identifier", TTs{IdentifierToken}}, // tdewolff/minify#449
	}

	for _, tt := range tokenTests {
		t.Run(tt.js, func(t *testing.T) {
			l := NewLexer(parse.NewInputString(tt.js))
			i := 0
			tokens := []TokenType{}
			for {
				token, _ := l.Next()
				if token == ErrorToken {
					if l.Err() != io.EOF {
						tokens = append(tokens, token)
					}
					break
				} else if token == WhitespaceToken {
					continue
				}
				tokens = append(tokens, token)
				i++
			}
			test.T(t, tokens, tt.expected, "token types must match")
		})
	}

	// coverage
	for _, start := range []int{0, 0x0100, 0x0200, 0x0600, 0x0800} {
		for i := start; ; i++ {
			if TokenType(i).String() == fmt.Sprintf("Invalid(%d)", i) {
				break
			}
		}
	}

	test.That(t, IsPunctuator(CommaToken))
	test.That(t, IsPunctuator(GtGtEqToken))
	test.That(t, !IsPunctuator(WhileToken))
	test.That(t, !IsOperator(CommaToken))
	test.That(t, IsOperator(GtGtEqToken))
	test.That(t, !IsOperator(WhileToken))
	test.That(t, !IsIdentifier(CommaToken))
	test.That(t, !IsIdentifier(GtGtEqToken))
	test.That(t, IsReservedWord(WhileToken))
	test.That(t, IsIdentifier(AsyncToken))
	test.That(t, IsIdentifierName(WhileToken))
	test.That(t, IsIdentifierName(AsToken))

	test.That(t, IsIdentifierStart([]byte("a")))
	test.That(t, !IsIdentifierStart([]byte("6")))
	test.That(t, !IsIdentifierStart([]byte("[")))
	test.That(t, IsIdentifierContinue([]byte("a")))
	test.That(t, IsIdentifierContinue([]byte("6")))
	test.That(t, !IsIdentifierContinue([]byte("[")))
	test.That(t, IsIdentifierEnd([]byte(".a")))
	test.That(t, IsIdentifierEnd([]byte(".6")))
	test.That(t, !IsIdentifierEnd([]byte(".[")))
}

func TestRegExp(t *testing.T) {
	var tokenTests = []struct {
		js       string
		expected []TokenType
	}{
		{"a = /[a-z/]/g", TTs{IdentifierToken, EqToken, RegExpToken}},
		{"a=/=/g1", TTs{IdentifierToken, EqToken, RegExpToken}},
		{"a = /'\\\\/\n", TTs{IdentifierToken, EqToken, RegExpToken, LineTerminatorToken}},
		{"a=/\\//g1", TTs{IdentifierToken, EqToken, RegExpToken}},
		{"new RegExp(a + /\\d{1,2}/.source)", TTs{NewToken, IdentifierToken, OpenParenToken, IdentifierToken, AddToken, RegExpToken, DotToken, IdentifierToken, CloseParenToken}},
		{"a=/regexp\x00/;return", TTs{IdentifierToken, EqToken, RegExpToken, SemicolonToken, ReturnToken}},
		{"a=/regexp\\\x00/;return", TTs{IdentifierToken, EqToken, RegExpToken, SemicolonToken, ReturnToken}},
		{"a=/x/\u200C\u3009", TTs{IdentifierToken, EqToken, RegExpToken, ErrorToken}},
		{"a=/end", TTs{IdentifierToken, EqToken, ErrorToken}},
		{"a=/\\\nend", TTs{IdentifierToken, EqToken, ErrorToken}},
		{"a=/\\\u2028", TTs{IdentifierToken, EqToken, ErrorToken}},
		{"a=/regexp/Ø", TTs{IdentifierToken, EqToken, RegExpToken}},
	}

	for _, tt := range tokenTests {
		t.Run(tt.js, func(t *testing.T) {
			l := NewLexer(parse.NewInputString(tt.js))
			i := 0
			tokens := []TokenType{}
			for {
				token, _ := l.Next()
				if token == DivToken || token == DivEqToken {
					token, _ = l.RegExp()
				}
				if token == ErrorToken {
					if l.Err() != io.EOF {
						tokens = append(tokens, token)
					}
					break
				} else if token == WhitespaceToken {
					continue
				}
				tokens = append(tokens, token)
				i++
			}
			test.T(t, tokens, tt.expected, "token types must match")
		})
	}

	token, _ := NewLexer(parse.NewInputString("")).RegExp()
	test.T(t, token, ErrorToken)
}

func TestOffset(t *testing.T) {
	z := parse.NewInputString(`var i=5;`)
	l := NewLexer(z)
	test.T(t, z.Offset(), 0)
	_, _ = l.Next()
	test.T(t, z.Offset(), 3) // var
	_, _ = l.Next()
	test.T(t, z.Offset(), 4) // ws
	_, _ = l.Next()
	test.T(t, z.Offset(), 5) // i
	_, _ = l.Next()
	test.T(t, z.Offset(), 6) // =
	_, _ = l.Next()
	test.T(t, z.Offset(), 7) // 5
	_, _ = l.Next()
	test.T(t, z.Offset(), 8) // ;
}

func TestLexerErrors(t *testing.T) {
	var tests = []struct {
		js  string
		err string
	}{
		{"@", "unexpected @"},
		{"\x00", "unexpected 0x00"},
		{"\x7f", "unexpected 0x7F"},
		{"\u200F", "unexpected U+200F"},
		{"\u2010", "unexpected \u2010"},
		{".0E", "invalid number"},
		{`"a`, "unterminated string literal"},
		{"'a\nb'", "unterminated string literal"},
		{"`", "unterminated template literal"},
	}

	for _, tt := range tests {
		t.Run(tt.js, func(t *testing.T) {
			l := NewLexer(parse.NewInputString(tt.js))
			for l.Err() == nil {
				l.Next()
			}
			test.T(t, l.Err().(*parse.Error).Message, tt.err)
		})
	}

	l := NewLexer(parse.NewInputString(""))
	l.RegExp()
	test.T(t, l.Err().(*parse.Error).Message, "expected / or /=")

	l = NewLexer(parse.NewInputString("/"))
	l.Next()
	l.RegExp()
	test.T(t, l.Err().(*parse.Error).Message, "unexpected EOF or newline")

	// see #118
	l = NewLexer(parse.NewInputString("\uFFFDa"))
	tt, data := l.Next()
	test.T(t, tt, ErrorToken)
	test.T(t, string(data), "�")
	test.T(t, l.Err().(*parse.Error).Message, "unexpected �")
	tt, data = l.Next()
	test.T(t, tt, IdentifierToken)
	test.T(t, string(data), "a")
}

////////////////////////////////////////////////////////////////

func ExampleNewLexer() {
	l := NewLexer(parse.NewInputString("var x = 'lorem ipsum';"))
	var out strings.Builder
	for {
		tt, data := l.Next()
		if tt == ErrorToken {
			break
		}
		out.WriteString(string(data))
	}
	fmt.Println(out.String())
	// Output: var x = 'lorem ipsum';
}
