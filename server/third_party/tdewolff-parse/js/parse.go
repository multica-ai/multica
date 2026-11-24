package js

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/buffer"
)

var NestedStmtLimit = 1000
var NestedExprLimit = 1000

type Options struct {
	WhileToFor bool
	Inline     bool

	// ClassicScriptFields uses the classic-script await grammar for class
	// field initializers while retaining the module grammar at the root.
	ClassicScriptFields bool
}

// Parser is the state for the parser.
type Parser struct {
	l   *Lexer
	o   Options
	err error

	data                           []byte
	tt                             TokenType
	prevLT                         bool
	in, await, yield, deflt, retrn bool
	assumeArrowFunc                bool
	allowDirectivePrologue         bool
	comments                       []IStmt

	stmtLevel int
	exprLevel int

	scope *Scope
}

// Parse returns a JS AST tree of.
func Parse(r *parse.Input, o Options) (*AST, error) {
	ast := &AST{}
	p := &Parser{
		l:     NewLexer(r),
		o:     o,
		tt:    WhitespaceToken, // trick so that next() works
		in:    true,
		await: true,
	}

	if o.Inline {
		p.next()
		p.retrn = true
		p.allowDirectivePrologue = true
		p.enterScope(&ast.BlockStmt.Scope, true)
		for {
			if p.tt == ErrorToken {
				break
			}
			ast.BlockStmt.List = append(ast.BlockStmt.List, p.parseStmt(true))
		}
	} else {
		// catch shebang in first line
		var shebang []byte
		if r.Peek(0) == '#' && r.Peek(1) == '!' {
			r.Move(2)
			p.l.consumeSingleLineComment() // consume till end-of-line
			shebang = r.Shift()
		}

		// parse JS module
		p.next()
		ast.BlockStmt = p.parseModule()

		if 0 < len(shebang) {
			ast.BlockStmt.List = append([]IStmt{&Comment{shebang}}, ast.BlockStmt.List...)
		}
	}

	if p.err != nil {
		offset := p.l.r.Offset() - len(p.data)
		return nil, parse.NewError(buffer.NewReader(p.l.r.Bytes()), offset, p.err.Error())
	} else if p.l.Err() != nil && p.l.Err() != io.EOF {
		return nil, p.l.Err()
	}
	return ast, nil
}

////////////////////////////////////////////////////////////////

func (p *Parser) next() {
	p.prevLT = false
	p.tt, p.data = p.l.Next()
Loop:
	for {
		switch p.tt {
		case WhitespaceToken:
			// no-op
		case LineTerminatorToken:
			p.prevLT = true
		case CommentToken, CommentLineTerminatorToken:
			if 2 < len(p.data) && p.data[2] == '!' {
				p.comments = append(p.comments, &Comment{p.data})
			}
			if p.tt == CommentLineTerminatorToken {
				p.prevLT = true
			}
		default:
			break Loop
		}
		p.tt, p.data = p.l.Next()
	}
}

func (p *Parser) failMessage(msg string, args ...interface{}) {
	if p.err == nil {
		p.err = fmt.Errorf(msg, args...)
		p.tt = ErrorToken
	}
}

func (p *Parser) fail(in string, expected ...TokenType) {
	if p.err == nil {
		msg := "unexpected"
		if 0 < len(expected) {
			msg = "expected"
			for i, tt := range expected[:len(expected)-1] {
				if 0 < i {
					msg += ","
				}
				msg += " " + tt.String() + ""
			}
			if 2 < len(expected) {
				msg += ", or"
			} else if 1 < len(expected) {
				msg += " or"
			}
			msg += " " + expected[len(expected)-1].String() + " instead of"
		}

		if p.tt == ErrorToken {
			if p.l.Err() == io.EOF {
				msg += " EOF"
			} else if lexerErr, ok := p.l.Err().(*parse.Error); ok {
				msg = lexerErr.Message
			} else {
				// does not happen
			}
		} else {
			msg += " " + string(p.data) + ""
		}
		if in != "" {
			msg += " in " + in
		}

		p.err = errors.New(msg)
		p.tt = ErrorToken
	}
}

func (p *Parser) consume(in string, tt TokenType) bool {
	if p.tt != tt {
		p.fail(in, tt)
		return false
	}
	p.next()
	return true
}

func (p *Parser) enterScope(scope *Scope, isFunc bool) *Scope {
	// create a new scope object and add it to the parent
	parent := p.scope
	p.scope = scope
	*scope = Scope{
		Parent: parent,
	}
	if isFunc {
		scope.Func = scope
	} else if parent != nil {
		scope.Func = parent.Func
	}
	return parent
}

func (p *Parser) exitScope(parent *Scope) {
	p.scope.HoistUndeclared()
	p.scope = parent
}

func (p *Parser) parseModule() (module BlockStmt) {
	p.enterScope(&module.Scope, true)
	p.allowDirectivePrologue = true
	for {
		switch p.tt {
		case ErrorToken:
			if 0 < len(p.comments) {
				module.List = append(p.comments, module.List...)
				p.comments = p.comments[:0]
			}
			return
		case ImportToken:
			p.next()
			if p.tt == OpenParenToken {
				// could be an import call expression
				left := &LiteralExpr{ImportToken, []byte("import")}
				p.exprLevel++
				expr := p.parseExpressionSuffix(left, OpExpr, OpCall)
				p.exprLevel--
				module.List = append(module.List, &ExprStmt{expr})
				if !p.prevLT && p.tt == SemicolonToken {
					p.next()
				}
			} else if p.tt == DotToken {
				p.next()
				if !p.consume("import.meta expression", MetaToken) {
					return module
				}
				left := &ImportMetaExpr{}
				p.exprLevel++
				expr := p.parseExpressionSuffix(left, OpExpr, OpMember)
				p.exprLevel--
				module.List = append(module.List, &ExprStmt{expr})
				if !p.prevLT && p.tt == SemicolonToken {
					p.next()
				}
			} else {
				importStmt := p.parseImportStmt()
				module.List = append(module.List, &importStmt)
			}
		case ExportToken:
			exportStmt := p.parseExportStmt()
			module.List = append(module.List, &exportStmt)
		default:
			module.List = append(module.List, p.parseStmt(true))
		}
	}
}

