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
	fmt.Fprintln(tw, "PROVIDER\tLANE\tSTATUS\tDETAIL\tDURATION_MS")
	for _, result := range summary.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", result.Provider, result.Lane, result.Status, result.Detail, result.DurationMS)
	}
	_ = tw.Flush()
	return out.String()
}

func RenderJSON(summary Summary) ([]byte, error) {
	return json.Marshal(summary)
}
