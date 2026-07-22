package tests

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	parallelAPITestHelper = "parallelAPITest"
	serialAPITestHelper   = "serialAPITest"
)

// parallelAPITest declares that the test owns its mutable resources and may
// share a process with other API integration tests. Keep the declaration as the
// first statement so suite topology remains machine-checkable.
func parallelAPITest(t *testing.T) {
	t.Helper()
	t.Parallel()
}

// serialAPITest is the explicit escape hatch for a test that owns a shared or
// process-wide resource. The reason keeps serialized work visible during
// review instead of allowing new tests to join the critical path by accident.
func serialAPITest(t *testing.T, reason string) {
	t.Helper()
	if strings.TrimSpace(reason) == "" {
		t.Fatal("serial API test requires a reason")
	}
}

func TestAPITestSuiteTopology(t *testing.T) {
	parallelAPITest(t)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read API test directory: %v", err)
	}

	var violations []string
	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(files, entry.Name(), nil, 0)
		if parseErr != nil {
			violations = append(violations, fmt.Sprintf("%s: parse: %v", entry.Name(), parseErr))
			continue
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			if violation := validateAPITestScheduling(function); violation != "" {
				position := files.Position(function.Pos())
				violations = append(violations, fmt.Sprintf("%s:%d: %s", entry.Name(), position.Line, violation))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("API test suite topology violations:\n- %s", strings.Join(violations, "\n- "))
	}
}

func TestAPITestSchedulingValidation(t *testing.T) {
	parallelAPITest(t)

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "parallel", body: "parallelAPITest(t)"},
		{name: "serial", body: `serialAPITest(t, "shared process")`},
		{name: "missing", body: "t.Helper()", want: "app-owned"},
		{name: "empty serial reason", body: `serialAPITest(t, "")`, want: "must be non-empty"},
		{name: "dynamic serial reason", body: "serialAPITest(t, reason)", want: "must be a string literal"},
		{name: "unknown helper", body: "someOtherHelper(t)", want: "must declare"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf("package tests\nfunc TestFixture(t *testing.T) { %s }\n", test.body)
			parsed, err := parser.ParseFile(token.NewFileSet(), "fixture_test.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			function := parsed.Decls[0].(*ast.FuncDecl)
			got := validateAPITestScheduling(function)
			if test.want == "" && got != "" {
				t.Fatalf("unexpected violation: %s", got)
			}
			if test.want != "" && !strings.Contains(got, test.want) {
				t.Fatalf("violation %q does not contain %q", got, test.want)
			}
		})
	}
}

func validateAPITestScheduling(function *ast.FuncDecl) string {
	if function.Body == nil || len(function.Body.List) == 0 {
		return fmt.Sprintf("%s must declare parallelAPITest(t) or serialAPITest(t, reason) first", function.Name.Name)
	}
	expression, ok := function.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return fmt.Sprintf("%s must declare suite scheduling as its first statement", function.Name.Name)
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok {
		return fmt.Sprintf("%s must declare suite scheduling as its first statement", function.Name.Name)
	}
	helper, ok := call.Fun.(*ast.Ident)
	if !ok {
		return fmt.Sprintf("%s must use the app-owned suite scheduling helpers", function.Name.Name)
	}
	switch helper.Name {
	case parallelAPITestHelper:
		if len(call.Args) != 1 || !isIdentifier(call.Args[0], "t") {
			return fmt.Sprintf("%s must call parallelAPITest(t)", function.Name.Name)
		}
	case serialAPITestHelper:
		if len(call.Args) != 2 || !isIdentifier(call.Args[0], "t") {
			return fmt.Sprintf("%s must call serialAPITest(t, reason)", function.Name.Name)
		}
		reason, ok := call.Args[1].(*ast.BasicLit)
		if !ok || reason.Kind != token.STRING {
			return fmt.Sprintf("%s serial reason must be a string literal", function.Name.Name)
		}
		decoded, err := strconv.Unquote(reason.Value)
		if err != nil || strings.TrimSpace(decoded) == "" {
			return fmt.Sprintf("%s serial reason must be non-empty", function.Name.Name)
		}
	default:
		return fmt.Sprintf("%s must declare parallelAPITest(t) or serialAPITest(t, reason) first", function.Name.Name)
	}
	return ""
}

func isIdentifier(expression ast.Expr, name string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == name
}
