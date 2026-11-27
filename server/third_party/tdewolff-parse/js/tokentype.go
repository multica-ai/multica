package js

import "strconv"

// TokenType determines the type of token, eg. a number or a semicolon.
type TokenType uint16 // from LSB to MSB: 8 bits for tokens per category, 1 bit for numeric, 1 bit for punctuator, 1 bit for operator, 1 bit for identifier, 4 bits unused

// TokenType values.
const (
	ErrorToken TokenType = iota // extra token when errors occur
	WhitespaceToken
	LineTerminatorToken // \r \n \r\n
	CommentToken
	CommentLineTerminatorToken
	StringToken
	TemplateToken
	TemplateStartToken
	TemplateMiddleToken
	TemplateEndToken
	RegExpToken
	PrivateIdentifierToken
)

// Numeric token values.
const (
	NumericToken TokenType = 0x0100 + iota
	DecimalToken
	BinaryToken
	OctalToken
	HexadecimalToken
	IntegerToken
)

// Punctuator token values.
const (
	PunctuatorToken   TokenType = 0x0200 + iota
	OpenBraceToken              // {
	CloseBraceToken             // }
	OpenParenToken              // (
	CloseParenToken             // )
	OpenBracketToken            // [
	CloseBracketToken           // ]
	DotToken                    // .
	SemicolonToken              // ;
	CommaToken                  // ,
	QuestionToken               // ?
	ColonToken                  // :
	ArrowToken                  // =>
	EllipsisToken               // ...
)

// Operator token values.
const (
	OperatorToken  TokenType = 0x0600 + iota
	EqToken                  // =
	EqEqToken                // ==
	EqEqEqToken              // ===
	NotToken                 // !
	NotEqToken               // !=
	NotEqEqToken             // !==
	LtToken                  // <
	LtEqToken                // <=
	LtLtToken                // <<
	LtLtEqToken              // <<=
	GtToken                  // >
	GtEqToken                // >=
	GtGtToken                // >>
	GtGtEqToken              // >>=
	GtGtGtToken              // >>>
	GtGtGtEqToken            // >>>=
	AddToken                 // +
	AddEqToken               // +=
	IncrToken                // ++
	SubToken                 // -
	SubEqToken               // -=
	DecrToken                // --
	MulToken                 // *
	MulEqToken               // *=
	ExpToken                 // **
	ExpEqToken               // **=
	DivToken                 // /
	DivEqToken               // /=
	ModToken                 // %
	ModEqToken               // %=
	BitAndToken              // &
	BitOrToken               // |
	BitXorToken              // ^
	BitNotToken              // ~
	BitAndEqToken            // &=
	BitOrEqToken             // |=
	BitXorEqToken            // ^=
	AndToken                 // &&
	OrToken                  // ||
	NullishToken             // ??
	AndEqToken               // &&=
	OrEqToken                // ||=
	NullishEqToken           // ??=
	OptChainToken            // ?.

	// unused in lexer
	PosToken      // +a
	NegToken      // -a
	PreIncrToken  // ++a
	PreDecrToken  // --a
	PostIncrToken // a++
	PostDecrToken // a--
)

// Reserved token values.
const (
	ReservedToken TokenType = 0x0800 + iota
	AwaitToken
	BreakToken
	CaseToken
	CatchToken
	ClassToken
	ConstToken
	ContinueToken
	DebuggerToken
	DefaultToken
	DeleteToken
	DoToken
	ElseToken
	EnumToken
	ExportToken
	ExtendsToken
	FalseToken
	FinallyToken
	ForToken
	FunctionToken
	IfToken
	ImportToken
	InToken
	InstanceofToken
	NewToken
	NullToken
	ReturnToken
	SuperToken
	SwitchToken
	ThisToken
	ThrowToken
	TrueToken
	TryToken
	TypeofToken
	YieldToken
	VarToken
	VoidToken
	WhileToken
	WithToken
)

// Identifier token values.
const (
	IdentifierToken TokenType = 0x1000 + iota
	AsToken
	AsyncToken
	FromToken
	GetToken
	ImplementsToken
	InterfaceToken
	LetToken
	MetaToken
	OfToken
	PackageToken
	PrivateToken
	ProtectedToken
	PublicToken
	SetToken
	StaticToken
	TargetToken
)

// IsNumeric return true if token is numeric.
func IsNumeric(tt TokenType) bool {
	return tt&0x0100 != 0
}

// IsPunctuator return true if token is a punctuator.
func IsPunctuator(tt TokenType) bool {
	return tt&0x0200 != 0
}

// IsOperator return true if token is an operator.
func IsOperator(tt TokenType) bool {
	return tt&0x0400 != 0
}

// IsIdentifierName matches IdentifierName, i.e. any identifier
func IsIdentifierName(tt TokenType) bool {
	return tt&0x1800 != 0
}

// IsReservedWord matches ReservedWord
func IsReservedWord(tt TokenType) bool {
	return tt&0x0800 != 0
}

// IsIdentifier matches Identifier, i.e. IdentifierName but not ReservedWord. Does not match yield or await.
func IsIdentifier(tt TokenType) bool {
	return tt&0x1000 != 0
}

func (tt TokenType) String() string {
	s := tt.Bytes()
	if s == nil {
		return "Invalid(" + strconv.Itoa(int(tt)) + ")"
	}
	return string(s)
}