func (p *Parser) parseStmt(allowDeclaration bool) (stmt IStmt) {
	p.stmtLevel++
	if NestedStmtLimit < p.stmtLevel {
		p.failMessage("too many nested statements")
		return nil
	}

	allowDirectivePrologue := p.allowDirectivePrologue
	p.allowDirectivePrologue = false

	switch tt := p.tt; tt {
	case OpenBraceToken:
		stmt = p.parseBlockStmt("block statement")
	case ConstToken, VarToken:
		if !allowDeclaration && tt == ConstToken {
			p.fail("statement")
			return
		}
		p.next()
		varDecl := p.parseVarDecl(tt, true)
		stmt = varDecl
		if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
			if tt == ConstToken {
				p.fail("const declaration")
			} else {
				p.fail("var statement")
			}
			return
		}
	case LetToken:
		let := p.data
		p.next()
		if allowDeclaration && (IsIdentifier(p.tt) || p.tt == YieldToken || p.tt == AwaitToken || p.tt == OpenBracketToken || p.tt == OpenBraceToken) {
			stmt = p.parseVarDecl(tt, false)
			if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				p.fail("let declaration")
				return
			}
		} else if p.tt == OpenBracketToken {
			p.failMessage("unexpected let [ in single-statement context")
			return
		} else {
			// expression
			stmt = &ExprStmt{p.parseIdentifierExpression(OpExpr, let)}
			if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				p.fail("expression")
				return
			}
		}
	case IfToken:
		p.next()
		if !p.consume("if statement", OpenParenToken) {
			return
		}
		cond := p.parseExpression(OpExpr)
		if !p.consume("if statement", CloseParenToken) {
			return
		}
		body := p.parseStmt(false)

		var elseBody IStmt
		if p.tt == ElseToken {
			p.next()
			elseBody = p.parseStmt(false)
		}
		stmt = &IfStmt{cond, body, elseBody}
	case ContinueToken, BreakToken:
		tt := p.tt
		p.next()
		var label []byte
		if !p.prevLT && p.isIdentifierReference(p.tt) {
			label = p.data
			p.next()
		}
		stmt = &BranchStmt{tt, label}
	case WithToken:
		p.next()
		if !p.consume("with statement", OpenParenToken) {
			return
		}
		cond := p.parseExpression(OpExpr)
		if !p.consume("with statement", CloseParenToken) {
			return
		}

		p.scope.Func.HasWith = true
		stmt = &WithStmt{cond, p.parseStmt(false)}
	case DoToken:
		stmt = &DoWhileStmt{}
		p.next()
		body := p.parseStmt(false)
		if !p.consume("do-while statement", WhileToken) {
			return
		}
		if !p.consume("do-while statement", OpenParenToken) {
			return
		}
		stmt = &DoWhileStmt{p.parseExpression(OpExpr), body}
		if !p.consume("do-while statement", CloseParenToken) {
			return
		}
	case WhileToken:
		p.next()
		if !p.consume("while statement", OpenParenToken) {
			return
		}
		cond := p.parseExpression(OpExpr)
		if !p.consume("while statement", CloseParenToken) {
			return
		}
		body := p.parseStmt(false)
		if p.o.WhileToFor {
			varDecl := &VarDecl{TokenType: VarToken, Scope: p.scope, InFor: true}
			p.scope.Func.VarDecls = append(p.scope.Func.VarDecls, varDecl)

			block, ok := body.(*BlockStmt)
			if !ok {
				block = &BlockStmt{List: []IStmt{body}}
			}
			stmt = &ForStmt{varDecl, cond, nil, block}
		} else {
			stmt = &WhileStmt{cond, body}
		}
	case ForToken:
		p.next()
		await := p.await && p.tt == AwaitToken
		if await {
			p.next()
		}
		if !p.consume("for statement", OpenParenToken) {
			return
		}

		body := &BlockStmt{}
		parent := p.enterScope(&body.Scope, false)

		var init IExpr
		p.in = false
		if p.tt == VarToken || p.tt == LetToken || p.tt == ConstToken {
			tt := p.tt
			p.next()
			varDecl := p.parseVarDecl(tt, true)
			if p.err != nil {
				return
			} else if p.tt != SemicolonToken && (1 < len(varDecl.List) || varDecl.List[0].Default != nil) {
				p.fail("for statement")
				return
			} else if p.tt == SemicolonToken && varDecl.List[0].Default == nil {
				// all but the first item were already verified
				if _, ok := varDecl.List[0].Binding.(*Var); !ok {
					p.fail("for statement")
					return
				}
			}
			init = varDecl
		} else if await {
			init = p.parseExpression(OpLHS)
		} else if p.tt != SemicolonToken {
			init = p.parseExpression(OpExpr)
		}
		p.in = true

		isLHSExpr := isLHSExpr(init)
		if isLHSExpr && p.tt == InToken {
			if await {
				p.fail("for statement", OfToken)
				return
			}
			p.next()
			value := p.parseExpression(OpExpr)
			if !p.consume("for statement", CloseParenToken) {
				return
			}
			p.scope.MarkForStmt()
			if p.tt == OpenBraceToken {
				body.List = p.parseStmtList("")
			} else if p.tt != SemicolonToken {
				body.List = []IStmt{p.parseStmt(false)}
			} else {
				p.next()
			}
			if varDecl, ok := init.(*VarDecl); ok {
				varDecl.InForInOf = true
			}
			stmt = &ForInStmt{init, value, body}
		} else if isLHSExpr && p.tt == OfToken {
			p.next()
			value := p.parseExpression(OpAssign)
			if !p.consume("for statement", CloseParenToken) {
				return
			}
			p.scope.MarkForStmt()
			if p.tt == OpenBraceToken {
				body.List = p.parseStmtList("")
			} else if p.tt != SemicolonToken {
				body.List = []IStmt{p.parseStmt(false)}
			} else {
				p.next()
			}
			if varDecl, ok := init.(*VarDecl); ok {
				varDecl.InForInOf = true
			}
			stmt = &ForOfStmt{await, init, value, body}
		} else if p.tt == SemicolonToken {
			var cond, post IExpr
			if await {
				p.fail("for statement", OfToken)
				return
			}
			p.next()
			if p.tt != SemicolonToken {
				cond = p.parseExpression(OpExpr)
			}
			if !p.consume("for statement", SemicolonToken) {
				return
			}
			if p.tt != CloseParenToken {
				post = p.parseExpression(OpExpr)
			}
			if !p.consume("for statement", CloseParenToken) {
				return
			}
			p.scope.MarkForStmt()
			if p.tt == OpenBraceToken {
				body.List = p.parseStmtList("")
			} else if p.tt != SemicolonToken {
				body.List = []IStmt{p.parseStmt(false)}
			} else {
				p.next()
			}
			if init == nil {
				varDecl := &VarDecl{TokenType: VarToken, Scope: p.scope, InFor: true}
				p.scope.Func.VarDecls = append(p.scope.Func.VarDecls, varDecl)
				init = varDecl
			} else if varDecl, ok := init.(*VarDecl); ok {
				varDecl.InFor = true
			}
			stmt = &ForStmt{init, cond, post, body}
		} else if isLHSExpr {
			p.fail("for statement", InToken, OfToken, SemicolonToken)
			return
		} else {
			p.fail("for statement", SemicolonToken)
			return
		}
		p.exitScope(parent)
	case SwitchToken:
		p.next()
		if !p.consume("switch statement", OpenParenToken) {
			return
		}
		init := p.parseExpression(OpExpr)
		if !p.consume("switch statement", CloseParenToken) {
			return
		}

		// case block
		if !p.consume("switch statement", OpenBraceToken) {
			return
		}

		switchStmt := &SwitchStmt{Init: init}
		parent := p.enterScope(&switchStmt.Scope, false)
		for {
			if p.tt == ErrorToken {
				p.fail("switch statement")
				return
			} else if p.tt == CloseBraceToken {
				p.next()
				break
			}

			clause := p.tt
			var list IExpr
			if p.tt == CaseToken {
				p.next()
				list = p.parseExpression(OpExpr)
			} else if p.tt == DefaultToken {
				p.next()
			} else {
				p.fail("switch statement", CaseToken, DefaultToken)
				return
			}
			if !p.consume("switch statement", ColonToken) {
				return
			}

			var stmts []IStmt
			for p.tt != CaseToken && p.tt != DefaultToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				stmts = append(stmts, p.parseStmt(true))
			}
			switchStmt.List = append(switchStmt.List, CaseClause{clause, list, stmts})
		}
		p.exitScope(parent)
		stmt = switchStmt
	case FunctionToken:
		if !allowDeclaration {
			p.fail("statement")
			return
		}
		stmt = p.parseFuncDecl()
	case AsyncToken: // async function
		async := p.data
		p.next()
		if p.tt == FunctionToken && !p.prevLT {
			if !allowDeclaration {
				p.fail("statement")
				return
			}
			stmt = p.parseAsyncFuncDecl()
		} else {
			// expression
			stmt = &ExprStmt{p.parseAsyncExpression(OpExpr, async)}
			if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				p.fail("expression")
				return
			}
		}
	case ClassToken:
		if !allowDeclaration {
			p.fail("statement")
			return
		}
		stmt = p.parseClassDecl()
	case ThrowToken:
		p.next()
		if p.prevLT {
			p.failMessage("unexpected newline in throw statement")
			return
		}
		stmt = &ThrowStmt{p.parseExpression(OpExpr)}
	case TryToken:
		p.next()
		body := p.parseBlockStmt("try statement")
		var binding IBinding
		var catch, finally *BlockStmt
		if p.tt == CatchToken {
			p.next()
			catch = &BlockStmt{}
			parent := p.enterScope(&catch.Scope, false)
			if p.tt == OpenParenToken {
				p.next()
				binding = p.parseBinding(CatchDecl) // local to block scope of catch
				if !p.consume("try-catch statement", CloseParenToken) {
					return
				}
			}
			catch.List = p.parseStmtList("try-catch statement")
			p.exitScope(parent)
		} else if p.tt != FinallyToken {
			p.fail("try statement", CatchToken, FinallyToken)
			return
		}
		if p.tt == FinallyToken {
			p.next()
			finally = p.parseBlockStmt("try-finally statement")
		}
		stmt = &TryStmt{body, binding, catch, finally}
	case DebuggerToken:
		stmt = &DebuggerStmt{}
		p.next()
	case SemicolonToken:
		stmt = &EmptyStmt{}
		p.next()
	case ErrorToken:
		stmt = &EmptyStmt{}
		return
	default:
		if p.retrn && p.tt == ReturnToken {
			p.next()
			var value IExpr
			if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				value = p.parseExpression(OpExpr)
			}
			stmt = &ReturnStmt{value}
		} else if p.isIdentifierReference(p.tt) {
			// LabelledStatement, Expression
			label := p.data
			p.next()
			if p.tt == ColonToken {
				p.next()
				prevDeflt := p.deflt
				if p.tt == FunctionToken {
					p.deflt = false
				}
				stmt = &LabelledStmt{label, p.parseStmt(true)} // allows illegal async function, generator function, let, const, or class declarations
				p.deflt = prevDeflt
			} else {
				// expression
				stmt = &ExprStmt{p.parseIdentifierExpression(OpExpr, label)}
				if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
					p.fail("expression")
					return
				}
			}
		} else {
			// expression
			stmt = &ExprStmt{p.parseExpression(OpExpr)}
			if !p.prevLT && p.tt != SemicolonToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				p.fail("expression")
				return
			} else if lit, ok := stmt.(*ExprStmt).Value.(*LiteralExpr); ok && allowDirectivePrologue && lit.TokenType == StringToken && len(lit.Data) == 12 && bytes.Equal(lit.Data[1:11], []byte("use strict")) {
				stmt = &DirectivePrologueStmt{lit.Data}
				p.allowDirectivePrologue = true
			}
		}
	}
	if !p.prevLT && p.tt == SemicolonToken {
		p.next()
	}
	p.stmtLevel--
	return
}

