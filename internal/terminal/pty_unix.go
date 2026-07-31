//go:build !windows

package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixResizer struct {
	ptmx *os.File
}

func (r *unixResizer) Resize(cols, rows uint16) error {
	return pty.Setsize(r.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

func NewSession(cols, rows uint16) (*Session, error) {
	shell := os.Getenv("SHELLFB_COMMAND")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err != nil {
		return nil, err
	}

	id := make([]byte, 16)
	rand.Read(id)

	return &Session{
		id:      hex.EncodeToString(id),
		reader:  ptmx,
		writer:  ptmx,
		closer:  ptmx,
		cols:    cols,
		rows:    rows,
		resizer: &unixResizer{ptmx: ptmx},
	}, nil
}
