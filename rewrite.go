// Package pb2zap rewrites protobuf message use to zap-proto wire calls.
//
// It is syntactic — no type-checker — so it runs on a tree that does not yet
// compile (the usual mid-migration reality). It auto-rewrites the one
// transform that is unambiguous from syntax alone — message construction —
// and leaves the rest (field reads, which need accessors; field writes,
// which immutable ZAP cannot express) for a human, because guessing those
// without types is how you get slop.
package pb2zap

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// Rule maps one protobuf import to its zap-proto wire package.
//
//	{PB: ".../pb/mount_pb", Wire: ".../wire/mount", Name: "mountwire"}
type Rule struct {
	PB   string // protobuf import path
	Wire string // wire import path
	Name string // wire package name to use in rewritten code
}

// Rewrite applies rules to one Go source file. For every mapped protobuf
// package it turns construction —
//
//	&pb.Msg{F: v}   and   pb.Msg{F: v}
//
// into
//
//	wire.NewMsg(wire.MsgInput{F: v})
//
// adds the wire import, and drops the pb import once nothing else references
// it. It returns the formatted result, whether anything changed, and the
// count of pb selectors it left untouched (the field reads/calls a human
// still owns).
func Rewrite(filename string, src []byte, rules []Rule) (out []byte, changed bool, pbLeft int, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if perr != nil {
		return nil, false, 0, perr
	}

	byLocal := map[string]Rule{} // pb package's local name in this file -> rule
	for _, r := range rules {
		if name := importLocalName(f, r.PB); name != "" {
			byLocal[name] = r
		}
	}
	if len(byLocal) == 0 {
		return src, false, 0, nil
	}
	usedWire := map[string]bool{}

	toCall := func(cl *ast.CompositeLit) ast.Expr {
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return nil
		}
		r, ok := byLocal[pkg.Name]
		if !ok {
			return nil
		}
		usedWire[r.Wire] = true
		return &ast.CallExpr{
			Fun: &ast.SelectorExpr{X: ast.NewIdent(r.Name), Sel: ast.NewIdent("New" + sel.Sel.Name)},
			Args: []ast.Expr{&ast.CompositeLit{
				Type: &ast.SelectorExpr{X: ast.NewIdent(r.Name), Sel: ast.NewIdent(sel.Sel.Name + "Input")},
				Elts: cl.Elts,
			}},
		}
	}

	astutil.Apply(f, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				if cl, ok := n.X.(*ast.CompositeLit); ok {
					if call := toCall(cl); call != nil {
						c.Replace(call)
						changed = true
						return false
					}
				}
			}
		case *ast.CompositeLit:
			if call := toCall(n); call != nil {
				c.Replace(call)
				changed = true
				return false
			}
		}
		return true
	}, nil)

	for _, r := range rules {
		if usedWire[r.Wire] {
			astutil.AddNamedImport(fset, f, r.Name, r.Wire)
		}
	}
	for local, r := range byLocal {
		if !selectorUsed(f, local) {
			deletePBImport(fset, f, r.PB)
		}
	}

	for local := range byLocal {
		pbLeft += selectorCount(f, local)
	}

	var buf bytes.Buffer
	if ferr := format.Node(&buf, fset, f); ferr != nil {
		return nil, false, 0, ferr
	}
	return buf.Bytes(), changed, pbLeft, nil
}

// importLocalName returns the name path is bound to in f (its alias, or the
// last path segment when unaliased), or "" if f does not import it.
func importLocalName(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		seg := path[strings.LastIndex(path, "/")+1:]
		return seg
	}
	return ""
}

// deletePBImport removes the import of path from f, handling both the
// aliased (`mount_pb "..."`) and unaliased forms — astutil.DeleteImport only
// matches the unnamed form.
func deletePBImport(fset *token.FileSet, f *ast.File, path string) {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != path {
			continue
		}
		if imp.Name != nil {
			astutil.DeleteNamedImport(fset, f, imp.Name.Name, path)
		} else {
			astutil.DeleteImport(fset, f, path)
		}
		return
	}
}

// selectorUsed reports whether any `name.X` selector remains in f.
func selectorUsed(f *ast.File, name string) bool { return selectorCount(f, name) > 0 }

func selectorCount(f *ast.File, name string) (n int) {
	ast.Inspect(f, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == name {
				n++
			}
		}
		return true
	})
	return n
}
