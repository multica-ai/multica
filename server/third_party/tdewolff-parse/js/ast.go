package js

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
)

var ErrInvalidJSON = fmt.Errorf("invalid JSON")

type JSONer interface {
	JSON(io.Writer) error
}

// AST is the full ECMAScript abstract syntax tree.
type AST struct {
	BlockStmt // module
}

func (ast AST) String() string {
	var s strings.Builder
	for i, item := range ast.BlockStmt.List {
		if i != 0 {
			s.WriteString(" ")
		}
		s.WriteString(item.String())
	}
	return s.String()
}

// JS writes JavaScript to writer.
func (ast AST) JS(w io.Writer) {
	for i, item := range ast.List {
		if i != 0 {
			w.Write([]byte("\n"))
		}
		item.JS(w)
		if _, ok := item.(*VarDecl); ok {
			w.Write([]byte(";"))
		}
	}
}

// JSONString returns a string of JavaScript.
func (ast AST) JSString() string {
	sb := strings.Builder{}
	ast.JS(&sb)
	return sb.String()
}

// JSON writes JSON to writer.
func (ast AST) JSON(w io.Writer) error {
	if 1 < len(ast.List) {
		return fmt.Errorf("%v: JS must be a single statement", ErrInvalidJSON)
	} else if len(ast.List) == 0 {
		return nil
	}
	exprStmt, ok := ast.List[0].(*ExprStmt)
	if !ok {
		return fmt.Errorf("%v: JS must be an expression statement", ErrInvalidJSON)
	}
	expr := exprStmt.Value
	if group, ok := expr.(*GroupExpr); ok {
		expr = group.X // allow parsing expr contained in group expr
	}
	if val, ok := expr.(JSONer); !ok {
		return fmt.Errorf("%v: JS must be a valid JSON expression", ErrInvalidJSON)
	} else {
		return val.JSON(w)
	}
	return nil
}

// JSONString returns a string of JSON if valid.
func (ast AST) JSONString() (string, error) {
	sb := strings.Builder{}
	err := ast.JSON(&sb)
	return sb.String(), err
}

////////////////////////////////////////////////////////////////

// DeclType specifies the kind of declaration.
type DeclType uint16

// DeclType values.
const (
	NoDecl       DeclType = iota // undeclared variables
	VariableDecl                 // var
	PrivateDecl                  // private class field
	FunctionDecl                 // function
	ArgumentDecl                 // function and method arguments
	LexicalDecl                  // let, const, class
	CatchDecl                    // catch statement argument
	ExprDecl                     // function expression name or class expression name
)

func (decl DeclType) String() string {
	switch decl {
	case NoDecl:
		return "NoDecl"
	case VariableDecl:
		return "VariableDecl"
	case PrivateDecl:
		return "PrivateDecl"
	case FunctionDecl:
		return "FunctionDecl"
	case ArgumentDecl:
		return "ArgumentDecl"
	case LexicalDecl:
		return "LexicalDecl"
	case CatchDecl:
		return "CatchDecl"
	case ExprDecl:
		return "ExprDecl"
	}
	return "Invalid(" + strconv.Itoa(int(decl)) + ")"
}

// Var is a variable, where Decl is the type of declaration and can be var|function for function scoped variables, let|const|class for block scoped variables.
type Var struct {
	Data []byte
	Link *Var // is set when merging variable uses, as in:  {a} {var a}  where the first links to the second, only used for undeclared variables
	Uses uint16
	Decl DeclType
}

// Name returns the variable name.
func (v *Var) Name() []byte {
	for v.Link != nil {
		v = v.Link
	}
	return v.Data
}

func (v *Var) Info() string {
	s := fmt.Sprintf("%p type=%s name='%s' uses=%d", v, v.Decl, string(v.Data), v.Uses)
	links := 0
	for v.Link != nil {
		v = v.Link
		links++
	}
	if 0 < links {
		s += fmt.Sprintf(" links=%d => %p", links, v)
	}
	return s
}

func (v Var) String() string {
	return string(v.Name())
}

// JS writes JavaScript to writer.
func (v Var) JS(w io.Writer) {
	w.Write(v.Name())
}

// VarsByUses is sortable by uses in descending order.
type VarsByUses VarArray

func (vs VarsByUses) Len() int {
	return len(vs)
}

func (vs VarsByUses) Swap(i, j int) {
	vs[i], vs[j] = vs[j], vs[i]
}

func (vs VarsByUses) Less(i, j int) bool {
	return vs[i].Uses > vs[j].Uses
}

////////////////////////////////////////////////////////////////

// VarArray is a set of variables in scopes.
type VarArray []*Var

func (vs VarArray) String() string {
	var s strings.Builder
	s.WriteString("[")
	for i, v := range vs {
		if i != 0 {
			s.WriteString(", ")
		}
		links := 0
		for v.Link != nil {
			v = v.Link
			links++
		}
		s.WriteString(fmt.Sprintf("Var{%v %s %v %v}", v.Decl, string(v.Data), links, v.Uses))
	}
	return s.String() + "]"
}

// Scope is a function or block scope with a list of variables declared and used.
type Scope struct {
	Parent, Func   *Scope   // Parent is nil for global scope
	Declared       VarArray // Link in Var are always nil
	Undeclared     VarArray
	VarDecls       []*VarDecl
	NumForDecls    uint16 // offset into Declared to mark variables used in for statements
	NumFuncArgs    uint16 // offset into Declared to mark variables used in function arguments
	NumArgUses     uint16 // offset into Undeclared to mark variables used in arguments
	IsGlobalOrFunc bool
	HasWith        bool
}

func (s Scope) String() string {
	return "Scope{Declared: " + s.Declared.String() + ", Undeclared: " + s.Undeclared.String() + "}"
}

// Declare declares a new variable.
func (s *Scope) Declare(decl DeclType, name []byte) (*Var, bool) {
	// refer to new variable for previously undeclared symbols in the current and lower scopes
	// this happens in `{ a = 5; } var a` where both a's refer to the same variable
	curScope := s
	if decl == VariableDecl || decl == FunctionDecl {
		// find function scope for var and function declarations
		for s != s.Func {
			// make sure that `{let i;{var i}}` is an error
			if v := s.findDeclared(name, false); v != nil && v.Decl != decl && v.Decl != CatchDecl {
				return nil, false
			}
			s = s.Parent
		}
	}

	if v := s.findDeclared(name, true); v != nil {
		// variable already declared, might be an error or a duplicate declaration
		if (ArgumentDecl < v.Decl || FunctionDecl < decl) && v.Decl != ExprDecl {
			// only allow (v.Decl,decl) of: (var|private|function|argument,var|private|function), (expr,*), any other combination is a syntax error
			return nil, false
		}
		if v.Decl == ExprDecl {
			v.Decl = decl
		}
		v.Uses++
		for s != curScope {
			curScope.AddUndeclared(v) // add variable declaration as used variable to the current scope
			curScope = curScope.Parent
		}
		return v, true
	}

	var v *Var
	// reuse variable if previously used, as in:  a;var a
	if decl != ArgumentDecl { // in case of function f(a=b,b), where the first b is different from the second
		for i, uv := range s.Undeclared[s.NumArgUses:] {
			// no need to evaluate v.Link as v.Data stays the same and Link is nil in the active scope
			if 0 < uv.Uses && uv.Decl == NoDecl && bytes.Equal(name, uv.Data) {
				// must be NoDecl so that it can't be a var declaration that has been added
				v = uv
				s.Undeclared = append(s.Undeclared[:int(s.NumArgUses)+i], s.Undeclared[int(s.NumArgUses)+i+1:]...)
				break
			}
		}
	}
	if v == nil {
		// add variable to the context list and to the scope
		v = &Var{name, nil, 0, decl}
	} else {
		v.Decl = decl
	}
	v.Uses++
	s.Declared = append(s.Declared, v)
	for s != curScope {
		curScope.AddUndeclared(v) // add variable declaration as used variable to the current scope
		curScope = curScope.Parent
	}
	return v, true
}

