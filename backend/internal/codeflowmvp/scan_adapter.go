package codeflowmvp

import "strings"

type scanAdapter interface {
	canHandle(path string) bool
	extractFile(e *extractor, rootPath string, filePath string) error
}

var scanAdapters = []scanAdapter{
	goScanAdapter{},
	jsScanAdapter{},
}

func scanAdapterForPath(path string) scanAdapter {
	for _, adapter := range scanAdapters {
		if adapter.canHandle(path) {
			return adapter
		}
	}
	return nil
}

type goScanAdapter struct{}
type jsScanAdapter struct{}

func (goScanAdapter) canHandle(path string) bool {
	return strings.HasSuffix(path, ".go")
}

func (jsScanAdapter) canHandle(path string) bool {
	return isJSLikePath(path)
}

func isScannablePath(path string) bool {
	return strings.HasSuffix(path, ".go") || isJSLikePath(path)
}

func isJSLikePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".jsx") || strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".tsx")
}