func (p *Parser) parseStmtList(in string) (list []IStmt) {
	comments := len(p.comments)
	if !p.consume(in, OpenBraceToken) {
		return
	}
	for {
		if p.tt == ErrorToken {
			p.fail("")
			return
		} else if p.tt == CloseBraceToken {
			p.next()
			break
		}
		list = append(list, p.parseStmt(true))
	}
	if comments < len(p.comments) {
		list2 := make([]IStmt, 0, len(p.comments)-comments+len(list))
		list2 = append(list2, p.comments[comments:]...)
		list2 = append(list2, list...)
		list = list2
		p.comments = p.comments[:comments]
	}
	return
}

func (p *Parser) parseBlockStmt(in string) (blockStmt *BlockStmt) {
	blockStmt = &BlockStmt{}
	parent := p.enterScope(&blockStmt.Scope, false)
	blockStmt.List = p.parseStmtList(in)
	p.exitScope(parent)
	return
}

func (p *Parser) parseImportStmt() (importStmt ImportStmt) {
	// assume we're passed import
	if p.tt == StringToken {
		importStmt.Module = p.data
		p.next()
	} else {
		expectClause := true
		if IsIdentifier(p.tt) || p.tt == YieldToken {
			importStmt.Default = p.data
			p.next()
			expectClause = p.tt == CommaToken
			if expectClause {
				p.next()
			}
		}
		if expectClause && p.tt == MulToken {
			star := p.data
			p.next()
			if !p.consume("import statement", AsToken) {
				return
			}
			if !IsIdentifier(p.tt) && p.tt != YieldToken {
				p.fail("import statement", IdentifierToken)
				return
			}
			importStmt.List = []Alias{{star, p.data}}
			p.next()
		} else if expectClause && p.tt == OpenBraceToken {
			p.next()
			importStmt.List = []Alias{}
			for IsIdentifierName(p.tt) || p.tt == StringToken {
				tt := p.tt
				var name, binding []byte = nil, p.data
				p.next()
				if p.tt == AsToken {
					p.next()
					if !IsIdentifier(p.tt) && p.tt != YieldToken {
						p.fail("import statement", IdentifierToken)
						return
					}
					name = binding
					binding = p.data
					p.next()
				} else if !IsIdentifier(tt) && tt != YieldToken || tt == StringToken {
					p.fail("import statement", IdentifierToken, StringToken)
					return
				}
				importStmt.List = append(importStmt.List, Alias{name, binding})
				if p.tt == CommaToken {
					p.next()
					if p.tt == CloseBraceToken {
						importStmt.List = append(importStmt.List, Alias{})
						break
					}
				}
			}
			if !p.consume("import statement", CloseBraceToken) {
				return
			}
		} else if expectClause && importStmt.Default != nil {
			p.fail("import statement", MulToken, OpenBraceToken)
			return
		} else if importStmt.Default == nil {
			p.fail("import statement", StringToken, IdentifierToken, MulToken, OpenBraceToken)
			return
		}

		if !p.consume("import statement", FromToken) {
			return
		}
		if p.tt != StringToken {
			p.fail("import statement", StringToken)
			return
		}
		importStmt.Module = p.data
		p.next()
	}
	if p.tt == SemicolonToken {
		p.next()
	}
	return
}

func (p *Parser) parseExportStmt() (exportStmt ExportStmt) {
	// assume we're at export
	p.next()
	prevYield, prevAwait, prevDeflt := p.yield, p.await, p.deflt
	p.yield, p.await, p.deflt = false, true, true
	if p.tt == MulToken || p.tt == OpenBraceToken {
		if p.tt == MulToken {
			star := p.data
			p.next()
			if p.tt == AsToken {
				p.next()
				if !IsIdentifierName(p.tt) && p.tt != StringToken {
					p.fail("export statement", IdentifierToken, StringToken)
					return
				}
				exportStmt.List = []Alias{{star, p.data}}
				p.next()
			} else {
				exportStmt.List = []Alias{{nil, star}}
			}
			if p.tt != FromToken {
				p.fail("export statement", FromToken)
				return
			}
		} else {
			p.next()
			for IsIdentifierName(p.tt) || p.tt == StringToken {
				var name, binding []byte = nil, p.data
				p.next()
				if p.tt == AsToken {
					p.next()
					if !IsIdentifierName(p.tt) && p.tt != StringToken {
						p.fail("export statement", IdentifierToken, StringToken)
						return
					}
					name = binding
					binding = p.data
					p.next()
				}
				exportStmt.List = append(exportStmt.List, Alias{name, binding})
				if p.tt == CommaToken {
					p.next()
					if p.tt == CloseBraceToken {
						exportStmt.List = append(exportStmt.List, Alias{})
						break
					}
				}
			}
			if !p.consume("export statement", CloseBraceToken) {
				return
			}
		}
		if p.tt == FromToken {
			p.next()
			if p.tt != StringToken {
				p.fail("export statement", StringToken)
				return
			}
			exportStmt.Module = p.data
			p.next()
		}
	} else if p.tt == VarToken || p.tt == ConstToken || p.tt == LetToken {
		tt := p.tt
		p.next()
		exportStmt.Decl = p.parseVarDecl(tt, false)
	} else if p.tt == FunctionToken {
		exportStmt.Decl = p.parseFuncDecl()
	} else if p.tt == AsyncToken { // async function
		p.next()
		if p.tt != FunctionToken || p.prevLT {
			p.fail("export statement", FunctionToken)
			return
		}
		exportStmt.Decl = p.parseAsyncFuncDecl()
	} else if p.tt == ClassToken {
		exportStmt.Decl = p.parseClassDecl()
	} else if p.tt == DefaultToken {
		exportStmt.Default = true
		p.next()
		if p.tt == FunctionToken {
			// hoistable declaration
			exportStmt.Decl = p.parseFuncDecl()
		} else if p.tt == AsyncToken { // async function or async arrow function
			async := p.data
			p.next()
			if p.tt == FunctionToken && !p.prevLT {
				// hoistable declaration
				exportStmt.Decl = p.parseAsyncFuncDecl()
			} else {
				// expression
				exportStmt.Decl = p.parseAsyncExpression(OpAssign, async)
			}
		} else if p.tt == ClassToken {
			exportStmt.Decl = p.parseClassDecl()
		} else {
			exportStmt.Decl = p.parseExpression(OpAssign)
		}
	} else {
		p.fail("export statement", MulToken, OpenBraceToken, VarToken, LetToken, ConstToken, FunctionToken, AsyncToken, ClassToken, DefaultToken)
		return
	}
	if p.tt == SemicolonToken {
		p.next()
	}
	p.yield, p.await, p.deflt = prevYield, prevAwait, prevDeflt
	return
}

func (p *Parser) parseVarDecl(tt TokenType, canBeHoisted bool) (varDecl *VarDecl) {
	// assume we're past var, let or const
	varDecl = &VarDecl{
		TokenType: tt,
		Scope:     p.scope,
	}
	declType := LexicalDecl
	if tt == VarToken {
		declType = VariableDecl
		if canBeHoisted {
			p.scope.Func.VarDecls = append(p.scope.Func.VarDecls, varDecl)
		}
	}
	for {
		// binding element, var declaration in for-in or for-of can never have a default
		var bindingElement BindingElement
		bindingElement.Binding = p.parseBinding(declType)
		if p.tt == EqToken {
			p.next()
			bindingElement.Default = p.parseExpression(OpAssign)
		} else if _, ok := bindingElement.Binding.(*Var); !ok && (p.in || 0 < len(varDecl.List)) {
			p.fail("var statement", EqToken)
			return
		} else if tt == ConstToken && (p.in || !p.in && p.tt != OfToken && p.tt != InToken) {
			p.fail("const statement", EqToken)
		}

		varDecl.List = append(varDecl.List, bindingElement)
		if p.tt == CommaToken {
			p.next()
		} else {
			break
		}
	}
	return
}

func (p *Parser) parseFuncParams(in string) (params Params) {
	// FormalParameters
	if !p.consume(in, OpenParenToken) {
		return
	}

	for p.tt != CloseParenToken && p.tt != ErrorToken {
		if p.tt == EllipsisToken {
			// binding rest element
			p.next()
			params.Rest = p.parseBinding(ArgumentDecl)
			p.consume(in, CloseParenToken)
			return
		}
		params.List = append(params.List, p.parseBindingElement(ArgumentDecl))
		if p.tt != CommaToken {
			break
		}
		p.next()
	}
	if p.tt != CloseParenToken {
		p.fail(in)
		return
	}
	p.next()

	// mark undeclared vars as arguments in `function f(a=b){var b}` where the b's are different vars
	p.scope.MarkFuncArgs()
	return
}

func (p *Parser) parseFuncDecl() (funcDecl *FuncDecl) {
	return p.parseFunc(false, false)
}

func (p *Parser) parseAsyncFuncDecl() (funcDecl *FuncDecl) {
	return p.parseFunc(true, false)
}

func (p *Parser) parseFuncExpr() (funcDecl *FuncDecl) {
	return p.parseFunc(false, true)
}

