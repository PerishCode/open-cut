package controlcli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/PerishCode/open-cut/internal/timingreport"
)

func newTimingCommand(stdout, stderr io.Writer) *cobra.Command {
	parent := &cobra.Command{Use: "timing", Short: "Inspect structured operation timing reports"}
	requireSubcommand(parent)

	summary := &cobra.Command{Use: "summary", Short: "Render timing reports as Markdown", Args: cobra.NoArgs}
	reports := summary.Flags().StringArray("report", nil, "timing report JSON path; repeat for multiple operations")
	summary.RunE = func(*cobra.Command, []string) error {
		if len(*reports) == 0 {
			fmt.Fprintln(stderr, "timing summary requires at least one --report")
			return exitCodeError{code: 2}
		}
		loaded := make([]timingreport.Report, 0, len(*reports))
		for _, path := range *reports {
			report, err := timingreport.Read(path)
			if err != nil {
				fmt.Fprintf(stderr, "timing summary: read %s: %v\n", path, err)
				return exitCodeError{code: 1}
			}
			loaded = append(loaded, report)
		}
		_, err := fmt.Fprint(stdout, timingreport.Markdown(loaded))
		if err != nil {
			fmt.Fprintf(stderr, "timing summary: %v\n", err)
			return exitCodeError{code: 1}
		}
		return nil
	}
	parent.AddCommand(summary)

	compare := &cobra.Command{Use: "compare", Short: "Compare matching timing reports", Args: cobra.NoArgs}
	baselinePath := compare.Flags().String("baseline", "", "baseline timing report JSON path")
	candidatePath := compare.Flags().String("candidate", "", "candidate timing report JSON path")
	compare.RunE = func(*cobra.Command, []string) error {
		if *baselinePath == "" || *candidatePath == "" {
			fmt.Fprintln(stderr, "timing compare requires --baseline and --candidate")
			return exitCodeError{code: 2}
		}
		baseline, err := timingreport.Read(*baselinePath)
		if err != nil {
			fmt.Fprintf(stderr, "timing compare: read baseline: %v\n", err)
			return exitCodeError{code: 1}
		}
		candidate, err := timingreport.Read(*candidatePath)
		if err != nil {
			fmt.Fprintf(stderr, "timing compare: read candidate: %v\n", err)
			return exitCodeError{code: 1}
		}
		comparison, err := timingreport.ComparisonMarkdown(baseline, candidate)
		if err != nil {
			fmt.Fprintf(stderr, "timing compare: %v\n", err)
			return exitCodeError{code: 2}
		}
		if _, err := fmt.Fprint(stdout, comparison); err != nil {
			fmt.Fprintf(stderr, "timing compare: %v\n", err)
			return exitCodeError{code: 1}
		}
		return nil
	}
	parent.AddCommand(compare)

	decision := &cobra.Command{Use: "decision", Short: "Read one exact timing decision", Args: cobra.NoArgs}
	decisionReport := decision.Flags().String("report", "", "timing report JSON path")
	decisionName := decision.Flags().String("name", "", "decision name")
	decision.RunE = func(*cobra.Command, []string) error {
		if *decisionReport == "" || *decisionName == "" {
			fmt.Fprintln(stderr, "timing decision requires --report and --name")
			return exitCodeError{code: 2}
		}
		report, err := timingreport.Read(*decisionReport)
		if err != nil {
			fmt.Fprintf(stderr, "timing decision: read report: %v\n", err)
			return exitCodeError{code: 1}
		}
		value, err := timingreport.DecisionValue(report, *decisionName)
		if err != nil {
			fmt.Fprintf(stderr, "timing decision: %v\n", err)
			return exitCodeError{code: 1}
		}
		if _, err := fmt.Fprintln(stdout, value); err != nil {
			fmt.Fprintf(stderr, "timing decision: %v\n", err)
			return exitCodeError{code: 1}
		}
		return nil
	}
	parent.AddCommand(decision)

	cacheReport := &cobra.Command{Use: "cache-report", Short: "Record cache restore cohorts", Args: cobra.NoArgs}
	output := cacheReport.Flags().String("output", "", "timing report JSON path")
	target := cacheReport.Flags().String("target", "", "public build target")
	caches := cacheReport.Flags().StringArray("cache", nil, "name,primary-key,matched-key,cache-hit tuple")
	attributes := cacheReport.Flags().StringArray("attribute", nil, "name=value report attribute")
	cacheReport.RunE = func(*cobra.Command, []string) error {
		if *output == "" || *target == "" || len(*caches) == 0 {
			fmt.Fprintln(stderr, "timing cache-report requires --output, --target, and at least one --cache")
			return exitCodeError{code: 2}
		}
		report := timingreport.Report{
			Schema: timingreport.Schema, Operation: "cache-restore", Outcome: timingreport.OutcomeSucceeded,
			Attributes: map[string]string{"target": *target}, Phases: []timingreport.Phase{},
		}
		for _, attribute := range *attributes {
			name, value, ok := strings.Cut(attribute, "=")
			if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
				fmt.Fprintf(stderr, "timing cache-report: invalid attribute %q\n", attribute)
				return exitCodeError{code: 2}
			}
			report.Attributes[name] = value
		}
		for _, value := range *caches {
			fields := strings.SplitN(value, ",", 4)
			if len(fields) != 4 || strings.TrimSpace(fields[0]) == "" {
				fmt.Fprintf(stderr, "timing cache-report: invalid cache tuple %q\n", value)
				return exitCodeError{code: 2}
			}
			cohort := "miss"
			if fields[3] == "true" {
				cohort = "exact"
			} else if fields[2] != "" {
				cohort = "fallback"
			} else if fields[3] != "" && fields[3] != "false" {
				fmt.Fprintf(stderr, "timing cache-report: invalid cache-hit value %q\n", fields[3])
				return exitCodeError{code: 2}
			}
			report.Decisions = append(report.Decisions, timingreport.Decision{
				Name: fields[0], Value: cohort,
				Detail: fmt.Sprintf("primary=%s matched=%s", fields[1], fields[2]),
			})
		}
		if err := timingreport.Write(*output, report); err != nil {
			fmt.Fprintf(stderr, "timing cache-report: %v\n", err)
			return exitCodeError{code: 1}
		}
		return nil
	}
	parent.AddCommand(cacheReport)
	return parent
}
