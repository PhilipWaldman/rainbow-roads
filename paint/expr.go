package paint

import (
	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/ast"
	"github.com/antonmedv/expr/vm"
)

// operatorPairs is a map of operators, where each key-value pair are each other's dual.
var operatorPairs = map[string]string{
	"and": "or",
	"&&":  "||",
	"==":  "!=",
	">=":  "<",
	">":   "<=",
	"in":  "not in",
}

// init also adds the operators in opposite direction to operatorPairs.
func init() {
	for k, v := range operatorPairs {
		operatorPairs[v] = k
	}
}

// mustCompile compiles the input and returns its program. Panics if an error occurs while compiling.
func mustCompile(input string, ops ...expr.Option) *vm.Program {
	if program, err := expr.Compile(input, ops...); err != nil {
		panic(err)
	} else {
		return program
	}
}

// mustRun runs the program on the provided environment. Panics if an error occurs.
func mustRun(program *vm.Program, env any) any {
	if res, err := expr.Run(program, env); err != nil {
		panic(err)
	} else {
		return res
	}
}

// expandInArray represents a type with methods for expanding expressions involving arrays.
type expandInArray struct{}

// Enter is invoked when entering a node in the abstract syntax tree (AST).
// However, this method is empty and does not perform any actions upon entering a node.
func (*expandInArray) Enter(*ast.Node) {}

// Exit is invoked when exiting a node in the AST.
// This method expands expressions involving the "in" or "not in" operations.
//
// For example: "a not in ['b','c','d']" becomes "not (a=='b' or a=='c' or a=='d')"
func (*expandInArray) Exit(node *ast.Node) {
	// Interpret the node as a binary operation
	if bi := asBinaryIn(*node); bi != nil {
		// Check if the binary operation has an array on the right side
		if an, ok := bi.Right.(*ast.ArrayNode); ok {
			// If the array is empty, replace the original node with a boolean node
			if len(an.Nodes) == 0 {
				ast.Patch(node, &ast.BoolNode{})
			} else {
				// Iterate through array elements and construct equivalent expressions
				for i, n := range an.Nodes {
					// If it's the first element, replace the original node with an equality check
					if i == 0 {
						ast.Patch(node, &ast.BinaryNode{
							Operator: "==",
							Left:     bi.Left,
							Right:    n,
						})
					} else {
						// For subsequent elements, construct logical disjunctions between original node and equality checks
						ast.Patch(node, &ast.BinaryNode{
							Operator: "or",
							Left:     *node,
							Right: &ast.BinaryNode{
								Operator: "==",
								Left:     bi.Left,
								Right:    n,
							},
						})
					}
				}
			}
			// If the original operation was "not in", replace with a unary operation negating the result
			if bi.Operator == "not in" {
				ast.Patch(node, &ast.UnaryNode{
					Operator: "not",
					Node:     *node,
				})
			}
		}
	}
}

// expandInRange represents a type with methods for expanding range expressions.
type expandInRange struct{}

// Enter is invoked when entering a node in the abstract syntax tree (AST).
// This method is empty and does not perform any actions upon entering a node.
func (*expandInRange) Enter(*ast.Node) {}

// Exit is invoked when exiting a node in the AST.
// This method expands expressions involving range operations.
//
// For example: "a not in (2 .. 6)" becomes "not (a>=2 and a<=6)"
func (*expandInRange) Exit(node *ast.Node) {
	// Interpret the node as a binary operation
	if bi := asBinaryIn(*node); bi != nil {
		// Check if the right operand of the binary operation is another binary node with ".." operator
		if br, ok := bi.Right.(*ast.BinaryNode); ok && br.Operator == ".." {
			// If the range bounds are equal, replace the original node with an equality check of the bound
			if getValue(br.Left) == getValue(br.Right) {
				ast.Patch(node, &ast.BinaryNode{
					Operator: "==",
					Left:     bi.Left,
					Right:    br.Left,
				})
			} else {
				// Construct an "and" expression for ranges with distinct left and right bounds
				ast.Patch(node, &ast.BinaryNode{
					Operator: "and",
					Left: &ast.BinaryNode{
						Operator: ">=",
						Left:     bi.Left,
						Right:    br.Left,
					},
					Right: &ast.BinaryNode{
						Operator: "<=",
						Left:     bi.Left,
						Right:    br.Right,
					},
				})
			}

			// If the original operation was "not in", replace with a unary operation negating the result
			if bi.Operator == "not in" {
				ast.Patch(node, &ast.UnaryNode{
					Operator: "not",
					Node:     *node,
				})
			}
		}
	}
}

// getValue returns the value of the node.
func getValue(n ast.Node) any {
	switch a := n.(type) {
	case *ast.NilNode:
		return nil
	case *ast.IntegerNode:
		return a.Value
	case *ast.FloatNode:
		return a.Value
	case *ast.BoolNode:
		return a.Value
	case *ast.StringNode:
		return a.Value
	case *ast.ConstantNode:
		return a.Value
	default:
		return n
	}
}

// distributeAndFoldNot represents a type with methods for distributing and folding "not" unary operators.
type distributeAndFoldNot struct{}

