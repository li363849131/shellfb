//go:build windows

package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"

	"github.com/UserExistsError/conpty"
)

type conptyResizer struct {
	cpty *conpty.ConPty
}

func (r *conptyResizer) Resize(cols, rows uint16) error {
	return r.cpty.Resize(int(cols), int(rows))
}

type conptyCloser struct {
	cpty *conpty.ConPty
}

func (c *conptyCloser) Close() error {
	c.cpty.Close()
	c.cpty.Wait(context.Background())
	return nil
}

func NewSession(cols, rows uint16) (*Session, error) {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}

	cpty, err := conpty.Start(shell, conpty.ConPtyDimensions(int(cols), int(rows)))
	if err != nil {
		return nil, err
	}

	id := make([]byte, 16)
	rand.Read(id)

	reader := &conptyReader{cpty: cpty}

	return &Session{
		id:      hex.EncodeToString(id),
		reader:  reader,
		writer:  cpty,
		closer:  &conptyCloser{cpty: cpty},
		cols:    cols,
		rows:    rows,
		resizer: &conptyResizer{cpty: cpty},
	}, nil
}

type conptyReader struct {
	cpty *conpty.ConPty
}

func (r *conptyReader) Read(p []byte) (int, error) {
	n, err := r.cpty.Read(p)
	if err != nil {
		return 0, io.EOF
	}
	return n, nil
}
