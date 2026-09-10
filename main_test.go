package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMainSyncsTaskPluginsBeforeBackgroundLoop(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	require.NoError(t, err)

	var mainFunc *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" {
			mainFunc = fn
			break
		}
	}
	require.NotNil(t, mainFunc, "main function not found")

	seenInitialSync := false
	seenBackgroundLoop := false
	for _, stmt := range mainFunc.Body.List {
		switch node := stmt.(type) {
		case *ast.ExprStmt:
			call, ok := node.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			packageName, ok := selector.X.(*ast.Ident)
			if ok && packageName.Name == "controller" && selector.Sel.Name == "SyncTaskPluginsOnce" {
				seenInitialSync = true
			}
		case *ast.GoStmt:
			call := node.Call
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			packageName, ok := selector.X.(*ast.Ident)
			if ok && packageName.Name == "controller" && selector.Sel.Name == "SyncTaskPlugins" {
				seenBackgroundLoop = true
				require.True(t, seenInitialSync, "main must call controller.SyncTaskPluginsOnce before starting controller.SyncTaskPlugins")
			}
		}
	}

	require.True(t, seenInitialSync, "main must call controller.SyncTaskPluginsOnce")
	require.True(t, seenBackgroundLoop, "main must start controller.SyncTaskPlugins")
}