// Use increments the usage of a variable.
func (s *Scope) Use(name []byte) *Var {
	// check if variable is declared in the current scope
	v := s.findDeclared(name, false)
	if v == nil {
		// check if variable is already used before in the current or lower scopes
		v = s.findUndeclared(name)
		if v == nil {
			// add variable to the context list and to the scope's undeclared
			v = &Var{name, nil, 0, NoDecl}
			s.Undeclared = append(s.Undeclared, v)
		}
	}
	v.Uses++
	return v
}

// findDeclared finds a declared variable in the current scope.
func (s *Scope) findDeclared(name []byte, skipForDeclared bool) *Var {
	start := 0
	if skipForDeclared {
		// we skip the for initializer for declarations (only has effect for let/const)
		start = int(s.NumForDecls)
	}
	// reverse order to find the inner let first in `for(let a in []){let a; {a}}`
	for i := len(s.Declared) - 1; start <= i; i-- {
		v := s.Declared[i]
		// no need to evaluate v.Link as v.Data stays the same, and Link is always nil in Declared
		if bytes.Equal(name, v.Data) {
			return v
		}
	}
	return nil
}

// findUndeclared finds an undeclared variable in the current and contained scopes.
func (s *Scope) findUndeclared(name []byte) *Var {
	for _, v := range s.Undeclared {
		// no need to evaluate v.Link as v.Data stays the same and Link is nil in the active scope
		if 0 < v.Uses && bytes.Equal(name, v.Data) {
			return v
		}
	}
	return nil
}

// add undeclared variable to scope, this is called for the block scope when declaring a var in it
func (s *Scope) AddUndeclared(v *Var) {
	// don't add undeclared symbol if it's already there
	for _, vorig := range s.Undeclared {
		if v == vorig {
			return
		}
	}
	s.Undeclared = append(s.Undeclared, v) // add variable declaration as used variable to the current scope
}

// MarkForStmt marks the declared variables in current scope as for statement initializer to distinguish from declarations in body.
func (s *Scope) MarkForStmt() {
	s.NumForDecls = uint16(len(s.Declared))
	s.NumArgUses = uint16(len(s.Undeclared)) // ensures for different b's in for(var a in b){let b}
}

// MarkFuncArgs marks the declared/undeclared variables in the current scope as function arguments.
func (s *Scope) MarkFuncArgs() {
	s.NumFuncArgs = uint16(len(s.Declared))
	s.NumArgUses = uint16(len(s.Undeclared)) // ensures different b's in `function f(a=b){var b}`.
}

// HoistUndeclared copies all undeclared variables of the current scope to the parent scope.
func (s *Scope) HoistUndeclared() {
	for i, vorig := range s.Undeclared {
		// no need to evaluate vorig.Link as vorig.Data stays the same
		if 0 < vorig.Uses && vorig.Decl == NoDecl {
			if v := s.Parent.findDeclared(vorig.Data, false); v != nil {
				// check if variable is declared in parent scope
				v.Uses += vorig.Uses
				vorig.Link = v
				s.Undeclared[i] = v // point reference to existing var (to avoid many Link chains)
			} else if v := s.Parent.findUndeclared(vorig.Data); v != nil {
				// check if variable is already used before in parent scope
				v.Uses += vorig.Uses
				vorig.Link = v
				s.Undeclared[i] = v // point reference to existing var (to avoid many Link chains)
			} else {
				// add variable to the context list and to the scope's undeclared
				s.Parent.Undeclared = append(s.Parent.Undeclared, vorig)
			}
		}
	}
}

// UndeclareScope undeclares all declared variables in the current scope and adds them to the parent scope.
// Called when possible arrow func ends up being a parenthesized expression, scope is not further used.
func (s *Scope) UndeclareScope() {
	// look if the variable already exists in the parent scope, if so replace the Var pointer in original use
	for _, vorig := range s.Declared {
		// no need to evaluate vorig.Link as vorig.Data stays the same, and Link is always nil in Declared
		// vorig.Uses will be atleast 1
		if v := s.Parent.findDeclared(vorig.Data, false); v != nil {
			// check if variable has been declared in this scope
			v.Uses += vorig.Uses
			vorig.Link = v
		} else if v := s.Parent.findUndeclared(vorig.Data); v != nil {
			// check if variable is already used before in the current or lower scopes
			v.Uses += vorig.Uses
			vorig.Link = v
		} else {
			// add variable to the context list and to the scope's undeclared
			vorig.Decl = NoDecl
			s.Parent.Undeclared = append(s.Parent.Undeclared, vorig)
		}
	}
	s.Declared = s.Declared[:0]
	s.Undeclared = s.Undeclared[:0]
}

// Unscope moves all declared variables of the current scope to the parent scope. Undeclared variables are already in the parent scope.
func (s *Scope) Unscope() {
	for _, vorig := range s.Declared {
		// no need to evaluate vorig.Link as vorig.Data stays the same, and Link is always nil in Declared
		// vorig.Uses will be atleast 1
		s.Parent.Declared = append(s.Parent.Declared, vorig)
	}
	s.Declared = s.Declared[:0]
	s.Undeclared = s.Undeclared[:0]
}

////////////////////////////////////////////////////////////////

// INode is an interface for AST nodes
type INode interface {
	String() string
	JS(io.Writer)
}

// IStmt is a dummy interface for statements.
type IStmt interface {
	INode
	stmtNode()
}

// IBinding is a dummy interface for bindings.
type IBinding interface {
	INode
	bindingNode()
}

// IExpr is a dummy interface for expressions.
type IExpr interface {
	INode
	exprNode()
}

////////////////////////////////////////////////////////////////

// Comment block or line, usually a bang comment.
type Comment struct {
	Value []byte
}

func (n Comment) String() string {
	return "Stmt(" + string(n.Value) + ")"
}

// JS writes JavaScript to writer.
func (n Comment) JS(w io.Writer) {
	if wi, ok := w.(parse.Indenter); ok {
		wi.Writer.Write(n.Value)
	} else {
		w.Write(n.Value)
	}
}

// BlockStmt is a block statement.
type BlockStmt struct {
	List []IStmt
	Scope
}

func (n BlockStmt) String() string {
	var s strings.Builder
	s.WriteString("Stmt({")
	for _, item := range n.List {
		s.WriteString(" " + item.String())
	}
	return s.String() + " })"
}

// JS writes JavaScript to writer.
func (n BlockStmt) JS(w io.Writer) {
	if len(n.List) == 0 {
		w.Write([]byte("{}"))
		return
	}

	w.Write([]byte("{"))
	wi := parse.NewIndenter(w, 4)
	for _, item := range n.List {
		wi.Write([]byte("\n"))
		item.JS(wi)
		if _, ok := item.(*VarDecl); ok {
			w.Write([]byte(";"))
		}
	}
	w.Write([]byte("\n}"))
}

// EmptyStmt is an empty statement.
type EmptyStmt struct{}

func (n EmptyStmt) String() string {
	return "Stmt()"
}

// JS writes JavaScript to writer.
func (n EmptyStmt) JS(w io.Writer) {
	w.Write([]byte(";"))
}

// ExprStmt is an expression statement.
type ExprStmt struct {
	Value IExpr
}

func (n ExprStmt) String() string {
	val := n.Value.String()
	if val[0] == '(' && val[len(val)-1] == ')' {
		return "Stmt" + n.Value.String()
	}
	return "Stmt(" + n.Value.String() + ")"
}

// JS writes JavaScript to writer.
func (n ExprStmt) JS(w io.Writer) {
	buf := &bytes.Buffer{}
	wb := io.Writer(buf)
	if wi, ok := w.(parse.Indenter); ok {
		// make sure that buf is indenter if w is so as well
		// this is to prevent newlines in literals from indenting
		wb = parse.NewIndenter(wb, wi.Indent())
		w = wi.Writer
	}
	n.Value.JS(wb)
	expr := buf.Bytes()

	group := bytes.HasPrefix(expr, []byte("let "))
	if group {
		w.Write([]byte("("))
	}
	w.Write(expr)
	if group {
		w.Write([]byte(")"))
	}
	w.Write([]byte(";"))
}

// IfStmt is an if statement.
type IfStmt struct {
	Cond IExpr
	Body IStmt
	Else IStmt // can be nil
}

