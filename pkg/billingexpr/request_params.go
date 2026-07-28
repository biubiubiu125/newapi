package billingexpr

import (
	"fmt"
	"sort"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/tidwall/gjson"
)

type requestParamVisitor struct {
	callee  string
	values  map[string]struct{}
	invalid bool
}

func (v *requestParamVisitor) Visit(node *ast.Node) {
	call, ok := (*node).(*ast.CallNode)
	if !ok {
		return
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || callee.Value != v.callee {
		return
	}
	if len(call.Arguments) != 1 {
		v.invalid = true
		return
	}
	argument, ok := call.Arguments[0].(*ast.StringNode)
	if !ok {
		v.invalid = true
		return
	}
	value := strings.TrimSpace(argument.Value)
	if v.callee == "header" {
		value = strings.ToLower(value)
	}
	if value != "" {
		v.values[value] = struct{}{}
	}
}

func referencedRequestValues(expression string, callee string) ([]string, error) {
	tree, err := parser.Parse(expression)
	if err != nil {
		return nil, err
	}
	visitor := &requestParamVisitor{callee: callee, values: make(map[string]struct{})}
	ast.Walk(&tree.Node, visitor)
	if visitor.invalid {
		return nil, fmt.Errorf("%s() requires exactly one string literal argument", callee)
	}
	values := make([]string, 0, len(visitor.values))
	for value := range visitor.values {
		values = append(values, value)
	}
	sort.Strings(values)
	return values, nil
}

func ReferencedRequestParams(expression string) ([]string, error) {
	return referencedRequestValues(expression, "param")
}

func ReferencedRequestHeaders(expression string) ([]string, error) {
	return referencedRequestValues(expression, "header")
}

func CaptureRequestParams(expression string, body []byte) (map[string]any, error) {
	paths, err := ReferencedRequestParams(expression)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	params := make(map[string]any, len(paths))
	for _, path := range paths {
		result := gjson.GetBytes(body, path)
		if !result.Exists() {
			params[path] = nil
			continue
		}
		params[path] = result.Value()
	}
	return params, nil
}

func CaptureRequestHeaders(expression string, headers map[string]string) (map[string]string, error) {
	names, err := ReferencedRequestHeaders(expression)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 || len(headers) == 0 {
		return nil, nil
	}
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			normalized[key] = value
		}
	}
	captured := make(map[string]string, len(names))
	for _, name := range names {
		if value := normalized[name]; value != "" {
			captured[name] = value
		}
	}
	if len(captured) == 0 {
		return nil, nil
	}
	return captured, nil
}

func CloneRequestInput(src RequestInput) RequestInput {
	dst := RequestInput{}
	if len(src.Headers) > 0 {
		dst.Headers = make(map[string]string, len(src.Headers))
		for key, value := range src.Headers {
			dst.Headers[key] = value
		}
	}
	if len(src.Body) > 0 {
		dst.Body = append([]byte(nil), src.Body...)
	}
	if len(src.Params) > 0 {
		dst.Params = cloneRequestParams(src.Params)
	}
	return dst
}

func cloneRequestParams(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = cloneRequestParamValue(value)
	}
	return dst
}

func cloneRequestParamValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneRequestParams(typed)
	case []any:
		dst := make([]any, len(typed))
		for index, item := range typed {
			dst[index] = cloneRequestParamValue(item)
		}
		return dst
	default:
		return value
	}
}
