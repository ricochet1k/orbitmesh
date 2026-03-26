package codeflowmvp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	apiReqSeq    map[string]int
	apiRouteSeq  map[string]int
	stmtSeq      map[string]int
	blockSeq     map[string]int
	seenPackages map[string]PackageFact
}

func ScanPath(projectRoot string, scanPath string) (ExtractionSummary, error) {
	absPath, err := filepath.Abs(scanPath)
	if err != nil {
		return ExtractionSummary{}, fmt.Errorf("resolve path %q: %w", scanPath, err)
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return ExtractionSummary{}, fmt.Errorf("stat %q: %w", scanPath, err)
	}

	var scanRoot string
	if projectRoot != "" {
		scanRoot, err = filepath.Abs(projectRoot)
		if err != nil {
			scanRoot = absPath
		}
	} else {
		scanRoot = absPath
		if !stat.IsDir() {
			scanRoot = filepath.Dir(absPath)
		}
	}

	sourceFiles := make([]string, 0)

	if stat.IsDir() {
		err = filepath.WalkDir(absPath, func(curr string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if shouldSkipScanDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if isScannablePath(d.Name()) {
				sourceFiles = append(sourceFiles, curr)
			}
			return nil
		})
		if err != nil {
			return ExtractionSummary{}, fmt.Errorf("walk %q: %w", scanPath, err)
		}
	} else if isScannablePath(strings.ToLower(stat.Name())) {
		sourceFiles = append(sourceFiles, absPath)
	} else {
		return ExtractionSummary{}, fmt.Errorf("path %q is not a directory or supported source file (.go/.js/.jsx/.ts/.tsx)", scanPath)
	}

	sort.Strings(sourceFiles)

	ext := &extractor{
		summary:      ExtractionSummary{},
		callSeq:      map[string]int{},
		spawnSeq:     map[string]int{},
		apiReqSeq:    map[string]int{},
		apiRouteSeq:  map[string]int{},
		stmtSeq:      map[string]int{},
		blockSeq:     map[string]int{},
		seenPackages: map[string]PackageFact{},
	}

	for _, filePath := range sourceFiles {
		adapter := scanAdapterForPath(filePath)
		if adapter == nil {
			continue
		}
		if err := adapter.extractFile(ext, scanRoot, filePath); err != nil {
			continue
		}
	}
	if len(ext.summary.Files) == 0 {
		return ExtractionSummary{}, fmt.Errorf("scan %q: no parseable supported files found", scanPath)
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
		Blocks:    make([]rules.BlockFact, 0, len(ext.summary.Blocks)),
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
	for _, b := range ext.summary.Blocks {
		ruleFacts.Blocks = append(ruleFacts.Blocks, rules.BlockFact{
			ID:         b.ID,
			FunctionID: b.FunctionID,
			FileID:     b.FileID,
			IsDead:     b.IsDead,
			BlockKind:  b.BlockKind,
			Start:      rules.Position{Line: b.StartLine, Column: b.StartColumn},
			End:        rules.Position{Line: b.EndLine, Column: b.EndColumn},
		})
	}
	ext.summary.Findings = rules.Evaluate(ruleFacts)

	ext.summary.Counts = Counts{
		Files:      len(ext.summary.Files),
		Packages:   len(ext.summary.Packages),
		Functions:  len(ext.summary.Functions),
		Calls:      len(ext.summary.Calls),
		Spawns:     len(ext.summary.Spawns),
		APIReqs:    len(ext.summary.APIReqs),
		APIRoutes:  len(ext.summary.APIRoutes),
		Findings:   len(ext.summary.Findings),
		Blocks:     len(ext.summary.Blocks),
		CFGEdges:   len(ext.summary.CFGEdges),
		StmtEdges:  len(ext.summary.StmtEdges),
		Statements: len(ext.summary.Statements),
	}

	return ext.summary, nil
}

func shouldSkipScanDir(name string) bool {
	if name == "" {
		return false
	}
	switch name {
	case ".git", "node_modules", "dist", "build", ".next", "coverage", "test-results":
		return true
	default:
		return false
	}
}