func (n IfStmt) String() string {
	s := "Stmt(if " + n.Cond.String() + " " + n.Body.String()
	if n.Else != nil {
		s += " else " + n.Else.String()
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n IfStmt) JS(w io.Writer) {
	w.Write([]byte("if ("))
	n.Cond.JS(w)
	w.Write([]byte(")"))
	if _, ok := n.Body.(*EmptyStmt); !ok {
		w.Write([]byte(" "))
	}
	n.Body.JS(w)
	if _, ok := n.Body.(*VarDecl); ok {
		w.Write([]byte(";"))
	}
	if n.Else != nil {
		w.Write([]byte(" else"))
		if _, ok := n.Else.(*EmptyStmt); !ok {
			w.Write([]byte(" "))
		}
		n.Else.JS(w)
		if _, ok := n.Else.(*VarDecl); ok {
			w.Write([]byte(";"))
		}
	}
}

// DoWhileStmt is a do-while iteration statement.
type DoWhileStmt struct {
	Cond IExpr
	Body IStmt
}

func (n DoWhileStmt) String() string {
	return "Stmt(do " + n.Body.String() + " while " + n.Cond.String() + ")"
}

// JS writes JavaScript to writer.
func (n DoWhileStmt) JS(w io.Writer) {
	w.Write([]byte("do"))
	if _, ok := n.Body.(*EmptyStmt); !ok {
		w.Write([]byte(" "))
	}
	n.Body.JS(w)
	if _, ok := n.Body.(*VarDecl); ok {
		w.Write([]byte("; "))
	} else if _, ok := n.Body.(*Comment); !ok {
		w.Write([]byte(" "))
	}
	w.Write([]byte("while ("))
	n.Cond.JS(w)
	w.Write([]byte(");"))
}

// WhileStmt is a while iteration statement.
type WhileStmt struct {
	Cond IExpr
	Body IStmt
}

func (n WhileStmt) String() string {
	return "Stmt(while " + n.Cond.String() + " " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n WhileStmt) JS(w io.Writer) {
	w.Write([]byte("while ("))
	n.Cond.JS(w)
	w.Write([]byte(")"))
	if _, ok := n.Body.(*EmptyStmt); ok {
		w.Write([]byte(";"))
		return
	}
	w.Write([]byte(" "))
	n.Body.JS(w)
	if _, ok := n.Body.(*VarDecl); ok {
		w.Write([]byte(";"))
	}
}

// ForStmt is a regular for iteration statement.
type ForStmt struct {
	Init IExpr // can be nil
	Cond IExpr // can be nil
	Post IExpr // can be nil
	Body *BlockStmt
}

func (n ForStmt) String() string {
	s := "Stmt(for"
	if v, ok := n.Init.(*VarDecl); !ok && n.Init != nil || ok && len(v.List) != 0 {
		s += " " + n.Init.String()
	}
	s += " ;"
	if n.Cond != nil {
		s += " " + n.Cond.String()
	}
	s += " ;"
	if n.Post != nil {
		s += " " + n.Post.String()
	}
	return s + " " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n ForStmt) JS(w io.Writer) {
	w.Write([]byte("for ("))
	if v, ok := n.Init.(*VarDecl); !ok && n.Init != nil || ok && len(v.List) != 0 {
		n.Init.JS(w)
	} else {
		w.Write([]byte(" "))
	}
	w.Write([]byte("; "))
	if n.Cond != nil {
		n.Cond.JS(w)
	}
	w.Write([]byte("; "))
	if n.Post != nil {
		n.Post.JS(w)
	}
	w.Write([]byte(") "))
	n.Body.JS(w)
}

// ForInStmt is a for-in iteration statement.
type ForInStmt struct {
	Init  IExpr
	Value IExpr
	Body  *BlockStmt
}

func (n ForInStmt) String() string {
	return "Stmt(for " + n.Init.String() + " in " + n.Value.String() + " " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n ForInStmt) JS(w io.Writer) {
	w.Write([]byte("for ("))
	n.Init.JS(w)
	w.Write([]byte(" in "))
	n.Value.JS(w)
	w.Write([]byte(") "))
	n.Body.JS(w)
}

// ForOfStmt is a for-of iteration statement.
type ForOfStmt struct {
	Await bool
	Init  IExpr
	Value IExpr
	Body  *BlockStmt
}

func (n ForOfStmt) String() string {
	s := "Stmt(for"
	if n.Await {
		s += " await"
	}
	return s + " " + n.Init.String() + " of " + n.Value.String() + " " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n ForOfStmt) JS(w io.Writer) {
	w.Write([]byte("for"))
	if n.Await {
		w.Write([]byte(" await"))
	}
	w.Write([]byte(" ("))
	n.Init.JS(w)
	w.Write([]byte(" of "))
	n.Value.JS(w)
	w.Write([]byte(") "))
	n.Body.JS(w)
}

// CaseClause is a case clause or default clause for a switch statement.
type CaseClause struct {
	TokenType
	Cond IExpr // can be nil
	List []IStmt
}

func (n CaseClause) String() string {
	var s strings.Builder
	s.WriteString(" Clause(" + n.TokenType.String())
	if n.Cond != nil {
		s.WriteString(" " + n.Cond.String())
	}
	for _, item := range n.List {
		s.WriteString(" " + item.String())
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n CaseClause) JS(w io.Writer) {
	if n.Cond != nil {
		w.Write([]byte("case "))
		n.Cond.JS(w)
	} else {
		w.Write([]byte("default"))
	}
	w.Write([]byte(":"))
	wi := parse.NewIndenter(w, 4)
	for _, item := range n.List {
		wi.Write([]byte("\n"))
		item.JS(wi)
		if _, ok := item.(*VarDecl); ok {
			w.Write([]byte(";"))
		}
	}
}

// SwitchStmt is a switch statement.
type SwitchStmt struct {
	Init IExpr
	List []CaseClause
	Scope
}

func (n SwitchStmt) String() string {
	var s strings.Builder
	s.WriteString("Stmt(switch " + n.Init.String())
	for _, clause := range n.List {
		s.WriteString(clause.String())
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n SwitchStmt) JS(w io.Writer) {
	w.Write([]byte("switch ("))
	n.Init.JS(w)
	if len(n.List) == 0 {
		w.Write([]byte(") {}"))
		return
	}
	w.Write([]byte(") {"))
	for _, clause := range n.List {
		w.Write([]byte("\n"))
		clause.JS(w)
	}
	w.Write([]byte("\n}"))
}

// BranchStmt is a continue or break statement.
type BranchStmt struct {
	Type  TokenType
	Label []byte // can be nil
}

func (n BranchStmt) String() string {
	s := "Stmt(" + n.Type.String()
	if n.Label != nil {
		s += " " + string(n.Label)
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n BranchStmt) JS(w io.Writer) {
	w.Write(n.Type.Bytes())
	if n.Label != nil {
		w.Write([]byte(" "))
		w.Write(n.Label)
	}
	w.Write([]byte(";"))
}

// ReturnStmt is a return statement.
type ReturnStmt struct {
	Value IExpr // can be nil
}

func (n ReturnStmt) String() string {
	s := "Stmt(return"
	if n.Value != nil {
		s += " " + n.Value.String()
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n ReturnStmt) JS(w io.Writer) {
	w.Write([]byte("return"))
	if n.Value != nil {
		w.Write([]byte(" "))
		n.Value.JS(w)
	}
	w.Write([]byte(";"))
}

// WithStmt is a with statement.
type WithStmt struct {
	Cond IExpr
	Body IStmt
}

func (n WithStmt) String() string {
	return "Stmt(with " + n.Cond.String() + " " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n WithStmt) JS(w io.Writer) {
	w.Write([]byte("with ("))
	n.Cond.JS(w)
	w.Write([]byte(")"))
	if _, ok := n.Body.(*EmptyStmt); !ok {
		w.Write([]byte(" "))
	}
	n.Body.JS(w)
	if _, ok := n.Body.(*VarDecl); ok {
		w.Write([]byte(";"))
	}
}

// LabelledStmt is a labelled statement.
type LabelledStmt struct {
	Label []byte
	Value IStmt
}

func (n LabelledStmt) String() string {
	return "Stmt(" + string(n.Label) + " : " + n.Value.String() + ")"
}

// JS writes JavaScript to writer.
func (n LabelledStmt) JS(w io.Writer) {
	w.Write(n.Label)
	w.Write([]byte(":"))
	if _, ok := n.Value.(*EmptyStmt); !ok {
		w.Write([]byte(" "))
	}
	n.Value.JS(w)
	if _, ok := n.Value.(*VarDecl); ok {
		w.Write([]byte(";"))
	}
}

// ThrowStmt is a throw statement.
type ThrowStmt struct {
	Value IExpr
}

func (n ThrowStmt) String() string {
	return "Stmt(throw " + n.Value.String() + ")"
}

// JS writes JavaScript to writer.
func (n ThrowStmt) JS(w io.Writer) {
	w.Write([]byte("throw "))
	n.Value.JS(w)
	w.Write([]byte(";"))
}

// TryStmt is a try statement.
type TryStmt struct {
	Body    *BlockStmt
	Binding IBinding   // can be nil
	Catch   *BlockStmt // can be nil
	Finally *BlockStmt // can be nil
}

func (n TryStmt) String() string {
	s := "Stmt(try " + n.Body.String()
	if n.Catch != nil {
		s += " catch"
		if n.Binding != nil {
			s += " Binding(" + n.Binding.String() + ")"
		}
		s += " " + n.Catch.String()
	}
	if n.Finally != nil {
		s += " finally " + n.Finally.String()
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n TryStmt) JS(w io.Writer) {
	w.Write([]byte("try "))
	n.Body.JS(w)
	if n.Catch != nil {
		w.Write([]byte(" catch"))
		if n.Binding != nil {
			w.Write([]byte("("))
			n.Binding.JS(w)
			w.Write([]byte(")"))
		}
		w.Write([]byte(" "))
		n.Catch.JS(w)
	}
	if n.Finally != nil {
		w.Write([]byte(" finally "))
		n.Finally.JS(w)
	}
}

// DebuggerStmt is a debugger statement.
type DebuggerStmt struct{}

func (n DebuggerStmt) String() string {
	return "Stmt(debugger)"
}

// JS writes JavaScript to writer.
func (n DebuggerStmt) JS(w io.Writer) {
	w.Write([]byte("debugger;"))
}

// Alias is a name space import or import/export specifier for import/export statements.
type Alias struct {
	Name    []byte // can be nil
	Binding []byte // can be nil
}

func (alias Alias) String() string {
	s := ""
	if alias.Name != nil {
		s += string(alias.Name) + " as "
	}
	return s + string(alias.Binding)
}

// JS writes JavaScript to writer.
func (alias Alias) JS(w io.Writer) {
	if alias.Name != nil {
		w.Write(alias.Name)
		w.Write([]byte(" as "))
	}
	w.Write(alias.Binding)
}

// ImportStmt is an import statement.
type ImportStmt struct {
	List    []Alias
	Default []byte // can be nil
	Module  []byte
}

func (n ImportStmt) String() string {
	var s strings.Builder
	s.WriteString("Stmt(import")
	if n.Default != nil {
		s.WriteString(" " + string(n.Default))
		if n.List != nil {
			s.WriteString(" ,")
		}
	}
	if len(n.List) == 1 && len(n.List[0].Name) == 1 && n.List[0].Name[0] == '*' {
		s.WriteString(" " + n.List[0].String())
	} else if n.List != nil {
		s.WriteString(" {")
		for i, item := range n.List {
			if i != 0 {
				s.WriteString(" ,")
			}
			if item.Binding != nil {
				s.WriteString(" " + item.String())
			}
		}
		s.WriteString(" }")
	}
	if n.Default != nil || n.List != nil {
		s.WriteString(" from")
	}
	return s.String() + " " + string(n.Module) + ")"
}

// JS writes JavaScript to writer.
func (n ImportStmt) JS(w io.Writer) {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	w.Write([]byte("import"))
	if n.Default != nil {
		w.Write([]byte(" "))
		w.Write(n.Default)
		if n.List != nil {
			w.Write([]byte(","))
		}
	}
	if len(n.List) == 1 && len(n.List[0].Name) == 1 && n.List[0].Name[0] == '*' {
		w.Write([]byte(" "))
		n.List[0].JS(w)
	} else if n.List != nil {
		if len(n.List) == 0 {
			w.Write([]byte(" {}"))
		} else {
			w.Write([]byte(" {"))
			for j, item := range n.List {
				if j != 0 {
					w.Write([]byte(","))
				}
				if item.Binding != nil {
					w.Write([]byte(" "))
					item.JS(w)
				}
			}
			w.Write([]byte(" }"))
		}
	}
	if n.Default != nil || n.List != nil {
		w.Write([]byte(" from"))
	}
	w.Write([]byte(" "))
	w.Write(n.Module)
	w.Write([]byte(";"))
}

// ExportStmt is an export statement.
type ExportStmt struct {
	List    []Alias
	Module  []byte // can be nil
	Default bool
	Decl    IExpr
}

func (n ExportStmt) String() string {
	s := "Stmt(export"
	if n.Decl != nil {
		if n.Default {
			s += " default"
		}
		return s + " " + n.Decl.String() + ")"
	} else if len(n.List) == 1 && (len(n.List[0].Name) == 1 && n.List[0].Name[0] == '*' || n.List[0].Name == nil && len(n.List[0].Binding) == 1 && n.List[0].Binding[0] == '*') {
		s += " " + n.List[0].String()
	} else if 0 < len(n.List) {
		s += " {"
		for i, item := range n.List {
			if i != 0 {
				s += " ,"
			}
			if item.Binding != nil {
				s += " " + item.String()
			}
		}
		s += " }"
	}
	if n.Module != nil {
		s += " from " + string(n.Module)
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n ExportStmt) JS(w io.Writer) {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	w.Write([]byte("export"))
	if n.Decl != nil {
		if n.Default {
			w.Write([]byte(" default"))
		}
		w.Write([]byte(" "))
		n.Decl.JS(w)
		w.Write([]byte(";"))
		return
	} else if len(n.List) == 1 && (len(n.List[0].Name) == 1 && n.List[0].Name[0] == '*' || n.List[0].Name == nil && len(n.List[0].Binding) == 1 && n.List[0].Binding[0] == '*') {
		w.Write([]byte(" "))
		n.List[0].JS(w)
	} else if len(n.List) == 0 {
		w.Write([]byte(" {}"))
	} else {
		w.Write([]byte(" {"))
		for j, item := range n.List {
			if j != 0 {
				w.Write([]byte(","))
			}
			if item.Binding != nil {
				w.Write([]byte(" "))
				item.JS(w)
			}
		}
		w.Write([]byte(" }"))
	}
	if n.Module != nil {
		w.Write([]byte(" from "))
		w.Write(n.Module)
	}
	w.Write([]byte(";"))
}

// DirectivePrologueStmt is a string literal at the beginning of a function or module (usually "use strict").
type DirectivePrologueStmt struct {
	Value []byte
}

func (n DirectivePrologueStmt) String() string {
	return "Stmt(" + string(n.Value) + ")"
}

// JS writes JavaScript to writer.
func (n DirectivePrologueStmt) JS(w io.Writer) {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	w.Write(n.Value)
	w.Write([]byte(";"))
}

func (n Comment) stmtNode()               {}
func (n BlockStmt) stmtNode()             {}
func (n EmptyStmt) stmtNode()             {}
func (n ExprStmt) stmtNode()              {}
func (n IfStmt) stmtNode()                {}
func (n DoWhileStmt) stmtNode()           {}
func (n WhileStmt) stmtNode()             {}
func (n ForStmt) stmtNode()               {}
func (n ForInStmt) stmtNode()             {}
func (n ForOfStmt) stmtNode()             {}
func (n SwitchStmt) stmtNode()            {}
func (n BranchStmt) stmtNode()            {}
func (n ReturnStmt) stmtNode()            {}
func (n WithStmt) stmtNode()              {}
func (n LabelledStmt) stmtNode()          {}
func (n ThrowStmt) stmtNode()             {}
func (n TryStmt) stmtNode()               {}
func (n DebuggerStmt) stmtNode()          {}
func (n ImportStmt) stmtNode()            {}
func (n ExportStmt) stmtNode()            {}
func (n DirectivePrologueStmt) stmtNode() {}

////////////////////////////////////////////////////////////////

// PropertyName is a property name for binding properties, method names, and in object literals.
type PropertyName struct {
	Literal  LiteralExpr
	Computed IExpr // can be nil
}

// IsSet returns true is PropertyName is not nil.
func (n PropertyName) IsSet() bool {
	return n.IsComputed() || n.Literal.TokenType != ErrorToken
}

// IsComputed returns true if PropertyName is computed.
func (n PropertyName) IsComputed() bool {
	return n.Computed != nil
}

// IsIdent returns true if PropertyName equals the given identifier name.
func (n PropertyName) IsIdent(data []byte) bool {
	return !n.IsComputed() && n.Literal.TokenType == IdentifierToken && bytes.Equal(data, n.Literal.Data)
}

func (n PropertyName) String() string {
	if n.Computed != nil {
		val := n.Computed.String()
		if val[0] == '(' {
			return "[" + val[1:len(val)-1] + "]"
		}
		return "[" + val + "]"
	}
	return string(n.Literal.Data)
}

// JS writes JavaScript to writer.
func (n PropertyName) JS(w io.Writer) {
	if n.Computed != nil {
		w.Write([]byte("["))
		n.Computed.JS(w)
		w.Write([]byte("]"))
		return
	}
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	w.Write(n.Literal.Data)
}

// BindingArray is an array binding pattern.
type BindingArray struct {
	List []BindingElement
	Rest IBinding // can be nil
}

func (n BindingArray) String() string {
	var s strings.Builder
	s.WriteString("[")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(",")
		}
		s.WriteString(" " + item.String())
	}
	if n.Rest != nil {
		if len(n.List) != 0 {
			s.WriteString(",")
		}
		s.WriteString(" ...Binding(" + n.Rest.String() + ")")
	} else if 0 < len(n.List) && n.List[len(n.List)-1].Binding == nil {
		s.WriteString(",")
	}
	return s.String() + " ]"
}

// JS writes JavaScript to writer.
func (n BindingArray) JS(w io.Writer) {
	w.Write([]byte("["))
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(","))
		}
		if item.Binding != nil {
			if j != 0 {
				w.Write([]byte(" "))
			}
			item.JS(w)
		}
	}
	if n.Rest != nil {
		if len(n.List) != 0 {
			w.Write([]byte(", "))
		}
		w.Write([]byte("..."))
		n.Rest.JS(w)
	} else if 0 < len(n.List) && n.List[len(n.List)-1].Binding == nil {
		w.Write([]byte(","))
	}
	w.Write([]byte("]"))
}

// BindingObjectItem is a binding property.
type BindingObjectItem struct {
	Key   *PropertyName // can be nil
	Value BindingElement
}

func (n BindingObjectItem) String() string {
	s := ""
	if n.Key != nil {
		if v, ok := n.Value.Binding.(*Var); !ok || !n.Key.IsIdent(v.Data) {
			s += " " + n.Key.String() + ":"
		}
	}
	return s + " " + n.Value.String()
}

// JS writes JavaScript to writer.
func (n BindingObjectItem) JS(w io.Writer) {
	if n.Key != nil {
		if v, ok := n.Value.Binding.(*Var); !ok || !n.Key.IsIdent(v.Data) {
			n.Key.JS(w)
			w.Write([]byte(": "))
		}
	}
	n.Value.JS(w)
}

// BindingObject is an object binding pattern.
type BindingObject struct {
	List []BindingObjectItem
	Rest *Var // can be nil
}

func (n BindingObject) String() string {
	var s strings.Builder
	s.WriteString("{")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(",")
		}
		s.WriteString(item.String())
	}
	if n.Rest != nil {
		if len(n.List) != 0 {
			s.WriteString(",")
		}
		s.WriteString(" ...Binding(" + string(n.Rest.Data) + ")")
	}
	return s.String() + " }"
}

