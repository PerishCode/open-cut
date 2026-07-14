package harnessguard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type tokenKind uint8

const (
	identifierToken tokenKind = iota
	stringToken
	punctuationToken
	templateToken
)

type sourceToken struct {
	kind tokenKind
	text string
}

var styleImportPattern = regexp.MustCompile(`(?i)\.(css|less|pcss|sass|scss|sss|styl|stylus)(\?|$)`)

func inspectTypeScript(root string) []Violation {
	var violations []Violation
	for _, scope := range []struct {
		root       string
		components bool
	}{
		{root: filepath.Join(root, "apps", "web")},
		{root: filepath.Join(root, "packages", "components", "src"), components: true},
	} {
		files, err := typescriptFiles(scope.root)
		if err != nil {
			violations = append(violations, Violation{Rule: "typescript-policy", Path: slashRelative(root, scope.root), Detail: err.Error()})
			continue
		}
		for _, filename := range files {
			data, err := os.ReadFile(filename)
			if err != nil {
				violations = append(violations, violation(root, filename, "typescript-policy", err.Error()))
				continue
			}
			path := slashRelative(root, filename)
			tokens := lexTypeScript(string(data))
			if scope.components {
				violations = append(violations, inspectAtomicProps(path, tokens)...)
			} else {
				violations = append(violations, inspectWebSource(path, tokens)...)
			}
		}
	}
	return uniqueViolations(violations)
}

func inspectWebSource(path string, tokens []sourceToken) []Violation {
	var violations []Violation
	library := strings.HasPrefix(path, "apps/web/src/lib/")
	business := strings.HasPrefix(path, "apps/web/src/components/") || strings.HasPrefix(path, "apps/web/src/views/")
	if library && (filepath.Ext(path) == ".tsx" || filepath.Ext(path) == ".jsx") {
		violations = append(violations, Violation{Rule: "web-layout", Path: path, Detail: "apps/web/src/lib must use .ts and contain no JSX"})
	}
	for index, token := range tokens {
		if token.kind == identifierToken && (token.text == "className" || token.text == "style") {
			violations = append(violations, Violation{Rule: "style-boundary", Path: path, Detail: "Web source may not use styling identifier " + token.text})
		}
		if token.kind == identifierToken && token.text == "import" {
			if specifier := importedSpecifier(tokens, index); specifier != "" {
				if styleImportPattern.MatchString(specifier) {
					violations = append(violations, Violation{Rule: "style-boundary", Path: path, Detail: "Web source imports style file " + specifier})
				}
				if strings.HasPrefix(specifier, "styled-components") || strings.HasPrefix(specifier, "@emotion/") || strings.HasPrefix(specifier, "tailwindcss") {
					violations = append(violations, Violation{Rule: "style-boundary", Path: path, Detail: "Web source imports styling runtime " + specifier})
				}
				if specifier == "@open-cut/openapi" && !library {
					violations = append(violations, Violation{Rule: "web-api-boundary", Path: path, Detail: "Only apps/web/src/lib may import @open-cut/openapi"})
				}
			}
		}
		if token.kind == identifierToken && (token.text == "css" || token.text == "styled") && taggedTemplateAt(tokens, index) {
			violations = append(violations, Violation{Rule: "style-boundary", Path: path, Detail: "Web source contains a CSS-in-JS tagged template"})
		}
		if token.kind == identifierToken && (token.text == "createElement" || token.text == "setAttribute") && callUsesStyle(tokens, index) {
			violations = append(violations, Violation{Rule: "style-boundary", Path: path, Detail: fmt.Sprintf("Web source may not call %s(\"style\")", token.text)})
		}
		if token.text != "<" || index+1 >= len(tokens) || tokens[index+1].text == "/" {
			continue
		}
		if tokens[index+1].text == ">" {
			continue
		}
		if tokens[index+1].kind != identifierToken {
			continue
		}
		tag := tokens[index+1].text
		if business && startsLowercase(tag) {
			violations = append(violations, Violation{Rule: "atomic-components", Path: path, Detail: fmt.Sprintf("Business JSX may not render intrinsic element <%s>", tag)})
		}
	}
	return violations
}

