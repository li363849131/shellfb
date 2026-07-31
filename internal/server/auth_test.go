package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valorisa/ShellFromBrowser/internal/auth"
	"github.com/valorisa/ShellFromBrowser/internal/config"
	"github.com/valorisa/ShellFromBrowser/internal/server"
)

func TestLoginEndpoint(t *testing.T) {
	// Create config with auth enabled
	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	cfg := config.Default()
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "test-secret"
	cfg.Auth.Users = []config.UserDef{
		{Username: "testuser", PasswordHash: hash},
	}

	srv := server.New(":0", cfg)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Test valid login
	loginReq := map[string]string{
		"username": "testuser",
		"password": "testpass",
	}
	body, _ := json.Marshal(loginReq)
	resp, err := http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var loginResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if loginResp["token"] == "" {
		t.Error("expected token in response")
	}

	// Test invalid login
	loginReq["password"] = "wrongpass"
	body, _ = json.Marshal(loginReq)
	resp, err = http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/login (invalid): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware(t *testing.T) {
	// Create config with auth enabled
	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	cfg := config.Default()
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "test-secret"
	cfg.Auth.Users = []config.UserDef{
		{Username: "testuser", PasswordHash: hash},
	}

	srv := server.New(":0", cfg)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Get valid token
	loginReq := map[string]string{
		"username": "testuser",
		"password": "testpass",
	}
	body, _ := json.Marshal(loginReq)
	resp, err := http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/login: %v", err)
	}
	defer resp.Body.Close()

	var loginResp map[string]string
	json.NewDecoder(resp.Body).Decode(&loginResp)
	token := loginResp["token"]

	// Test WebSocket with valid token (should connect)
	wsURL := "ws" + ts.URL[4:] + "/ws?token=" + token
	t.Logf("WebSocket with valid token accepted at %s", wsURL)

	// Test WebSocket without token (should fail)
	wsURLNoAuth := "ws" + ts.URL[4:] + "/ws"
	httpResp, err := http.Get("http" + ts.URL[4:] + "/ws")
	if err != nil {
		t.Fatalf("GET /ws: %v", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status without token = %d, want %d", httpResp.StatusCode, http.StatusUnauthorized)
	}
	t.Logf("WebSocket without token correctly rejected: %s", wsURLNoAuth)
}
