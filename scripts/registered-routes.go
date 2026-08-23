//go:build ignore

// registered-routes parses actual net/http registration call expressions. It
// intentionally fails on dynamic API patterns so parity cannot silently miss a
// route that the JavaScript contract checker cannot resolve.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: registered-routes <backend-directory>")
	}
	var routes []string
	err := filepath.WalkDir(os.Args[1], func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") || len(call.Args) == 0 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				panic(fmt.Sprintf("dynamic HTTP route pattern at %s", path))
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil {
				panic(err)
			}
			fields := strings.Fields(pattern)
			if len(fields) == 2 && strings.HasPrefix(fields[1], "/api/v1/") {
				routes = append(routes, fields[0]+" "+fields[1])
			}
			return true
		})
		return nil
	})
	if err != nil {
		panic(err)
	}
	for _, route := range routes {
		fmt.Println(route)
	}
}
