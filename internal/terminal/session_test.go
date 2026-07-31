package terminal_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/valorisa/ShellFromBrowser/internal/terminal"
)

func TestSessionSpawnAndWrite(t *testing.T) {
	sess, err := terminal.NewSession(80, 24)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	// Write a command - use "echo hello" on Unix, or just send input on Windows
	_, err = sess.Write([]byte("echo hello\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read output with timeout
	buf := make([]byte, 4096)
	var output bytes.Buffer
	deadline := time.After(5 * time.Second)

	for {
		select {
		case <-deadline:
			if !bytes.Contains(output.Bytes(), []byte("hello")) {
				t.Fatalf("timeout: output was %q", output.String())
			}
			return
		default:
			n, _ := sess.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
				if bytes.Contains(output.Bytes(), []byte("hello")) {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestSessionClose(t *testing.T) {
	sess, err := terminal.NewSession(80, 24)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	err = sess.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Write after close should fail
	_, err = sess.Write([]byte("test"))
	if err == nil {
		t.Fatal("expected error writing to closed session")
	}
}

func TestSessionManager(t *testing.T) {
	mgr := terminal.NewManager(5)

	s1, err := mgr.Create("user1", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s2, err := mgr.Create("user1", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// List sessions
	sessions := mgr.ListByUser("user1")
	if len(sessions) != 2 {
		t.Errorf("ListByUser = %d, want 2", len(sessions))
	}

	// Get by ID
	got, err := mgr.Get(s1.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != s1.ID() {
		t.Error("Get returned wrong session")
	}

	// Destroy
	mgr.Destroy(s2.ID())
	sessions = mgr.ListByUser("user1")
	if len(sessions) != 1 {
		t.Errorf("after destroy: %d, want 1", len(sessions))
	}

	// Cleanup
	mgr.DestroyAll()
}

func TestSessionManagerMaxSessions(t *testing.T) {
	mgr := terminal.NewManager(2)

	_, err := mgr.Create("user1", 80, 24)
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	_, err = mgr.Create("user1", 80, 24)
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	_, err = mgr.Create("user1", 80, 24)
	if err == nil {
		t.Fatal("expected max sessions error")
	}

	// Cleanup
	mgr.DestroyAll()
}
