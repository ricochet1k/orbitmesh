package codeflowmvp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var goPackageLinePattern = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
var goRouteCallPattern = regexp.MustCompile(`\.(Get|Post|Put|Delete|Patch)\s*\(\s*"([^"]+)"\s*,\s*([^\n\r\)]+)\)`)

func (goScanAdapter) extractFile(e *extractor, rootPath string, filePath string) error {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %q: %w", filePath, err)
	}

	relPath, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		relPath = filePath
	}
	fileID := filepath.ToSlash(relPath)

	fallbackPkgName, fallbackPkgID := goPackageIdentity(fileID, source)

	boundTree, err := grammars.ParseFile(filePath, source)
	if err != nil {
		if fallbackPkgName != "" {
			routes := extractGoAPIHandlersFallback(e, fileID, fallbackPkgID, source)
			if len(routes) > 0 {
				e.summary.Files = append(e.summary.Files, FileFact{ID: fileID, Path: fileID, PackageID: fallbackPkgID})
				pkgDir := filepath.ToSlash(filepath.Dir(fileID))
				if pkgDir == "." {
					pkgDir = ""
				}
				e.seenPackages[fallbackPkgID] = PackageFact{ID: fallbackPkgID, Name: fallbackPkgName, Dir: pkgDir}
				e.summary.APIRoutes = append(e.summary.APIRoutes, routes...)
				return nil
			}
		}
		return fmt.Errorf("parse %q: %w", filePath, err)
	}
	defer boundTree.Release()

	lang := boundTree.Language()
	root := boundTree.RootNode()

	pkgNode := findFirstNodeByType(root, lang, "package_clause")
	if pkgNode == nil {
		if fallbackPkgName != "" {
			routes := extractGoAPIHandlersFallback(e, fileID, fallbackPkgID, source)
			if len(routes) > 0 {
				e.summary.Files = append(e.summary.Files, FileFact{ID: fileID, Path: fileID, PackageID: fallbackPkgID})
				pkgDir := filepath.ToSlash(filepath.Dir(fileID))
				if pkgDir == "." {
					pkgDir = ""
				}
				e.seenPackages[fallbackPkgID] = PackageFact{ID: fallbackPkgID, Name: fallbackPkgName, Dir: pkgDir}
				e.summary.APIRoutes = append(e.summary.APIRoutes, routes...)
				return nil
			}
		}
		return fmt.Errorf("file %q missing package clause", filePath)
	}

	pkgNameNode := pkgNode.ChildByFieldName("name", lang)
	if pkgNameNode == nil {
		pkgNameNode = findFirstNamedChildByType(pkgNode, lang, "package_identifier", "identifier")
	}
	if pkgNameNode == nil {
		if fallbackPkgName != "" {
			routes := extractGoAPIHandlersFallback(e, fileID, fallbackPkgID, source)
			if len(routes) > 0 {
				e.summary.Files = append(e.summary.Files, FileFact{ID: fileID, Path: fileID, PackageID: fallbackPkgID})
				pkgDir := filepath.ToSlash(filepath.Dir(fileID))
				if pkgDir == "." {
					pkgDir = ""
				}
				e.seenPackages[fallbackPkgID] = PackageFact{ID: fallbackPkgID, Name: fallbackPkgName, Dir: pkgDir}
				e.summary.APIRoutes = append(e.summary.APIRoutes, routes...)
				return nil
			}
		}
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

	e.walkGoNode(root, lang, source, fileID, pkgID, scanState{})
	return nil
}

func goPackageIdentity(fileID string, source []byte) (string, string) {
	match := goPackageLinePattern.FindSubmatch(source)
	if len(match) < 2 {
		return "", ""
	}
	pkgName := strings.TrimSpace(string(match[1]))
	if pkgName == "" {
		return "", ""
	}
	pkgDir := filepath.ToSlash(filepath.Dir(fileID))
	if pkgDir == "." {
		pkgDir = ""
	}
	pkgID := fmt.Sprintf("%s:%s", pkgDir, pkgName)
	if pkgDir == "" {
		pkgID = ":" + pkgName
	}
	return pkgName, pkgID
}

