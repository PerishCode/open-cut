package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/PerishCode/open-cut/product/application"
)

const (
	HelpSchemaVersion    = "open-cut/cli-help/v1"
	CommandSchemaVersion = "open-cut/command/v1"
	FingerprintSchema    = "open-cut/command-fingerprint/v1"
)

var (
	ErrDuplicateCommand = errors.New("duplicate command path")
	ErrInvalidCommand   = errors.New("invalid command descriptor")
	ErrUnknownCommand   = errors.New("unknown command path")
)

type Mutability string

const (
	ReadOnly        Mutability = "read"
	OperationalRead Mutability = "operational-read"
	Creative        Mutability = "creative"
	Durable         Mutability = "durable-work"
)

type ApprovalPolicy string

const (
	ApprovalNone  ApprovalPolicy = "none"
	ApprovalExact ApprovalPolicy = "exact-impact"
)

type ReceiptPolicy = application.CommandReceiptClass

const (
	ReceiptNone     = application.CommandReceiptNone
	ReceiptEvidence = application.CommandReceiptEvidence
	ReceiptOutcome  = application.CommandReceiptOutcome
)

// Scope is internal authorization metadata shared by the CLI transport and API
// verifier. It is deliberately absent from Agent-facing discovery output.
type Scope string

const (
	ScopeProjectRead  Scope = "project:read"
	ScopeActivityRead Scope = "activity:read"
	ScopeRunRead      Scope = "run:read"
	ScopeRunWrite     Scope = "run:write"
	ScopeEditRead     Scope = "edit:read"
	ScopeEditWrite    Scope = "edit:write"
	ScopeAssetRead    Scope = "asset:read"
	ScopeProductRead  Scope = "product:read"
	ScopeExportRead   Scope = "export:read"
	ScopeExportWrite  Scope = "export:write"
)

type AppStateRequirements struct {
	Project  bool `json:"project"`
	Sequence bool `json:"sequence"`
	Run      bool `json:"run"`
	Turn     bool `json:"turn"`
}

type MutationLimits struct {
	CanonicalBytes  int `json:"canonicalBytes,omitempty"`
	Operations      int `json:"operations,omitempty"`
	LocalSymbols    int `json:"localSymbols,omitempty"`
	Preconditions   int `json:"preconditions,omitempty"`
	ChangedEntities int `json:"changedEntities,omitempty"`
	TextFieldBytes  int `json:"textFieldBytes,omitempty"`
}

var InitialMutationLimits = MutationLimits{
	CanonicalBytes: 1 << 20, Operations: 512, LocalSymbols: 1024,
	Preconditions: 2048, ChangedEntities: 2048, TextFieldBytes: 256 << 10,
}

type Descriptor struct {
	Path            []string
	Summary         string
	InputType       reflect.Type
	ResultType      reflect.Type
	Mutability      Mutability
	AppState        AppStateRequirements
	Statuses        []Status
	RequestIdentity bool
	Approval        ApprovalPolicy
	Receipt         ReceiptPolicy
	RequiredScope   Scope
	Limits          MutationLimits
}

type Registry struct {
	commands map[string]Descriptor
}

