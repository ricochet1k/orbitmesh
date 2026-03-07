package codeflowmvp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func (jsScanAdapter) extractFile(e *extractor, rootPath string, filePath string) error {
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

	pkgDir := filepath.ToSlash(filepath.Dir(fileID))
	if pkgDir == "." {
		pkgDir = ""
	}
	pkgID := "js:" + pkgDir
	if pkgDir == "" {
		pkgID = "js:root"
	}

	e.summary.Files = append(e.summary.Files, FileFact{
		ID:        fileID,
		Path:      fileID,
		PackageID: pkgID,
	})
	e.seenPackages[pkgID] = PackageFact{ID: pkgID, Name: "js", Dir: pkgDir}

	e.walkJSNode(root, lang, source, fileID, scanState{})
	return nil
}

func (e *extractor) walkJSNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, state scanState) {
	if node == nil {
		return
	}

	nextState := state
	nodeType := node.Type(lang)
	if nodeType == "function_declaration" || nodeType == "method_definition" || nodeType == "arrow_function" || nodeType == "function" {
		nextState.currentFunctionID = jsFunctionID(fileID, node, lang, source, state.currentFunctionID)
	}

	if nodeType == "call_expression" {
		e.extractJSFetch(node, lang, source, fileID, state.currentFunctionID)
	}

	for _, child := range node.Children() {
		e.walkJSNode(child, lang, source, fileID, nextState)
	}
}

func (e *extractor) extractJSFetch(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, callerID string) {
	calleeNode := node.ChildByFieldName("function", lang)
	if calleeNode == nil {
		return
	}
	callee := strings.TrimSpace(calleeNode.Text(source))
	if !isFetchCallee(callee) {
		return
	}

	argsNode := node.ChildByFieldName("arguments", lang)
	if argsNode == nil {
		return
	}
	args := namedChildren(argsNode)
	if len(args) == 0 {
		return
	}

	rawPath, ok := parseStringLiteral(strings.TrimSpace(args[0].Text(source)))
	if !ok {
		return
	}

	method := "GET"
	if len(args) > 1 {
		if parsedMethod := jsFetchMethodFromOptions(args[1], lang, source); parsedMethod != "" {
			method = parsedMethod
		}
	}
	method = normalizeHTTPMethod(method)
	normalizedPath := normalizeAPIPath(rawPath)
	if normalizedPath == "" {
		return
	}

	requestID := e.nextAPIRequestID(fileID, method, normalizedPath)
	e.summary.APIReqs = append(e.summary.APIReqs, APIRequestFact{
		ID:             requestID,
		FileID:         fileID,
		CallerID:       callerID,
		Method:         method,
		Path:           rawPath,
		NormalizedPath: normalizedPath,
		Start:          toPosition(node.StartPoint()),
		End:            toPosition(node.EndPoint()),
	})
}

func jsFunctionID(fileID string, node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, parent string) string {
	if node == nil {
		return parent
	}

	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		nameNode = node.ChildByFieldName("property", lang)
	}
	if nameNode == nil {
		nameNode = findFirstNamedChildByType(node, lang, "identifier", "property_identifier")
	}
	if nameNode == nil {
		return parent
	}

	name := strings.TrimSpace(nameNode.Text(source))
	if name == "" {
		return parent
	}
	return fmt.Sprintf("js:%s:%s", fileID, name)
}

func jsFetchMethodFromOptions(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Type(lang) != "object" {
		return ""
	}
	for _, child := range node.Children() {
		if child.Type(lang) != "pair" {
			continue
		}
		key := child.ChildByFieldName("key", lang)
		value := child.ChildByFieldName("value", lang)
		if key == nil || value == nil {
			continue
		}
		keyText := strings.TrimSpace(key.Text(source))
		keyText = strings.Trim(keyText, "\"'")
		if strings.ToLower(keyText) != "method" {
			continue
		}
		if method, ok := parseStringLiteral(strings.TrimSpace(value.Text(source))); ok {
			return method
		}
	}
	return ""
}

func isFetchCallee(callee string) bool {
	callee = strings.TrimSpace(callee)
	switch callee {
	case "fetch", "window.fetch", "globalThis.fetch":
		return true
	default:
		return false
	}
}

func namedChildren(node *gotreesitter.Node) []*gotreesitter.Node {
	children := node.Children()
	if len(children) == 0 {
		return nil
	}
	out := make([]*gotreesitter.Node, 0, len(children))
	for _, child := range children {
		if child.IsNamed() {
			out = append(out, child)
		}
	}
	return out
}

func (e *extractor) nextAPIRequestID(fileID string, method string, normalizedPath string) string {
	key := fileID + "|" + method + "|" + normalizedPath
	e.apiReqSeq[key]++
	return fmt.Sprintf("%s#request:%s:%s:%d", fileID, strings.ToLower(method), normalizedPath, e.apiReqSeq[key])
}
