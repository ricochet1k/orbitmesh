package codeflowmvp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/ricochet1k/orbitmesh/internal/codeflowmvp/rules"
)

type scanState struct {
	currentFunctionID string
	loopDepth         int
}

type extractor struct {
	summary      ExtractionSummary
	callSeq      map[string]int
	spawnSeq     map[string]int
	seenPackages map[string]PackageFact
}

func ScanPath(path string) (ExtractionSummary, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ExtractionSummary{}, fmt.Errorf("resolve path %q: %w", path, err)
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return ExtractionSummary{}, fmt.Errorf("stat %q: %w", path, err)
	}

	goFiles := make([]string, 0)
	scanRoot := absPath
	if stat.IsDir() {
		err = filepath.WalkDir(absPath, func(curr string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(d.Name(), ".go") {
				goFiles = append(goFiles, curr)
			}
			return nil
		})
		if err != nil {
			return ExtractionSummary{}, fmt.Errorf("walk %q: %w", path, err)
		}
	} else if strings.HasSuffix(strings.ToLower(stat.Name()), ".go") {
		goFiles = append(goFiles, absPath)
		scanRoot = filepath.Dir(absPath)
	} else {
		return ExtractionSummary{}, fmt.Errorf("path %q is not a directory or .go file", path)
	}

	sort.Strings(goFiles)

	ext := &extractor{
		summary:      ExtractionSummary{},
		callSeq:      map[string]int{},
		spawnSeq:     map[string]int{},
		seenPackages: map[string]PackageFact{},
	}

	for _, filePath := range goFiles {
		if err := ext.extractFile(scanRoot, filePath); err != nil {
			return ExtractionSummary{}, err
		}
	}

	for _, pkg := range ext.seenPackages {
		ext.summary.Packages = append(ext.summary.Packages, pkg)
	}
	sort.Slice(ext.summary.Packages, func(i, j int) bool {
		return ext.summary.Packages[i].ID < ext.summary.Packages[j].ID
	})

	ruleFacts := rules.Facts{
		Functions: make([]rules.FunctionFact, 0, len(ext.summary.Functions)),
		Calls:     make([]rules.CallFact, 0, len(ext.summary.Calls)),
		Spawns:    make([]rules.SpawnFact, 0, len(ext.summary.Spawns)),
	}
	for _, fn := range ext.summary.Functions {
		ruleFacts.Functions = append(ruleFacts.Functions, rules.FunctionFact{
			ID:     fn.ID,
			FileID: fn.FileID,
		})
	}
	for _, spawn := range ext.summary.Spawns {
		ruleFacts.Spawns = append(ruleFacts.Spawns, rules.SpawnFact{
			ID:         spawn.ID,
			CallerID:   spawn.CallerID,
			CalleeExpr: spawn.CalleeExpr,
			InsideLoop: spawn.InsideLoop,
			Start:      rules.Position{Line: spawn.Start.Line, Column: spawn.Start.Column},
			End:        rules.Position{Line: spawn.End.Line, Column: spawn.End.Column},
		})
	}
	for _, call := range ext.summary.Calls {
		ruleFacts.Calls = append(ruleFacts.Calls, rules.CallFact{
			ID:         call.ID,
			CallerID:   call.CallerID,
			CalleeExpr: call.CalleeExpr,
			InsideLoop: call.InsideLoop,
			Start:      rules.Position{Line: call.Start.Line, Column: call.Start.Column},
			End:        rules.Position{Line: call.End.Line, Column: call.End.Column},
		})
	}
	ext.summary.Findings = rules.Evaluate(ruleFacts)

	ext.summary.Counts = Counts{
		Files:     len(ext.summary.Files),
		Packages:  len(ext.summary.Packages),
		Functions: len(ext.summary.Functions),
		Calls:     len(ext.summary.Calls),
		Spawns:    len(ext.summary.Spawns),
		Findings:  len(ext.summary.Findings),
	}

	return ext.summary, nil
}

func (e *extractor) extractFile(rootPath string, filePath string) error {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %q: %w", filePath, err)
	}

	boundTree, err := grammars.ParseFile(filePath, source)
	if err != nil {
		return fmt.Errorf("parse %q: %w", filePath, err)
	}
	defer boundTree.Release()

	lang := boundTree.Language()
	root := boundTree.RootNode()

	relPath, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		relPath = filePath
	}
	fileID := filepath.ToSlash(relPath)

	pkgNode := findFirstNodeByType(root, lang, "package_clause")
	if pkgNode == nil {
		return fmt.Errorf("file %q missing package clause", filePath)
	}

	pkgNameNode := pkgNode.ChildByFieldName("name", lang)
	if pkgNameNode == nil {
		pkgNameNode = findFirstNamedChildByType(pkgNode, lang, "package_identifier", "identifier")
	}
	if pkgNameNode == nil {
		return fmt.Errorf("file %q package clause missing name", filePath)
	}

	pkgName := strings.TrimSpace(pkgNameNode.Text(source))
	pkgDir := filepath.ToSlash(filepath.Dir(fileID))
	if pkgDir == "." {
		pkgDir = ""
	}
	pkgID := fmt.Sprintf("%s:%s", pkgDir, pkgName)
	if pkgDir == "" {
		pkgID = ":" + pkgName
	}

	e.summary.Files = append(e.summary.Files, FileFact{
		ID:        fileID,
		Path:      fileID,
		PackageID: pkgID,
	})
	e.seenPackages[pkgID] = PackageFact{ID: pkgID, Name: pkgName, Dir: pkgDir}

	e.walkNode(root, lang, source, fileID, pkgID, scanState{})
	return nil
}

