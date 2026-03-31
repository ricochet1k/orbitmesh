package codeflowmvp

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gotreesitter"
)

type cfgBuildResult struct {
	entry    string
	exits    []string
	stmtNode []string
}

// cfgBuildCtx carries control-flow context through recursive CFG construction.
type cfgBuildCtx struct {
	loopHead   string    // block ID of the innermost loop header (for continue)
	breakExits *[]string // collects block IDs that exit via break
}

func (e *extractor) nextStatementID(functionID, kind string) string {
	key := functionID + "|" + kind
	e.stmtSeq[key]++
	return fmt.Sprintf("%s#stmt:%s:%d", functionID, kind, e.stmtSeq[key])
}

func (e *extractor) nextBlockID(functionID string) string {
	e.blockSeq[functionID]++
	return fmt.Sprintf("%s#block:%d", functionID, e.blockSeq[functionID]-1)
}

func (e *extractor) buildGoCFGForFunction(fnNode *gotreesitter.Node, bodyNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, functionID string) {
	if bodyNode == nil {
		return
	}

	startBlockIndex := len(e.summary.Blocks)
	res := e.buildGoBlockStatements(functionID, fileID, bodyNode, lang, source, cfgBuildCtx{})
	if res.entry == "" {
		_ = e.appendBlock(functionID, fileID, toPosition(fnNode.StartPoint()), toPosition(fnNode.EndPoint()), 0, true, true, "normal")
		return
	}

	blockIndexByID := map[string]int{}
	for i := startBlockIndex; i < len(e.summary.Blocks); i++ {
		blockIndexByID[e.summary.Blocks[i].ID] = i
	}

	if idx, ok := blockIndexByID[res.entry]; ok {
		e.summary.Blocks[idx].IsEntry = true
	}
	for _, id := range res.exits {
		if idx, ok := blockIndexByID[id]; ok {
			e.summary.Blocks[idx].IsExit = true
		}
	}
	for i := 1; i < len(res.stmtNode); i++ {
		e.summary.StmtEdges = append(e.summary.StmtEdges, StmtEdgeFact{FromNodeID: res.stmtNode[i-1], ToNodeID: res.stmtNode[i]})
	}

	// Reachability analysis: mark blocks not reachable from entry as dead code.
	e.markDeadBlocks(functionID, startBlockIndex, blockIndexByID, res.entry)
}

// markDeadBlocks performs a forward BFS from entryID and marks any blocks in
// this function that are not reachable as IsDead = true.
func (e *extractor) markDeadBlocks(functionID string, startBlockIndex int, blockIndexByID map[string]int, entryID string) {
	funcBlocks := make(map[string]bool, len(blockIndexByID))
	for id := range blockIndexByID {
		funcBlocks[id] = true
	}

	// Build forward adjacency from CFG edges scoped to this function.
	adjacency := make(map[string][]string)
	for _, edge := range e.summary.CFGEdges {
		if funcBlocks[edge.FromBlockID] {
			adjacency[edge.FromBlockID] = append(adjacency[edge.FromBlockID], edge.ToBlockID)
		}
	}

	reachable := make(map[string]bool)
	queue := []string{entryID}
	reachable[entryID] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[cur] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}

	for i := startBlockIndex; i < len(e.summary.Blocks); i++ {
		b := &e.summary.Blocks[i]
		if b.FunctionID == functionID && !reachable[b.ID] {
			b.IsDead = true
		}
	}
}

func (e *extractor) buildGoBlockStatements(functionID, fileID string, blockNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx cfgBuildCtx) cfgBuildResult {
	if blockNode == nil {
		return cfgBuildResult{}
	}
	return e.buildGoStmtList(functionID, fileID, namedChildren(blockNode), lang, source, ctx)
}