// Enter is invoked when entering a node in the abstract syntax tree (AST).
// This method distributes and folds "not" unary operators within binary expressions.
func (d *distributeAndFoldNot) Enter(node *ast.Node) {
	// Check if the node is a unary "not" operation
	if un := asUnaryNot(*node); un != nil {
		// Check if the operand of the "not" operation is a binary node
		if bn, ok := un.Node.(*ast.BinaryNode); ok {
			// Check if there exists a valid operator to replace with
			if op, ok := operatorPairs[bn.Operator]; ok {
				switch bn.Operator {
				case "and", "&&", "or", "||":
					// Distribute the "not" operation to the left and right operands
					bn.Left = &ast.UnaryNode{
						Operator: un.Operator,
						Node:     bn.Left,
					}
					bn.Right = &ast.UnaryNode{
						Operator: un.Operator,
						Node:     bn.Right,
					}
				}
				// Replace the original binary operation with its dual
				bn.Operator = op
				ast.Patch(node, bn)
			}
		} else if n := asUnaryNot(un.Node); n != nil {
			// If it is a negation node, the original and this negation can be removed
			ast.Walk(&n.Node, d)
			ast.Patch(node, n.Node)
		} else if b, ok := un.Node.(*ast.BoolNode); ok {
			// If it is an boolean node, invert its value
			b.Value = !b.Value
			ast.Patch(node, b)
		}
	}
}

// Exit is invoked when exiting a node in the AST.
// This method is empty and does not perform any actions upon exiting a node.
func (*distributeAndFoldNot) Exit(*ast.Node) {}

func toDNF(node *ast.Node) {
	for limit := 1000; limit >= 0; limit-- {
		f := &dnf{}
		ast.Walk(node, f)
		if !f.applied {
			return
		}
	}
}

// dnf represents a type used for transforming logical expressions into Disjunctive Normal Form (DNF).
type dnf struct {
	depth   int  // depth represents the depth of the logical expression traversal
	applied bool // applied indicates whether a transformation has been applied
}

// Enter is invoked when entering a node in the abstract syntax tree (AST).
func (f *dnf) Enter(node *ast.Node) {
	// Increment the depth if the current node is not a binary node or if the operator is not "and" or "or"
	if f.depth > 0 {
		f.depth++
	} else if bn, ok := (*node).(*ast.BinaryNode); !ok || (bn.Operator != "and" && bn.Operator != "&&" && bn.Operator != "or" && bn.Operator != "||") {
		f.depth++
	}
}

// Exit is invoked when exiting a node in the AST.
func (f *dnf) Exit(node *ast.Node) {
	// Decrement the depth if the traversal depth is greater than 0
	if f.depth > 0 {
		f.depth--
		return
	}

	// Check if the node represents a binary AND operation
	if ba := asBinaryAnd(*node); ba != nil {
		// Check if the left operand of the AND operation is a binary OR operation
		if bo := asBinaryOr(ba.Left); bo != nil {
			// Transform the expression into DNF by distributing the OR operation over AND
			ast.Patch(node, &ast.BinaryNode{
				Operator: bo.Operator,
				Left: &ast.BinaryNode{
					Operator: ba.Operator,
					Left:     bo.Left,
					Right:    ba.Right,
				},
				Right: &ast.BinaryNode{
					Operator: ba.Operator,
					Left:     bo.Right,
					Right:    ba.Right,
				},
			})
			f.applied = true
			return
		}

		// Check if the right operand of the AND operation is a binary OR operation
		if bo := asBinaryOr(ba.Right); bo != nil {
			// Transform the expression into DNF by distributing the OR operation over AND
			ast.Patch(node, &ast.BinaryNode{
				Operator: bo.Operator,
				Left: &ast.BinaryNode{
					Operator: ba.Operator,
					Left:     ba.Left,
					Right:    bo.Left,
				},
				Right: &ast.BinaryNode{
					Operator: ba.Operator,
					Left:     ba.Left,
					Right:    bo.Right,
				},
			})
			f.applied = true
			return
		}
	}
}

// asBinaryIn checks if the given node represents a binary inclusion operation ('in' or 'not in').
// If so, it returns the binary node; otherwise, it returns nil.
func asBinaryIn(node ast.Node) *ast.BinaryNode {
	if bn, ok := node.(*ast.BinaryNode); ok && (bn.Operator == "in" || bn.Operator == "not in") {
		return bn
	}
	return nil
}

// asBinaryAnd checks if the given node represents a binary conjunction operation ('and' or '&&').
// If so, it returns the binary node; otherwise, it returns nil.
func asBinaryAnd(node ast.Node) *ast.BinaryNode {
	if bn, ok := node.(*ast.BinaryNode); ok && (bn.Operator == "and" || bn.Operator == "&&") {
		return bn
	}
	return nil
}

// asBinaryOr checks if the given node represents a binary disjunction operation ('or' or '||').
// If so, it returns the binary node; otherwise, it returns nil.
func asBinaryOr(node ast.Node) *ast.BinaryNode {
	if bn, ok := node.(*ast.BinaryNode); ok && (bn.Operator == "or" || bn.Operator == "||") {
		return bn
	}
	return nil
}

// asUnaryNot checks if the given node represents a unary negation operation ('not' or '!').
// If so, it returns the unary node; otherwise, it returns nil.
func asUnaryNot(node ast.Node) *ast.UnaryNode {
	if un, ok := node.(*ast.UnaryNode); ok && (un.Operator == "not" || un.Operator == "!") {
		return un
	}
	return nil
}