func (p *Parser) parseAsyncFuncExpr() (funcDecl *FuncDecl) {
	return p.parseFunc(true, true)
}

func (p *Parser) parseFunc(async, expr bool) (funcDecl *FuncDecl) {
	// assume we're at function
	p.next()
	funcDecl = &FuncDecl{}
	funcDecl.Async = async
	funcDecl.Generator = p.tt == MulToken
	if funcDecl.Generator {
		p.next()
	}
	var ok bool
	var name []byte
	if expr && (IsIdentifier(p.tt) || p.tt == YieldToken || p.tt == AwaitToken) || !expr && p.isIdentifierReference(p.tt) {
		name = p.data
		if !expr {
			funcDecl.Name, ok = p.scope.Declare(FunctionDecl, p.data)
			if !ok {
				p.failMessage("identifier %s has already been declared", string(p.data))
				return
			}
		}
		p.next()
	} else if !expr && !p.deflt {
		p.fail("function declaration", IdentifierToken)
		return
	} else if p.tt != OpenParenToken {
		p.fail("function declaration", IdentifierToken, OpenParenToken)
		return
	}
	parent := p.enterScope(&funcDecl.Body.Scope, true)
	prevAwait, prevYield, prevRetrn := p.await, p.yield, p.retrn
	p.await, p.yield, p.retrn = funcDecl.Async, funcDecl.Generator, true

	if expr && name != nil {
		funcDecl.Name, _ = p.scope.Declare(ExprDecl, name) // cannot fail
	}
	funcDecl.Params = p.parseFuncParams("function declaration")

	prevAllowDirectivePrologue := p.allowDirectivePrologue
	p.allowDirectivePrologue = true
	funcDecl.Body.List = p.parseStmtList("function declaration")
	p.allowDirectivePrologue = prevAllowDirectivePrologue

	p.await, p.yield, p.retrn = prevAwait, prevYield, prevRetrn
	p.exitScope(parent)
	return
}

func (p *Parser) parseClassDecl() (classDecl *ClassDecl) {
	return p.parseAnyClass(false)
}

func (p *Parser) parseClassExpr() (classDecl *ClassDecl) {
	return p.parseAnyClass(true)
}

func (p *Parser) parseAnyClass(expr bool) (classDecl *ClassDecl) {
	// assume we're at class
	p.next()
	classDecl = &ClassDecl{}
	if IsIdentifier(p.tt) || p.tt == YieldToken || p.tt == AwaitToken {
		if !expr {
			var ok bool
			classDecl.Name, ok = p.scope.Declare(LexicalDecl, p.data)
			if !ok {
				p.failMessage("identifier %s has already been declared", string(p.data))
				return
			}
		} else {
			//classDecl.Name, ok = p.scope.Declare(ExprDecl, p.data) // classes do not register vars
			classDecl.Name = &Var{p.data, nil, 1, ExprDecl}
		}
		p.next()
	} else if !expr && !p.deflt {
		p.fail("class declaration", IdentifierToken)
		return
	}
	if p.tt == ExtendsToken {
		p.next()
		classDecl.Extends = p.parseExpression(OpLHS)
	}

	if !p.consume("class declaration", OpenBraceToken) {
		return
	}
	parent := p.enterScope(&classDecl.Scope, false)
	for {
		if p.tt == ErrorToken {
			p.fail("class declaration")
			return
		} else if p.tt == SemicolonToken {
			p.next()
			continue
		} else if p.tt == CloseBraceToken {
			p.next()
			break
		}

		classDecl.List = append(classDecl.List, p.parseClassElement())
	}
	p.exitScope(parent)
	return
}

func (p *Parser) parseClassElement() ClassElement {
	method := &MethodDecl{}
	var data []byte // either static, async, get, or set
	if p.tt == StaticToken {
		method.Static = true
		data = p.data
		p.next()
		if p.tt == OpenBraceToken {
			prevYield, prevAwait, prevRetrn := p.yield, p.await, p.retrn
			p.yield, p.await, p.retrn = false, true, false
			elem := ClassElement{StaticBlock: p.parseBlockStmt("class static block")}
			p.yield, p.await, p.retrn = prevYield, prevAwait, prevRetrn
			return elem
		}
	}
	if p.tt == MulToken {
		method.Generator = true
		p.next()
	} else if p.tt == AsyncToken {
		data = p.data
		p.next()
		if !p.prevLT {
			method.Async = true
			if p.tt == MulToken {
				method.Generator = true
				data = nil
				p.next()
			}
		}
	} else if p.tt == GetToken {
		method.Get = true
		data = p.data
		p.next()
	} else if p.tt == SetToken {
		method.Set = true
		data = p.data
		p.next()
	}

	isField := false
	if data != nil && p.tt == OpenParenToken {
		// (static) method name is: static, async, get, or set
		method.Name.Literal = LiteralExpr{IdentifierToken, data}
		if method.Async || method.Get || method.Set {
			method.Async = false
			method.Get = false
			method.Set = false
		} else {
			method.Static = false
		}
	} else if data != nil && (p.tt == EqToken || p.tt == SemicolonToken || p.tt == CloseBraceToken) {
		// (static) field name is: static, async, get, or set
		method.Name.Literal = LiteralExpr{IdentifierToken, data}
		if !method.Async && !method.Get && !method.Set {
			method.Static = false
		}
		isField = true
	} else {
		if p.tt == PrivateIdentifierToken {
			var ok bool
			method.Name.Private, ok = p.scope.Declare(PrivateDecl, p.data)
			if !ok {
				p.failMessage("identifier %s has already been declared", string(p.data))
				return ClassElement{}
			}
			p.next()
		} else {
			method.Name.PropertyName = p.parsePropertyName("method or field definition")
		}
		if (data == nil || method.Static) && p.tt != OpenParenToken {
			isField = true
		}
	}

	if isField {
		var init IExpr
		if p.tt == EqToken {
			p.next()
			prevAwait := p.await
			if p.o.ClassicScriptFields {
				p.await = method.Static
			}
			init = p.parseExpression(OpAssign)
			p.await = prevAwait
		}
		return ClassElement{Field: Field{Static: method.Static, Name: method.Name, Init: init}}
	}

	parent := p.enterScope(&method.Body.Scope, true)
	prevAwait, prevYield, prevRetrn := p.await, p.yield, p.retrn
	p.await, p.yield, p.retrn = method.Async, method.Generator, true

	method.Params = p.parseFuncParams("method definition")

	prevAllowDirectivePrologue := p.allowDirectivePrologue
	p.allowDirectivePrologue = true
	method.Body.List = p.parseStmtList("method function")
	p.allowDirectivePrologue = prevAllowDirectivePrologue

	p.await, p.yield, p.retrn = prevAwait, prevYield, prevRetrn
	p.exitScope(parent)
	return ClassElement{Method: method}
}

func (p *Parser) parsePropertyName(in string) (propertyName PropertyName) {
	if IsIdentifierName(p.tt) {
		propertyName.Literal = LiteralExpr{IdentifierToken, p.data}
		p.next()
	} else if p.tt == StringToken {
		// reinterpret string as identifier or number if we can, except for empty strings
		if isIdent := AsIdentifierName(p.data[1 : len(p.data)-1]); isIdent {
			propertyName.Literal = LiteralExpr{IdentifierToken, p.data[1 : len(p.data)-1]}
		} else if isNum := AsDecimalLiteral(p.data[1 : len(p.data)-1]); isNum {
			propertyName.Literal = LiteralExpr{DecimalToken, p.data[1 : len(p.data)-1]}
		} else {
			propertyName.Literal = LiteralExpr{p.tt, p.data}
		}
		p.next()
	} else if IsNumeric(p.tt) {
		propertyName.Literal = LiteralExpr{p.tt, p.data}
		p.next()
	} else if p.tt == OpenBracketToken {
		p.next()
		propertyName.Computed = p.parseExpression(OpAssign)
		if !p.consume(in, CloseBracketToken) {
			return
		}
	} else {
		p.fail(in, IdentifierToken, StringToken, NumericToken, OpenBracketToken)
		return
	}
	return
}

func (p *Parser) parseBindingElement(decl DeclType) (bindingElement BindingElement) {
	// BindingElement
	bindingElement.Binding = p.parseBinding(decl)
	if p.tt == EqToken {
		p.next()
		bindingElement.Default = p.parseExpression(OpAssign)
	}
	return
}

