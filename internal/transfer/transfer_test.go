package transfer_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/valorisa/ShellFromBrowser/internal/transfer"
)

func TestUpload(t *testing.T) {
	uploadDir := t.TempDir()
	handler := transfer.NewHandler(uploadDir, 10*1024*1024)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("hello world"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	handler.Upload(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. Body: %s", w.Code, w.Body.String())
	}

	content, err := os.ReadFile(filepath.Join(uploadDir, "test.txt"))
	if err != nil {
		t.Fatalf("file not found: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("content = %q, want 'hello world'", content)
	}
}

func TestDownload(t *testing.T) {
	downloadDir := t.TempDir()
	os.WriteFile(filepath.Join(downloadDir, "data.bin"), []byte("binary content"), 0644)

	handler := transfer.NewHandler(downloadDir, 10*1024*1024)

	req := httptest.NewRequest(http.MethodGet, "/api/download?file=data.bin", nil)
	w := httptest.NewRecorder()

	handler.Download(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "binary content" {
		t.Errorf("body = %q", body)
	}
}

func TestDownloadPathTraversal(t *testing.T) {
	handler := transfer.NewHandler(t.TempDir(), 10*1024*1024)

	req := httptest.NewRequest(http.MethodGet, "/api/download?file=../../../etc/passwd", nil)
	w := httptest.NewRecorder()

	handler.Download(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("path traversal not blocked: status = %d", w.Code)
	}
}