func (e *extractor) walkNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, pkgID string, state scanState) {
	if node == nil {
		return
	}

	nodeType := node.Type(lang)
	nextState := state
	if nodeType == "for_statement" || nodeType == "range_clause" {
		nextState.loopDepth++
	}

	switch nodeType {
	case "function_declaration":
		nameNode := node.ChildByFieldName("name", lang)
		if nameNode == nil {
			nameNode = findFirstNamedChildByType(node, lang, "identifier")
		}
		if nameNode != nil {
			name := strings.TrimSpace(nameNode.Text(source))
			functionID := fmt.Sprintf("%s.%s", pkgID, name)
			e.summary.Functions = append(e.summary.Functions, FunctionFact{
				ID:        functionID,
				PackageID: pkgID,
				FileID:    fileID,
				Name:      name,
				Start:     toPosition(node.StartPoint()),
				End:       toPosition(node.EndPoint()),
			})
			nextState.currentFunctionID = functionID
		}
	case "method_declaration":
		nameNode := node.ChildByFieldName("name", lang)
		receiverNode := node.ChildByFieldName("receiver", lang)
		if nameNode == nil {
			nameNode = findFirstNamedChildByType(node, lang, "field_identifier", "identifier")
		}
		if receiverNode == nil {
			receiverNode = findFirstNamedChildByType(node, lang, "parameter_list")
		}
		if nameNode != nil {
			name := strings.TrimSpace(nameNode.Text(source))
			receiver := receiverType(receiverNode, lang, source)
			methodID := fmt.Sprintf("%s.(%s).%s", pkgID, receiver, name)
			e.summary.Functions = append(e.summary.Functions, FunctionFact{
				ID:        methodID,
				PackageID: pkgID,
				FileID:    fileID,
				Name:      name,
				Receiver:  receiver,
				Start:     toPosition(node.StartPoint()),
				End:       toPosition(node.EndPoint()),
			})
			nextState.currentFunctionID = methodID
		}
	case "call_expression":
		if state.currentFunctionID != "" {
			if parent := node.Parent(); parent == nil || parent.Type(lang) != "go_statement" {
				calleeNode := node.ChildByFieldName("function", lang)
				callee := strings.TrimSpace(node.Text(source))
				if calleeNode != nil {
					callee = strings.TrimSpace(calleeNode.Text(source))
				}
				id := e.nextCallID(state.currentFunctionID, callee)
				e.summary.Calls = append(e.summary.Calls, CallSiteFact{
					ID:         id,
					CallerID:   state.currentFunctionID,
					CalleeExpr: callee,
					InsideLoop: state.loopDepth > 0,
					Start:      toPosition(node.StartPoint()),
					End:        toPosition(node.EndPoint()),
				})
			}
		}
	case "go_statement":
		if state.currentFunctionID != "" {
			callNode := findFirstNodeByType(node, lang, "call_expression")
			if callNode != nil {
				calleeNode := callNode.ChildByFieldName("function", lang)
				callee := strings.TrimSpace(callNode.Text(source))
				if calleeNode != nil {
					callee = strings.TrimSpace(calleeNode.Text(source))
				}
				id := e.nextSpawnID(state.currentFunctionID, callee)
				e.summary.Spawns = append(e.summary.Spawns, SpawnSiteFact{
					ID:         id,
					CallerID:   state.currentFunctionID,
					CalleeExpr: callee,
					InsideLoop: state.loopDepth > 0,
					Start:      toPosition(node.StartPoint()),
					End:        toPosition(node.EndPoint()),
				})
			}
		}
	}

	for _, child := range node.Children() {
		e.walkNode(child, lang, source, fileID, pkgID, nextState)
	}
}

func (e *extractor) nextCallID(callerID string, callee string) string {
	key := callerID + "->" + callee
	e.callSeq[key]++
	return fmt.Sprintf("%s#call:%s:%d", callerID, callee, e.callSeq[key])
}

func (e *extractor) nextSpawnID(callerID string, callee string) string {
	key := callerID + "=>" + callee
	e.spawnSeq[key]++
	return fmt.Sprintf("%s#go:%s:%d", callerID, callee, e.spawnSeq[key])
}

func findFirstNodeByType(node *gotreesitter.Node, lang *gotreesitter.Language, nodeType string) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.Type(lang) == nodeType {
		return node
	}
	for _, child := range node.Children() {
		if match := findFirstNodeByType(child, lang, nodeType); match != nil {
			return match
		}
	}
	return nil
}

func findFirstNamedChildByType(node *gotreesitter.Node, lang *gotreesitter.Language, nodeTypes ...string) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	for _, child := range node.Children() {
		if !child.IsNamed() {
			continue
		}
		childType := child.Type(lang)
		for _, nodeType := range nodeTypes {
			if childType == nodeType {
				return child
			}
		}
	}
	return nil
}

func receiverType(receiverNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if receiverNode == nil {
		return "unknown"
	}
	param := findFirstNodeByType(receiverNode, lang, "parameter_declaration")
	if param == nil {
		return "unknown"
	}
	typeNode := param.ChildByFieldName("type", lang)
	if typeNode == nil {
		return "unknown"
	}
	raw := strings.TrimSpace(typeNode.Text(source))
	return normalizeType(raw)
}

func normalizeType(raw string) string {
	normalized := strings.ReplaceAll(raw, " ", "")
	for strings.HasPrefix(normalized, "*") {
		normalized = strings.TrimPrefix(normalized, "*")
	}
	if idx := strings.Index(normalized, "["); idx >= 0 {
		normalized = normalized[:idx]
	}
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func toPosition(point gotreesitter.Point) Position {
	return Position{Line: int(point.Row) + 1, Column: int(point.Column) + 1}
}
