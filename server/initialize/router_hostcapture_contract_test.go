package initialize

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRoutersRegistersHostCaptureMiddleware(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate contract test source")
	}

	routerFile := filepath.Join(filepath.Dir(testFile), "router.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), routerFile, nil, 0)
	if err != nil {
		t.Fatalf("parse router.go: %v", err)
	}

	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Routers" || function.Body == nil {
			continue
		}

		registered := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Use" {
				return true
			}
			for _, argument := range call.Args {
				middlewareCall, ok := argument.(*ast.CallExpr)
				if !ok {
					continue
				}
				middlewareSelector, ok := middlewareCall.Fun.(*ast.SelectorExpr)
				if !ok || middlewareSelector.Sel.Name != "Middleware" {
					continue
				}
				packageName, ok := middlewareSelector.X.(*ast.Ident)
				if ok && packageName.Name == "hostcapture" {
					registered = true
					return false
				}
			}
			return true
		})

		if registered {
			return
		}
	}

	t.Fatal("Routers must register hostcapture.Middleware()")
}
