package paint

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/NathanBaulch/rainbow-roads/conv"
	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/antonmedv/expr/ast"
	"github.com/antonmedv/expr/parser"
	"golang.org/x/exp/slices"
)

// buildQuery constructs an Overpass query based on the provided region and filter criteria.
// It returns the constructed query string or any encountered error.
func buildQuery(region geo.Circle, filter string) (string, error) {
	// Build criteria based on the filter string
	if crits, err := buildCriteria(filter); err != nil {
		return "", fmt.Errorf("overpass query error: %w", err)
	} else {
		// Construct the query prefix with the specified region
		prefix := fmt.Sprintf("way(around:%s,%s,%s)",
			conv.FormatFloat(region.Radius),
			conv.FormatFloat(geo.RadiansToDegrees(region.Origin.Lat)),
			conv.FormatFloat(geo.RadiansToDegrees(region.Origin.Lon)),
		)

		// Build the parts of the query
		parts := make([]string, 0, len(crits)*3+2)
		parts = append(parts, "[out:json];(")
		for _, crit := range crits {
			parts = append(parts, prefix, crit, ";")
		}
		parts = append(parts, ");out tags geom qt;")

		// Join the parts to form the final query string
		return strings.Join(parts, ""), nil
	}
}

// buildCriteria parses the filter string and
// returns the constructed criteria as an array of strings and any encountered error.
func buildCriteria(filter string) ([]string, error) {
	// Parse the filter string into an abstract syntax tree (AST)
	tree, err := parser.Parse(filter)
	if err != nil {
		return nil, err
	}

	// Process the AST:
	// expand "in array"
	ast.Walk(&tree.Node, &expandInArray{})
	// expand "in range"
	ast.Walk(&tree.Node, &expandInRange{})
	// distribute and fold negations
	ast.Walk(&tree.Node, &distributeAndFoldNot{})
	// convert to DNF
	toDNF(&tree.Node)

	// Build query criteria from the AST using the query builder
	qb := queryBuilder{}
	ast.Walk(&tree.Node, &qb)
	if qb.err != nil {
		return nil, qb.err
	}

	// Ensure wrapped criteria are properly formatted
	for i, crit := range qb.stack {
		if !isWrapped(crit) {
			qb.stack[i] = fmt.Sprintf("(if:%s)", crit)
		}
	}

	return qb.stack, nil
}

// queryBuilder is a struct for building queries using an abstract syntax tree (AST).
type queryBuilder struct {
	stack []string // stack keeps track of query elements
	not   []bool   // not keeps track of whether elements are negated
	depth int      // depth keeps track of the depth of the AST
	err   error    // err stores any errors encountered during query building
}

// Enter is called when entering a node in the AST.
func (q *queryBuilder) Enter(node *ast.Node) {
	if q.depth > 0 {
		q.depth++
	} else if not := asUnaryNot(*node) != nil; !not && asBinaryAnd(*node) == nil && asBinaryOr(*node) == nil {
		q.depth++
	} else {
		q.not = append(q.not, not)
	}
}

