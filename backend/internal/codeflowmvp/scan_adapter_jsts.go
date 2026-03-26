package codeflowmvp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// jsFileContext holds per-file state for JS/TS extraction, including
// resolved constants and import mappings needed for template literal resolution.
type jsFileContext struct {
	fileID   string
	pkgID    string
	constMap map[string]string // top-level const name -> resolved string value
}

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

	ctx := &jsFileContext{
		fileID:   fileID,
		pkgID:    pkgID,
		constMap: make(map[string]string),
	}

	// First pass: collect top-level const/variable declarations with string values.
	// This resolves patterns like `const BASE_URL = "/api"` so that template
	// literals like `${BASE_URL}/sessions` can be resolved to `/api/sessions`.
	e.collectJSConstants(root, lang, source, ctx)

	// Register this file's constants for cross-file import resolution.
	if len(ctx.constMap) > 0 {
		if e.jsConstRegistry == nil {
			e.jsConstRegistry = make(map[string]map[string]string)
		}
		e.jsConstRegistry[fileID] = make(map[string]string, len(ctx.constMap))
		for k, v := range ctx.constMap {
			e.jsConstRegistry[fileID][k] = v
		}
	}

	// Second pass: also pull in constants from known imports.
	// When we see `import { BASE_URL } from "./_base"`, and we've already
	// scanned that file, we can resolve the imported value.
	e.resolveJSImportedConstants(root, lang, source, ctx)

	e.walkJSNode(root, lang, source, fileID, ctx, scanState{})
	return nil
}

func (e *extractor) walkJSNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, ctx *jsFileContext, state scanState) {
	if node == nil {
		return
	}

	nextState := state
	nodeType := node.Type(lang)

	if nodeType == "for_statement" || nodeType == "for_in_statement" || nodeType == "while_statement" || nodeType == "do_statement" {
		nextState.loopDepth++
	}

	if nodeType == "function_declaration" || nodeType == "method_definition" || nodeType == "arrow_function" || nodeType == "function" {
		funcID := jsFunctionID(fileID, node, lang, source, state.currentFunctionID)
		nextState.currentFunctionID = funcID

		// Emit FunctionFact for named functions
		name := jsFunctionName(node, lang, source)
		if name != "" {
			exported := isJSExported(node, lang, source)
			e.summary.Functions = append(e.summary.Functions, FunctionFact{
				ID:        funcID,
				PackageID: ctx.pkgID,
				FileID:    fileID,
				Name:      name,
				Start:     toPosition(node.StartPoint()),
				End:       toPosition(node.EndPoint()),
			})
			_ = exported // will be used for metadata in future
		}
	}

	if nodeType == "call_expression" {
		e.extractJSFetch(node, lang, source, fileID, ctx, state.currentFunctionID)
	}

	for _, child := range node.Children() {
		e.walkJSNode(child, lang, source, fileID, ctx, nextState)
	}
}