// JS writes JavaScript to writer.
func (n BindingObject) JS(w io.Writer) {
	w.Write([]byte("{"))
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(", "))
		}
		item.JS(w)
	}
	if n.Rest != nil {
		if len(n.List) != 0 {
			w.Write([]byte(", "))
		}
		w.Write([]byte("..."))
		w.Write(n.Rest.Data)
	}
	w.Write([]byte("}"))
}

// BindingElement is a binding element.
type BindingElement struct {
	Binding IBinding // can be nil (in case of ellision)
	Default IExpr    // can be nil
}

func (n BindingElement) String() string {
	if n.Binding == nil {
		return "Binding()"
	}
	s := "Binding(" + n.Binding.String()
	if n.Default != nil {
		s += " = " + n.Default.String()
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n BindingElement) JS(w io.Writer) {
	if n.Binding == nil {
		return
	}

	n.Binding.JS(w)
	if n.Default != nil {
		w.Write([]byte(" = "))
		n.Default.JS(w)
	}
}

func (v *Var) bindingNode()          {}
func (n BindingArray) bindingNode()  {}
func (n BindingObject) bindingNode() {}

////////////////////////////////////////////////////////////////

// VarDecl is a variable statement or lexical declaration.
type VarDecl struct {
	TokenType
	List             []BindingElement
	Scope            *Scope
	InFor, InForInOf bool
}

func (n VarDecl) String() string {
	var s strings.Builder
	s.WriteString("Decl(" + n.TokenType.String())
	for _, item := range n.List {
		s.WriteString(" " + item.String())
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n VarDecl) JS(w io.Writer) {
	w.Write(n.TokenType.Bytes())
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(","))
		}
		w.Write([]byte(" "))
		item.JS(w)
	}
}