func inspectAtomicProps(path string, tokens []sourceToken) []Violation {
	var violations []Violation
	for index, token := range tokens {
		if token.kind == identifierToken && (token.text == "className" || token.text == "style") {
			cursor := index + 1
			if cursor < len(tokens) && tokens[cursor].text == "?" {
				cursor++
			}
			if cursor < len(tokens) && tokens[cursor].text == ":" {
				violations = append(violations, Violation{Rule: "atomic-props", Path: path, Detail: "Atomic component public props may not expose " + token.text})
			}
		}
		if token.kind == identifierToken && (token.text == "HTMLAttributes" || token.text == "ComponentProps") {
			violations = append(violations, Violation{Rule: "atomic-props", Path: path, Detail: "Atomic component props may not inherit raw DOM styling props"})
		}
		if token.kind == identifierToken && token.text == "JSX" && index+2 < len(tokens) && tokens[index+1].text == "." && tokens[index+2].text == "IntrinsicElements" {
			violations = append(violations, Violation{Rule: "atomic-props", Path: path, Detail: "Atomic component props may not inherit raw DOM styling props"})
		}
	}
	return violations
}

func importedSpecifier(tokens []sourceToken, start int) string {
	for index := start + 1; index < len(tokens) && index <= start+32; index++ {
		if tokens[index].kind == stringToken {
			return tokens[index].text
		}
		if tokens[index].text == ";" {
			return ""
		}
	}
	return ""
}

func taggedTemplateAt(tokens []sourceToken, index int) bool {
	if index+1 < len(tokens) && tokens[index+1].kind == templateToken {
		return true
	}
	return tokens[index].text == "styled" && index+3 < len(tokens) && tokens[index+1].text == "." &&
		tokens[index+2].kind == identifierToken && tokens[index+3].kind == templateToken
}

func callUsesStyle(tokens []sourceToken, index int) bool {
	return index+2 < len(tokens) && tokens[index+1].text == "(" && tokens[index+2].kind == stringToken && tokens[index+2].text == "style"
}

func startsLowercase(value string) bool {
	return value != "" && value[0] >= 'a' && value[0] <= 'z'
}

func typescriptFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if filename != root && (entry.Name() == "dist" || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(entry.Name())
		switch extension {
		case ".cjs", ".cts", ".js", ".jsx", ".mjs", ".mts", ".ts", ".tsx":
			files = append(files, filename)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func uniqueViolations(input []Violation) []Violation {
	seen := make(map[string]bool, len(input))
	result := make([]Violation, 0, len(input))
	for _, violation := range input {
		key := violation.Rule + "\x00" + violation.Path + "\x00" + violation.Detail
		if !seen[key] {
			seen[key] = true
			result = append(result, violation)
		}
	}
	return result
}

func lexTypeScript(source string) []sourceToken {
	tokens := make([]sourceToken, 0, len(source)/5)
	for index := 0; index < len(source); {
		character := source[index]
		if isSpace(character) {
			index++
			continue
		}
		if character == '/' && index+1 < len(source) && source[index+1] == '/' {
			index = skipLineComment(source, index+2)
			continue
		}
		if character == '/' && index+1 < len(source) && source[index+1] == '*' {
			index = skipBlockComment(source, index+2)
			continue
		}
		if character == '\'' || character == '"' {
			value, next := scanQuoted(source, index, character)
			tokens = append(tokens, sourceToken{kind: stringToken, text: value})
			index = next
			continue
		}
		if character == '`' {
			tokens = append(tokens, sourceToken{kind: templateToken, text: "`"})
			embedded, next := scanTemplate(source, index+1)
			tokens = append(tokens, embedded...)
			index = next
			continue
		}
		if isIdentifierStart(character) {
			end := index + 1
			for end < len(source) && isIdentifierPart(source[end]) {
				end++
			}
			tokens = append(tokens, sourceToken{kind: identifierToken, text: source[index:end]})
			index = end
			continue
		}
		if character == '/' && beginsRegularExpression(tokens) {
			index = skipRegularExpression(source, index+1)
			continue
		}
		tokens = append(tokens, sourceToken{kind: punctuationToken, text: string(character)})
		index++
	}
	return tokens
}

func scanQuoted(source string, start int, quote byte) (string, int) {
	var value strings.Builder
	for index := start + 1; index < len(source); index++ {
		if source[index] == '\\' && index+1 < len(source) {
			index++
			value.WriteByte(source[index])
			continue
		}
		if source[index] == quote {
			return value.String(), index + 1
		}
		value.WriteByte(source[index])
	}
	return value.String(), len(source)
}

func scanTemplate(source string, index int) ([]sourceToken, int) {
	var embedded []sourceToken
	for index < len(source) {
		if source[index] == '\\' && index+1 < len(source) {
			index += 2
			continue
		}
		if source[index] == '`' {
			return embedded, index + 1
		}
		if source[index] == '$' && index+1 < len(source) && source[index+1] == '{' {
			start := index + 2
			end := templateExpressionEnd(source, start)
			embedded = append(embedded, lexTypeScript(source[start:end])...)
			if end >= len(source) {
				return embedded, end
			}
			index = end + 1
			continue
		}
		index++
	}
	return embedded, index
}

func templateExpressionEnd(source string, index int) int {
	depth := 1
	var tokens []sourceToken
	for index < len(source) {
		character := source[index]
		if isSpace(character) {
			index++
			continue
		}
		if character == '/' && index+1 < len(source) && source[index+1] == '/' {
			index = skipLineComment(source, index+2)
			continue
		}
		if character == '/' && index+1 < len(source) && source[index+1] == '*' {
			index = skipBlockComment(source, index+2)
			continue
		}
		if character == '\'' || character == '"' {
			value, next := scanQuoted(source, index, character)
			tokens = append(tokens, sourceToken{kind: stringToken, text: value})
			index = next
			continue
		}
		if character == '`' {
			_, next := scanTemplate(source, index+1)
			tokens = append(tokens, sourceToken{kind: templateToken, text: "`"})
			index = next
			continue
		}
		if isIdentifierStart(character) {
			end := index + 1
			for end < len(source) && isIdentifierPart(source[end]) {
				end++
			}
			tokens = append(tokens, sourceToken{kind: identifierToken, text: source[index:end]})
			index = end
			continue
		}
		if character == '/' && beginsRegularExpression(tokens) {
			index = skipRegularExpression(source, index+1)
			continue
		}
		if character == '{' {
			depth++
		} else if character == '}' {
			depth--
			if depth == 0 {
				return index
			}
		}
		tokens = append(tokens, sourceToken{kind: punctuationToken, text: string(character)})
		index++
	}
	return index
}

func skipRegularExpression(source string, index int) int {
	inClass := false
	for index < len(source) {
		switch source[index] {
		case '\\':
			index += 2
			continue
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				index++
				for index < len(source) && isIdentifierPart(source[index]) {
					index++
				}
				return index
			}
		case '\n', '\r':
			return index
		}
		index++
	}
	return index
}

func beginsRegularExpression(tokens []sourceToken) bool {
	if len(tokens) == 0 {
		return true
	}
	previous := tokens[len(tokens)-1]
	if previous.kind == identifierToken {
		switch previous.text {
		case "case", "delete", "return", "throw", "typeof", "void", "yield":
			return true
		default:
			return false
		}
	}
	return strings.Contains("=(:,[!&|?{};", previous.text)
}

func skipLineComment(source string, index int) int {
	for index < len(source) && source[index] != '\n' {
		index++
	}
	return index
}

func skipBlockComment(source string, index int) int {
	for index+1 < len(source) {
		if source[index] == '*' && source[index+1] == '/' {
			return index + 2
		}
		index++
	}
	return len(source)
}

func isSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func isIdentifierStart(character byte) bool {
	return character == '_' || character == '$' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func isIdentifierPart(character byte) bool {
	return isIdentifierStart(character) || character >= '0' && character <= '9'
}