func (e *extractor) extractJSFetch(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, fileID string, ctx *jsFileContext, callerID string) {
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

	rawPath := resolveJSStringExpression(args[0], lang, source, ctx)
	if rawPath == "" {
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
		CalleeExpr:     callee,
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

// collectJSConstants scans top-level variable declarations for const assignments
// with string literal values, populating ctx.constMap.
func (e *extractor) collectJSConstants(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx *jsFileContext) {
	for _, child := range root.Children() {
		nodeType := child.Type(lang)

		// Handle: export const BASE_URL = "/api";
		if nodeType == "export_statement" {
			for _, exportChild := range child.Children() {
				if exportChild.Type(lang) == "lexical_declaration" {
					collectConstFromDeclaration(exportChild, lang, source, ctx)
				}
			}
			continue
		}

		// Handle: const BASE_URL = "/api";
		if nodeType == "lexical_declaration" {
			collectConstFromDeclaration(child, lang, source, ctx)
		}
	}
}

func collectConstFromDeclaration(declNode *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx *jsFileContext) {
	// Check if this is a `const` declaration (not `let` or `var`)
	declText := strings.TrimSpace(declNode.Text(source))
	if !strings.HasPrefix(declText, "const ") {
		return
	}

	for _, child := range declNode.Children() {
		if child.Type(lang) != "variable_declarator" {
			continue
		}
		nameNode := child.ChildByFieldName("name", lang)
		valueNode := child.ChildByFieldName("value", lang)
		if nameNode == nil || valueNode == nil {
			continue
		}
		name := strings.TrimSpace(nameNode.Text(source))
		if name == "" {
			continue
		}
		// Only capture string literals as const values
		if val, ok := parseStringLiteral(strings.TrimSpace(valueNode.Text(source))); ok {
			ctx.constMap[name] = val
		}
	}
}

// resolveJSImportedConstants looks at import statements and resolves imported
// names from files we've already scanned (using e.jsConstRegistry).
func (e *extractor) resolveJSImportedConstants(root *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx *jsFileContext) {
	for _, child := range root.Children() {
		if child.Type(lang) != "import_statement" {
			continue
		}
		// Look for the import source (the "from" path)
		sourceNode := child.ChildByFieldName("source", lang)
		if sourceNode == nil {
			continue
		}
		importPath, ok := parseStringLiteral(strings.TrimSpace(sourceNode.Text(source)))
		if !ok || importPath == "" {
			continue
		}

		// Resolve the import path relative to this file's directory
		resolvedFileID := resolveJSImportPath(ctx.fileID, importPath)
		if resolvedFileID == "" {
			continue
		}

		// Find imported names from the named imports clause
		importClause := findFirstNamedChildByType(child, lang, "import_clause")
		if importClause == nil {
			continue
		}
		namedImports := findFirstNodeByType(importClause, lang, "named_imports")
		if namedImports == nil {
			continue
		}

		for _, specifier := range namedImports.Children() {
			if specifier.Type(lang) != "import_specifier" {
				continue
			}
			nameNode := specifier.ChildByFieldName("name", lang)
			if nameNode == nil {
				continue
			}
			importedName := strings.TrimSpace(nameNode.Text(source))
			if importedName == "" {
				continue
			}

			// Check if the alias is different
			aliasNode := specifier.ChildByFieldName("alias", lang)
			localName := importedName
			if aliasNode != nil {
				localName = strings.TrimSpace(aliasNode.Text(source))
			}

			// Look up the constant value from the registry
			if val, found := e.lookupJSExportedConst(resolvedFileID, importedName); found {
				ctx.constMap[localName] = val
			}
		}
	}
}

// resolveJSImportPath resolves a relative import path against the current file.
// e.g., fileID="frontend/src/api/sessions.ts", importPath="./_base"
// → "frontend/src/api/_base.ts" (tries .ts, .js, .tsx, .jsx, /index.ts)
func resolveJSImportPath(fileID string, importPath string) string {
	if !strings.HasPrefix(importPath, ".") {
		return "" // Only resolve relative imports
	}
	dir := filepath.ToSlash(filepath.Dir(fileID))
	resolved := filepath.ToSlash(filepath.Join(dir, importPath))
	// The resolved path doesn't have an extension; we need to find the matching file
	return resolved
}

// lookupJSExportedConst looks up a constant value exported from a previously scanned file.
func (e *extractor) lookupJSExportedConst(resolvedBasePath string, name string) (string, bool) {
	if e.jsConstRegistry == nil {
		return "", false
	}
	// Try common extensions
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		candidate := resolvedBasePath + ext
		if consts, ok := e.jsConstRegistry[candidate]; ok {
			if val, found := consts[name]; found {
				return val, true
			}
		}
	}
	// Try /index variants
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		candidate := resolvedBasePath + "/index" + ext
		if consts, ok := e.jsConstRegistry[candidate]; ok {
			if val, found := consts[name]; found {
				return val, true
			}
		}
	}
	return "", false
}