func (e *extractor) buildGoStmtList(functionID, fileID string, stmts []*gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx cfgBuildCtx) cfgBuildResult {
	var out cfgBuildResult
	var prevExits []string
	for _, st := range stmts {
		t := st.Type(lang)
		if t == "{" || t == "}" {
			continue
		}
		var res cfgBuildResult
		switch t {
		case "statement_list", "block":
			res = e.buildGoBlockStatements(functionID, fileID, st, lang, source, ctx)
		case "if_statement":
			res = e.buildGoIf(functionID, fileID, st, lang, source, ctx)
		case "for_statement", "range_clause":
			res = e.buildGoFor(functionID, fileID, st, lang, source, ctx)
		case "expression_switch_statement", "type_switch_statement":
			res = e.buildGoSwitch(functionID, fileID, st, lang, source, ctx)
		case "return_statement":
			b := e.appendBlock(functionID, fileID, toPosition(st.StartPoint()), toPosition(st.EndPoint()), 1, false, true, "return")
			nodeID := e.statementNodeFor(functionID, fileID, st, lang, source)
			// Empty exits: nothing can execute after a return.
			res = cfgBuildResult{entry: b, exits: nil, stmtNode: []string{nodeID}}
		case "break_statement":
			b := e.appendBlock(functionID, fileID, toPosition(st.StartPoint()), toPosition(st.EndPoint()), 1, false, false, "break")
			nodeID := e.statementNodeFor(functionID, fileID, st, lang, source)
			if ctx.breakExits != nil {
				*ctx.breakExits = append(*ctx.breakExits, b)
			}
			// Empty exits: control jumps to the enclosing loop/switch exit.
			res = cfgBuildResult{entry: b, exits: nil, stmtNode: []string{nodeID}}
		case "continue_statement":
			b := e.appendBlock(functionID, fileID, toPosition(st.StartPoint()), toPosition(st.EndPoint()), 1, false, false, "continue")
			nodeID := e.statementNodeFor(functionID, fileID, st, lang, source)
			if ctx.loopHead != "" {
				e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: b, ToBlockID: ctx.loopHead, Condition: "continue"})
			}
			// Empty exits: control jumps back to the loop header.
			res = cfgBuildResult{entry: b, exits: nil, stmtNode: []string{nodeID}}
		default:
			b := e.appendBlock(functionID, fileID, toPosition(st.StartPoint()), toPosition(st.EndPoint()), 1, false, false, "normal")
			nodeID := e.statementNodeFor(functionID, fileID, st, lang, source)
			res = cfgBuildResult{entry: b, exits: []string{b}, stmtNode: []string{nodeID}}
		}
		if res.entry == "" {
			continue
		}
		if out.entry == "" {
			out.entry = res.entry
		}
		for _, p := range prevExits {
			e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: p, ToBlockID: res.entry, Condition: "unconditional"})
		}
		prevExits = res.exits
		out.stmtNode = append(out.stmtNode, res.stmtNode...)
	}
	out.exits = prevExits
	return out
}

func (e *extractor) buildGoIf(functionID, fileID string, ifNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx cfgBuildCtx) cfgBuildResult {
	cond := e.appendBlock(functionID, fileID, toPosition(ifNode.StartPoint()), toPosition(ifNode.StartPoint()), 1, false, false, "normal")
	stmtNodes := []string{e.statementNode(functionID, fileID, "if_condition", toPosition(ifNode.StartPoint()), toPosition(ifNode.StartPoint()))}
	cons := ifNode.ChildByFieldName("consequence", lang)
	thenRes := e.buildGoBlockStatements(functionID, fileID, cons, lang, source, ctx)
	if thenRes.entry == "" {
		thenRes.entry = cond
		thenRes.exits = []string{cond}
	}
	e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: cond, ToBlockID: thenRes.entry, Condition: "true"})
	exits := append([]string{}, thenRes.exits...)
	stmtNodes = append(stmtNodes, thenRes.stmtNode...)
	alt := ifNode.ChildByFieldName("alternative", lang)
	if alt != nil {
		var elseRes cfgBuildResult
		if alt.Type(lang) == "if_statement" {
			elseRes = e.buildGoIf(functionID, fileID, alt, lang, source, ctx)
		} else {
			elseRes = e.buildGoBlockStatements(functionID, fileID, alt, lang, source, ctx)
		}
		if elseRes.entry != "" {
			e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: cond, ToBlockID: elseRes.entry, Condition: "false"})
			exits = append(exits, elseRes.exits...)
			stmtNodes = append(stmtNodes, elseRes.stmtNode...)
		} else {
			exits = append(exits, cond)
		}
	} else {
		exits = append(exits, cond)
	}
	return cfgBuildResult{entry: cond, exits: exits, stmtNode: stmtNodes}
}

func (e *extractor) buildGoFor(functionID, fileID string, forNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx cfgBuildCtx) cfgBuildResult {
	head := e.appendBlock(functionID, fileID, toPosition(forNode.StartPoint()), toPosition(forNode.StartPoint()), 1, false, false, "normal")
	stmtNodes := []string{e.statementNode(functionID, fileID, "for_header", toPosition(forNode.StartPoint()), toPosition(forNode.StartPoint()))}

	// Create a new context scoped to this loop. break exits the loop; continue
	// jumps back to the loop header.
	var loopBreaks []string
	loopCtx := cfgBuildCtx{loopHead: head, breakExits: &loopBreaks}

	body := forNode.ChildByFieldName("body", lang)
	bodyRes := e.buildGoBlockStatements(functionID, fileID, body, lang, source, loopCtx)
	if bodyRes.entry != "" {
		e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: head, ToBlockID: bodyRes.entry, Condition: "true"})
		for _, ex := range bodyRes.exits {
			e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: ex, ToBlockID: head, Condition: "unconditional"})
		}
		stmtNodes = append(stmtNodes, bodyRes.stmtNode...)
	}

	// Loop exits: the head block (condition false) plus any explicit break exits.
	exits := append([]string{head}, loopBreaks...)
	return cfgBuildResult{entry: head, exits: exits, stmtNode: stmtNodes}
}

