package timingreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/PerishCode/open-cut/utils/atomicfile"
)

const Schema = 1

const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
)

type Phase struct {
	Name       string `json:"name"`
	Outcome    string `json:"outcome"`
	DurationMS int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
}

type Decision struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
}

type Report struct {
	Schema     int               `json:"schema"`
	Operation  string            `json:"operation"`
	Outcome    string            `json:"outcome"`
	DurationMS int64             `json:"durationMs"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Decisions  []Decision        `json:"decisions,omitempty"`
	Phases     []Phase           `json:"phases"`
	Error      string            `json:"error,omitempty"`
}

type Recorder struct {
	operation  string
	attributes map[string]string
	started    time.Time
	now        func() time.Time
	decisions  []Decision
	phases     []Phase
	activeName string
	activeAt   time.Time
}

func New(operation string, attributes map[string]string) *Recorder {
	return newRecorder(operation, attributes, time.Now)
}

func newRecorder(operation string, attributes map[string]string, now func() time.Time) *Recorder {
	copied := make(map[string]string, len(attributes))
	for name, value := range attributes {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(value) != "" {
			copied[name] = value
		}
	}
	return &Recorder{operation: operation, attributes: copied, started: now(), now: now}
}

func (recorder *Recorder) Begin(name string) func(error) {
	started := recorder.now()
	finished := false
	return func(err error) {
		if finished {
			return
		}
		finished = true
		phase := Phase{Name: name, Outcome: OutcomeSucceeded, DurationMS: elapsedMS(started, recorder.now())}
		if err != nil {
			phase.Outcome = OutcomeFailed
			phase.Detail = err.Error()
		}
		recorder.phases = append(recorder.phases, phase)
	}
}

func (recorder *Recorder) Step(name string) {
	recorder.closeActive(nil)
	recorder.activeName = name
	recorder.activeAt = recorder.now()
}

func (recorder *Recorder) Decide(name, value, detail string) {
	recorder.decisions = append(recorder.decisions, Decision{Name: name, Value: value, Detail: detail})
}

func (recorder *Recorder) Attribute(name, value string) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
		return
	}
	if recorder.attributes == nil {
		recorder.attributes = make(map[string]string)
	}
	recorder.attributes[name] = value
}

func (recorder *Recorder) Finish(err error) Report {
	recorder.closeActive(err)
	report := Report{
		Schema: Schema, Operation: recorder.operation, Outcome: OutcomeSucceeded,
		DurationMS: elapsedMS(recorder.started, recorder.now()),
		Attributes: cloneMap(recorder.attributes),
		Decisions:  append([]Decision(nil), recorder.decisions...),
		Phases:     append([]Phase(nil), recorder.phases...),
	}
	if err != nil {
		report.Outcome = OutcomeFailed
		report.Error = err.Error()
	}
	return report
}

func (recorder *Recorder) closeActive(err error) {
	if recorder.activeName == "" {
		return
	}
	phase := Phase{
		Name: recorder.activeName, Outcome: OutcomeSucceeded,
		DurationMS: elapsedMS(recorder.activeAt, recorder.now()),
	}
	if err != nil {
		phase.Outcome = OutcomeFailed
		phase.Detail = err.Error()
	}
	recorder.phases = append(recorder.phases, phase)
	recorder.activeName = ""
}

func Write(path string, report Report) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("timing report path is required")
	}
	if err := Validate(report); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return atomicfile.WriteJSON(absolute, report, 0o600)
}

func Read(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, err
	}
	if err := Validate(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

// DecisionValue returns one unambiguous build decision for automation. A
// missing or duplicate decision fails closed: callers must never guess whether
// an expensive artifact was actually inspected, reused, or rebuilt.
func DecisionValue(report Report, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("timing decision name is required")
	}
	value := ""
	found := false
	for _, decision := range report.Decisions {
		if decision.Name != name {
			continue
		}
		if found {
			return "", fmt.Errorf("timing decision %q is ambiguous", name)
		}
		found = true
		value = decision.Value
	}
	if !found {
		return "", fmt.Errorf("timing decision %q is unavailable", name)
	}
	return value, nil
}

func Validate(report Report) error {
	if report.Schema != Schema || strings.TrimSpace(report.Operation) == "" ||
		(report.Outcome != OutcomeSucceeded && report.Outcome != OutcomeFailed) || report.DurationMS < 0 {
		return fmt.Errorf("invalid timing report")
	}
	for _, phase := range report.Phases {
		if strings.TrimSpace(phase.Name) == "" ||
			(phase.Outcome != OutcomeSucceeded && phase.Outcome != OutcomeFailed) || phase.DurationMS < 0 {
			return fmt.Errorf("invalid timing phase")
		}
	}
	for _, decision := range report.Decisions {
		if strings.TrimSpace(decision.Name) == "" || strings.TrimSpace(decision.Value) == "" {
			return fmt.Errorf("invalid timing decision")
		}
	}
	return nil
}

func Markdown(reports []Report) string {
	var output strings.Builder
	for _, report := range reports {
		title := report.Operation
		if target := report.Attributes["target"]; target != "" {
			title += " · " + target
		}
		fmt.Fprintf(&output, "### %s\n\n", markdownCell(title))
		fmt.Fprintf(&output, "Outcome: `%s` · total: `%s`\n\n", report.Outcome, displayDuration(report.DurationMS))
		if len(report.Decisions) > 0 {
			output.WriteString("| Decision | Value | Detail |\n| --- | --- | --- |\n")
			for _, decision := range report.Decisions {
				fmt.Fprintf(&output, "| %s | `%s` | %s |\n", markdownCell(decision.Name), markdownCell(decision.Value), markdownCell(decision.Detail))
			}
			output.WriteString("\n")
		}
		output.WriteString("| Phase | Outcome | Duration |\n| --- | --- | ---: |\n")
		for _, phase := range report.Phases {
			fmt.Fprintf(&output, "| %s | `%s` | %s |\n", markdownCell(phase.Name), phase.Outcome, displayDuration(phase.DurationMS))
		}
		output.WriteString("\n")
	}
	return output.String()
}

func ComparisonMarkdown(baseline, candidate Report) (string, error) {
	if baseline.Operation != candidate.Operation {
		return "", fmt.Errorf("timing comparison requires matching operations")
	}
	baselineTarget, candidateTarget := baseline.Attributes["target"], candidate.Attributes["target"]
	if baselineTarget != "" && candidateTarget != "" && baselineTarget != candidateTarget {
		return "", fmt.Errorf("timing comparison requires matching targets")
	}
	title := baseline.Operation
	if baselineTarget != "" {
		title += " · " + baselineTarget
	} else if candidateTarget != "" {
		title += " · " + candidateTarget
	}
	var output strings.Builder
	fmt.Fprintf(&output, "### Timing comparison · %s\n\n", markdownCell(title))
	fmt.Fprintf(
		&output, "Outcome: `%s` → `%s` · total: `%s` → `%s` (%s, %s)\n\n",
		baseline.Outcome, candidate.Outcome,
		displayDuration(baseline.DurationMS), displayDuration(candidate.DurationMS),
		displayDelta(candidate.DurationMS-baseline.DurationMS), displayPercent(baseline.DurationMS, candidate.DurationMS),
	)
	decisionNames := orderedDecisionNames(baseline.Decisions, candidate.Decisions)
	if len(decisionNames) > 0 {
		baselineDecisions, candidateDecisions := decisionValues(baseline.Decisions), decisionValues(candidate.Decisions)
		output.WriteString("| Decision | Baseline | Candidate |\n| --- | --- | --- |\n")
		for _, name := range decisionNames {
			fmt.Fprintf(
				&output, "| %s | `%s` | `%s` |\n",
				markdownCell(name), markdownCell(baselineDecisions[name]), markdownCell(candidateDecisions[name]),
			)
		}
		output.WriteString("\n")
	}
	baselinePhases, candidatePhases := phaseDurations(baseline.Phases), phaseDurations(candidate.Phases)
	output.WriteString("| Phase | Baseline | Candidate | Delta | Change |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, name := range orderedPhaseNames(baseline.Phases, candidate.Phases) {
		baselineDuration, baselineFound := baselinePhases[name]
		candidateDuration, candidateFound := candidatePhases[name]
		if !baselineFound || !candidateFound {
			fmt.Fprintf(
				&output, "| %s | %s | %s | n/a | n/a |\n",
				markdownCell(name), optionalDuration(baselineDuration, baselineFound), optionalDuration(candidateDuration, candidateFound),
			)
			continue
		}
		fmt.Fprintf(
			&output, "| %s | %s | %s | %s | %s |\n",
			markdownCell(name), displayDuration(baselineDuration), displayDuration(candidateDuration),
			displayDelta(candidateDuration-baselineDuration), displayPercent(baselineDuration, candidateDuration),
		)
	}
	output.WriteString("\n")
	return output.String(), nil
}

func elapsedMS(started, finished time.Time) int64 {
	duration := finished.Sub(started)
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}

func cloneMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result[key] = values[key]
	}
	return result
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func displayDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "0s"
	}
	return (time.Duration(milliseconds) * time.Millisecond).Round(time.Millisecond).String()
}

func displayDelta(milliseconds int64) string {
	if milliseconds == 0 {
		return "0s"
	}
	prefix := "+"
	if milliseconds < 0 {
		prefix = "-"
		milliseconds = -milliseconds
	}
	return prefix + displayDuration(milliseconds)
}

func displayPercent(baseline, candidate int64) string {
	if baseline == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.1f%%", float64(candidate-baseline)/float64(baseline)*100)
}

func optionalDuration(duration int64, found bool) string {
	if !found {
		return "n/a"
	}
	return displayDuration(duration)
}

func orderedDecisionNames(groups ...[]Decision) []string {
	seen := make(map[string]bool)
	var names []string
	for _, decisions := range groups {
		for _, decision := range decisions {
			if !seen[decision.Name] {
				seen[decision.Name] = true
				names = append(names, decision.Name)
			}
		}
	}
	return names
}

func decisionValues(decisions []Decision) map[string]string {
	values := make(map[string]string, len(decisions))
	for _, decision := range decisions {
		values[decision.Name] = decision.Value
	}
	return values
}

func orderedPhaseNames(groups ...[]Phase) []string {
	seen := make(map[string]bool)
	var names []string
	for _, phases := range groups {
		for _, phase := range phases {
			if !seen[phase.Name] {
				seen[phase.Name] = true
				names = append(names, phase.Name)
			}
		}
	}
	return names
}

func phaseDurations(phases []Phase) map[string]int64 {
	values := make(map[string]int64, len(phases))
	for _, phase := range phases {
		values[phase.Name] = phase.DurationMS
	}
	return values
}
