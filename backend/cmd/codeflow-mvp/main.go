package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp"
)

type scanOutput struct {
	Extraction  codeflowmvp.ExtractionSummary   `json:"extraction"`
	Persistence *codeflowmvp.PersistenceSummary `json:"persistence,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsageAndExit()
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "scan":
		runScan(os.Args[2:])
	case "findings":
		runFindings(os.Args[2:])
	case "query":
		runQuery(os.Args[2:])
	case "explain-impact":
		runExplainImpact(os.Args[2:])
	default:
		printUsageAndExit()
	}
}

func runScan(args []string) {
	flags := flag.NewFlagSet("scan", flag.ExitOnError)
	persist := flags.Bool("persist", true, "persist extracted graph facts")
	dbPath := flags.String("db", "", "goraphdb path (defaults to .codeflow-mvp.goraphdb)")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(2)
	}
	if flags.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s scan [--persist=true|false] [--db <path>] <path>\n", os.Args[0])
		os.Exit(2)
	}
	scanPath := flags.Arg(0)

	result, err := codeflowmvp.ScanPath("", scanPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(1)
	}

	output := scanOutput{Extraction: result}
	if *persist {
		summary, err := codeflowmvp.PersistExtraction(result, codeflowmvp.PersistOptions{DBPath: *dbPath})
		if err != nil {
			fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
			os.Exit(1)
		}
		output.Persistence = &summary
	}

	writeJSON(output)
}

func runExplainImpact(args []string) {
	flags := flag.NewFlagSet("explain-impact", flag.ExitOnError)
	dbPath := flags.String("db", "", "goraphdb path (defaults to .codeflow-mvp.goraphdb)")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(2)
	}
	if flags.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s explain-impact [--db <path>] <symbol-or-file>\n", os.Args[0])
		os.Exit(2)
	}

	target := flags.Arg(0)
	report, err := codeflowmvp.ExplainImpact(target, codeflowmvp.ImpactOptions{DBPath: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(1)
	}

	writeJSON(report)
}

func runFindings(args []string) {
	flags := flag.NewFlagSet("findings", flag.ExitOnError)
	dbPath := flags.String("db", "", "goraphdb path (defaults to .codeflow-mvp.goraphdb)")
	format := flags.String("format", "json", "output format: json|markdown")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(2)
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "usage: %s findings [--db <path>] [--format json|markdown]\n", os.Args[0])
		os.Exit(2)
	}

	report, err := codeflowmvp.ReadFindings(codeflowmvp.FindingsOptions{DBPath: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json", "":
		writeJSON(report)
	case "markdown":
		fmt.Print(codeflowmvp.FormatFindingsMarkdown(report))
	default:
		fmt.Fprintf(os.Stderr, "codeflow-mvp: unsupported findings format %q (expected json|markdown)\n", *format)
		os.Exit(2)
	}
}

func runQuery(args []string) {
	flags := flag.NewFlagSet("query", flag.ExitOnError)
	dbPath := flags.String("db", "", "goraphdb path (defaults to .codeflow-mvp.goraphdb)")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(2)
	}
	if flags.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "usage: %s query [--db <path>] <cypher>\n", os.Args[0])
		os.Exit(2)
	}

	result, err := codeflowmvp.QueryGraph(strings.Join(flags.Args(), " "), codeflowmvp.QueryOptions{DBPath: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: %v\n", err)
		os.Exit(1)
	}

	writeJSON(result)
}

func writeJSON(v any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "codeflow-mvp: encode output: %v\n", err)
		os.Exit(1)
	}
}

func printUsageAndExit() {
	fmt.Fprintf(os.Stderr, "usage:\n")
	fmt.Fprintf(os.Stderr, "  %s scan [--persist=true|false] [--db <path>] <path>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s findings [--db <path>] [--format json|markdown]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s query [--db <path>] <cypher>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s explain-impact [--db <path>] <symbol-or-file>\n", os.Args[0])
	os.Exit(2)
}