func (p *Parser) parseBinding(decl DeclType) (binding IBinding) {
	// BindingIdentifier, BindingPattern
	if p.isIdentifierReference(p.tt) {
		var ok bool
		binding, ok = p.scope.Declare(decl, p.data)
		if !ok {
			p.failMessage("identifier %s has already been declared", string(p.data))
			return
		}
		p.next()
	} else if p.tt == OpenBracketToken {
		p.next()
		array := BindingArray{}
		if p.tt == CommaToken {
			array.List = append(array.List, BindingElement{})
		}
		last := 0
		for p.tt != CloseBracketToken {
			// elision
			for p.tt == CommaToken {
				p.next()
				if p.tt == CommaToken {
					array.List = append(array.List, BindingElement{})
				}
			}
			// binding rest element
			if p.tt == EllipsisToken {
				p.next()
				array.Rest = p.parseBinding(decl)
				if p.tt != CloseBracketToken {
					p.fail("array binding pattern", CloseBracketToken)
					return
				}
				break
			} else if p.tt == CloseBracketToken {
				array.List = array.List[:last]
				break
			}

			array.List = append(array.List, p.parseBindingElement(decl))
			last = len(array.List)

			if p.tt != CommaToken && p.tt != CloseBracketToken {
				p.fail("array binding pattern", CommaToken, CloseBracketToken)
				return
			}
		}
		p.next() // always CloseBracketToken
		binding = &array
	} else if p.tt == OpenBraceToken {
		p.next()
		object := BindingObject{}
		for p.tt != CloseBraceToken {
			// binding rest property
			if p.tt == EllipsisToken {
				p.next()
				if !p.isIdentifierReference(p.tt) {
					p.fail("object binding pattern", IdentifierToken)
					return
				}
				var ok bool
				object.Rest, ok = p.scope.Declare(decl, p.data)
				if !ok {
					p.failMessage("identifier %s has already been declared", string(p.data))
					return
				}
				p.next()
				if p.tt != CloseBraceToken {
					p.fail("object binding pattern", CloseBraceToken)
					return
				}
				break
			}

			item := BindingObjectItem{}
			if p.isIdentifierReference(p.tt) {
				name := p.data
				item.Key = &PropertyName{LiteralExpr{IdentifierToken, p.data}, nil}
				p.next()
				if p.tt == ColonToken {
					// property name + : + binding element
					p.next()
					item.Value = p.parseBindingElement(decl)
				} else {
					// single name binding
					var ok bool
					item.Key.Literal.Data = parse.Copy(item.Key.Literal.Data) // copy so that renaming doesn't rename the key
					item.Value.Binding, ok = p.scope.Declare(decl, name)
					if !ok {
						p.failMessage("identifier %s has already been declared", string(name))
						return
					}
					if p.tt == EqToken {
						p.next()
						item.Value.Default = p.parseExpression(OpAssign)
					}
				}
			} else {
				propertyName := p.parsePropertyName("object binding pattern")
				item.Key = &propertyName
				if !p.consume("object binding pattern", ColonToken) {
					return
				}
				item.Value = p.parseBindingElement(decl)
			}
			object.List = append(object.List, item)

			if p.tt == CommaToken {
				p.next()
			} else if p.tt != CloseBraceToken {
				p.fail("object binding pattern", CommaToken, CloseBraceToken)
				return
			}
		}
		p.next() // always CloseBracketToken
		binding = &object
	} else {
		p.fail("binding")
		return
	}
	return
}

func (p *Parser) parseArrayLiteral() (array ArrayExpr) {
	// assume we're on [
	p.next()
	prevComma := true
	for {
		if p.tt == ErrorToken {
			p.fail("expression")
			return
		} else if p.tt == CloseBracketToken {
			p.next()
			break
		} else if p.tt == CommaToken {
			if prevComma {
				array.List = append(array.List, Element{})
			}
			prevComma = true
			p.next()
		} else {
			spread := p.tt == EllipsisToken
			if spread {
				p.next()
			}
			array.List = append(array.List, Element{p.parseAssignExprOrParam(), spread})
			prevComma = false
			if spread && p.tt != CloseBracketToken {
				p.assumeArrowFunc = false
			}
		}
	}
	return
}

func (p *Parser) parseObjectLiteral() (object ObjectExpr) {
	// assume we're on {
	p.next()
	for {
		if p.tt == ErrorToken {
			p.fail("object literal", CloseBraceToken)
			return
		} else if p.tt == CloseBraceToken {
			p.next()
			break
		}

		property := Property{}
		if p.tt == EllipsisToken {
			p.next()
			property.Spread = true
			property.Value = p.parseAssignExprOrParam()
			if _, isIdent := property.Value.(*Var); !isIdent || p.tt != CloseBraceToken {
				p.assumeArrowFunc = false
			}
		} else {
			// try to parse as MethodDefinition, otherwise fall back to PropertyName:AssignExpr or IdentifierReference
			var data []byte
			method := MethodDecl{}
			if p.tt == MulToken {
				p.next()
				method.Generator = true
			} else if p.tt == AsyncToken {
				data = p.data
				p.next()
				if !p.prevLT {
					method.Async = true
					if p.tt == MulToken {
						p.next()
						method.Generator = true
						data = nil
					}
				} else {
					method.Name.Literal = LiteralExpr{IdentifierToken, data}
					data = nil
				}
			} else if p.tt == GetToken {
				data = p.data
				p.next()
				method.Get = true
			} else if p.tt == SetToken {
				data = p.data
				p.next()
				method.Set = true
			}

			// PropertyName
			if data != nil && !method.Generator && (p.tt == EqToken || p.tt == CommaToken || p.tt == CloseBraceToken || p.tt == ColonToken || p.tt == OpenParenToken) {
				method.Name.Literal = LiteralExpr{IdentifierToken, data}
				method.Async = false
				method.Get = false
				method.Set = false
			} else if !method.Name.IsSet() { // did not parse async [LT]
				method.Name.PropertyName = p.parsePropertyName("object literal")
				if !method.Name.IsSet() {
					return
				}
			}

			if p.tt == OpenParenToken {
				// MethodDefinition
				parent := p.enterScope(&method.Body.Scope, true)
				prevAwait, prevYield, prevRetrn := p.await, p.yield, p.retrn
				p.await, p.yield, p.retrn = method.Async, method.Generator, true
				p.assumeArrowFunc = false

				method.Params = p.parseFuncParams("method definition")
				method.Body.List = p.parseStmtList("method definition")

				p.await, p.yield, p.retrn = prevAwait, prevYield, prevRetrn
				p.exitScope(parent)
				property.Value = &method
			} else if p.tt == ColonToken {
				// PropertyName : AssignmentExpression
				p.next()
				property.Name = &method.Name.PropertyName
				property.Value = p.parseAssignExprOrParam()
			} else if method.Name.IsComputed() || !p.isIdentifierReference(method.Name.Literal.TokenType) {
				p.fail("object literal", ColonToken, OpenParenToken)
				return
			} else {
				// IdentifierReference (= AssignmentExpression)?
				name := method.Name.Literal.Data
				method.Name.Literal.Data = parse.Copy(method.Name.Literal.Data) // copy so that renaming doesn't rename the key
				property.Name = &method.Name.PropertyName                       // set key explicitly so after renaming the original is still known
				if p.assumeArrowFunc {
					var ok bool
					property.Value, ok = p.scope.Declare(ArgumentDecl, name)
					if !ok {
						property.Value = p.scope.Use(name)
						p.assumeArrowFunc = false
					}
				} else {
					property.Value = p.scope.Use(name)
				}
				if p.tt == EqToken {
					p.next()
					prevAssumeArrowFunc := p.assumeArrowFunc
					p.assumeArrowFunc = false
					property.Init = p.parseExpression(OpAssign)
					p.assumeArrowFunc = prevAssumeArrowFunc
				}
			}
		}
		object.List = append(object.List, property)
		if p.tt == CommaToken {
			p.next()
		} else if p.tt != CloseBraceToken {
			p.fail("object literal")
			return
		}
	}
	return
}

func (p *Parser) parseTemplateLiteral(precLeft OpPrec) (template TemplateExpr) {
	// assume we're on 'Template' or 'TemplateStart'
	template.Prec = OpMember
	if precLeft < OpMember {
		template.Prec = OpCall
	}
	for p.tt == TemplateStartToken || p.tt == TemplateMiddleToken {
		tpl := p.data
		p.next()
		template.List = append(template.List, TemplatePart{tpl, p.parseExpression(OpExpr)})
	}
	if p.tt != TemplateToken && p.tt != TemplateEndToken {
		p.fail("template literal", TemplateToken)
		return
	}
	template.Tail = p.data
	p.next() // TemplateEndToken
	return
}

func (p *Parser) parseArguments() (args Args) {
	// assume we're on (
	p.next()
	args.List = make([]Arg, 0, 4)
	for p.tt != CloseParenToken && p.tt != ErrorToken {
		rest := p.tt == EllipsisToken
		if rest {
			p.next()
		}
		args.List = append(args.List, Arg{
			Value: p.parseExpression(OpAssign),
			Rest:  rest,
		})
		if p.tt != CloseParenToken {
			if p.tt != CommaToken {
				p.fail("arguments", CommaToken, CloseParenToken)
				return
			} else {
				p.next() // CommaToken
			}
		}
	}
	p.consume("arguments", CloseParenToken)
	return
}

