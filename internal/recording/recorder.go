package recording

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type header struct {
	Version   int    `json:"version"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Timestamp int64  `json:"timestamp"`
	Title     string `json:"title,omitempty"`
}

type Recorder struct {
	file      *os.File
	startTime time.Time
	mu        sync.Mutex
}

func New(dir, sessionID string, cols, rows int) (*Recorder, error) {
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, sessionID+".cast")
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	h := header{
		Version:   2,
		Width:     cols,
		Height:    rows,
		Timestamp: time.Now().Unix(),
	}
	headerJSON, _ := json.Marshal(h)
	file.Write(headerJSON)
	file.Write([]byte("\n"))

	return &Recorder{
		file:      file,
		startTime: time.Now(),
	}, nil
}

func (r *Recorder) Write(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.startTime).Seconds()
	escaped, _ := json.Marshal(string(data))
	line := fmt.Sprintf("[%.6f, \"o\", %s]\n", elapsed, escaped)
	r.file.WriteString(line)
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}

type CastInfo struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Timestamp time.Time `json:"timestamp"`
	Size      int64     `json:"size"`
}

func List(dir string) ([]CastInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var casts []CastInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cast") {
			continue
		}

		info, _ := entry.Info()
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		lines := strings.SplitN(string(data), "\n", 2)
		if len(lines) == 0 {
			continue
		}

		var h header
		json.Unmarshal([]byte(lines[0]), &h)

		id := strings.TrimSuffix(entry.Name(), ".cast")
		casts = append(casts, CastInfo{
			ID:        id,
			Filename:  entry.Name(),
			Width:     h.Width,
			Height:    h.Height,
			Timestamp: time.Unix(h.Timestamp, 0),
			Size:      info.Size(),
		})
	}
	return casts, nil
}
