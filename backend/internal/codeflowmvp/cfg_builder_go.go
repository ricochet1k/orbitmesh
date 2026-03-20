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
	res := e.buildGoBlockStatements(functionID, fileID, bodyNode, lang, source)
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
}

func (e *extractor) buildGoBlockStatements(functionID, fileID string, blockNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte) cfgBuildResult {
	if blockNode == nil {
		return cfgBuildResult{}
	}
	kids := namedChildren(blockNode)
	var out cfgBuildResult
	var prevExits []string
	for _, st := range kids {
		t := st.Type(lang)
		if t == "{" || t == "}" {
			continue
		}
		var res cfgBuildResult
		switch t {
		case "statement_list", "block":
			res = e.buildGoBlockStatements(functionID, fileID, st, lang, source)
		case "if_statement":
			res = e.buildGoIf(functionID, fileID, st, lang, source)
		case "for_statement", "range_clause":
			res = e.buildGoFor(functionID, fileID, st, lang, source)
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

func (e *extractor) buildGoIf(functionID, fileID string, ifNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte) cfgBuildResult {
	cond := e.appendBlock(functionID, fileID, toPosition(ifNode.StartPoint()), toPosition(ifNode.StartPoint()), 1, false, false, "normal")
	stmtNodes := []string{e.statementNode(functionID, fileID, "if_condition", toPosition(ifNode.StartPoint()), toPosition(ifNode.StartPoint()))}
	cons := ifNode.ChildByFieldName("consequence", lang)
	thenRes := e.buildGoBlockStatements(functionID, fileID, cons, lang, source)
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
			elseRes = e.buildGoIf(functionID, fileID, alt, lang, source)
		} else {
			elseRes = e.buildGoBlockStatements(functionID, fileID, alt, lang, source)
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

func (e *extractor) buildGoFor(functionID, fileID string, forNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte) cfgBuildResult {
	head := e.appendBlock(functionID, fileID, toPosition(forNode.StartPoint()), toPosition(forNode.StartPoint()), 1, false, false, "normal")
	stmtNodes := []string{e.statementNode(functionID, fileID, "for_header", toPosition(forNode.StartPoint()), toPosition(forNode.StartPoint()))}
	body := forNode.ChildByFieldName("body", lang)
	bodyRes := e.buildGoBlockStatements(functionID, fileID, body, lang, source)
	if bodyRes.entry != "" {
		e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: head, ToBlockID: bodyRes.entry, Condition: "true"})
		for _, ex := range bodyRes.exits {
			e.summary.CFGEdges = append(e.summary.CFGEdges, CFGEdgeFact{FromBlockID: ex, ToBlockID: head, Condition: "unconditional"})
		}
		stmtNodes = append(stmtNodes, bodyRes.stmtNode...)
	}
	return cfgBuildResult{entry: head, exits: []string{head}, stmtNode: stmtNodes}
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
