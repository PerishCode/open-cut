package harnessguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectTreeRejectsResourceLinesStylesAndMisplacedTests(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "apps/web/src/style.css", "body {}\n")
	writeFixture(t, root, "apps/web/src/view.test.ts", "export {}\n")
	writeFixture(t, root, "apps/web/src/large.ts", strings.Repeat("line\n", MaxFileLines+1))
	resource := filepath.Join(root, "apps", "web", "public", "image.png")
	if err := os.MkdirAll(filepath.Dir(resource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resource, make([]byte, MaxResourceBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "apps/web/public/unknown.bin", strings.Repeat("x", MaxResourceBytes+1))
	violations := inspectTree(root)
	want := map[string]bool{"line-count": false, "resource-size": false, "style-boundary": false, "test-layout": false}
	for _, violation := range violations {
		if _, exists := want[violation.Rule]; exists {
			want[violation.Rule] = true
		}
	}
	for rule, found := range want {
		if !found {
			t.Fatalf("missing %s violation: %+v", rule, violations)
		}
	}
}

func TestInspectAPILayersRejectsDependencyEscape(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "apps/api/model/item.go", "package model\nimport _ \"github.com/PerishCode/open-cut/apps/api/repository\"\n")
	violations := inspectAPILayers(root)
	if len(violations) != 1 || violations[0].Rule != "api-layer" {
		t.Fatalf("violations=%+v", violations)
	}
}

func TestTypeScriptPolicyDetectsArchitectureEscapes(t *testing.T) {
	source := `
import { getHealth } from "@open-cut/openapi";
import "./forbidden.css";
const view = <div className="bad" />;
element.style.color = "red";
document.createElement("style");
fetch("/api");
new EventSource("/api/events");
const sheet = css` + "`body { color: red; }`" + `;
`
	violations := inspectWebSource("apps/web/src/components/example.tsx", lexTypeScript(source))
	want := map[string]bool{"atomic-components": false, "style-boundary": false, "web-contracts": false}
	for _, violation := range violations {
		if _, ok := want[violation.Rule]; ok {
			want[violation.Rule] = true
		}
	}
	for rule, found := range want {
		if !found {
			t.Fatalf("missing %s violation: %+v", rule, violations)
		}
	}
}

func TestTypeScriptLexerIgnoresForbiddenTextInCommentsStringsAndRegex(t *testing.T) {
	source := `
// <div className="ignored" />
const text = "element.style and import './ignored.css'";
const pattern = /\.style/;
const view = <Surface>ok</Surface>;
`
	if violations := inspectWebSource("apps/web/src/components/example.tsx", lexTypeScript(source)); len(violations) != 0 {
		t.Fatalf("violations=%+v", violations)
	}
}

func TestTypeScriptLexerInspectsTemplateExpressions(t *testing.T) {
	source := "const value = `safe ${element.style.color}`;\n"
	violations := inspectWebSource("apps/web/src/components/example.tsx", lexTypeScript(source))
	if len(violations) != 1 || violations[0].Rule != "style-boundary" {
		t.Fatalf("violations=%+v", violations)
	}
}

func TestAtomicPropsRejectStylingEscapeHatches(t *testing.T) {
	source := `
export interface SurfaceProps extends React.HTMLAttributes<HTMLElement> {
  className?: string;
  style: object;
}
`
	violations := inspectAtomicProps("packages/components/src/surface.tsx", lexTypeScript(source))
	if len(violations) != 3 {
		t.Fatalf("violations=%+v", violations)
	}
}

func writeFixture(t *testing.T, root, path, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
