package transfer

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Handler struct {
	baseDir string
	maxSize int64
}

func NewHandler(baseDir string, maxSize int64) *Handler {
	os.MkdirAll(baseDir, 0755)
	return &Handler{baseDir: baseDir, maxSize: maxSize}
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxSize)

	if err := r.ParseMultipartForm(h.maxSize); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	if filename == "." || filename == ".." {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	targetDir := h.baseDir
	if relDir := r.URL.Query().Get("dir"); relDir != "" {
		cleaned := filepath.Clean(relDir)
		if !strings.Contains(cleaned, "..") && !filepath.IsAbs(cleaned) {
			fullPath := filepath.Join(h.baseDir, cleaned)
			absBase, _ := filepath.Abs(h.baseDir)
			absPath, _ := filepath.Abs(fullPath)
			if strings.HasPrefix(absPath, absBase) {
				targetDir = absPath
			}
		}
	}

	destPath := filepath.Join(targetDir, filename)
	dest, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "failed to create file", http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	written, err := io.Copy(dest, file)
	if err != nil {
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	absPath, _ := filepath.Abs(destPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"filename": filename,
		"size":     written,
		"path":     absPath,
	})
}

// FileEntry holds metadata for a file or directory.
type FileEntry struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	IsDir    bool   `json:"is_dir"`
}

// ListFiles returns a JSON list of files and directories in the requested directory.
func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	targetDir := h.baseDir
	if relDir := r.URL.Query().Get("dir"); relDir != "" {
		cleaned := filepath.Clean(relDir)
		if !strings.Contains(cleaned, "..") && !filepath.IsAbs(cleaned) {
			fullPath := filepath.Join(h.baseDir, cleaned)
			absBase, _ := filepath.Abs(h.baseDir)
			absPath, _ := filepath.Abs(fullPath)
			if strings.HasPrefix(absPath, absBase) {
				targetDir = absPath
			}
		}
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}
	var files []FileEntry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileEntry{
			Name:     e.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().Format("01-02 15:04"),
			IsDir:    e.IsDir(),
		})
	}
	if files == nil {
		files = []FileEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = time.Now()
	json.NewEncoder(w).Encode(files)
}

// Delete removes a file or directory.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "file parameter required", http.StatusBadRequest)
		return
	}
	cleaned := filepath.Clean(filename)
	if strings.Contains(cleaned, "..") || filepath.IsAbs(cleaned) {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	targetPath := filepath.Join(h.baseDir, cleaned)
	absBase, _ := filepath.Abs(h.baseDir)
	absPath, _ := filepath.Abs(targetPath)
	if !strings.HasPrefix(absPath, absBase) {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}
	if err := os.RemoveAll(targetPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
		} else {
			http.Error(w, "failed to delete file", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "deleted", "file": filepath.Base(cleaned)})
}
