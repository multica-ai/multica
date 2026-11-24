package js

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/test"
)

type walker struct{}

func (w *walker) Enter(n INode) IVisitor {
	switch n := n.(type) {
	case *Var:
		if bytes.Equal(n.Data, []byte("x")) {
			n.Data = []byte("obj")
		}
	}

	return w
}

func (w *walker) Exit(n INode) {}

func TestWalk(t *testing.T) {
	js := `
	if (true) {
		for (i = 0; i < 1; i++) {
			x.y = i
		}
	}`

	ast, err := Parse(parse.NewInputString(js), Options{})
	if err != nil {
		t.Fatal(err)
	}

	Walk(&walker{}, ast)

	re := regexp.MustCompile("\n *")
	t.Run("TestWalk", func(t *testing.T) {
		src := ast.JSString()
		src = re.ReplaceAllString(src, " ")
		test.String(t, src, "if (true) { for (i = 0; i < 1; i++) { obj.y = i; } }")
	})
}

func TestWalkNilNode(t *testing.T) {
	nodes := []INode{
		&AST{},
		&Var{},
		&BlockStmt{},
		&EmptyStmt{},
		&ExprStmt{},
		&IfStmt{},
		&DoWhileStmt{},
		&WhileStmt{},
		&ForStmt{},
		&ForInStmt{},
		&ForOfStmt{},
		&CaseClause{},
		&SwitchStmt{},
		&BranchStmt{},
		&ReturnStmt{},
		&WithStmt{},
		&LabelledStmt{},
		&ThrowStmt{},
		&TryStmt{},
		&DebuggerStmt{},
		&Alias{},
		&ImportStmt{},
		&ExportStmt{},
		&DirectivePrologueStmt{},
		&PropertyName{},
		&BindingArray{},
		&BindingObjectItem{},
		&BindingObject{},
		&BindingElement{},
		&VarDecl{},
		&Params{},
		&FuncDecl{},
		&MethodDecl{},
		&Field{},
		&ClassElement{},
		&ClassDecl{},
		&LiteralExpr{},
		&Element{},
		&ArrayExpr{},
		&Property{},
		&ObjectExpr{},
		&TemplatePart{},
		&TemplateExpr{},
		&GroupExpr{},
		&IndexExpr{},
		&DotExpr{},
		&NewTargetExpr{},
		&ImportMetaExpr{},
		&Arg{},
		&Args{},
		&NewExpr{},
		&CallExpr{},
		&UnaryExpr{},
		&BinaryExpr{},
		&CondExpr{},
		&YieldExpr{},
		&ArrowFunc{},
	}

	t.Run("TestWalkNilNode", func(t *testing.T) {
		for _, n := range nodes {
			Walk(&walker{}, n)
		}
	})
}