// Params is a list of parameters for functions, methods, and arrow function.
type Params struct {
	List []BindingElement
	Rest IBinding // can be nil
}

func (n Params) String() string {
	var s strings.Builder
	s.WriteString("Params(")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(", ")
		}
		s.WriteString(item.String())
	}
	if n.Rest != nil {
		if len(n.List) != 0 {
			s.WriteString(", ")
		}
		s.WriteString("...Binding(" + n.Rest.String() + ")")
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n Params) JS(w io.Writer) {
	w.Write([]byte("("))
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(", "))
		}
		item.JS(w)
	}
	if n.Rest != nil {
		if len(n.List) != 0 {
			w.Write([]byte(", "))
		}
		w.Write([]byte("..."))
		n.Rest.JS(w)
	}
	w.Write([]byte(")"))
}

// FuncDecl is an (async) (generator) function declaration or expression.
type FuncDecl struct {
	Async     bool
	Generator bool
	Name      *Var // can be nil
	Params    Params
	Body      BlockStmt
}

func (n FuncDecl) String() string {
	s := "Decl("
	if n.Async {
		s += "async function"
	} else {
		s += "function"
	}
	if n.Generator {
		s += "*"
	}
	if n.Name != nil {
		s += " " + string(n.Name.Data)
	}
	return s + " " + n.Params.String() + " " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n FuncDecl) JS(w io.Writer) {
	if n.Async {
		w.Write([]byte("async function"))
	} else {
		w.Write([]byte("function"))
	}

	if n.Generator {
		w.Write([]byte("*"))
	}
	if n.Name != nil {
		w.Write([]byte(" "))
		w.Write(n.Name.Data)
	}
	n.Params.JS(w)
	w.Write([]byte(" "))
	n.Body.JS(w)
}

// ClassElementName is either a private method/field or a property name for a method/field.
type ClassElementName struct {
	PropertyName
	Private *Var // can be nil
}

func (n ClassElementName) String() string {
	if n.Private != nil {
		return n.Private.String()
	} else {
		return n.PropertyName.String()
	}
}

// JS writes JavaScript to writer.
func (n ClassElementName) JS(w io.Writer) {
	if n.Private != nil {
		n.Private.JS(w)
	} else {
		n.PropertyName.JS(w)
	}
}