func extractGoAPIHandlersFallback(e *extractor, fileID string, pkgID string, source []byte) []APIHandlerFact {
	matches := goRouteCallPattern.FindAllSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return nil
	}

	routes := make([]APIHandlerFact, 0, len(matches))
	for _, idx := range matches {
		if len(idx) < 8 {
			continue
		}
		method := normalizeHTTPMethod(string(source[idx[2]:idx[3]]))
		rawPath := string(source[idx[4]:idx[5]])
		handlerExpr := strings.TrimSpace(string(source[idx[6]:idx[7]]))
		normalizedPath := normalizeAPIPath(rawPath)
		if method == "" || normalizedPath == "" || handlerExpr == "" {
			continue
		}

		start := byteIndexToPosition(source, idx[0])
		end := byteIndexToPosition(source, idx[1])
		routeID := e.nextAPIHandlerID(fileID, method, normalizedPath)
		routes = append(routes, APIHandlerFact{
			ID:             routeID,
			FileID:         fileID,
			PackageID:      pkgID,
			HandlerExpr:    handlerExpr,
			Method:         method,
			Path:           rawPath,
			NormalizedPath: normalizedPath,
			Start:          start,
			End:            end,
		})
	}

	return routes
}

func byteIndexToPosition(source []byte, index int) Position {
	if index < 0 {
		index = 0
	}
	if index > len(source) {
		index = len(source)
	}
	line := 1
	col := 1
	for i := 0; i < index; i++ {
		if source[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return Position{Line: line, Column: col}
}

func (e *extractor) walkGoNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, pkgID string, state scanState) {
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
			if bodyNode := node.ChildByFieldName("body", lang); bodyNode != nil {
				e.buildGoCFGForFunction(node, bodyNode, lang, source, fileID, functionID)
			}
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
			if bodyNode := node.ChildByFieldName("body", lang); bodyNode != nil {
				e.buildGoCFGForFunction(node, bodyNode, lang, source, fileID, methodID)
			}
			nextState.currentFunctionID = methodID
		}
	case "call_expression":
		e.extractGoAPIHandler(node, lang, source, fileID, pkgID)
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
		e.walkGoNode(child, lang, source, fileID, pkgID, nextState)
	}
}

func (e *extractor) extractGoAPIHandler(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, pkgID string) {
	calleeNode := node.ChildByFieldName("function", lang)
	if calleeNode == nil {
		return
	}
	callee := strings.TrimSpace(calleeNode.Text(source))

	// Check for chi sub-router: r.Route("/prefix", func(sub chi.Router) { ... })
	if isRouteGroupCallee(callee) {
		e.extractGoRouteGroup(node, lang, source, fileID, pkgID)
		return
	}

	method := goRouteMethodFromCallee(callee)
	if method == "" {
		return
	}

	argsNode := node.ChildByFieldName("arguments", lang)
	if argsNode == nil {
		return
	}
	args := namedChildren(argsNode)
	if len(args) < 2 {
		return
	}

	rawPath, ok := parseStringLiteral(strings.TrimSpace(args[0].Text(source)))
	if !ok {
		return
	}
	handlerExpr := strings.TrimSpace(args[1].Text(source))
	normalizedPath := normalizeAPIPath(rawPath)
	if normalizedPath == "" || handlerExpr == "" {
		return
	}

	// Attempt to resolve the handler expression to a function ID.
	functionID := e.resolveGoHandlerFunctionID(handlerExpr, pkgID)

	apiHandlerID := e.nextAPIHandlerID(fileID, method, normalizedPath)
	e.summary.APIRoutes = append(e.summary.APIRoutes, APIHandlerFact{
		ID:             apiHandlerID,
		FileID:         fileID,
		PackageID:      pkgID,
		FunctionID:     functionID,
		HandlerExpr:    handlerExpr,
		Method:         method,
		Path:           rawPath,
		NormalizedPath: normalizedPath,
		Start:          toPosition(node.StartPoint()),
		End:            toPosition(node.EndPoint()),
	})
}

