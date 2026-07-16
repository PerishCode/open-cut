package command

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"
)

func TestInitialRegistryDiscoversOnlyImplementedCommandTree(t *testing.T) {
	registry := InitialRegistry()
	root, err := registry.Discover(nil, "0.1.0-test")
	if err != nil {
		t.Fatal(err)
	}
	if root.Schema != HelpSchemaVersion || len(root.Children) != 11 || root.Children[0].Name != "activity" ||
		root.Children[0].Leaf || root.Children[1].Name != "asset" || root.Children[1].Leaf ||
		root.Children[2].Name != "edit" || root.Children[2].Leaf ||
		root.Children[4].Name != "export" || root.Children[4].Leaf ||
		root.Children[6].Name != "product" || root.Children[6].Leaf ||
		root.Children[9].Name != "sequence" || root.Children[9].Leaf ||
		root.Children[10].Name != "transcript" || root.Children[10].Leaf {
		t.Fatalf("root help = %+v", root)
	}
	export, err := registry.Discover([]string{"export"}, "0.1.0-test")
	if err != nil || len(export.Children) != 4 || export.Children[0].Name != "cancel" ||
		export.Children[1].Name != "retry" || export.Children[2].Name != "show" ||
		export.Children[3].Name != "start" {
		t.Fatalf("export help = %+v err=%v", export, err)
	}
	project, err := registry.Discover([]string{"project"}, "0.1.0-test")
	if err != nil || len(project.Children) != 2 || project.Children[0].Name != "list" || project.Children[1].Name != "show" {
		t.Fatalf("project help = %+v err=%v", project, err)
	}
	edit, err := registry.Discover([]string{"edit"}, "0.1.0-test")
	if err != nil || len(edit.Children) != 7 || edit.Children[0].Name != "apply" ||
		edit.Children[1].Name != "derive-captions" || edit.Children[2].Name != "derive-rough-cut" ||
		edit.Children[6].Name != "undo" {
		t.Fatalf("edit help = %+v err=%v", edit, err)
	}
	if _, err := registry.Discover([]string{"unknown"}, "0.1.0-test"); !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("unknown error = %v", err)
	}
}

func TestProjectHelpUsesExactStringSchemas(t *testing.T) {
	help, err := InitialRegistry().Discover([]string{"project", "show"}, "0.1.0-test")
	if err != nil {
		t.Fatal(err)
	}
	projectID := help.Input.Properties["projectId"]
	if projectID.Type != "string" || projectID.Format != "uuid" || projectID.Pattern != uuidV7Pattern {
		t.Fatalf("projectId schema = %+v", projectID)
	}
	overview := help.Result.Properties["data"]
	if overview == nil {
		t.Fatalf("result schema = %+v", help.Result)
	}
	if containsUnsafeExactNumber(help.Result) {
		t.Fatalf("exact product scalar escaped as JSON number: %+v", help.Result)
	}
	encoded, err := json.Marshal(help)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Discovery
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Schema != HelpSchemaVersion {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func TestLeafHelpCarriesStableRegistryFingerprint(t *testing.T) {
	registry := InitialRegistry()
	for _, path := range [][]string{
		{"activity", "list"}, {"product", "status"}, {"project", "list"}, {"project", "show"},
		{"asset", "list"}, {"asset", "inspect"}, {"asset", "frames"},
		{"transcript", "read"},
		{"run", "begin"}, {"run", "show"}, {"run", "resume"}, {"run", "complete"}, {"run", "cancel"},
		{"narrative", "show"}, {"sequence", "show"}, {"entity", "show"},
		{"edit", "show"}, {"edit", "history"}, {"edit", "derive-captions"}, {"edit", "derive-rough-cut"},
		{"edit", "propose"}, {"edit", "apply"}, {"edit", "undo"},
	} {
		help, err := registry.Discover(path, "0.1.0-test")
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, err := registry.Fingerprint(path)
		if err != nil {
			t.Fatal(err)
		}
		if help.Fingerprint != fingerprint || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(fingerprint) {
			t.Fatalf("path=%v help=%q fingerprint=%q", path, help.Fingerprint, fingerprint)
		}
	}
}

func TestRegistryRejectsDuplicatesAndOneLevelCommands(t *testing.T) {
	descriptor := Descriptor{
		Path: []string{"project", "list"}, Summary: "list",
		InputType: reflect.TypeFor[ProjectListInput](), ResultType: reflect.TypeFor[Result[ProjectListData]](),
		Mutability: ReadOnly, Statuses: []Status{StatusSucceeded}, Approval: ApprovalNone,
		Receipt: ReceiptNone, RequiredScope: ScopeProjectRead,
	}
	if _, err := NewRegistry(descriptor, descriptor); !errors.Is(err, ErrDuplicateCommand) {
		t.Fatalf("duplicate error = %v", err)
	}
	descriptor.Path = []string{"project"}
	if _, err := NewRegistry(descriptor); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("one-level error = %v", err)
	}
}

func containsUnsafeExactNumber(schema *JSONSchema) bool {
	if schema == nil {
		return false
	}
	if schema.Format == "int64" || schema.Format == "uint64" {
		return schema.Type == "integer" || schema.Type == "number"
	}
	for _, child := range schema.Properties {
		if containsUnsafeExactNumber(child) {
			return true
		}
	}
	return containsUnsafeExactNumber(schema.Items)
}
