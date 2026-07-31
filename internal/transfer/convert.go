package transfer

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var libreOfficePath = ""

func init() {
	for _, p := range []string{"libreoffice", "soffice"} {
		if path, err := exec.LookPath(p); err == nil {
			libreOfficePath = path
			break
		}
	}
}

// Convert converts an office document (docx, xlsx, pptx, etc.) to PDF using LibreOffice and serves it.
func (h *Handler) Convert(w http.ResponseWriter, r *http.Request) {
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

	if _, err := os.Stat(fullPath); err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if libreOfficePath == "" {
		http.Error(w, "LibreOffice not available", http.StatusServiceUnavailable)
		return
	}

	// Create temp dir for isolated LibreOffice UserInstallation and output
	tmpDir, err := os.MkdirTemp("", "shellfb-convert-*")
	if err != nil {
		http.Error(w, "failed to create temp dir", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	userProfileDir := filepath.Join(tmpDir, "lo_profile")

	cmd := exec.Command(libreOfficePath,
		fmt.Sprintf("-env:UserInstallation=file://%s", userProfileDir),
		"--headless",
		"--convert-to", "pdf",
		"--outdir", tmpDir,
		fullPath,
	)
	cmd.Env = append(os.Environ(), fmt.Sprintf("HOME=%s", tmpDir))

	if err := cmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("conversion failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Find the output PDF file
	base := strings.TrimSuffix(filepath.Base(cleaned), filepath.Ext(cleaned))
	pdfPath := filepath.Join(tmpDir, base+".pdf")
	if _, err := os.Stat(pdfPath); err != nil {
		http.Error(w, "converted PDF file not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename=\""+base+".pdf\"")
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	http.ServeFile(w, r, pdfPath)
}

// ConvertAvailable returns whether LibreOffice is available for conversion.
func ConvertAvailable() bool {
	return libreOfficePath != ""
}