// MethodDecl is a method definition in a class declaration.
type MethodDecl struct {
	Static    bool
	Async     bool
	Generator bool
	Get       bool
	Set       bool
	Name      ClassElementName
	Params    Params
	Body      BlockStmt
}

func (n MethodDecl) String() string {
	s := ""
	if n.Static {
		s += " static"
	}
	if n.Async {
		s += " async"
	}
	if n.Generator {
		s += " *"
	}
	if n.Get {
		s += " get"
	}
	if n.Set {
		s += " set"
	}
	s += " " + n.Name.String() + " " + n.Params.String() + " " + n.Body.String()
	return "Method(" + s[1:] + ")"
}

// JS writes JavaScript to writer.
func (n MethodDecl) JS(w io.Writer) {
	writen := false
	if n.Static {
		w.Write([]byte("static"))
		writen = true
	}
	if n.Async {
		if writen {
			w.Write([]byte(" "))
		}
		w.Write([]byte("async"))
		writen = true
	}
	if n.Generator {
		if writen {
			w.Write([]byte(" "))
		}
		w.Write([]byte("*"))
		writen = true
	}
	if n.Get {
		if writen {
			w.Write([]byte(" "))
		}
		w.Write([]byte("get"))
		writen = true
	}
	if n.Set {
		if writen {
			w.Write([]byte(" "))
		}
		w.Write([]byte("set"))
		writen = true
	}
	if writen {
		w.Write([]byte(" "))
	}
	n.Name.JS(w)
	w.Write([]byte(" "))
	n.Params.JS(w)
	w.Write([]byte(" "))
	n.Body.JS(w)
}

// Field is a field definition in a class declaration.
type Field struct {
	Static bool
	Name   ClassElementName
	Init   IExpr // can be nil
}

func (n Field) String() string {
	s := "Field("
	if n.Static {
		s += "static "
	}
	s += n.Name.String()
	if n.Init != nil {
		s += " = " + n.Init.String()
	}
	return s + ")"
}

// JS writes JavaScript to writer.
func (n Field) JS(w io.Writer) {
	if n.Static {
		w.Write([]byte("static "))
	}
	n.Name.JS(w)
	if n.Init != nil {
		w.Write([]byte(" = "))
		n.Init.JS(w)
	}
}

// ClassElement is a class element that is either a static block, a field definition, or a class method
type ClassElement struct {
	StaticBlock *BlockStmt  // can be nil
	Method      *MethodDecl // can be nil
	Field
}

func (n ClassElement) String() string {
	if n.StaticBlock != nil {
		return "Static(" + n.StaticBlock.String() + ")"
	} else if n.Method != nil {
		return n.Method.String()
	}
	return n.Field.String()
}

// JS writes JavaScript to writer.
func (n ClassElement) JS(w io.Writer) {
	if n.StaticBlock != nil {
		w.Write([]byte("static "))
		n.StaticBlock.JS(w)
		return
	} else if n.Method != nil {
		n.Method.JS(w)
		return
	}
	n.Field.JS(w)
	w.Write([]byte(";"))
}

// ClassDecl is a class declaration.
type ClassDecl struct {
	Name    *Var  // can be nil
	Extends IExpr // can be nil
	List    []ClassElement
	Scope   // for private elements
}

func (n ClassDecl) String() string {
	var s strings.Builder
	s.WriteString("Decl(class")
	if n.Name != nil {
		s.WriteString(" " + string(n.Name.Data))
	}
	if n.Extends != nil {
		s.WriteString(" extends " + n.Extends.String())
	}
	for _, item := range n.List {
		s.WriteString(" " + item.String())
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n ClassDecl) JS(w io.Writer) {
	w.Write([]byte("class"))
	if n.Name != nil {
		w.Write([]byte(" "))
		w.Write(n.Name.Data)
	}
	if n.Extends != nil {
		w.Write([]byte(" extends "))
		n.Extends.JS(w)
	}
	if len(n.List) == 0 {
		w.Write([]byte(" {}"))
		return
	}
	w.Write([]byte(" {"))
	wi := parse.NewIndenter(w, 4)
	for _, item := range n.List {
		wi.Write([]byte("\n"))
		item.JS(wi)
	}
	w.Write([]byte("\n}"))
}

func (n VarDecl) stmtNode()   {}
func (n FuncDecl) stmtNode()  {}
func (n ClassDecl) stmtNode() {}

func (n VarDecl) exprNode()    {} // not a real IExpr, used for ForInit and ExportDecl
func (n FuncDecl) exprNode()   {}
func (n ClassDecl) exprNode()  {}
func (n MethodDecl) exprNode() {} // not a real IExpr, used for ObjectExpression PropertyName

////////////////////////////////////////////////////////////////

// LiteralExpr can be this, null, boolean, numeric, string, or regular expression literals.
type LiteralExpr struct {
	TokenType
	Data []byte
}

func (n LiteralExpr) String() string {
	return string(n.Data)
}

// JS writes JavaScript to writer.
func (n LiteralExpr) JS(w io.Writer) {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	w.Write(n.Data)
}

// JSON writes JSON to writer.
func (n LiteralExpr) JSON(w io.Writer) error {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	if n.TokenType == TrueToken || n.TokenType == FalseToken || n.TokenType == NullToken || n.TokenType == DecimalToken || n.TokenType == IntegerToken {
		w.Write(n.Data)
		return nil
	} else if n.TokenType == StringToken {
		data := n.Data
		if n.Data[0] == '\'' {
			data = parse.Copy(data)
			data = bytes.ReplaceAll(data, []byte(`\'`), []byte(`'`))
			data = bytes.ReplaceAll(data, []byte(`"`), []byte(`\"`))
			data[0] = '"'
			data[len(data)-1] = '"'
		}
		w.Write(data)
		return nil
	}
	js := &strings.Builder{}
	n.JS(js)
	return fmt.Errorf("%v: literal expression is not valid JSON: %v", ErrInvalidJSON, js.String())
}

// Element is an array literal element.
type Element struct {
	Value  IExpr // can be nil
	Spread bool
}

func (n Element) String() string {
	s := ""
	if n.Value != nil {
		if n.Spread {
			s += "..."
		}
		s += n.Value.String()
	}
	return s
}

// JS writes JavaScript to writer.
func (n Element) JS(w io.Writer) {
	if n.Value != nil {
		if n.Spread {
			w.Write([]byte("..."))
		}
		n.Value.JS(w)
	}
}

// ArrayExpr is an array literal.
type ArrayExpr struct {
	List []Element
}

func (n ArrayExpr) String() string {
	var s strings.Builder
	s.WriteString("[")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(", ")
		}
		if item.Value != nil {
			if item.Spread {
				s.WriteString("...")
			}
			s.WriteString(item.Value.String())
		}
	}
	if 0 < len(n.List) && n.List[len(n.List)-1].Value == nil {
		s.WriteString(",")
	}
	return s.String() + "]"
}

// JS writes JavaScript to writer.
func (n ArrayExpr) JS(w io.Writer) {
	w.Write([]byte("["))
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(", "))
		}
		if item.Value != nil {
			if item.Spread {
				w.Write([]byte("..."))
			}
			item.Value.JS(w)
		}
	}
	if 0 < len(n.List) && n.List[len(n.List)-1].Value == nil {
		w.Write([]byte(","))
	}
	w.Write([]byte("]"))
}

// JSON writes JSON to writer.
func (n ArrayExpr) JSON(w io.Writer) error {
	w.Write([]byte("["))
	for i, item := range n.List {
		if i != 0 {
			w.Write([]byte(", "))
		}
		if item.Value == nil || item.Spread {
			js := &strings.Builder{}
			n.JS(js)
			return fmt.Errorf("%v: array literal is not valid JSON: %v", ErrInvalidJSON, js.String())
		}
		if val, ok := item.Value.(JSONer); !ok {
			js := &strings.Builder{}
			item.Value.JS(js)
			return fmt.Errorf("%v: value is not valid JSON: %v", ErrInvalidJSON, js.String())
		} else if err := val.JSON(w); err != nil {
			return err
		}
	}
	w.Write([]byte("]"))
	return nil
}

