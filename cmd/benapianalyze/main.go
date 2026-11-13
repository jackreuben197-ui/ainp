package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	var (
		input         = flag.String("input", "build/benapi", "comma-separated files, directories (scanned recursively), or glob patterns")
		output        = flag.String("output", "reports/benapi-analysis.json", "JSON report path")
		configPath    = flag.String("config", "conf/config.yaml", "AinP configuration file")
		profiles      = flag.String("profiles", "FPCH_30_15,FPCH_default", "comma-separated AinP profiles")
		maxCompare    = flag.Int("max-compare", 4000, "maximum stratified BenAPI decisions retained for AinP comparison")
		equitySamples = flag.Int("equity-samples", 1000, "AinP Monte Carlo samples per compared decision")
		maxExamples   = flag.Int("max-examples", 100, "maximum disagreement examples per profile")
	)
	flag.Parse()

	report, err := analyze(options{
		Input: strings.Split(*input, ","), ConfigPath: *configPath, Profiles: splitNonEmpty(*profiles),
		MaxCompare: *maxCompare, EquitySamples: *equitySamples, MaxExamples: *maxExamples,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "benapi analysis:", err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode report:", err)
		os.Exit(1)
	}
	if err := writeReport(*output, data); err != nil {
		fmt.Fprintln(os.Stderr, "write report:", err)
		os.Exit(1)
	}
	compared := 0
	for _, item := range report.Comparisons {
		compared += item.Evaluated
	}
	fmt.Printf("benapi decisions=%d raw_decisions=%d duplicates=%d callbacks=%d malformed=%d retained=%d ainp_evaluations=%d report=%s\n",
		report.Decisions, report.Dataset.RawDecisionRecords, report.Dataset.DuplicateDecisionRecords, report.Callbacks,
		report.MalformedRecords, report.RetainedForComparison, compared, *output)
}

func splitNonEmpty(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
