package js

// IVisitor represents the AST Visitor
// Each INode encountered by `Walk` is passed to `Enter`, children nodes will be ignored if the returned IVisitor is nil
// `Exit` is called upon the exit of a node
type IVisitor interface {
	Enter(n INode) IVisitor
	Exit(n INode)
}

// Walk traverses an AST in depth-first order
func Walk(v IVisitor, n INode) {
	if n == nil {
		return
	}

	if v = v.Enter(n); v == nil {
		return
	}

	defer v.Exit(n)

	switch n := n.(type) {
	case *AST:
		Walk(v, &n.BlockStmt)
	case *Var:
		return
	case *BlockStmt:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, n.List[i])
			}
		}
	case *EmptyStmt:
		return
	case *ExprStmt:
		Walk(v, n.Value)
	case *IfStmt:
		Walk(v, n.Body)
		Walk(v, n.Else)
		Walk(v, n.Cond)
	case *DoWhileStmt:
		Walk(v, n.Body)
		Walk(v, n.Cond)
	case *WhileStmt:
		Walk(v, n.Body)
		Walk(v, n.Cond)
	case *ForStmt:
		if n.Body != nil {
			Walk(v, n.Body)
		}

		Walk(v, n.Init)
		Walk(v, n.Cond)
		Walk(v, n.Post)
	case *ForInStmt:
		if n.Body != nil {
			Walk(v, n.Body)
		}

		Walk(v, n.Init)
		Walk(v, n.Value)
	case *ForOfStmt:
		if n.Body != nil {
			Walk(v, n.Body)
		}

		Walk(v, n.Init)
		Walk(v, n.Value)
	case *CaseClause:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, n.List[i])
			}
		}

		Walk(v, n.Cond)
	case *SwitchStmt:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}

		Walk(v, n.Init)
	case *BranchStmt:
		return
	case *ReturnStmt:
		Walk(v, n.Value)
	case *WithStmt:
		Walk(v, n.Body)
		Walk(v, n.Cond)
	case *LabelledStmt:
		Walk(v, n.Value)
	case *ThrowStmt:
		Walk(v, n.Value)
	case *TryStmt:
		if n.Body != nil {
			Walk(v, n.Body)
		}

		if n.Catch != nil {
			Walk(v, n.Catch)
		}

		if n.Finally != nil {
			Walk(v, n.Finally)
		}

		Walk(v, n.Binding)
	case *DebuggerStmt:
		return
	case *Alias:
		return
	case *ImportStmt:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}
	case *ExportStmt:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}

		Walk(v, n.Decl)
	case *DirectivePrologueStmt:
		return
	case *PropertyName:
		Walk(v, &n.Literal)
		Walk(v, n.Computed)
	case *BindingArray:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}

		Walk(v, n.Rest)
	case *BindingObjectItem:
		if n.Key != nil {
			Walk(v, n.Key)
		}

		Walk(v, &n.Value)
	case *BindingObject:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}

		if n.Rest != nil {
			Walk(v, n.Rest)
		}
	case *BindingElement:
		Walk(v, n.Binding)
		Walk(v, n.Default)
	case *VarDecl:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}
	case *Params:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}

		Walk(v, n.Rest)
	case *FuncDecl:
		Walk(v, &n.Body)
		Walk(v, &n.Params)

		if n.Name != nil {
			Walk(v, n.Name)
		}
	case *MethodDecl:
		Walk(v, &n.Body)
		Walk(v, &n.Params)
		Walk(v, &n.Name)
	case *Field:
		Walk(v, &n.Name)
		Walk(v, n.Init)
	case *ClassDecl:
		if n.Name != nil {
			Walk(v, n.Name)
		}

		Walk(v, n.Extends)

		for _, item := range n.List {
			if item.StaticBlock != nil {
				Walk(v, item.StaticBlock)
			} else if item.Method != nil {
				Walk(v, item.Method)
			} else {
				Walk(v, &item.Field)
			}
		}
	case *LiteralExpr:
		return
	case *Element:
		Walk(v, n.Value)
	case *ArrayExpr:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}
	case *Property:
		if n.Name != nil {
			Walk(v, n.Name)
		}

		Walk(v, n.Value)
		Walk(v, n.Init)
	case *ObjectExpr:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}
	case *TemplatePart:
		Walk(v, n.Expr)
	case *TemplateExpr:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}

		Walk(v, n.Tag)
	case *GroupExpr:
		Walk(v, n.X)
	case *IndexExpr:
		Walk(v, n.X)
		Walk(v, n.Y)
	case *DotExpr:
		Walk(v, n.X)
		Walk(v, n.Y)
	case *NewTargetExpr:
		return
	case *ImportMetaExpr:
		return
	case *Arg:
		Walk(v, n.Value)
	case *Args:
		if n.List != nil {
			for i := 0; i < len(n.List); i++ {
				Walk(v, &n.List[i])
			}
		}
	case *NewExpr:
		if n.Args != nil {
			Walk(v, n.Args)
		}

		Walk(v, n.X)
	case *CallExpr:
		Walk(v, &n.Args)
		Walk(v, n.X)
	case *UnaryExpr:
		Walk(v, n.X)
	case *BinaryExpr:
		Walk(v, n.X)
		Walk(v, n.Y)
	case *CondExpr:
		Walk(v, n.Cond)
		Walk(v, n.X)
		Walk(v, n.Y)
	case *YieldExpr:
		Walk(v, n.X)
	case *ArrowFunc:
		Walk(v, &n.Body)
		Walk(v, &n.Params)
	case *CommaExpr:
		for _, item := range n.List {
			Walk(v, item)
		}
	default:
		return
	}
}