var operatorBytes = [][]byte{
	[]byte("Operator"),
	[]byte("="),
	[]byte("=="),
	[]byte("==="),
	[]byte("!"),
	[]byte("!="),
	[]byte("!=="),
	[]byte("<"),
	[]byte("<="),
	[]byte("<<"),
	[]byte("<<="),
	[]byte(">"),
	[]byte(">="),
	[]byte(">>"),
	[]byte(">>="),
	[]byte(">>>"),
	[]byte(">>>="),
	[]byte("+"),
	[]byte("+="),
	[]byte("++"),
	[]byte("-"),
	[]byte("-="),
	[]byte("--"),
	[]byte("*"),
	[]byte("*="),
	[]byte("**"),
	[]byte("**="),
	[]byte("/"),
	[]byte("/="),
	[]byte("%"),
	[]byte("%="),
	[]byte("&"),
	[]byte("|"),
	[]byte("^"),
	[]byte("~"),
	[]byte("&="),
	[]byte("|="),
	[]byte("^="),
	[]byte("&&"),
	[]byte("||"),
	[]byte("??"),
	[]byte("&&="),
	[]byte("||="),
	[]byte("??="),
	[]byte("?."),
	[]byte("+"),
	[]byte("-"),
	[]byte("++"),
	[]byte("--"),
	[]byte("++"),
	[]byte("--"),
}

var reservedWordBytes = [][]byte{
	[]byte("Reserved"),
	[]byte("await"),
	[]byte("break"),
	[]byte("case"),
	[]byte("catch"),
	[]byte("class"),
	[]byte("const"),
	[]byte("continue"),
	[]byte("debugger"),
	[]byte("default"),
	[]byte("delete"),
	[]byte("do"),
	[]byte("else"),
	[]byte("enum"),
	[]byte("export"),
	[]byte("extends"),
	[]byte("false"),
	[]byte("finally"),
	[]byte("for"),
	[]byte("function"),
	[]byte("if"),
	[]byte("import"),
	[]byte("in"),
	[]byte("instanceof"),
	[]byte("new"),
	[]byte("null"),
	[]byte("return"),
	[]byte("super"),
	[]byte("switch"),
	[]byte("this"),
	[]byte("throw"),
	[]byte("true"),
	[]byte("try"),
	[]byte("typeof"),
	[]byte("yield"),
	[]byte("var"),
	[]byte("void"),
	[]byte("while"),
	[]byte("with"),
}

var identifierBytes = [][]byte{
	[]byte("Identifier"),
	[]byte("as"),
	[]byte("async"),
	[]byte("from"),
	[]byte("get"),
	[]byte("implements"),
	[]byte("interface"),
	[]byte("let"),
	[]byte("meta"),
	[]byte("of"),
	[]byte("package"),
	[]byte("private"),
	[]byte("protected"),
	[]byte("public"),
	[]byte("set"),
	[]byte("static"),
	[]byte("target"),
}

// Bytes returns the string representation of a TokenType.
func (tt TokenType) Bytes() []byte {
	if IsOperator(tt) && int(tt-OperatorToken) < len(operatorBytes) {
		return operatorBytes[tt-OperatorToken]
	} else if IsReservedWord(tt) && int(tt-ReservedToken) < len(reservedWordBytes) {
		return reservedWordBytes[tt-ReservedToken]
	} else if IsIdentifier(tt) && int(tt-IdentifierToken) < len(identifierBytes) {
		return identifierBytes[tt-IdentifierToken]
	}

	switch tt {
	case ErrorToken:
		return []byte("Error")
	case WhitespaceToken:
		return []byte("Whitespace")
	case LineTerminatorToken:
		return []byte("LineTerminator")
	case CommentToken:
		return []byte("Comment")
	case CommentLineTerminatorToken:
		return []byte("CommentLineTerminator")
	case StringToken:
		return []byte("String")
	case TemplateToken:
		return []byte("Template")
	case TemplateStartToken:
		return []byte("TemplateStart")
	case TemplateMiddleToken:
		return []byte("TemplateMiddle")
	case TemplateEndToken:
		return []byte("TemplateEnd")
	case RegExpToken:
		return []byte("RegExp")
	case PrivateIdentifierToken:
		return []byte("PrivateIdentifier")
	case NumericToken:
		return []byte("Numeric")
	case DecimalToken:
		return []byte("Decimal")
	case BinaryToken:
		return []byte("Binary")
	case OctalToken:
		return []byte("Octal")
	case HexadecimalToken:
		return []byte("Hexadecimal")
	case IntegerToken:
		return []byte("Integer")
	case PunctuatorToken:
		return []byte("Punctuator")
	case OpenBraceToken:
		return []byte("{")
	case CloseBraceToken:
		return []byte("}")
	case OpenParenToken:
		return []byte("(")
	case CloseParenToken:
		return []byte(")")
	case OpenBracketToken:
		return []byte("[")
	case CloseBracketToken:
		return []byte("]")
	case DotToken:
		return []byte(".")
	case SemicolonToken:
		return []byte(";")
	case CommaToken:
		return []byte(",")
	case QuestionToken:
		return []byte("?")
	case ColonToken:
		return []byte(":")
	case ArrowToken:
		return []byte("=>")
	case EllipsisToken:
		return []byte("...")
	}
	return nil
}
