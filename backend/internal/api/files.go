package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	chi "github.com/go-chi/chi/v5"

	apiTypes "github.com/ricochet1k/orbitmesh/pkg/api"
)

// knownTextExtensions is the set of extensions that should be treated as text
// even when mime.TypeByExtension returns an empty string.
var knownTextExtensions = map[string]bool{
	".go":           true,
	".ts":           true,
	".tsx":          true,
	".js":           true,
	".jsx":          true,
	".md":           true,
	".yaml":         true,
	".yml":          true,
	".toml":         true,
	".sql":          true,
	".sh":           true,
	".bash":         true,
	".env":          true,
	".gitignore":    true,
	".dockerignore": true,
	".makefile":     true,
	".tf":           true,
	".proto":        true,
	".graphql":      true,
	".gql":          true,
	".py":           true,
	".rb":           true,
	".rs":           true,
	".c":            true,
	".cpp":          true,
	".h":            true,
	".java":         true,
	".kt":           true,
	".swift":        true,
	".cs":           true,
	".php":          true,
	".lua":          true,
}

// knownTextMimeTypes is the set of non-"text/" mime types that should be
// treated as text for the purposes of file reading.
var knownTextMimeTypes = map[string]bool{
	"application/json":          true,
	"application/xml":           true,
	"application/javascript":    true,
	"application/typescript":    true,
	"application/x-sh":          true,
	"application/x-shellscript": true,
}

// isTextMime returns true if the mime type (or extension fallback) indicates a
// human-readable text file.
func isTextMime(mimeType, ext string) bool {
	if mimeType == "" {
		return knownTextExtensions[strings.ToLower(ext)]
	}
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	// Strip parameters (e.g. "; charset=utf-8") before map lookup.
	bare := strings.SplitN(mimeType, ";", 2)[0]
	bare = strings.TrimSpace(bare)
	return knownTextMimeTypes[bare]
}

// resolveProjectPath resolves a relative path within the project root, applies
// filepath.Clean and verifies it does not escape the project directory.
// It returns the absolute path, the path relative to the project root, and any
// error suitable for returning directly to the caller (already written to w).
func resolveProjectPath(w http.ResponseWriter, projectPath, requestedPath string) (absPath, relPath string, ok bool) {
	joined := filepath.Join(projectPath, requestedPath)
	abs := filepath.Clean(joined)

	// Safety check: the cleaned path must remain inside the project root.
	prefix := projectPath
	if !strings.HasPrefix(abs, prefix+string(filepath.Separator)) && abs != prefix {
		writeError(w, http.StatusBadRequest, "path outside project", fmt.Sprintf("resolved path %q is outside project root %q", abs, prefix))
		return "", "", false
	}

	rel, err := filepath.Rel(projectPath, abs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compute relative path", err.Error())
		return "", "", false
	}

	return abs, rel, true
}

func wildcardPath(r *http.Request) string {
	requestedPath := chi.URLParam(r, "*")

	escapedPath := r.URL.EscapedPath()
	idx := strings.Index(escapedPath, "/files/")
	if idx == -1 {
		return requestedPath
	}

	escapedWildcard := escapedPath[idx+len("/files/"):]
	if escapedWildcard == "" {
		return requestedPath
	}

	decodedWildcard, err := url.PathUnescape(escapedWildcard)
	if err != nil {
		return requestedPath
	}

	return decodedWildcard
}

// listProjectFiles handles GET /api/v1/projects/{id}/files
// Optional query param: path (default ".")
func (h *Handler) listProjectFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	project, err := h.projectStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found", err.Error())
		return
	}

	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		queryPath = "."
	}

	absPath, _, ok := resolveProjectPath(w, project.Path, queryPath)
	if !ok {
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read directory", err.Error())
		return
	}

	fileEntries := make([]apiTypes.FileEntry, 0, len(entries))
	for _, entry := range entries {
		entryAbs := filepath.Join(absPath, entry.Name())

		relPath, relErr := filepath.Rel(project.Path, entryAbs)
		if relErr != nil {
			relPath = strings.TrimPrefix(entryAbs, project.Path+string(filepath.Separator))
		}

		fe := apiTypes.FileEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Path:  relPath,
		}

		if info, infoErr := entry.Info(); infoErr == nil {
			fe.Modified = info.ModTime()
			if !entry.IsDir() {
				fe.Size = info.Size()
			}
		}

		if !entry.IsDir() {
			ext := filepath.Ext(entry.Name())
			fe.MimeType = mime.TypeByExtension(ext)
		}

		fileEntries = append(fileEntries, fe)
	}

	// Sort: directories first, then files; each group sorted alphabetically.
	sort.Slice(fileEntries, func(i, j int) bool {
		if fileEntries[i].IsDir != fileEntries[j].IsDir {
			return fileEntries[i].IsDir
		}
		return fileEntries[i].Name < fileEntries[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(fileEntries)
}

// readProjectFile handles GET /api/v1/projects/{id}/files/*path
func (h *Handler) readProjectFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	project, err := h.projectStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found", err.Error())
		return
	}

	// chi wildcard param for routes registered with /*path
	requestedPath := wildcardPath(r)

	absPath, relPath, ok := resolveProjectPath(w, project.Path, requestedPath)
	if !ok {
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file not found", err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to stat file", err.Error())
		}
		return
	}

	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "path is a directory", "use the list endpoint to browse directories")
		return
	}

	// Cap file size at 10 MB.
	const maxFileSize = 10 * 1024 * 1024
	if info.Size() > maxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large", fmt.Sprintf("file size %d exceeds 10 MB limit", info.Size()))
		return
	}

	ext := filepath.Ext(absPath)
	mimeType := mime.TypeByExtension(ext)
	bareType := strings.SplitN(mimeType, ";", 2)[0]
	bareType = strings.TrimSpace(bareType)

	isImage := strings.HasPrefix(bareType, "image/")
	isText := isTextMime(bareType, ext)

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	if !isText && !isImage {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "binary file not supported"})
		return
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file", err.Error())
		return
	}

	resp := apiTypes.FileReadResponse{
		Path:     relPath,
		MimeType: mimeType,
	}

	if isImage {
		resp.Encoding = "base64"
		resp.Content = base64.StdEncoding.EncodeToString(data)
	} else {
		resp.Encoding = "utf8"
		resp.Content = string(data)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// writeProjectFile handles PUT /api/v1/projects/{id}/files/*path
func (h *Handler) writeProjectFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	project, err := h.projectStorage.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found", err.Error())
		return
	}

	requestedPath := wildcardPath(r)

	absPath, _, ok := resolveProjectPath(w, project.Path, requestedPath)
	if !ok {
		return
	}

	// Reject writes to existing directories.
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		writeError(w, http.StatusBadRequest, "path is a directory", "cannot write to a directory")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Ensure parent directories exist.
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, fs.ModePerm); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create directories", err.Error())
		return
	}

	if err := os.WriteFile(absPath, []byte(body.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
