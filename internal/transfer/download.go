package transfer

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "file parameter required", http.StatusBadRequest)
		return
	}

	cleaned := filepath.Clean(filename)
	if strings.Contains(cleaned, "..") || filepath.IsAbs(cleaned) {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(h.baseDir, cleaned)

	absBase, _ := filepath.Abs(h.baseDir)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absBase) {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	ext := strings.ToLower(filepath.Ext(cleaned))
	baseName := filepath.Base(cleaned)
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	// Serve images and PDFs inline (browser can display them natively).
	// Serve common text/code extensions and dotfiles inline so the JS fetch can read them.
	textExts := map[string]bool{
		".txt": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
		".toml": true, ".csv": true, ".log": true, ".conf": true, ".cfg": true,
		".ini": true, ".env": true, ".js": true, ".ts": true, ".jsx": true,
		".tsx": true, ".py": true, ".go": true, ".sh": true, ".bash": true,
		".zsh": true, ".fish": true, ".html": true, ".htm": true, ".css": true,
		".xml": true, ".sql": true, ".java": true, ".c": true, ".cpp": true,
		".h": true, ".hpp": true, ".rs": true, ".rb": true, ".php": true,
		".r": true, ".kt": true, ".swift": true, ".dart": true, ".vue": true,
		".lua": true, ".vim": true, ".el": true, ".hs": true, ".ml": true,
		".scala": true, ".tex": true, ".diff": true, ".patch": true,
		".makefile": true, ".dockerfile": true, ".gitignore": true,
		".tf": true, ".hcl": true, ".proto": true, ".graphql": true, ".gql": true,
		".bashrc": true, ".bash_history": true, ".profile": true, ".bash_profile": true,
	}
	isImage := strings.HasPrefix(mimeType, "image/")
	isPDF := mimeType == "application/pdf"
	isText := strings.HasPrefix(mimeType, "text/") || textExts[ext] || strings.HasPrefix(baseName, ".")
	if isText {
		mimeType = "text/plain; charset=utf-8"
	}
	inline := isImage || isPDF || isText
	if inline {
		w.Header().Set("Content-Disposition", "inline; filename=\""+baseName+"\"")
		w.Header().Set("Content-Type", mimeType)
	} else {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+baseName+"\"")
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeFile(w, r, fullPath)
}
