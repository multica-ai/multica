package js

import "strconv"

// OpPrec is the operator precedence
type OpPrec int

// OpPrec values.
const (
	OpExpr     OpPrec = iota // a,b
	OpAssign                 // a?b:c, yield x, ()=>x, async ()=>x, a=b, a+=b, ...
	OpCoalesce               // a??b
	OpOr                     // a||b
	OpAnd                    // a&&b
	OpBitOr                  // a|b
	OpBitXor                 // a^b
	OpBitAnd                 // a&b
	OpEquals                 // a==b, a!=b, a===b, a!==b
	OpCompare                // a<b, a>b, a<=b, a>=b, a instanceof b, a in b
	OpShift                  // a<<b, a>>b, a>>>b
	OpAdd                    // a+b, a-b
	OpMul                    // a*b, a/b, a%b
	OpExp                    // a**b
	OpUnary                  // ++x, --x, delete x, void x, typeof x, +x, -x, ~x, !x, await x
	OpUpdate                 // x++, x--
	OpLHS                    // NewExpr, CallExpr, OptChainExpr
	OpOpt                    // a?.b, a?.(), a?.[], a?.``
	OpCall                   // a(b), super(a), import(a)
	OpNew                    // new a
	OpMember                 // a[b], a.b, a`b`, super[x], super.x, new.target, import.meta, new a(b)
	OpPrimary                // literal, function, class, parenthesized
)

func (prec OpPrec) String() string {
	switch prec {
	case OpExpr:
		return "OpExpr"
	case OpAssign:
		return "OpAssign"
	case OpCoalesce:
		return "OpCoalesce"
	case OpOr:
		return "OpOr"
	case OpAnd:
		return "OpAnd"
	case OpBitOr:
		return "OpBitOr"
	case OpBitXor:
		return "OpBitXor"
	case OpBitAnd:
		return "OpBitAnd"
	case OpEquals:
		return "OpEquals"
	case OpCompare:
		return "OpCompare"
	case OpShift:
		return "OpShift"
	case OpAdd:
		return "OAdd"
	case OpMul:
		return "OpMul"
	case OpExp:
		return "OpExp"
	case OpUnary:
		return "OpUnary"
	case OpUpdate:
		return "OpUpdate"
	case OpLHS:
		return "OpLHS"
	case OpOpt:
		return "OpOpt"
	case OpCall:
		return "OpCall"
	case OpNew:
		return "OpNew"
	case OpMember:
		return "OpMember"
	case OpPrimary:
		return "OpPrimary"
	}
	return "Invalid(" + strconv.Itoa(int(prec)) + ")"
}

// Keywords is a map of reserved, strict, and other keywords
var Keywords = map[string]TokenType{
	// reserved
	"await":      AwaitToken,
	"break":      BreakToken,
	"case":       CaseToken,
	"catch":      CatchToken,
	"class":      ClassToken,
	"const":      ConstToken,
	"continue":   ContinueToken,
	"debugger":   DebuggerToken,
	"default":    DefaultToken,
	"delete":     DeleteToken,
	"do":         DoToken,
	"else":       ElseToken,
	"enum":       EnumToken,
	"export":     ExportToken,
	"extends":    ExtendsToken,
	"false":      FalseToken,
	"finally":    FinallyToken,
	"for":        ForToken,
	"function":   FunctionToken,
	"if":         IfToken,
	"import":     ImportToken,
	"in":         InToken,
	"instanceof": InstanceofToken,
	"new":        NewToken,
	"null":       NullToken,
	"return":     ReturnToken,
	"super":      SuperToken,
	"switch":     SwitchToken,
	"this":       ThisToken,
	"throw":      ThrowToken,
	"true":       TrueToken,
	"try":        TryToken,
	"typeof":     TypeofToken,
	"var":        VarToken,
	"void":       VoidToken,
	"while":      WhileToken,
	"with":       WithToken,
	"yield":      YieldToken,

	// strict mode
	"let":        LetToken,
	"static":     StaticToken,
	"implements": ImplementsToken,
	"interface":  InterfaceToken,
	"package":    PackageToken,
	"private":    PrivateToken,
	"protected":  ProtectedToken,
	"public":     PublicToken,

	// extra
	"as":     AsToken,
	"async":  AsyncToken,
	"from":   FromToken,
	"get":    GetToken,
	"meta":   MetaToken,
	"of":     OfToken,
	"set":    SetToken,
	"target": TargetToken,
}
