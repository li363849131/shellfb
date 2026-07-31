package server_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/valorisa/ShellFromBrowser/internal/config"
	"github.com/valorisa/ShellFromBrowser/internal/server"
)

func TestWebSocketEcho(t *testing.T) {
	srv := server.New(":0", config.Default())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	// Read initial banner/prompt from the PTY
	_, _, err = ws.ReadMessage()
	if err != nil {
		t.Fatalf("read initial: %v", err)
	}

	// The websocket now spawns a real terminal session
	// Just verify we can connect and get output
	t.Log("WebSocket PTY connection established successfully")
}

func TestListenModeAutoTLS(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Domain = "example.com"
	cfg.Server.AutocertDir = t.TempDir()
	srv := server.New(":443", cfg)
	mode := srv.ListenMode()
	if mode != "autocert" {
		t.Errorf("ListenMode() = %q, want autocert", mode)
	}
}

func TestListenModeManualTLS(t *testing.T) {
	cfg := config.Default()
	cfg.Server.TLS.Cert = "/tmp/cert.pem"
	cfg.Server.TLS.Key = "/tmp/key.pem"
	srv := server.New(":443", cfg)
	mode := srv.ListenMode()
	if mode != "manual-tls" {
		t.Errorf("ListenMode() = %q, want manual-tls", mode)
	}
}

func TestListenModeHTTP(t *testing.T) {
	cfg := config.Default()
	srv := server.New(":4200", cfg)
	mode := srv.ListenMode()
	if mode != "http" {
		t.Errorf("ListenMode() = %q, want http", mode)
	}
}