func (p *Parser) parseAsyncArrowFunc() (arrowFunc *ArrowFunc) {
	// expect we're at Identifier or Yield or (
	arrowFunc = &ArrowFunc{}
	parent := p.enterScope(&arrowFunc.Body.Scope, true)
	prevAwait, prevYield := p.await, p.yield
	p.await, p.yield = true, false

	if IsIdentifier(p.tt) || !prevYield && p.tt == YieldToken {
		ref, _ := p.scope.Declare(ArgumentDecl, p.data) // cannot fail
		p.next()
		arrowFunc.Params.List = []BindingElement{{Binding: ref}}
	} else {
		arrowFunc.Params = p.parseFuncParams("arrow function")
		// CallExpression of 'async(params)' already handled
	}

	arrowFunc.Async = true
	arrowFunc.Body.List = p.parseArrowFuncBody()

	p.await, p.yield = prevAwait, prevYield
	p.exitScope(parent)
	return
}

func (p *Parser) parseIdentifierArrowFunc(v *Var) (arrowFunc *ArrowFunc) {
	// expect we're at =>
	arrowFunc = &ArrowFunc{}
	parent := p.enterScope(&arrowFunc.Body.Scope, true)
	prevAwait, prevYield := p.await, p.yield
	p.await, p.yield = false, false

	if 1 < v.Uses {
		v.Uses--
		v, _ = p.scope.Declare(ArgumentDecl, parse.Copy(v.Data)) // cannot fail
	} else {
		// if v.Uses==1 it must be undeclared and be the last added
		p.scope.Parent.Undeclared = p.scope.Parent.Undeclared[:len(p.scope.Parent.Undeclared)-1]
		v.Decl = ArgumentDecl
		p.scope.Declared = append(p.scope.Declared, v)
	}

	arrowFunc.Params.List = []BindingElement{{v, nil}}
	arrowFunc.Body.List = p.parseArrowFuncBody()

	p.await, p.yield = prevAwait, prevYield
	p.exitScope(parent)
	return
}

func (p *Parser) parseArrowFuncBody() (list []IStmt) {
	// expect we're at arrow
	if p.tt != ArrowToken {
		p.fail("arrow function", ArrowToken)
		return
	} else if p.prevLT {
		p.fail("expression")
		return
	}
	p.next()

	// mark undeclared vars as arguments in `function f(a=b){var b}` where the b's are different vars
	p.scope.MarkFuncArgs()

	if p.tt == OpenBraceToken {
		prevIn, prevRetrn := p.in, p.retrn
		p.in, p.retrn = true, true

		prevAllowDirectivePrologue := p.allowDirectivePrologue
		p.allowDirectivePrologue = true
		list = p.parseStmtList("arrow function")
		p.allowDirectivePrologue = prevAllowDirectivePrologue

		p.in, p.retrn = prevIn, prevRetrn
	} else {
		list = []IStmt{&ReturnStmt{p.parseExpression(OpAssign)}}
	}
	return
}

func (p *Parser) parseIdentifierExpression(prec OpPrec, ident []byte) IExpr {
	var left IExpr
	left = p.scope.Use(ident)
	return p.parseExpressionSuffix(left, prec, OpPrimary)
}

func (p *Parser) parseAsyncExpression(prec OpPrec, async []byte) IExpr {
	// IdentifierReference, AsyncFunctionExpression, AsyncGeneratorExpression
	// CoverCallExpressionAndAsyncArrowHead, AsyncArrowFunction
	// assume we're at a token after async
	var left IExpr
	precLeft := OpPrimary
	if !p.prevLT && p.tt == FunctionToken {
		// primary expression
		left = p.parseAsyncFuncExpr()
	} else if !p.prevLT && prec <= OpAssign && (p.tt == OpenParenToken || IsIdentifier(p.tt) || p.tt == YieldToken || p.tt == AwaitToken) {
		// async arrow function expression or call expression
		if p.tt == AwaitToken || p.yield && p.tt == YieldToken {
			p.fail("arrow function")
			return nil
		} else if p.tt == OpenParenToken {
			return p.parseParenthesizedExpression(prec, async)
		}
		left = p.parseAsyncArrowFunc()
		precLeft = OpAssign
	} else {
		left = p.scope.Use(async)
	}
	// can be async(args), async => ..., or e.g. async + ...
	return p.parseExpressionSuffix(left, prec, precLeft)
}

// parseExpression parses an expression that has a precedence of prec or higher.
func (p *Parser) parseExpression(prec OpPrec) IExpr {
	p.exprLevel++
	if NestedExprLimit < p.exprLevel {
		p.failMessage("too many nested expressions")
		return nil
	}

	// reparse input if we have / or /= as the beginning of a new expression, this should be a regular expression!
	if p.tt == DivToken || p.tt == DivEqToken {
		p.tt, p.data = p.l.RegExp()
		if p.tt == ErrorToken {
			p.fail("regular expression")
			return nil
		}
	}

	var left IExpr
	precLeft := OpPrimary

	if IsIdentifier(p.tt) && p.tt != AsyncToken {
		left = p.scope.Use(p.data)
		p.next()
		suffix := p.parseExpressionSuffix(left, prec, precLeft)
		p.exprLevel--
		return suffix
	} else if IsNumeric(p.tt) {
		left = &LiteralExpr{p.tt, p.data}
		p.next()
		suffix := p.parseExpressionSuffix(left, prec, precLeft)
		p.exprLevel--
		return suffix
	}

	switch tt := p.tt; tt {
	case StringToken, ThisToken, NullToken, TrueToken, FalseToken, RegExpToken:
		left = &LiteralExpr{p.tt, p.data}
		p.next()
	case OpenBracketToken:
		prevIn := p.in
		p.in = true
		array := p.parseArrayLiteral()
		p.in = prevIn
		left = &array
	case OpenBraceToken:
		prevIn := p.in
		p.in = true
		object := p.parseObjectLiteral()
		p.in = prevIn
		left = &object
	case OpenParenToken:
		// parenthesized expression or arrow parameter list
		if OpAssign < prec {
			// must be a parenthesized expression
			p.next()
			prevIn := p.in
			p.in = true
			left = &GroupExpr{p.parseExpression(OpExpr)}
			p.in = prevIn
			if !p.consume("expression", CloseParenToken) {
				return nil
			}
			break
		}
		suffix := p.parseParenthesizedExpression(prec, nil)
		p.exprLevel--
		return suffix
	case NotToken, BitNotToken, TypeofToken, VoidToken, DeleteToken:
		if OpUnary < prec {
			p.fail("expression")
			return nil
		}
		p.next()
		left = &UnaryExpr{tt, p.parseExpression(OpUnary)}
		precLeft = OpUnary
	case AddToken:
		if OpUnary < prec {
			p.fail("expression")
			return nil
		}
		p.next()
		left = &UnaryExpr{PosToken, p.parseExpression(OpUnary)}
		precLeft = OpUnary
	case SubToken:
		if OpUnary < prec {
			p.fail("expression")
			return nil
		}
		p.next()
		left = &UnaryExpr{NegToken, p.parseExpression(OpUnary)}
		precLeft = OpUnary
	case IncrToken:
		if OpUpdate < prec {
			p.fail("expression")
			return nil
		}
		p.next()
		left = &UnaryExpr{PreIncrToken, p.parseExpression(OpUnary)}
		precLeft = OpUnary
	case DecrToken:
		if OpUpdate < prec {
			p.fail("expression")
			return nil
		}
		p.next()
		left = &UnaryExpr{PreDecrToken, p.parseExpression(OpUnary)}
		precLeft = OpUnary
	case AwaitToken:
		// either accepted as IdentifierReference or as AwaitExpression
		if p.await && prec <= OpUnary {
			p.next()
			left = &UnaryExpr{tt, p.parseExpression(OpUnary)}
			precLeft = OpUnary
		} else if p.await {
			p.fail("expression")
			return nil
		} else {
			left = p.scope.Use(p.data)
			p.next()
		}
	case NewToken:
		p.next()
		if p.tt == DotToken {
			p.next()
			if !p.consume("new.target expression", TargetToken) {
				return nil
			}
			left = &NewTargetExpr{}
			precLeft = OpMember
		} else {
			newExpr := &NewExpr{p.parseExpression(OpNew), nil}
			if p.tt == OpenParenToken {
				args := p.parseArguments()
				if len(args.List) != 0 {
					newExpr.Args = &args
				}
				precLeft = OpMember
			} else {
				precLeft = OpNew
			}
			left = newExpr
		}
	case ImportToken:
		// OpMember < prec does never happen
		left = &LiteralExpr{p.tt, p.data}
		p.next()
		if p.tt == DotToken {
			p.next()
			if !p.consume("import.meta expression", MetaToken) {
				return nil
			}
			left = &ImportMetaExpr{}
			precLeft = OpMember
		} else if p.tt != OpenParenToken {
			p.fail("import expression", OpenParenToken)
			return nil
		} else if OpCall < prec {
			p.fail("expression")
			return nil
		} else {
			precLeft = OpCall
		}
	case SuperToken:
		// OpMember < prec does never happen
		left = &LiteralExpr{p.tt, p.data}
		p.next()
		if OpCall < prec && p.tt != DotToken && p.tt != OpenBracketToken {
			p.fail("super expression", OpenBracketToken, DotToken)
			return nil
		} else if p.tt != DotToken && p.tt != OpenBracketToken && p.tt != OpenParenToken {
			p.fail("super expression", OpenBracketToken, OpenParenToken, DotToken)
			return nil
		}
		if OpCall < prec {
			precLeft = OpMember
		} else {
			precLeft = OpCall
		}
	case YieldToken:
		// either accepted as IdentifierReference or as YieldExpression
		if p.yield && prec <= OpAssign {
			// YieldExpression
			p.next()
			yieldExpr := YieldExpr{}
			if !p.prevLT {
				yieldExpr.Generator = p.tt == MulToken
				if yieldExpr.Generator {
					p.next()
					yieldExpr.X = p.parseExpression(OpAssign)
				} else if p.tt != CloseBraceToken && p.tt != CloseBracketToken && p.tt != CloseParenToken && p.tt != ColonToken && p.tt != CommaToken && p.tt != SemicolonToken {
					yieldExpr.X = p.parseExpression(OpAssign)
				}
			}
			left = &yieldExpr
			precLeft = OpAssign
		} else if p.yield {
			p.fail("expression")
			return nil
		} else {
			left = p.scope.Use(p.data)
			p.next()
		}
	case AsyncToken:
		async := p.data
		p.next()
		prevIn := p.in
		p.in = true
		left = p.parseAsyncExpression(prec, async)
		p.in = prevIn
	case ClassToken:
		prevIn := p.in
		p.in = true
		left = p.parseClassExpr()
		p.in = prevIn
	case FunctionToken:
		prevIn := p.in
		p.in = true
		left = p.parseFuncExpr()
		p.in = prevIn
	case TemplateToken, TemplateStartToken:
		prevIn := p.in
		p.in = true
		template := p.parseTemplateLiteral(precLeft)
		left = &template
		p.in = prevIn
	case PrivateIdentifierToken:
		if OpCompare < prec || !p.in {
			p.fail("expression")
			return nil
		}
		left = p.scope.Use(p.data)
		p.next()
		if p.tt != InToken {
			p.fail("relational expression", InToken)
			return nil
		}
	default:
		p.fail("expression")
		return nil
	}
	suffix := p.parseExpressionSuffix(left, prec, precLeft)
	p.exprLevel--
	return suffix
}

