package recording_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valorisa/ShellFromBrowser/internal/recording"
)

func TestRecorder(t *testing.T) {
	dir := t.TempDir()
	rec, err := recording.New(dir, "test-session", 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec.Write([]byte("hello "))
	time.Sleep(10 * time.Millisecond)
	rec.Write([]byte("world\r\n"))
	rec.Close()

	path := filepath.Join(dir, "test-session.cast")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 events), got %d", len(lines))
	}

	var header map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &header)
	if header["version"] != float64(2) {
		t.Errorf("version = %v, want 2", header["version"])
	}
	if header["width"] != float64(80) {
		t.Errorf("width = %v, want 80", header["width"])
	}
}

func TestRecorderListCasts(t *testing.T) {
	dir := t.TempDir()

	rec1, _ := recording.New(dir, "sess1", 80, 24)
	rec1.Write([]byte("data"))
	rec1.Close()

	rec2, _ := recording.New(dir, "sess2", 120, 40)
	rec2.Write([]byte("data"))
	rec2.Close()

	casts, err := recording.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(casts) != 2 {
		t.Errorf("len = %d, want 2", len(casts))
	}
}
