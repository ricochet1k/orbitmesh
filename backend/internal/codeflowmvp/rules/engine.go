package rules

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

const RuleSpawnInLoop = "spawn_in_loop"
const RuleSourceToSinkUnsanitized = "source_to_sink_unsanitized"
const RuleUnreachableCode = "unreachable_code"

var sourceCalleeSuffixes = []string{
	"os.Getenv",
	"URL.Query",
	"FormValue",
	"PostFormValue",
	"Header.Get",
	"Body.Read",
}

var sinkCalleeSuffixes = []string{
	".Exec",
	".ExecContext",
	".Query",
	".QueryContext",
	"exec.Command",
	"exec.CommandContext",
	".Write",
	"http.Error",
	"log.Print",
	"log.Printf",
	"log.Println",
}

var sanitizerCalleeSuffixes = []string{
	"strconv.Atoi",
	"url.QueryEscape",
	"template.HTMLEscapeString",
	"html.EscapeString",
}

func Evaluate(facts Facts) []Finding {
	fileByFunction := make(map[string]string, len(facts.Functions))
	for _, fn := range facts.Functions {
		fileByFunction[fn.ID] = fn.FileID
	}

	findings := make([]Finding, 0)
	for _, spawn := range facts.Spawns {
		if !spawn.InsideLoop {
			continue
		}

		findings = append(findings, Finding{
			RuleID:   RuleSpawnInLoop,
			Severity: "medium",
			Message:  "goroutine spawn inside loop can create unbounded concurrent execution",
			Location: Location{
				FileID:     fileByFunction[spawn.CallerID],
				FunctionID: spawn.CallerID,
				FactID:     spawn.ID,
				CalleeExpr: spawn.CalleeExpr,
				Start:      spawn.Start,
				End:        spawn.End,
			},
			Fingerprint: fingerprint(RuleSpawnInLoop, spawn.ID, spawn.CallerID, spawn.CalleeExpr),
			Confidence:  0.95,
		})
	}

	findings = append(findings, evaluateSourceToSinkUnsanitized(facts, fileByFunction)...)
	findings = append(findings, evaluateUnreachableCode(facts, fileByFunction)...)

	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Fingerprint < findings[j].Fingerprint
	})

	return findings
}

func evaluateSourceToSinkUnsanitized(facts Facts, fileByFunction map[string]string) []Finding {
	callsByFunction := make(map[string][]CallFact)
	for _, call := range facts.Calls {
		callsByFunction[call.CallerID] = append(callsByFunction[call.CallerID], call)
	}

	out := make([]Finding, 0)
	for functionID, calls := range callsByFunction {
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].Start.Line == calls[j].Start.Line {
				return calls[i].Start.Column < calls[j].Start.Column
			}
			return calls[i].Start.Line < calls[j].Start.Line
		})

		seenSource := false
		sanitizedAfterSource := false
		for _, call := range calls {
			if matchesAnyCallee(call.CalleeExpr, sourceCalleeSuffixes) {
				seenSource = true
				sanitizedAfterSource = false
				continue
			}
			if !seenSource {
				continue
			}
			if matchesAnyCallee(call.CalleeExpr, sanitizerCalleeSuffixes) {
				sanitizedAfterSource = true
				continue
			}
			if sanitizedAfterSource {
				continue
			}
			if !matchesAnyCallee(call.CalleeExpr, sinkCalleeSuffixes) {
				continue
			}

			out = append(out, Finding{
				RuleID:   RuleSourceToSinkUnsanitized,
				Severity: "high",
				Message:  "possible unsanitized data flow from source to sink in same function",
				Location: Location{
					FileID:     fileByFunction[functionID],
					FunctionID: functionID,
					FactID:     call.ID,
					CalleeExpr: call.CalleeExpr,
					Start:      call.Start,
					End:        call.End,
				},
				Fingerprint: fingerprint(RuleSourceToSinkUnsanitized, functionID, call.ID, call.CalleeExpr),
				Confidence:  0.72,
			})
		}
	}

	return out
}

func evaluateUnreachableCode(facts Facts, fileByFunction map[string]string) []Finding {
	out := make([]Finding, 0)
	for _, block := range facts.Blocks {
		if !block.IsDead {
			continue
		}
		fileID := block.FileID
		if fileID == "" {
			fileID = fileByFunction[block.FunctionID]
		}
		out = append(out, Finding{
			RuleID:   RuleUnreachableCode,
			Severity: "low",
			Message:  "unreachable code detected after unconditional exit",
			Location: Location{
				FileID:     fileID,
				FunctionID: block.FunctionID,
				FactID:     block.ID,
				Start:      block.Start,
				End:        block.End,
			},
			Fingerprint: fingerprint(RuleUnreachableCode, block.ID, block.FunctionID),
			Confidence:  0.90,
		})
	}
	return out
}

func matchesAnyCallee(callee string, allowlist []string) bool {
	trimmed := strings.TrimSpace(callee)
	for _, suffix := range allowlist {
		if trimmed == suffix || strings.HasSuffix(trimmed, suffix) {
			return true
		}
	}
	return false
}

func fingerprint(parts ...string) string {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{'|'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