func NewRegistry(descriptors ...Descriptor) (*Registry, error) {
	registry := &Registry{commands: make(map[string]Descriptor, len(descriptors))}
	for _, descriptor := range descriptors {
		if err := registry.Register(descriptor); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func InitialRegistry() *Registry {
	registry, err := NewRegistry(
		Descriptor{
			Path: []string{"product", "status"}, Summary: "Show semantic product feature availability",
			InputType: reflect.TypeFor[ProductStatusInput](), ResultType: reflect.TypeFor[Result[ProductStatusData]](),
			Mutability: ReadOnly, Statuses: []Status{StatusSucceeded, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptNone, RequiredScope: ScopeProductRead,
		},
		Descriptor{
			Path: []string{"project", "list"}, Summary: "List bounded Project summaries",
			InputType: reflect.TypeFor[ProjectListInput](), ResultType: reflect.TypeFor[Result[ProjectListData]](),
			Mutability: ReadOnly, Statuses: []Status{StatusSucceeded, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptNone, RequiredScope: ScopeProjectRead,
		},
		Descriptor{
			Path: []string{"asset", "list"}, Summary: "List bounded Asset summaries for the current Project",
			InputType: reflect.TypeFor[AssetListInput](), ResultType: reflect.TypeFor[Result[AssetListData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeAssetRead,
		},
		Descriptor{
			Path: []string{"asset", "inspect"}, Summary: "Inspect one Asset, its facts, artifacts, and durable jobs",
			InputType: reflect.TypeFor[AssetInspectInput](), ResultType: reflect.TypeFor[Result[AssetInspectData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeAssetRead,
		},
		Descriptor{
			Path: []string{"asset", "frames"}, Summary: "Request bounded exact frames for one Asset SourceStream",
			InputType: reflect.TypeFor[AssetFramesInput](), ResultType: reflect.TypeFor[Result[AssetFramesData]](),
			Mutability: OperationalRead, AppState: AppStateRequirements{Project: true, Run: true, Turn: true},
			Statuses: []Status{
				StatusSucceeded, StatusAccepted, StatusScopeUpgradeRequired, StatusStaleTurn,
				StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed,
			},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeAssetRead,
		},
		Descriptor{
			Path: []string{"transcript", "read"}, Summary: "Read bounded original recognition from one transcript artifact",
			InputType: reflect.TypeFor[TranscriptReadInput](), ResultType: reflect.TypeFor[Result[TranscriptReadData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeAssetRead,
		},
		Descriptor{
			Path: []string{"activity", "list"}, Summary: "List durable activity after a scoped cursor",
			InputType: reflect.TypeFor[ActivityListInput](), ResultType: reflect.TypeFor[Result[ActivityListData]](),
			Mutability: ReadOnly, Statuses: []Status{StatusSucceeded, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptNone, RequiredScope: ScopeActivityRead,
		},
		Descriptor{
			Path: []string{"project", "show"}, Summary: "Show one bounded Project overview",
			InputType: reflect.TypeFor[ProjectShowInput](), ResultType: reflect.TypeFor[Result[ProjectShowData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeProjectRead,
		},
		Descriptor{
			Path: []string{"run", "begin"}, Summary: "Begin a durable standalone AgentRun and first writer turn",
			InputType: reflect.TypeFor[RunBeginInput](), ResultType: reflect.TypeFor[Result[RunData]](),
			Mutability: Durable, AppState: AppStateRequirements{Project: true},
			Statuses:        []Status{StatusSucceeded, StatusScopeUpgradeRequired, StatusInvalid, StatusNotFound, StatusUnavailable, StatusFailed},
			RequestIdentity: true, Approval: ApprovalNone, Receipt: ReceiptOutcome, RequiredScope: ScopeRunWrite,
		},
		Descriptor{
			Path: []string{"run", "show"}, Summary: "Show one durable AgentRun and its current turn",
			InputType: reflect.TypeFor[RunShowInput](), ResultType: reflect.TypeFor[Result[RunData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true, Run: true},
			Statuses: []Status{StatusSucceeded, StatusScopeUpgradeRequired, StatusNotFound, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptNone, RequiredScope: ScopeRunRead,
		},
		Descriptor{
			Path: []string{"run", "wait"}, Summary: "Wait a bounded interval for durable AgentRun activity",
			InputType: reflect.TypeFor[RunWaitInput](), ResultType: reflect.TypeFor[Result[RunData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true, Run: true},
			Statuses: []Status{StatusSucceeded, StatusScopeUpgradeRequired, StatusNotFound, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptNone, RequiredScope: ScopeRunRead,
		},
		runTransitionDescriptor("resume", "Resume a durable AgentRun with a new writer-turn generation", reflect.TypeFor[RunResumeInput]()),
		runTransitionDescriptor("complete", "Explicitly complete a durable AgentRun", reflect.TypeFor[RunCompleteInput]()),
		runTransitionDescriptor("cancel", "Explicitly cancel a durable AgentRun without undoing committed work", reflect.TypeFor[RunCancelInput]()),
		Descriptor{
			Path: []string{"narrative", "show"}, Summary: "Show one bounded restricted PaperEdit subtree",
			InputType: reflect.TypeFor[NarrativeShowInput](), ResultType: reflect.TypeFor[Result[NarrativeShowData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		Descriptor{
			Path: []string{"sequence", "show"}, Summary: "Show one bounded Sequence time window",
			InputType: reflect.TypeFor[SequenceShowInput](), ResultType: reflect.TypeFor[Result[SequenceShowData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true, Sequence: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		Descriptor{
			Path: []string{"sequence", "frames"}, Summary: "Inspect bounded exact frames of one committed Sequence revision",
			InputType: reflect.TypeFor[SequenceFramesInput](), ResultType: reflect.TypeFor[Result[SequenceFramesData]](),
			Mutability: OperationalRead,
			AppState:   AppStateRequirements{Project: true, Sequence: true, Run: true, Turn: true},
			Statuses: []Status{
				StatusSucceeded, StatusAccepted, StatusScopeUpgradeRequired, StatusStaleTurn,
				StatusConflict, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed,
			},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		exportDescriptor("start", "Start one pinned full-quality Sequence export", reflect.TypeFor[ExportStartInput](), Durable, true, ScopeExportWrite),
		exportDescriptor("show", "Show one durable export lineage", reflect.TypeFor[ExportShowInput](), ReadOnly, false, ScopeExportRead),
		exportDescriptor("retry", "Retry one recoverable export lineage", reflect.TypeFor[ExportRetryInput](), Durable, false, ScopeExportWrite),
		exportDescriptor("cancel", "Cancel one active export lineage", reflect.TypeFor[ExportCancelInput](), Durable, true, ScopeExportWrite),
		Descriptor{
			Path: []string{"entity", "show"}, Summary: "Show one editable entity with its exact revision",
			InputType: reflect.TypeFor[EntityShowInput](), ResultType: reflect.TypeFor[Result[EntityShowData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		Descriptor{
			Path: []string{"edit", "show"}, Summary: "Show one durable Edit Proposal",
			InputType: reflect.TypeFor[EditShowInput](), ResultType: reflect.TypeFor[Result[EditShowData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		Descriptor{
			Path: []string{"edit", "history"}, Summary: "List bounded committed Edit history",
			InputType: reflect.TypeFor[EditHistoryInput](), ResultType: reflect.TypeFor[Result[EditHistoryData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		Descriptor{
			Path:      []string{"edit", "derive-captions"},
			Summary:   "Preview one deterministic SourceExcerpt-to-Clip caption operation",
			InputType: reflect.TypeFor[CaptionDeriveInput](), ResultType: reflect.TypeFor[Result[CaptionDeriveData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true, Sequence: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		Descriptor{
			Path:      []string{"edit", "derive-rough-cut"},
			Summary:   "Preview one deterministic PaperEdit-to-Sequence rough-cut operation",
			InputType: reflect.TypeFor[RoughCutDeriveInput](), ResultType: reflect.TypeFor[Result[RoughCutDeriveData]](),
			Mutability: ReadOnly, AppState: AppStateRequirements{Project: true, Sequence: true},
			Statuses: []Status{StatusSucceeded, StatusNotFound, StatusConflict, StatusInvalid, StatusUnavailable, StatusFailed},
			Approval: ApprovalNone, Receipt: ReceiptEvidence, RequiredScope: ScopeEditRead,
		},
		editWriteDescriptor("propose", "Normalize and durably journal an Edit Proposal", reflect.TypeFor[EditProposeInput](), reflect.TypeFor[Result[EditProposalData]]()),
		editWriteDescriptor("apply", "Atomically apply an exact Edit Proposal", reflect.TypeFor[EditApplyInput](), reflect.TypeFor[Result[EditCommitData]]()),
		editWriteDescriptor("undo", "Commit the exact stored inverse of an Edit Transaction", reflect.TypeFor[EditUndoInput](), reflect.TypeFor[Result[EditCommitData]]()),
	)
	if err != nil {
		panic(err)
	}
	return registry
}

func editWriteDescriptor(subcommand, summary string, input, result reflect.Type) Descriptor {
	return Descriptor{
		Path: []string{"edit", subcommand}, Summary: summary,
		InputType: input, ResultType: result, Mutability: Creative,
		AppState: AppStateRequirements{Project: true, Sequence: true, Run: true, Turn: true},
		Statuses: []Status{
			StatusSucceeded, StatusScopeUpgradeRequired, StatusStaleTurn, StatusConflict,
			StatusInvalid, StatusNotFound, StatusUnavailable, StatusFailed,
		},
		RequestIdentity: true, Approval: ApprovalExact, Receipt: ReceiptOutcome, RequiredScope: ScopeEditWrite,
		Limits: InitialMutationLimits,
	}
}

func runTransitionDescriptor(subcommand, summary string, input reflect.Type) Descriptor {
	return Descriptor{
		Path: []string{"run", subcommand}, Summary: summary,
		InputType: input, ResultType: reflect.TypeFor[Result[RunData]](),
		Mutability: Durable, AppState: AppStateRequirements{Project: true, Run: true, Turn: true},
		Statuses: []Status{
			StatusSucceeded, StatusScopeUpgradeRequired, StatusStaleTurn, StatusConflict,
			StatusInvalid, StatusNotFound, StatusUnavailable, StatusFailed,
		},
		RequestIdentity: true, Approval: ApprovalNone, Receipt: ReceiptOutcome, RequiredScope: ScopeRunWrite,
	}
}

func exportDescriptor(
	subcommand, summary string,
	input reflect.Type,
	mutability Mutability,
	requestIdentity bool,
	scope Scope,
) Descriptor {
	state := AppStateRequirements{Project: true, Run: true, Turn: true}
	if subcommand == "start" {
		state.Sequence = true
	}
	return Descriptor{
		Path: []string{"export", subcommand}, Summary: summary,
		InputType: input, ResultType: reflect.TypeFor[Result[ExportData]](),
		Mutability: mutability, AppState: state,
		Statuses: []Status{
			StatusSucceeded, StatusAccepted, StatusScopeUpgradeRequired, StatusStaleTurn,
			StatusConflict, StatusNotFound, StatusInvalid, StatusUnavailable, StatusFailed,
		},
		RequestIdentity: requestIdentity, Approval: ApprovalNone, RequiredScope: scope,
		Receipt: func() ReceiptPolicy {
			if subcommand == "show" {
				return ReceiptNone
			}
			return ReceiptOutcome
		}(),
	}
}

func (registry *Registry) Register(descriptor Descriptor) error {
	if len(descriptor.Path) < 2 || descriptor.Summary == "" || descriptor.InputType == nil ||
		descriptor.ResultType == nil || descriptor.Mutability == "" || descriptor.Approval == "" || descriptor.Receipt == "" ||
		!validScope(descriptor.RequiredScope) || len(descriptor.Statuses) == 0 {
		return ErrInvalidCommand
	}
	for _, segment := range descriptor.Path {
		if segment == "" || strings.ContainsAny(segment, " /\t\r\n") {
			return ErrInvalidCommand
		}
	}
	key := commandKey(descriptor.Path)
	if _, exists := registry.commands[key]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateCommand, key)
	}
	if _, err := schemaFor(descriptor.InputType); err != nil {
		return err
	}
	if _, err := schemaFor(descriptor.ResultType); err != nil {
		return err
	}
	descriptor.Path = append([]string(nil), descriptor.Path...)
	descriptor.Statuses = append([]Status(nil), descriptor.Statuses...)
	registry.commands[key] = descriptor
	return nil
}

func (registry *Registry) Lookup(path []string) (Descriptor, error) {
	descriptor, exists := registry.commands[commandKey(path)]
	if !exists {
		return Descriptor{}, ErrUnknownCommand
	}
	descriptor.Path = append([]string(nil), descriptor.Path...)
	descriptor.Statuses = append([]Status(nil), descriptor.Statuses...)
	return descriptor, nil
}

func (registry *Registry) AgentScopes() []Scope {
	seen := make(map[Scope]struct{})
	for _, descriptor := range registry.commands {
		seen[descriptor.RequiredScope] = struct{}{}
	}
	result := make([]Scope, 0, len(seen))
	for scope := range seen {
		result = append(result, scope)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

type ChildHelp struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Leaf    bool   `json:"leaf"`
}

type Discovery struct {
	Schema               string               `json:"schema"`
	CLIVersion           string               `json:"cliVersion"`
	CommandSchemaVersion string               `json:"commandSchemaVersion"`
	Fingerprint          string               `json:"fingerprint,omitempty"`
	Path                 []string             `json:"path"`
	Summary              string               `json:"summary"`
	Children             []ChildHelp          `json:"children,omitempty"`
	Input                *JSONSchema          `json:"input,omitempty"`
	Result               *JSONSchema          `json:"result,omitempty"`
	Mutability           Mutability           `json:"mutability,omitempty"`
	AppState             AppStateRequirements `json:"appState"`
	Statuses             []Status             `json:"statuses,omitempty"`
	RequestIdentity      bool                 `json:"requestIdentity"`
	Approval             ApprovalPolicy       `json:"approval,omitempty"`
	Receipt              ReceiptPolicy        `json:"receipt,omitempty"`
	Limits               MutationLimits       `json:"limits"`
}

func (registry *Registry) Discover(path []string, cliVersion string) (Discovery, error) {
	discovery := Discovery{
		Schema: HelpSchemaVersion, CLIVersion: cliVersion,
		CommandSchemaVersion: CommandSchemaVersion, Path: append([]string(nil), path...),
		AppState: AppStateRequirements{}, Limits: MutationLimits{},
	}
	if descriptor, exists := registry.commands[commandKey(path)]; exists {
		input, err := schemaFor(descriptor.InputType)
		if err != nil {
			return Discovery{}, err
		}
		result, err := schemaFor(descriptor.ResultType)
		if err != nil {
			return Discovery{}, err
		}
		discovery.Summary = descriptor.Summary
		discovery.Fingerprint, err = registry.Fingerprint(path)
		if err != nil {
			return Discovery{}, err
		}
		discovery.Input = input
		discovery.Result = result
		discovery.Mutability = descriptor.Mutability
		discovery.AppState = descriptor.AppState
		discovery.Statuses = append([]Status(nil), descriptor.Statuses...)
		discovery.RequestIdentity = descriptor.RequestIdentity
		discovery.Approval = descriptor.Approval
		discovery.Receipt = descriptor.Receipt
		discovery.Limits = descriptor.Limits
		return discovery, nil
	}
	children := registry.children(path)
	if len(children) == 0 {
		return Discovery{}, ErrUnknownCommand
	}
	discovery.Summary = "Discover available Open Cut commands"
	discovery.Children = children
	return discovery, nil
}

type fingerprintDocument struct {
	Schema          string               `json:"schema"`
	Path            []string             `json:"path"`
	Summary         string               `json:"summary"`
	Input           *JSONSchema          `json:"input"`
	Result          *JSONSchema          `json:"result"`
	Mutability      Mutability           `json:"mutability"`
	AppState        AppStateRequirements `json:"appState"`
	Statuses        []Status             `json:"statuses"`
	RequestIdentity bool                 `json:"requestIdentity"`
	Approval        ApprovalPolicy       `json:"approval"`
	Receipt         ReceiptPolicy        `json:"receipt"`
	RequiredScope   Scope                `json:"requiredScope"`
	Limits          MutationLimits       `json:"limits"`
}

func (registry *Registry) Fingerprint(path []string) (string, error) {
	descriptor, exists := registry.commands[commandKey(path)]
	if !exists {
		return "", ErrUnknownCommand
	}
	input, err := schemaFor(descriptor.InputType)
	if err != nil {
		return "", err
	}
	result, err := schemaFor(descriptor.ResultType)
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(fingerprintDocument{
		Schema: FingerprintSchema, Path: descriptor.Path, Summary: descriptor.Summary,
		Input: input, Result: result, Mutability: descriptor.Mutability, AppState: descriptor.AppState,
		Statuses: descriptor.Statuses, RequestIdentity: descriptor.RequestIdentity,
		Approval: descriptor.Approval, Receipt: descriptor.Receipt,
		RequiredScope: descriptor.RequiredScope, Limits: descriptor.Limits,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (registry *Registry) children(path []string) []ChildHelp {
	prefix := commandKey(path)
	if prefix != "" {
		prefix += " "
	}
	children := make(map[string]ChildHelp)
	for key, descriptor := range registry.commands {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		remainder := strings.TrimPrefix(key, prefix)
		parts := strings.Split(remainder, " ")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		child := ChildHelp{Name: parts[0], Leaf: len(parts) == 1}
		if child.Leaf {
			child.Summary = descriptor.Summary
		} else {
			child.Summary = "Discover " + parts[0] + " commands"
		}
		children[child.Name] = child
	}
	result := make([]ChildHelp, 0, len(children))
	for _, child := range children {
		result = append(result, child)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func commandKey(path []string) string {
	return strings.Join(path, " ")
}

func validScope(scope Scope) bool {
	return scope == ScopeProjectRead || scope == ScopeActivityRead ||
		scope == ScopeRunRead || scope == ScopeRunWrite ||
		scope == ScopeEditRead || scope == ScopeEditWrite || scope == ScopeAssetRead ||
		scope == ScopeProductRead || scope == ScopeExportRead || scope == ScopeExportWrite
}