// isRouteGroupCallee checks if a callee expression is a chi Route/Group call.
func isRouteGroupCallee(callee string) bool {
	parts := strings.Split(callee, ".")
	if len(parts) == 0 {
		return false
	}
	switch parts[len(parts)-1] {
	case "Route", "Group":
		return true
	default:
		return false
	}
}

// extractGoRouteGroup handles chi sub-router patterns like:
//   r.Route("/api/v1", func(sub chi.Router) { sub.Get("/providers", h.listProviders) })
// It extracts routes from the callback function with the prefix prepended.
func (e *extractor) extractGoRouteGroup(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, pkgID string) {
	argsNode := node.ChildByFieldName("arguments", lang)
	if argsNode == nil {
		return
	}
	args := namedChildren(argsNode)
	if len(args) < 2 {
		return
	}

	// First arg is the path prefix
	prefix, ok := parseStringLiteral(strings.TrimSpace(args[0].Text(source)))
	if !ok {
		prefix = ""
	}

	// Second arg is the callback function — walk it to find sub-routes
	fnNode := args[1]
	if fnNode.Type(lang) != "func_literal" {
		return
	}

	bodyNode := fnNode.ChildByFieldName("body", lang)
	if bodyNode == nil {
		return
	}

	// Walk the callback body looking for route registrations
	e.walkGoRouteGroupBody(bodyNode, lang, source, fileID, pkgID, prefix)
}

// walkGoRouteGroupBody walks a chi router callback body to extract sub-routes.
func (e *extractor) walkGoRouteGroupBody(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, pkgID string, prefix string) {
	if node == nil {
		return
	}

	if node.Type(lang) == "call_expression" {
		calleeNode := node.ChildByFieldName("function", lang)
		if calleeNode != nil {
			callee := strings.TrimSpace(calleeNode.Text(source))

			// Recursive sub-route group
			if isRouteGroupCallee(callee) {
				argsNode := node.ChildByFieldName("arguments", lang)
				if argsNode != nil {
					args := namedChildren(argsNode)
					if len(args) >= 2 {
						subPrefix, ok := parseStringLiteral(strings.TrimSpace(args[0].Text(source)))
						if ok {
							fnNode := args[1]
							if fnNode.Type(lang) == "func_literal" {
								bodyNode := fnNode.ChildByFieldName("body", lang)
								if bodyNode != nil {
									e.walkGoRouteGroupBody(bodyNode, lang, source, fileID, pkgID, prefix+subPrefix)
								}
							}
						}
					}
				}
				return
			}

			method := goRouteMethodFromCallee(callee)
			if method != "" {
				argsNode := node.ChildByFieldName("arguments", lang)
				if argsNode != nil {
					args := namedChildren(argsNode)
					if len(args) >= 2 {
						rawPath, ok := parseStringLiteral(strings.TrimSpace(args[0].Text(source)))
						if ok {
							fullPath := prefix + rawPath
							handlerExpr := strings.TrimSpace(args[1].Text(source))
							normalizedPath := normalizeAPIPath(fullPath)
							if normalizedPath != "" && handlerExpr != "" {
								functionID := e.resolveGoHandlerFunctionID(handlerExpr, pkgID)
								apiHandlerID := e.nextAPIHandlerID(fileID, method, normalizedPath)
								e.summary.APIRoutes = append(e.summary.APIRoutes, APIHandlerFact{
									ID:             apiHandlerID,
									FileID:         fileID,
									PackageID:      pkgID,
									FunctionID:     functionID,
									HandlerExpr:    handlerExpr,
									Method:         method,
									Path:           fullPath,
									NormalizedPath: normalizedPath,
									Start:          toPosition(node.StartPoint()),
									End:            toPosition(node.EndPoint()),
								})
							}
						}
					}
				}
				return
			}
		}
	}

	for _, child := range node.Children() {
		e.walkGoRouteGroupBody(child, lang, source, fileID, pkgID, prefix)
	}
}