// Property is a property definition in an object literal.
type Property struct {
	// either Name or Spread are set. When Spread is set then Value is AssignmentExpression
	// if Init is set then Value is IdentifierReference, otherwise it can also be MethodDefinition
	Name   *PropertyName // can be nil
	Spread bool
	Value  IExpr // Var, MethodDecl, AssignExpr, or IdentExpr
	Init   IExpr // can be nil
}

func (n Property) String() string {
	s := ""
	if n.Name != nil {
		if v, ok := n.Value.(*Var); !ok || !n.Name.IsIdent(v.Data) {
			s += n.Name.String() + ": "
		}
	} else if n.Spread {
		s += "..."
	}
	s += n.Value.String()
	if n.Init != nil {
		s += " = " + n.Init.String()
	}
	return s
}

// JS writes JavaScript to writer.
func (n Property) JS(w io.Writer) {
	if n.Name != nil {
		if v, ok := n.Value.(*Var); !ok || !n.Name.IsIdent(v.Data) {
			n.Name.JS(w)
			w.Write([]byte(": "))
		}
	} else if n.Spread {
		w.Write([]byte("..."))
	}
	n.Value.JS(w)
	if n.Init != nil {
		w.Write([]byte(" = "))
		n.Init.JS(w)
	}
}

// JSON writes JSON to writer.
func (n Property) JSON(w io.Writer) error {
	if n.Name == nil || n.Spread || n.Init != nil {
		js := &strings.Builder{}
		n.JS(js)
		return fmt.Errorf("%v: property is not valid JSON: %v", ErrInvalidJSON, js.String())
	} else if n.Name.Literal.TokenType == StringToken {
		_ = n.Name.Literal.JSON(w)
	} else if n.Name.Literal.TokenType == IdentifierToken || n.Name.Literal.TokenType == IntegerToken || n.Name.Literal.TokenType == DecimalToken {
		w.Write([]byte(`"`))
		w.Write(n.Name.Literal.Data)
		w.Write([]byte(`"`))
	} else {
		js := &strings.Builder{}
		n.JS(js)
		return fmt.Errorf("%v: property is not valid JSON: %v", ErrInvalidJSON, js.String())
	}
	w.Write([]byte(": "))

	if val, ok := n.Value.(JSONer); !ok {
		js := &strings.Builder{}
		n.Value.JS(js)
		return fmt.Errorf("%v: value is not valid JSON: %v", ErrInvalidJSON, js.String())
	} else if err := val.JSON(w); err != nil {
		return err
	}
	return nil
}

// ObjectExpr is an object literal.
type ObjectExpr struct {
	List []Property
}

func (n ObjectExpr) String() string {
	var s strings.Builder
	s.WriteString("{")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(", ")
		}
		s.WriteString(item.String())
	}
	return s.String() + "}"
}

// JS writes JavaScript to writer.
func (n ObjectExpr) JS(w io.Writer) {
	w.Write([]byte("{"))
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(", "))
		}
		item.JS(w)
	}
	w.Write([]byte("}"))
}

// JSON writes JSON to writer.
func (n ObjectExpr) JSON(w io.Writer) error {
	w.Write([]byte("{"))
	for i, item := range n.List {
		if i != 0 {
			w.Write([]byte(", "))
		}
		if err := item.JSON(w); err != nil {
			return err
		}
	}
	w.Write([]byte("}"))
	return nil
}

// TemplatePart is a template head or middle.
type TemplatePart struct {
	Value []byte
	Expr  IExpr
}

func (n TemplatePart) String() string {
	return string(n.Value) + n.Expr.String()
}

// JS writes JavaScript to writer.
func (n TemplatePart) JS(w io.Writer) {
	w.Write(n.Value)
	n.Expr.JS(w)
}

// TemplateExpr is a template literal or member/call expression, super property, or optional chain with template literal.
type TemplateExpr struct {
	Tag      IExpr // can be nil
	List     []TemplatePart
	Tail     []byte
	Prec     OpPrec
	Optional bool
}

func (n TemplateExpr) String() string {
	var s strings.Builder
	if n.Tag != nil {
		s.WriteString(n.Tag.String())
		if n.Optional {
			s.WriteString("?.")
		}
	}
	for _, item := range n.List {
		s.WriteString(item.String())
	}
	return s.String() + string(n.Tail)
}

// JS writes JavaScript to writer.
func (n TemplateExpr) JS(w io.Writer) {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	if n.Tag != nil {
		n.Tag.JS(w)
		if n.Optional {
			w.Write([]byte("?."))
		}
	}
	for _, item := range n.List {
		item.JS(w)
	}
	w.Write(n.Tail)
}

// JSON writes JSON to writer.
func (n TemplateExpr) JSON(w io.Writer) error {
	if wi, ok := w.(parse.Indenter); ok {
		w = wi.Writer
	}
	if n.Tag != nil || len(n.List) != 0 {
		js := &strings.Builder{}
		n.JS(js)
		return fmt.Errorf("%v: value is not valid JSON: %v", ErrInvalidJSON, js.String())
	}

	// allow template literal string to be converted to normal string (to allow for minified JS)
	data := parse.Copy(n.Tail)
	data = bytes.ReplaceAll(data, []byte("\n"), []byte("\\n"))
	data = bytes.ReplaceAll(data, []byte("\r"), []byte("\\r"))
	data = bytes.ReplaceAll(data, []byte("\\`"), []byte("`"))
	data = bytes.ReplaceAll(data, []byte("\\$"), []byte("$"))
	data = bytes.ReplaceAll(data, []byte(`"`), []byte(`\"`))
	data[0] = '"'
	data[len(data)-1] = '"'
	w.Write(data)
	return nil
}

// GroupExpr is a parenthesized expression.
type GroupExpr struct {
	X IExpr
}

func (n GroupExpr) String() string {
	return "(" + n.X.String() + ")"
}

// JS writes JavaScript to writer.
func (n GroupExpr) JS(w io.Writer) {
	w.Write([]byte("("))
	n.X.JS(w)
	w.Write([]byte(")"))
}

// IndexExpr is a member/call expression, super property, or optional chain with an index expression.
type IndexExpr struct {
	X        IExpr
	Y        IExpr
	Prec     OpPrec
	Optional bool
}

func (n IndexExpr) String() string {
	if n.Optional {
		return "(" + n.X.String() + "?.[" + n.Y.String() + "])"
	}
	return "(" + n.X.String() + "[" + n.Y.String() + "])"
}

// JS writes JavaScript to writer.
func (n IndexExpr) JS(w io.Writer) {
	n.X.JS(w)
	if n.Optional {
		w.Write([]byte("?.["))
	} else {
		w.Write([]byte("["))
	}
	n.Y.JS(w)
	w.Write([]byte("]"))
}

// DotExpr is a member/call expression, super property, or optional chain with a dot expression.
type DotExpr struct {
	X        IExpr
	Y        IExpr // LiteralExpr or Var
	Prec     OpPrec
	Optional bool
}

func (n DotExpr) String() string {
	if n.Optional {
		return "(" + n.X.String() + "?." + n.Y.String() + ")"
	}
	return "(" + n.X.String() + "." + n.Y.String() + ")"
}

// JS writes JavaScript to writer.
func (n DotExpr) JS(w io.Writer) {
	lit, ok := n.X.(*LiteralExpr)
	group := ok && !n.Optional && (lit.TokenType == DecimalToken || lit.TokenType == IntegerToken)
	if group {
		w.Write([]byte("("))
	}
	n.X.JS(w)
	if n.Optional {
		w.Write([]byte("?."))
	} else {
		if group {
			w.Write([]byte(")"))
		}
		w.Write([]byte("."))
	}
	n.Y.JS(w)
}

// NewTargetExpr is a new target meta property.
type NewTargetExpr struct{}

func (n NewTargetExpr) String() string {
	return "(new.target)"
}

// JS writes JavaScript to writer.
func (n NewTargetExpr) JS(w io.Writer) {
	w.Write([]byte("new.target"))
}

