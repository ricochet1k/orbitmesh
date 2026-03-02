package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/tabwriter"
)

func RenderTable(summary Summary) string {
	var out bytes.Buffer
	tw := tabwriter.NewWriter(&out, 0, 8, 2, ' ', 0)
	fmt.Fprintf(tw, "TOTALS\tproviders=%d\tpass=%d\tpartial=%d\tblocked_by_config=%d\tfail=%d\tskipped=%d\n",
		summary.Totals.Providers,
		summary.Totals.Passed,
		summary.Totals.Partial,
		summary.Totals.Blocked,
		summary.Totals.Failed,
		summary.Totals.Skipped,
	)
	fmt.Fprintln(tw, "PROVIDER\tLANE\tSTATUS\tDETAIL\tDURATION_MS")
	for _, result := range summary.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", result.Provider, result.Lane, result.Status, result.Detail, result.DurationMS)
		for _, scenario := range result.Scenarios {
			reason := scenario.Reason
			if reason == "" {
				reason = "-"
			}
			detail := scenario.Detail
			if detail == "" {
				detail = "-"
			}
			fmt.Fprintf(tw, "  - %s\t%s\t%s\t%s\t\n", scenario.ID, scenario.Status, reason, detail)
		}
	}
	_ = tw.Flush()
	return out.String()
}

func RenderJSON(summary Summary) ([]byte, error) {
	return json.Marshal(summary)
}