// parseExpressionSuffix parses an expression recursively by their suffixes. prec is the precedence level (or higher) that is allowed (we may return earlier when an operator has a lower precedence), while precLeft is the precedence level of the preceding (left side) of the expression at this point (may be an error if it is lower than allowed). For example: when we encounter || we parse LogicalOR: LogicalOR || LogicalAND, if prec is higher than LogicalOR we return the expression and parse || and the rest higher up, if precLeft is lower than LogicalOR (the part before ||), this is invalid syntax.
func (p *Parser) parseExpressionSuffix(left IExpr, prec, precLeft OpPrec) IExpr {
	for i := 0; ; i++ {
		if 1000 < p.exprLevel+i {
			p.failMessage("too many nested expressions")
			return nil
		}

		switch tt := p.tt; tt {
		case EqToken, MulEqToken, DivEqToken, ModEqToken, ExpEqToken, AddEqToken, SubEqToken, LtLtEqToken, GtGtEqToken, GtGtGtEqToken, BitAndEqToken, BitXorEqToken, BitOrEqToken, AndEqToken, OrEqToken, NullishEqToken:
			if OpAssign < prec {
				return left
			} else if precLeft < OpLHS {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpAssign)}
			precLeft = OpAssign
		case LtToken, LtEqToken, GtToken, GtEqToken, InToken, InstanceofToken:
			if OpCompare < prec || !p.in && tt == InToken {
				return left
			} else if precLeft < OpCompare {
				// can only fail after a yield or arrow function expression
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpShift)}
			precLeft = OpCompare
		case EqEqToken, NotEqToken, EqEqEqToken, NotEqEqToken:
			if OpEquals < prec {
				return left
			} else if precLeft < OpEquals {
				// can only fail after a yield or arrow function expression
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpCompare)}
			precLeft = OpEquals
		case AndToken:
			if OpAnd < prec {
				return left
			} else if precLeft < OpAnd {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpBitOr)}
			precLeft = OpAnd
		case OrToken:
			if OpOr < prec {
				return left
			} else if precLeft < OpOr {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpAnd)}
			precLeft = OpOr
		case NullishToken:
			if OpCoalesce < prec {
				return left
			} else if precLeft < OpBitOr && precLeft != OpCoalesce {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpBitOr)}
			precLeft = OpCoalesce
		case DotToken:
			// OpMember < prec does never happen
			if precLeft < OpLHS {
				p.fail("expression")
				return nil
			} else if OpMember < precLeft {
				precLeft = OpMember
			}
			p.next()
			if !IsIdentifierName(p.tt) && p.tt != PrivateIdentifierToken {
				p.fail("dot expression", IdentifierToken)
				return nil
			}
			if p.tt == PrivateIdentifierToken {
				left = &DotExpr{left, p.scope.Use(p.data), precLeft, false}
			} else {
				left = &DotExpr{left, LiteralExpr{IdentifierToken, p.data}, precLeft, false}
			}
			p.next()
		case OpenBracketToken:
			// OpMember < prec does never happen
			if precLeft < OpLHS {
				p.fail("expression")
				return nil
			} else if OpMember < precLeft {
				precLeft = OpMember
			}
			p.next()
			prevIn := p.in
			p.in = true
			left = &IndexExpr{left, p.parseExpression(OpExpr), precLeft, false}
			p.in = prevIn
			if !p.consume("index expression", CloseBracketToken) {
				return nil
			}
		case OpenParenToken:
			if OpCall < prec {
				return left
			} else if precLeft < OpLHS {
				p.fail("expression")
				return nil
			} else if OpCall < precLeft {
				precLeft = OpCall
			}
			prevIn := p.in
			p.in = true
			left = &CallExpr{left, p.parseArguments(), precLeft, false}
			p.in = prevIn
		case TemplateToken, TemplateStartToken:
			// OpMember < prec does never happen
			if precLeft < OpLHS {
				p.fail("expression")
				return nil
			} else if OpMember < precLeft {
				precLeft = OpMember
			}
			prevIn := p.in
			p.in = true
			template := p.parseTemplateLiteral(precLeft)
			template.Tag = left
			left = &template
			p.in = prevIn
		case OptChainToken:
			if OpOpt < prec {
				return left
			} else if precLeft < OpLHS {
				p.fail("expression")
				return nil
			}
			p.next()
			if p.tt == OpenParenToken {
				left = &CallExpr{left, p.parseArguments(), OpOpt, true}
			} else if p.tt == OpenBracketToken {
				p.next()
				left = &IndexExpr{left, p.parseExpression(OpExpr), OpOpt, true}
				if !p.consume("optional chaining expression", CloseBracketToken) {
					return nil
				}
			} else if p.tt == TemplateToken || p.tt == TemplateStartToken {
				template := p.parseTemplateLiteral(precLeft)
				template.Prec = OpOpt
				template.Tag = left
				template.Optional = true
				left = &template
			} else if IsIdentifierName(p.tt) {
				left = &DotExpr{left, LiteralExpr{IdentifierToken, p.data}, OpOpt, true}
				p.next()
			} else if p.tt == PrivateIdentifierToken {
				left = &DotExpr{left, p.scope.Use(p.data), OpOpt, true}
				p.next()
			} else {
				p.fail("optional chaining expression", IdentifierToken, OpenParenToken, OpenBracketToken, TemplateToken)
				return nil
			}
			precLeft = OpOpt
		case IncrToken:
			if p.prevLT || OpUpdate < prec {
				return left
			} else if precLeft < OpLHS {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &UnaryExpr{PostIncrToken, left}
			precLeft = OpUpdate
		case DecrToken:
			if p.prevLT || OpUpdate < prec {
				return left
			} else if precLeft < OpLHS {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &UnaryExpr{PostDecrToken, left}
			precLeft = OpUpdate
		case ExpToken:
			if OpExp < prec {
				return left
			} else if precLeft < OpUpdate {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpExp)}
			precLeft = OpExp
		case MulToken, DivToken, ModToken:
			if OpMul < prec {
				return left
			} else if precLeft < OpMul {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpExp)}
			precLeft = OpMul
		case AddToken, SubToken:
			if OpAdd < prec {
				return left
			} else if precLeft < OpAdd {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpMul)}
			precLeft = OpAdd
		case LtLtToken, GtGtToken, GtGtGtToken:
			if OpShift < prec {
				return left
			} else if precLeft < OpShift {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpAdd)}
			precLeft = OpShift
		case BitAndToken:
			if OpBitAnd < prec {
				return left
			} else if precLeft < OpBitAnd {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpEquals)}
			precLeft = OpBitAnd
		case BitXorToken:
			if OpBitXor < prec {
				return left
			} else if precLeft < OpBitXor {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpBitAnd)}
			precLeft = OpBitXor
		case BitOrToken:
			if OpBitOr < prec {
				return left
			} else if precLeft < OpBitOr {
				p.fail("expression")
				return nil
			}
			p.next()
			left = &BinaryExpr{tt, left, p.parseExpression(OpBitXor)}
			precLeft = OpBitOr
		case QuestionToken:
			if OpAssign < prec {
				return left
			} else if precLeft < OpCoalesce {
				p.fail("expression")
				return nil
			}
			p.next()
			prevIn := p.in
			p.in = true
			ifExpr := p.parseExpression(OpAssign)
			p.in = prevIn
			if !p.consume("conditional expression", ColonToken) {
				return nil
			}
			elseExpr := p.parseExpression(OpAssign)
			left = &CondExpr{left, ifExpr, elseExpr}
			precLeft = OpAssign
		case CommaToken:
			if OpExpr < prec {
				return left
			}
			p.next()
			if commaExpr, ok := left.(*CommaExpr); ok {
				commaExpr.List = append(commaExpr.List, p.parseExpression(OpAssign))
				i-- // adjust expression nesting limit
			} else {
				left = &CommaExpr{[]IExpr{left, p.parseExpression(OpAssign)}}
			}
			precLeft = OpExpr
		case ArrowToken:
			// handle identifier => ..., where identifier could also be yield or await
			if OpAssign < prec {
				return left
			} else if precLeft < OpPrimary || p.prevLT {
				p.fail("expression")
				return nil
			}

			v, ok := left.(*Var)
			if !ok {
				p.fail("expression")
				return nil
			}

			left = p.parseIdentifierArrowFunc(v)
			precLeft = OpAssign
		default:
			return left
		}
	}
}