// ImportMetaExpr is a import meta meta property.
type ImportMetaExpr struct{}

func (n ImportMetaExpr) String() string {
	return "(import.meta)"
}

// JS writes JavaScript to writer.
func (n ImportMetaExpr) JS(w io.Writer) {
	w.Write([]byte("import.meta"))
}

type Arg struct {
	Value IExpr
	Rest  bool
}

func (n Arg) String() string {
	s := ""
	if n.Rest {
		s += "..."
	}
	return s + n.Value.String()
}

// JS writes JavaScript to writer.
func (n Arg) JS(w io.Writer) {
	if n.Rest {
		w.Write([]byte("..."))
	}
	n.Value.JS(w)
}

// Args is a list of arguments as used by new and call expressions.
type Args struct {
	List []Arg
}

func (n Args) String() string {
	var s strings.Builder
	s.WriteString("(")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(", ")
		}
		s.WriteString(item.String())
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n Args) JS(w io.Writer) {
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(", "))
		}
		item.JS(w)
	}
}

// NewExpr is a new expression or new member expression.
type NewExpr struct {
	X    IExpr
	Args *Args // can be nil
}

func (n NewExpr) String() string {
	if n.Args != nil {
		return "(new " + n.X.String() + n.Args.String() + ")"
	}
	return "(new " + n.X.String() + ")"
}

// JS writes JavaScript to writer.
func (n NewExpr) JS(w io.Writer) {
	w.Write([]byte("new "))
	n.X.JS(w)
	if n.Args != nil {
		w.Write([]byte("("))
		n.Args.JS(w)
		w.Write([]byte(")"))
	} else {
		w.Write([]byte("()"))
	}
}

// CallExpr is a call expression.
type CallExpr struct {
	X        IExpr
	Args     Args
	Prec     OpPrec
	Optional bool
}

func (n CallExpr) String() string {
	if n.Optional {
		return "(" + n.X.String() + "?." + n.Args.String() + ")"
	}
	return "(" + n.X.String() + n.Args.String() + ")"
}

// JS writes JavaScript to writer.
func (n CallExpr) JS(w io.Writer) {
	n.X.JS(w)
	if n.Optional {
		w.Write([]byte("?.("))
	} else {
		w.Write([]byte("("))
	}
	n.Args.JS(w)
	w.Write([]byte(")"))
}

// UnaryExpr is an update or unary expression.
type UnaryExpr struct {
	Op TokenType
	X  IExpr
}

func (n UnaryExpr) String() string {
	if n.Op == PostIncrToken || n.Op == PostDecrToken {
		return "(" + n.X.String() + n.Op.String() + ")"
	} else if IsIdentifierName(n.Op) {
		return "(" + n.Op.String() + " " + n.X.String() + ")"
	}
	return "(" + n.Op.String() + n.X.String() + ")"
}

// JS writes JavaScript to writer.
func (n UnaryExpr) JS(w io.Writer) {
	if n.Op == PostIncrToken || n.Op == PostDecrToken {
		n.X.JS(w)
		w.Write(n.Op.Bytes())
		return
	} else if unary, ok := n.X.(*UnaryExpr); ok && (n.Op == PosToken && (unary.Op == PreIncrToken || unary.Op == PosToken) || n.Op == NegToken && (unary.Op == PreDecrToken || unary.Op == NegToken)) || IsIdentifierName(n.Op) {
		w.Write(n.Op.Bytes())
		w.Write([]byte(" "))
		n.X.JS(w)
		return
	}
	w.Write(n.Op.Bytes())
	n.X.JS(w)
}

// JSON writes JSON to writer.
func (n UnaryExpr) JSON(w io.Writer) error {
	if lit, ok := n.X.(*LiteralExpr); ok && n.Op == NegToken && (lit.TokenType == DecimalToken || lit.TokenType == IntegerToken) {
		w.Write([]byte("-"))
		w.Write(lit.Data)
		return nil
	} else if n.Op == NotToken && lit.TokenType == IntegerToken && (lit.Data[0] == '0' || lit.Data[0] == '1') {
		if lit.Data[0] == '0' {
			w.Write([]byte("true"))
		} else {
			w.Write([]byte("false"))
		}
		return nil
	}
	js := &strings.Builder{}
	n.JS(js)
	return fmt.Errorf("%v: unary expression is not valid JSON: %v", ErrInvalidJSON, js.String())
}

// BinaryExpr is a binary expression.
type BinaryExpr struct {
	Op   TokenType
	X, Y IExpr
}

func (n BinaryExpr) String() string {
	if IsIdentifierName(n.Op) {
		return "(" + n.X.String() + " " + n.Op.String() + " " + n.Y.String() + ")"
	}
	return "(" + n.X.String() + n.Op.String() + n.Y.String() + ")"
}

// JS writes JavaScript to writer.
func (n BinaryExpr) JS(w io.Writer) {
	n.X.JS(w)
	w.Write([]byte(" "))
	w.Write(n.Op.Bytes())
	w.Write([]byte(" "))
	n.Y.JS(w)
}

// CondExpr is a conditional expression.
type CondExpr struct {
	Cond, X, Y IExpr
}

func (n CondExpr) String() string {
	return "(" + n.Cond.String() + " ? " + n.X.String() + " : " + n.Y.String() + ")"
}

// JS writes JavaScript to writer.
func (n CondExpr) JS(w io.Writer) {
	n.Cond.JS(w)
	w.Write([]byte(" ? "))
	n.X.JS(w)
	w.Write([]byte(" : "))
	n.Y.JS(w)
}

// YieldExpr is a yield expression.
type YieldExpr struct {
	Generator bool
	X         IExpr // can be nil
}

func (n YieldExpr) String() string {
	if n.X == nil {
		return "(yield)"
	}
	s := "(yield"
	if n.Generator {
		s += "*"
	}
	return s + " " + n.X.String() + ")"
}

// JS writes JavaScript to writer.
func (n YieldExpr) JS(w io.Writer) {
	w.Write([]byte("yield"))
	if n.X == nil {
		return
	}
	if n.Generator {
		w.Write([]byte("*"))
	}
	w.Write([]byte(" "))
	n.X.JS(w)
}

// ArrowFunc is an (async) arrow function.
type ArrowFunc struct {
	Async  bool
	Params Params
	Body   BlockStmt
}

func (n ArrowFunc) String() string {
	s := "("
	if n.Async {
		s += "async "
	}
	return s + n.Params.String() + " => " + n.Body.String() + ")"
}

// JS writes JavaScript to writer.
func (n ArrowFunc) JS(w io.Writer) {
	if n.Async {
		w.Write([]byte("async "))
	}
	n.Params.JS(w)
	w.Write([]byte(" => "))
	n.Body.JS(w)
}

// CommaExpr is a series of comma expressions.
type CommaExpr struct {
	List []IExpr
}

func (n CommaExpr) String() string {
	var s strings.Builder
	s.WriteString("(")
	for i, item := range n.List {
		if i != 0 {
			s.WriteString(",")
		}
		s.WriteString(item.String())
	}
	return s.String() + ")"
}

// JS writes JavaScript to writer.
func (n CommaExpr) JS(w io.Writer) {
	for j, item := range n.List {
		if j != 0 {
			w.Write([]byte(","))
		}
		item.JS(w)
	}
}

func (v *Var) exprNode()           {}
func (n LiteralExpr) exprNode()    {}
func (n ArrayExpr) exprNode()      {}
func (n ObjectExpr) exprNode()     {}
func (n TemplateExpr) exprNode()   {}
func (n GroupExpr) exprNode()      {}
func (n DotExpr) exprNode()        {}
func (n IndexExpr) exprNode()      {}
func (n NewTargetExpr) exprNode()  {}
func (n ImportMetaExpr) exprNode() {}
func (n NewExpr) exprNode()        {}
func (n CallExpr) exprNode()       {}
func (n UnaryExpr) exprNode()      {}
func (n BinaryExpr) exprNode()     {}
func (n CondExpr) exprNode()       {}
func (n YieldExpr) exprNode()      {}
func (n ArrowFunc) exprNode()      {}
func (n CommaExpr) exprNode()      {}
