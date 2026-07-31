package recording

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Player struct {
	dir string
}

func NewPlayer(dir string) *Player {
	return &Player{dir: dir}
}

func (p *Player) HandleList(w http.ResponseWriter, r *http.Request) {
	casts, err := List(p.dir)
	if err != nil {
		http.Error(w, "failed to list recordings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(casts)
}

func (p *Player) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" || strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	path := filepath.Join(p.dir, id+".cast")
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "recording not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