// resolveJSStringExpression resolves a JS expression node to a string value.
// It handles:
// - String literals: "foo", 'foo'
// - Template literals: `${BASE_URL}/sessions`
// - String concatenation: BASE_URL + "/sessions"
// - Variable references to known constants: url (when url = "/api/sessions")
func resolveJSStringExpression(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx *jsFileContext) string {
	if node == nil {
		return ""
	}

	nodeType := node.Type(lang)
	text := strings.TrimSpace(node.Text(source))

	// Simple string literal
	if val, ok := parseStringLiteral(text); ok {
		return val
	}

	// Template literal / template_string
	if nodeType == "template_string" {
		return resolveJSTemplateLiteral(node, lang, source, ctx)
	}

	// Binary expression (string concatenation)
	if nodeType == "binary_expression" {
		return resolveJSBinaryConcat(node, lang, source, ctx)
	}

	// Variable reference — look up in const map
	if nodeType == "identifier" {
		if val, ok := ctx.constMap[text]; ok {
			return val
		}
	}

	return ""
}

// resolveJSTemplateLiteral resolves a template_string node like `${BASE_URL}/sessions/${id}`.
// Known constants are substituted, unknown expressions become {param}.
func resolveJSTemplateLiteral(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx *jsFileContext) string {
	var parts []string

	for _, child := range node.Children() {
		childType := child.Type(lang)
		switch childType {
		case "string_fragment", "template_content":
			// Literal text portion of the template
			parts = append(parts, child.Text(source))
		case "template_substitution":
			// ${expression} — try to resolve it
			expr := findFirstNamedChild(child)
			if expr == nil {
				parts = append(parts, "{param}")
				continue
			}
			resolved := resolveJSStringExpression(expr, lang, source, ctx)
			if resolved != "" {
				parts = append(parts, resolved)
			} else {
				// Check if it's a simple identifier we know
				exprText := strings.TrimSpace(expr.Text(source))
				if val, ok := ctx.constMap[exprText]; ok {
					parts = append(parts, val)
				} else {
					parts = append(parts, "{param}")
				}
			}
		case "`":
			// Opening/closing backtick — skip
		default:
			// Other child types — try to resolve
			text := strings.TrimSpace(child.Text(source))
			if text != "" && text != "`" {
				parts = append(parts, text)
			}
		}
	}

	result := strings.Join(parts, "")
	if result == "" {
		return ""
	}
	return result
}

// resolveJSBinaryConcat handles string concatenation like BASE_URL + "/sessions".
func resolveJSBinaryConcat(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, ctx *jsFileContext) string {
	left := node.ChildByFieldName("left", lang)
	right := node.ChildByFieldName("right", lang)
	operator := node.ChildByFieldName("operator", lang)

	if operator == nil || strings.TrimSpace(operator.Text(source)) != "+" {
		return ""
	}

	leftVal := resolveJSStringExpression(left, lang, source, ctx)
	rightVal := resolveJSStringExpression(right, lang, source, ctx)

	if leftVal == "" && rightVal == "" {
		return ""
	}
	if leftVal == "" {
		leftVal = "{param}"
	}
	if rightVal == "" {
		rightVal = "{param}"
	}
	return leftVal + rightVal
}

// findFirstNamedChild returns the first named (non-anonymous) child of a node.
func findFirstNamedChild(node *gotreesitter.Node) *gotreesitter.Node {
	for _, child := range node.Children() {
		if child.IsNamed() {
			return child
		}
	}
	return nil
}

// jsFunctionName extracts the name of a JS/TS function declaration.
func jsFunctionName(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	if node == nil {
		return ""
	}
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		nameNode = node.ChildByFieldName("property", lang)
	}
	if nameNode == nil {
		nameNode = findFirstNamedChildByType(node, lang, "identifier", "property_identifier")
	}
	if nameNode == nil {
		return ""
	}
	return strings.TrimSpace(nameNode.Text(source))
}

// isJSExported checks if a function/variable node is exported.
func isJSExported(node *gotreesitter.Node, lang *gotreesitter.Language, _ []byte) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	return parent.Type(lang) == "export_statement"
}