// resolveGoHandlerFunctionID attempts to resolve a handler expression like "h.listSessions"
// to a function ID like "backend/internal/api:api.(Handler).listSessions".
func (e *extractor) resolveGoHandlerFunctionID(handlerExpr string, pkgID string) string {
	// Handle method value expressions: h.listSessions, handler.Mount
	parts := strings.Split(handlerExpr, ".")
	if len(parts) == 2 {
		methodName := parts[1]
		// Search known functions in the same package for a matching method
		for _, fn := range e.summary.Functions {
			if fn.PackageID != pkgID {
				continue
			}
			if fn.Name == methodName && fn.Receiver != "" {
				return fn.ID
			}
		}
	}

	// Handle bare function references: listSessions
	if len(parts) == 1 && !strings.Contains(handlerExpr, "(") {
		for _, fn := range e.summary.Functions {
			if fn.PackageID != pkgID {
				continue
			}
			if fn.Name == handlerExpr {
				return fn.ID
			}
		}
	}

	return ""
}

func goRouteMethodFromCallee(callee string) string {
	if callee == "" {
		return ""
	}
	parts := strings.Split(callee, ".")
	if len(parts) == 0 {
		return ""
	}
	switch parts[len(parts)-1] {
	case "Get", "Post", "Put", "Delete", "Patch":
		return normalizeHTTPMethod(parts[len(parts)-1])
	default:
		return ""
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

func (e *extractor) nextAPIHandlerID(fileID string, method string, normalizedPath string) string {
	key := fileID + "|" + method + "|" + normalizedPath
	e.apiRouteSeq[key]++
	return fmt.Sprintf("%s#route:%s:%s:%d", fileID, strings.ToLower(method), normalizedPath, e.apiRouteSeq[key])
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

// resolveAPIHandlerFunctionIDs performs a post-extraction pass to resolve
// handler function IDs that couldn't be resolved during extraction.
// This handles cases where the handler method is in a different file
// within the same package, or was extracted after the route registration.
func (e *extractor) resolveAPIHandlerFunctionIDs() {
	// Build lookup: pkgID -> methodName -> functionID (for methods with receivers)
	// Also: pkgID -> funcName -> functionID (for bare functions)
	type methodKey struct {
		pkgID      string
		methodName string
	}

	methodIndex := make(map[methodKey]string, len(e.summary.Functions))
	funcIndex := make(map[methodKey]string, len(e.summary.Functions))

	for _, fn := range e.summary.Functions {
		if fn.Receiver != "" {
			methodIndex[methodKey{fn.PackageID, fn.Name}] = fn.ID
		}
		funcIndex[methodKey{fn.PackageID, fn.Name}] = fn.ID
	}

	for i := range e.summary.APIRoutes {
		route := &e.summary.APIRoutes[i]
		if route.FunctionID != "" {
			continue // Already resolved
		}

		handlerExpr := route.HandlerExpr
		parts := strings.Split(handlerExpr, ".")
		pkgID := route.PackageID

		// Method value: h.listSessions
		if len(parts) == 2 {
			methodName := parts[1]
			if id, ok := methodIndex[methodKey{pkgID, methodName}]; ok {
				route.FunctionID = id
				continue
			}
			// Fallback to any function with that name
			if id, ok := funcIndex[methodKey{pkgID, methodName}]; ok {
				route.FunctionID = id
			}
			continue
		}

		// Bare function reference: listSessions
		if len(parts) == 1 && !strings.Contains(handlerExpr, "(") {
			if id, ok := funcIndex[methodKey{pkgID, handlerExpr}]; ok {
				route.FunctionID = id
			}
		}
	}
}

func toPosition(point gotreesitter.Point) Position {
	return Position{Line: int(point.Row) + 1, Column: int(point.Column) + 1}
}
