package auth_test

import (
	"testing"
	"time"

	"github.com/valorisa/ShellFromBrowser/internal/auth"
	"github.com/valorisa/ShellFromBrowser/internal/config"
)

func TestLocalAuth(t *testing.T) {
	hash, err := auth.HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	cfg := &config.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		Users: []config.UserDef{
			{Username: "admin", PasswordHash: hash},
		},
	}

	provider := auth.NewLocalProvider(cfg)

	// Valid credentials
	token, err := provider.Authenticate("admin", "secret123")
	if err != nil {
		t.Fatalf("Authenticate valid: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}

	// Validate token
	claims, err := provider.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Username != "admin" {
		t.Errorf("username = %q, want admin", claims.Username)
	}

	// Invalid password
	_, err = provider.Authenticate("admin", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}

	// Invalid username
	_, err = provider.Authenticate("nobody", "secret123")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestTokenExpiry(t *testing.T) {
	cfg := &config.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret",
		Users:     []config.UserDef{{Username: "u", PasswordHash: ""}},
	}
	provider := auth.NewLocalProvider(cfg)
	provider.SetTokenDuration(1 * time.Millisecond)

	token, _ := provider.Authenticate("u", "")
	time.Sleep(10 * time.Millisecond)

	_, err := provider.ValidateToken(token)
	if err == nil {
		t.Fatal("expected expired token error")
	}
}