// Exit is called when exiting a node in the AST.
func (q *queryBuilder) Exit(node *ast.Node) {
	// Return immediately if an error has occurred
	if q.err != nil {
		return
	}

	// Decrement depth if not at root level
	if q.depth > 0 {
		q.depth--
	}

	// Determine if at root level and if the expression is negated
	root := q.depth == 0
	not := false
	if root && len(q.not) > 0 {
		i := len(q.not) - 1
		not = q.not[i]
		q.not = q.not[:i]
	}

	// Handle negation for certain types
	if not {
		switch (*node).(type) {
		case *ast.IntegerNode, *ast.FloatNode, *ast.StringNode:
			q.err = fmt.Errorf("inverted %s not supported", nodeName(*node))
			return
		}
	}

	switch n := (*node).(type) {
	// Handle identifier node
	case *ast.IdentifierNode:
		// Extract identifier name
		name := n.Value
		// Check if identifier contains non-alphabetic characters and quote if necessary
		if slices.IndexFunc([]rune(n.Value), func(c rune) bool { return !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') }) >= 0 {
			name = strconv.Quote(n.Value)
		}
		// Push identifier to the query stack with appropriate negation handling
		if !root {
			q.push(bangIf(name, not))
		} else if not {
			q.push("[", name, `="no"]`)
		} else {
			q.push("[", name, `="yes"]`)
		}
	// Handle integer node
	case *ast.IntegerNode:
		q.push(n.Value)
	// Handle float node
	case *ast.FloatNode:
		q.push(n.Value)
	// Handle boolean node
	case *ast.BoolNode:
		// Push boolean value to the query stack with appropriate negation handling
		if n.Value != not {
			q.push(`"yes"`)
		} else {
			q.push(`"no"`)
		}
	// Handle string node
	case *ast.StringNode:
		// Push quoted string value to the query stack
		q.push(strconv.Quote(n.Value))
	// Handle unary node
	case *ast.UnaryNode:
		// Push unary operator with negation handling and its operand to the query stack
		if !root || (n.Operator != "not" && n.Operator != "!") {
			q.push(bangIf(n.Operator, not), "(", q.pop(), ")")
		}
	// Handle binary node
	case *ast.BinaryNode:
		// Pop right-hand side (rhs) and left-hand side (lhs) operands from the query stack
		rhs, lhs := q.pop(), q.pop()
		// Do something based on the binary operator
		switch n.Operator {
		// Handle logical AND operator
		case "and", "&&":
			// Check if root node or if both sides are not wrapped
			if !root || (!isWrapped(lhs) && !isWrapped(rhs)) {
				// Push AND operation with lhs and rhs to the query stack
				q.push(lhs, "&&", rhs)
			} else {
				// If lhs or rhs is not wrapped, wrap them with "if:" before pushing to the query stack
				if !isWrapped(lhs) {
					lhs = fmt.Sprintf("(if:%s)", lhs)
				}
				if !isWrapped(rhs) {
					rhs = fmt.Sprintf("(if:%s)", rhs)
				}
				q.push(lhs, rhs)
			}
		// Handle logical OR operator
		case "or", "||":
			// Check if root node or if both sides are not wrapped
			if !root || (!isWrapped(lhs) && !isWrapped(rhs)) {
				// Push OR operation with lhs and rhs to the query stack
				q.push(lhs, "||", rhs)
			} else {
				// Push lhs and rhs separately to the query stack
				q.push(lhs)
				q.push(rhs)
			}
		// Handle comparison operators
		case ">", ">=", "<", "<=":
			// If left-hand side is an identifier node, format lhs accordingly
			if _, ok := n.Left.(*ast.IdentifierNode); ok {
				if lhs[0] != '"' {
					lhs = strconv.Quote(lhs)
				}
				lhs = fmt.Sprintf("t[%s]", lhs)
			}
			// Push comparison operation with lhs, operator, and rhs to the query stack
			q.push(lhs, n.Operator, rhs)
		// Handle other operators
		default:
			op := n.Operator
			switch op {
			// Transform custom operators to standard regex operators
			case "contains":
				op = "~"
				if _, ok := n.Right.(*ast.StringNode); ok {
					rhs = regexp.QuoteMeta(rhs)
				}
			case "startsWith":
				op = "~"
				if _, ok := n.Right.(*ast.StringNode); ok {
					rhs = rhs[:1] + "^" + regexp.QuoteMeta(rhs[1:])
				}
			case "endsWith":
				op = "~"
				if _, ok := n.Right.(*ast.StringNode); ok {
					rhs = regexp.QuoteMeta(rhs[:len(rhs)-1]) + "$" + rhs[len(rhs)-1:]
				}
			}
			// Check if lhs or rhs contains function nodes
			_, okl := n.Left.(*ast.FunctionNode)
			_, okr := n.Right.(*ast.FunctionNode)
			// If either side contains function nodes, it's not a root node
			if okl || okr {
				root = false
			}
			// If it's a root node, construct appropriate query expression
			if root {
				// Handle special cases for equality and inequality operators
				if op == "==" || op == "!=" {
					if _, ok := n.Right.(*ast.IdentifierNode); ok {
						lhs, rhs = rhs, lhs
					}
					// Handle negation for inequality operators
					if op == "!=" {
						not = !not
					}
					// Transform equality comparison with empty string to regex operator
					if rhs == `""` {
						op = "~"
						rhs = `"^$"`
					} else {
						op = "="
					}
				}
				// Push constructed query expression to the query stack
				q.push("[", lhs, bangIf(op, not), rhs, "]")
			} else {
				// If not a root node, push lhs, operator, and rhs to the query stack
				q.push(lhs, bangIf(op, not), rhs)
			}
		}
	// Handle matches node
	case *ast.MatchesNode:
		// Pop right-hand side (rhs) and left-hand side (lhs) operands from the query stack
		rhs, lhs := q.pop(), q.pop()
		// Construct the query expression based on whether it's a root node
		if root {
			// If root node, construct query expression with appropriate operator and operands
			q.push("[", lhs, bangIf("~", not), rhs, "]")
		} else {
			// If not a root node, push lhs, operator, and rhs to the query stack
			q.push(lhs, bangIf("~", not), rhs)
		}
	// Handle function node
	case *ast.FunctionNode:
		if root && n.Name == "is_tag" && len(n.Arguments) == 1 {
			// If root node and the function is "is_tag" with one argument, construct query expression with negation
			q.push("[", bangIf(q.pop(), not), "]")
		} else {
			// If not a root node or different function, construct the query expression
			parts := make([]any, 0, len(n.Arguments)+3)
			parts = append(parts, bangIf(n.Name, not), "(")
			for range n.Arguments {
				// Pop arguments from the query stack and append to the parts
				parts = append(parts, q.pop())
			}
			parts = append(parts, ")")
			// Push the constructed parts to the query stack
			q.push(parts...)
		}
	// Handle conditional node
	case *ast.ConditionalNode:
		// Pop e1 and e2 from the query stack
		e2, e1 := q.pop(), q.pop()
		// Push the conditional expression to the query stack in ternairy operator format: e ? e1 : e2
		q.push(q.pop(), "?", e1, ":", e2)
	// Handle other cases
	default:
		// If the node type is not supported, set an error indicating the unsupported node type
		q.err = fmt.Errorf("%s not supported", nodeName(n))
	}
}

// nodeName returns the lowercased name of the type of the given AST node, removing the "Node" suffix.
func nodeName(n ast.Node) string {
	name := reflect.TypeOf(n).Elem().Name()
	return strings.ToLower(name[:len(name)-4])
}

// isWrapped checks if the given string is wrapped within square brackets or starts with "(if:".
func isWrapped(str string) bool {
	return str[0] == '[' || strings.HasPrefix(str, "(if:")
}

// bangIf returns a string prepended with "!" if the not flag is true, otherwise returns the original string.
func bangIf(str string, not bool) string {
	if not {
		return "!" + str
	}
	return str
}

// push adds elements to the query stack.
func (q *queryBuilder) push(a ...any) {
	q.stack = append(q.stack, fmt.Sprint(a...))
}

// pop removes and returns the top element from the query stack.
func (q *queryBuilder) pop() string {
	i := len(q.stack) - 1
	str := q.stack[i]
	q.stack = q.stack[:i]
	return str
}
