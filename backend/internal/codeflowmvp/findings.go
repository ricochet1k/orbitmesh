package codeflowmvp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	graphdb "github.com/mstrYoda/goraphdb"
)

type FindingsOptions struct {
	DBPath string
}

type FindingsReport struct {
	DBPath   string          `json:"db_path"`
	Count    int             `json:"count"`
	Findings []StoredFinding `json:"findings"`
}

type StoredFinding struct {
	ID          string  `json:"id"`
	RuleID      string  `json:"rule_id"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	ScanEpoch   string  `json:"scan_epoch,omitempty"`
	FileID      string  `json:"file_id,omitempty"`
	FunctionID  string  `json:"function_id,omitempty"`
	FactID      string  `json:"fact_id,omitempty"`
	CalleeExpr  string  `json:"callee_expr,omitempty"`
	Line        int     `json:"line,omitempty"`
	Column      int     `json:"column,omitempty"`
	EndLine     int     `json:"end_line,omitempty"`
	EndColumn   int     `json:"end_column,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Status      string  `json:"status,omitempty"`
	Fingerprint string  `json:"fingerprint,omitempty"`
}

func ReadFindings(opts FindingsOptions) (FindingsReport, error) {
	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return FindingsReport{}, fmt.Errorf("resolve db path %q: %w", dbPath, err)
	}

	db, err := graphdb.Open(absPath, graphdb.DefaultOptions())
	if err != nil {
		return FindingsReport{}, fmt.Errorf("open graph db %q: %w", absPath, err)
	}
	defer db.Close()

	nodes, err := db.FindByLabel(NodeLabelFinding)
	if err != nil {
		return FindingsReport{}, fmt.Errorf("list finding nodes: %w", err)
	}

	findings := make([]StoredFinding, 0, len(nodes))
	for _, node := range nodes {
		findings = append(findings, storedFindingFromNode(node))
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].RuleID == findings[j].RuleID {
			if findings[i].FileID == findings[j].FileID {
				if findings[i].Line == findings[j].Line {
					return findings[i].ID < findings[j].ID
				}
				return findings[i].Line < findings[j].Line
			}
			return findings[i].FileID < findings[j].FileID
		}
		return findings[i].RuleID < findings[j].RuleID
	})

	return FindingsReport{
		DBPath:   absPath,
		Count:    len(findings),
		Findings: findings,
	}, nil
}

func FormatFindingsMarkdown(report FindingsReport) string {
	var b strings.Builder
	b.WriteString("# Findings\n\n")
	b.WriteString("- Database: `")
	b.WriteString(report.DBPath)
	b.WriteString("`\n")
	b.WriteString("- Count: ")
	b.WriteString(strconv.Itoa(report.Count))
	b.WriteString("\n\n")

	if len(report.Findings) == 0 {
		b.WriteString("No findings.\n")
		return b.String()
	}

	for idx, finding := range report.Findings {
		b.WriteString("## ")
		b.WriteString(strconv.Itoa(idx + 1))
		b.WriteString(". [")
		b.WriteString(finding.Severity)
		b.WriteString("] `")
		b.WriteString(finding.RuleID)
		b.WriteString("`\n")
		b.WriteString(finding.Message)
		b.WriteString("\n\n")
		b.WriteString("- ID: `")
		b.WriteString(finding.ID)
		b.WriteString("`\n")
		if finding.FileID != "" {
			b.WriteString("- File: `")
			b.WriteString(finding.FileID)
			b.WriteString("`\n")
		}
		if finding.FunctionID != "" {
			b.WriteString("- Function: `")
			b.WriteString(finding.FunctionID)
			b.WriteString("`\n")
		}
		if finding.FactID != "" {
			b.WriteString("- Fact: `")
			b.WriteString(finding.FactID)
			b.WriteString("`\n")
		}
		if finding.CalleeExpr != "" {
			b.WriteString("- Callee: `")
			b.WriteString(finding.CalleeExpr)
			b.WriteString("`\n")
		}
		if finding.Line > 0 && finding.Column > 0 {
			b.WriteString("- Location: ")
			b.WriteString(strconv.Itoa(finding.Line))
			b.WriteString(":")
			b.WriteString(strconv.Itoa(finding.Column))
			if finding.EndLine > 0 && finding.EndColumn > 0 {
				b.WriteString("-")
				b.WriteString(strconv.Itoa(finding.EndLine))
				b.WriteString(":")
				b.WriteString(strconv.Itoa(finding.EndColumn))
			}
			b.WriteString("\n")
		}
		if finding.Confidence > 0 {
			b.WriteString("- Confidence: ")
			b.WriteString(strconv.FormatFloat(finding.Confidence, 'f', 2, 64))
			b.WriteString("\n")
		}
		if finding.Status != "" {
			b.WriteString("- Status: `")
			b.WriteString(finding.Status)
			b.WriteString("`\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

func storedFindingFromNode(node *graphdb.Node) StoredFinding {
	if node == nil {
		return StoredFinding{}
	}

	return StoredFinding{
		ID:          node.GetString("id"),
		RuleID:      node.GetString("rule_id"),
		Severity:    node.GetString("severity"),
		Message:     node.GetString("message"),
		ScanEpoch:   node.GetString("scan_epoch"),
		FileID:      node.GetString("file_id"),
		FunctionID:  node.GetString("function_id"),
		FactID:      node.GetString("fact_id"),
		CalleeExpr:  node.GetString("callee_expr"),
		Line:        intProp(node.Props, "line"),
		Column:      intProp(node.Props, "column"),
		EndLine:     intProp(node.Props, "end_line"),
		EndColumn:   intProp(node.Props, "end_column"),
		Confidence:  floatProp(node.Props, "confidence"),
		Status:      node.GetString("status"),
		Fingerprint: node.GetString("fingerprint"),
	}
}

func intProp(props graphdb.Props, key string) int {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func floatProp(props graphdb.Props, key string) float64 {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint8:
		return float64(n)
	case uint16:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	default:
		return 0
	}
}