func (p *Parser) parseAssignExprOrParam() IExpr {
	// this could be a BindingElement or an AssignmentExpression. Here we handle BindingIdentifier with a possible Initializer, BindingPattern will be handled by parseArrayLiteral or parseObjectLiteral
	if p.assumeArrowFunc && p.isIdentifierReference(p.tt) {
		tt := p.tt
		data := p.data
		p.next()
		if p.tt == EqToken || p.tt == CommaToken || p.tt == CloseParenToken || p.tt == CloseBraceToken || p.tt == CloseBracketToken {
			var ok bool
			var left IExpr
			left, ok = p.scope.Declare(ArgumentDecl, data)
			if ok {
				p.assumeArrowFunc = false
				left = p.parseExpressionSuffix(left, OpAssign, OpPrimary)
				p.assumeArrowFunc = true
				return left
			}
		}
		p.assumeArrowFunc = false
		if tt == AsyncToken {
			return p.parseAsyncExpression(OpAssign, data)
		}
		return p.parseIdentifierExpression(OpAssign, data)
	} else if p.tt != OpenBracketToken && p.tt != OpenBraceToken {
		p.assumeArrowFunc = false
	}
	return p.parseExpression(OpAssign)
}

func (p *Parser) parseParenthesizedExpression(prec OpPrec, async []byte) IExpr {
	// parse ArrowFunc, AsyncArrowFunc, AsyncCallExpr, ParenthesizedExpr
	var left IExpr
	precLeft := OpPrimary

	// expect to be at (
	p.next()

	isAsync := async != nil // prevLT is false before open parenthesis
	arrowFunc := &ArrowFunc{}
	parent := p.enterScope(&arrowFunc.Body.Scope, true)
	prevAssumeArrowFunc, prevIn := p.assumeArrowFunc, p.in
	p.assumeArrowFunc, p.in = true, true

	// parse an Arguments expression but assume we might be parsing an (async) arrow function or ParenthesisedExpression. If this is really an arrow function, parsing as an Arguments expression cannot fail as AssignmentExpression, ArrayLiteral, and ObjectLiteral are supersets of SingleNameBinding, ArrayBindingPattern, and ObjectBindingPattern respectively. Any identifier that would be a BindingIdentifier in case of an arrow function, will be added as such to the scope. If finally this is not an arrow function, we will demote those variables as undeclared and merge them with the parent scope.

	rests := 0
	var args Args
	for p.tt != CloseParenToken && p.tt != ErrorToken {
		if 0 < len(args.List) && args.List[len(args.List)-1].Rest {
			// only last parameter can have ellipsis
			p.assumeArrowFunc = false
			if !isAsync {
				p.fail("arrow function", CloseParenToken)
			}
		}

		rest := p.tt == EllipsisToken
		if rest {
			p.next()
			rests++
		}

		args.List = append(args.List, Arg{p.parseAssignExprOrParam(), rest})
		if p.tt != CommaToken {
			break
		}
		p.next()
	}
	if p.tt != CloseParenToken {
		p.fail("expression")
		return nil
	}
	p.next()
	isArrowFunc := !p.prevLT && p.tt == ArrowToken && p.assumeArrowFunc
	hasLastRest := 0 < rests && p.assumeArrowFunc
	p.assumeArrowFunc, p.in = prevAssumeArrowFunc, prevIn

	if isArrowFunc {
		prevAwait, prevYield := p.await, p.yield
		p.await, p.yield = isAsync, false

		// arrow function
		arrowFunc.Async = isAsync
		arrowFunc.Params = Params{List: make([]BindingElement, 0, len(args.List)-rests)}
		for _, arg := range args.List {
			if arg.Rest {
				arrowFunc.Params.Rest = p.exprToBinding(arg.Value)
			} else {
				arrowFunc.Params.List = append(arrowFunc.Params.List, p.exprToBindingElement(arg.Value)) // can not fail when assumArrowFunc is set
			}
		}
		arrowFunc.Body.List = p.parseArrowFuncBody()

		p.await, p.yield = prevAwait, prevYield
		p.exitScope(parent)

		left = arrowFunc
		precLeft = OpAssign
	} else if !isAsync && (len(args.List) == 0 || hasLastRest) {
		p.fail("arrow function", ArrowToken)
		return nil
	} else if isAsync && OpCall < prec || !isAsync && 0 < rests {
		p.fail("expression")
		return nil
	} else {
		// for any nested FuncExpr/ArrowFunc scope, Parent will point to the temporary scope created in case this was an arrow function instead of a parenthesized expression. This is not a problem as Parent is only used for defining new variables, and we already parsed all the nested scopes so that Parent (not Func) are not relevant anymore. Anyways, the Parent will just point to an empty scope, whose Parent/Func will point to valid scopes. This should not be a big deal.
		// Here we move all declared ArgumentDecls (in case of an arrow function) to its parent scope as undeclared variables (identifiers used in a parenthesized expression).
		p.exitScope(parent)
		arrowFunc.Body.Scope.UndeclareScope()

		if isAsync {
			// call expression
			left = p.scope.Use(async)
			left = &CallExpr{left, args, OpCall, false}
			precLeft = OpCall
		} else {
			// parenthesized expression
			if 1 < len(args.List) {
				commaExpr := &CommaExpr{}
				for _, arg := range args.List {
					commaExpr.List = append(commaExpr.List, arg.Value)
				}
				left = &GroupExpr{commaExpr}
			} else {
				left = &GroupExpr{args.List[0].Value}
			}
		}
	}
	return p.parseExpressionSuffix(left, prec, precLeft)
}

// exprToBindingElement and exprToBinding convert a CoverParenthesizedExpressionAndArrowParameterList into FormalParameters.
// Any unbound variables of the parameters (Initializer, ComputedPropertyName) are kept in the parent scope
func (p *Parser) exprToBindingElement(expr IExpr) (bindingElement BindingElement) {
	if assign, ok := expr.(*BinaryExpr); ok && assign.Op == EqToken {
		bindingElement.Binding = p.exprToBinding(assign.X)
		bindingElement.Default = assign.Y
	} else {
		bindingElement.Binding = p.exprToBinding(expr)
	}
	return
}

func (p *Parser) exprToBinding(expr IExpr) (binding IBinding) {
	if expr == nil {
		// no-op
	} else if v, ok := expr.(*Var); ok {
		binding = v
	} else if array, ok := expr.(*ArrayExpr); ok {
		bindingArray := BindingArray{}
		for _, item := range array.List {
			if item.Spread {
				// can only BindingIdentifier or BindingPattern
				bindingArray.Rest = p.exprToBinding(item.Value)
				break
			}
			var bindingElement BindingElement
			bindingElement = p.exprToBindingElement(item.Value)
			bindingArray.List = append(bindingArray.List, bindingElement)
		}
		binding = &bindingArray
	} else if object, ok := expr.(*ObjectExpr); ok {
		bindingObject := BindingObject{}
		for _, item := range object.List {
			if item.Spread {
				// can only be BindingIdentifier
				bindingObject.Rest = item.Value.(*Var)
				break
			}

			bindingElement := p.exprToBindingElement(item.Value)
			if v, ok := item.Value.(*Var); item.Name == nil || (ok && item.Name.IsIdent(v.Data)) {
				// IdentifierReference : Initializer
				bindingElement.Default = item.Init
			}
			bindingObject.List = append(bindingObject.List, BindingObjectItem{Key: item.Name, Value: bindingElement})
		}
		binding = &bindingObject
	} else {
		p.failMessage("invalid parameters in arrow function")
	}
	return
}

func (p *Parser) isIdentifierReference(tt TokenType) bool {
	return IsIdentifier(tt) || !p.yield && tt == YieldToken || !p.await && tt == AwaitToken
}