// buildGoSwitch handles expression_switch_statement and type_switch_statement.
// Each case clause becomes a conditional branch from the switch header.
func (e *extractor) buildGoSwitch(functionID, fileID string, switchNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx cfgBuildCtx) cfgBuildResult {
	header := e.appendBlock(functionID, fileID, toPosition(switchNode.StartPoint()), toPosition(switchNode.StartPoint()), 1, false, false, "switch_header")
	stmtNodes := []string{e.statementNode(functionID, fileID, "switch_expr", toPosition(switchNode.StartPoint()), toPosition(switchNode.StartPoint()))}

	// break inside a switch case exits to after the switch, not an outer loop.
	var switchBreaks []string
	switchCtx := cfgBuildCtx{
		loopHead:   ctx.loopHead, // preserve outer loop head for continue
		breakExits: &switchBreaks,
	}

	var allExits []string
	hasDefault := false
	caseNum := 0

	for _, child := range namedChildren(switchNode) {
		childType := child.Type(lang)
		var isDefault bool

		switch childType {
		case "expression_case", "type_case":
			isDefault = false
		case "default_case":
			isDefault = true
			hasDefault = true
		default:
			continue
		}

		// The body statements live inside the statement_list child of the case node.
		var caseBodyStmts []*gotreesitter.Node
		for _, cc := range namedChildren(child) {
			ccType := cc.Type(lang)
			if ccType == "expression_list" || ccType == "type_list" {
				// These are the case value expressions, not body statements.
				continue
			}
			if ccType == "statement_list" {
				caseBodyStmts = append(caseBodyStmts, namedChildren(cc)...)
			} else {
				caseBodyStmts = append(caseBodyStmts, cc)
			}
		}

		// Create a block for the case condition check.
		caseBlock := e.appendBlock(functionID, fileID, toPosition(child.StartPoint()), toPosition(child.StartPoint()), 1, false, false, "case")

		var condition string
		if isDefault {
			condition = "default"
		} else {
			caseNum++
			condition = fmt.Sprintf("case_%d", caseNum)
		}
		e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: header, ToBlockID: caseBlock, Condition: condition})
		stmtNodes = append(stmtNodes, e.statementNode(functionID, fileID, "case_header", toPosition(child.StartPoint()), toPosition(child.StartPoint())))

		if len(caseBodyStmts) > 0 {
			bodyRes := e.buildGoStmtList(functionID, fileID, caseBodyStmts, lang, source, switchCtx)
			if bodyRes.entry != "" {
				e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: caseBlock, ToBlockID: bodyRes.entry, Condition: "unconditional"})
				allExits = append(allExits, bodyRes.exits...)
				stmtNodes = append(stmtNodes, bodyRes.stmtNode...)
			} else {
				allExits = append(allExits, caseBlock)
			}
		} else {
			// Empty case body falls through to after the switch.
			allExits = append(allExits, caseBlock)
		}
	}

	// Collect explicit break exits from inside case bodies.
	allExits = append(allExits, switchBreaks...)

	// When there is no default clause the switch header is also an exit
	// (no case matched → execution falls through to after the switch).
	if !hasDefault {
		allExits = append(allExits, header)
	}

	return cfgBuildResult{entry: header, exits: allExits, stmtNode: stmtNodes}
}

func (e *extractor) appendBlock(functionID, fileID string, start Position, end Position, stmtCount int, isEntry bool, isExit bool, kind string) string {
	id := e.nextBlockID(functionID)
	if kind == "" {
		kind = "normal"
	}
	e.summary.Blocks = append(e.summary.Blocks, BlockFact{ID: id, FunctionID: functionID, FileID: fileID, BlockIndex: e.blockSeq[functionID] - 1, StartLine: start.Line, StartColumn: start.Column, EndLine: end.Line, EndColumn: end.Column, StmtCount: stmtCount, IsEntry: isEntry, IsExit: isExit, BlockKind: kind})
	return id
}

func (e *extractor) statementNode(functionID, fileID, kind string, start, end Position) string {
	id := e.nextStatementID(functionID, kind)
	e.summary.Statements = append(e.summary.Statements, StatementFact{ID: id, FunctionID: functionID, FileID: fileID, Kind: kind, Start: start, End: end})
	return id
}

func (e *extractor) statementNodeFor(functionID, fileID string, st *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	start := toPosition(st.StartPoint())
	end := toPosition(st.EndPoint())
	text := strings.TrimSpace(st.Text(source))
	kind := st.Type(lang)
	if strings.HasPrefix(kind, "expression") && strings.Contains(text, "(") {
		kind = "statement"
	}
	return e.statementNode(functionID, fileID, kind, start, end)
}
