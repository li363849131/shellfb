package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valorisa/ShellFromBrowser/internal/config"
)

func TestLoadConfig(t *testing.T) {
	yaml := `
server:
  addr: ":9090"
  tls:
    enabled: true
    cert: "/etc/ssl/cert.pem"
    key: "/etc/ssl/key.pem"
auth:
  enabled: true
  users:
    - username: admin
      password_hash: "$2a$10$abcdefghijklmnopqrstuuxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
shell:
  command: "/bin/bash"
  env:
    - "TERM=xterm-256color"
sessions:
  max_per_user: 5
  idle_timeout: "30m"
ssh:
  enabled: true
  known_hosts: "~/.ssh/known_hosts"
recording:
  enabled: true
  dir: "./recordings"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Addr != ":9090" {
		t.Errorf("addr = %q, want :9090", cfg.Server.Addr)
	}
	if !cfg.Server.TLS.Enabled {
		t.Error("TLS should be enabled")
	}
	if !cfg.Auth.Enabled {
		t.Error("Auth should be enabled")
	}
	if len(cfg.Auth.Users) != 1 {
		t.Errorf("users = %d, want 1", len(cfg.Auth.Users))
	}
	if cfg.Sessions.MaxPerUser != 5 {
		t.Errorf("max_per_user = %d, want 5", cfg.Sessions.MaxPerUser)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.Server.Addr != ":4200" {
		t.Errorf("default addr = %q, want :4200", cfg.Server.Addr)
	}
	if cfg.Auth.Enabled {
		t.Error("auth should be disabled by default")
	}
}

func TestApplyEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		initial  *config.Config
		expected *config.Config
	}{
		{
			name: "SHELLFB_ADDR overrides addr",
			env: map[string]string{
				"SHELLFB_ADDR": ":3000",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{Addr: ":3000"},
			},
		},
		{
			name: "SHELLFB_DOMAIN sets domain",
			env: map[string]string{
				"SHELLFB_DOMAIN": "example.com",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{
					Addr:   ":4200",
					Domain: "example.com",
				},
			},
		},
		{
			name: "SHELLFB_AUTOCERT_DIR sets autocert dir",
			env: map[string]string{
				"SHELLFB_AUTOCERT_DIR": "/var/cache/autocert",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{
					Addr:        ":4200",
					AutocertDir: "/var/cache/autocert",
				},
			},
		},
		{
			name: "SHELLFB_TLS_CERT and SHELLFB_TLS_KEY set TLS",
			env: map[string]string{
				"SHELLFB_TLS_CERT": "/etc/ssl/cert.pem",
				"SHELLFB_TLS_KEY":  "/etc/ssl/key.pem",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{
					Addr: ":4200",
					TLS: config.TLSConfig{
						Cert: "/etc/ssl/cert.pem",
						Key:  "/etc/ssl/key.pem",
					},
				},
			},
		},
		{
			name: "SHELLFB_AUTH_ENABLED sets auth enabled",
			env: map[string]string{
				"SHELLFB_AUTH_ENABLED": "true",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{Addr: ":4200"},
				Auth:   config.AuthConfig{Enabled: true},
			},
		},
		{
			name: "SHELLFB_JWT_SECRET sets JWT secret",
			env: map[string]string{
				"SHELLFB_JWT_SECRET": "my-secret-key",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{Addr: ":4200"},
				Auth:   config.AuthConfig{JWTSecret: "my-secret-key"},
			},
		},
		{
			name: "multiple env vars override",
			env: map[string]string{
				"SHELLFB_ADDR":         ":9000",
				"SHELLFB_DOMAIN":       "app.example.com",
				"SHELLFB_AUTOCERT_DIR": "/tmp/certs",
				"SHELLFB_AUTH_ENABLED": "true",
				"SHELLFB_JWT_SECRET":   "supersecret",
			},
			initial: config.Default(),
			expected: &config.Config{
				Server: config.ServerConfig{
					Addr:        ":9000",
					Domain:      "app.example.com",
					AutocertDir: "/tmp/certs",
				},
				Auth: config.AuthConfig{
					Enabled:   true,
					JWTSecret: "supersecret",
				},
			},
		},
		{
			name: "SHELLFB_AUTH_ENABLED=false disables auth",
			env: map[string]string{
				"SHELLFB_AUTH_ENABLED": "false",
			},
			initial: &config.Config{
				Server: config.ServerConfig{Addr: ":4200"},
				Auth:   config.AuthConfig{Enabled: true},
			},
			expected: &config.Config{
				Server: config.ServerConfig{Addr: ":4200"},
				Auth:   config.AuthConfig{Enabled: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env vars
			for k, v := range tt.env {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			cfg := tt.initial
			cfg.ApplyEnv()

			// Check Server fields
			if cfg.Server.Addr != tt.expected.Server.Addr {
				t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, tt.expected.Server.Addr)
			}
			if cfg.Server.Domain != tt.expected.Server.Domain {
				t.Errorf("Server.Domain = %q, want %q", cfg.Server.Domain, tt.expected.Server.Domain)
			}
			if cfg.Server.AutocertDir != tt.expected.Server.AutocertDir {
				t.Errorf("Server.AutocertDir = %q, want %q", cfg.Server.AutocertDir, tt.expected.Server.AutocertDir)
			}
			if cfg.Server.TLS.Cert != tt.expected.Server.TLS.Cert {
				t.Errorf("Server.TLS.Cert = %q, want %q", cfg.Server.TLS.Cert, tt.expected.Server.TLS.Cert)
			}
			if cfg.Server.TLS.Key != tt.expected.Server.TLS.Key {
				t.Errorf("Server.TLS.Key = %q, want %q", cfg.Server.TLS.Key, tt.expected.Server.TLS.Key)
			}

			// Check Auth fields
			if cfg.Auth.Enabled != tt.expected.Auth.Enabled {
				t.Errorf("Auth.Enabled = %v, want %v", cfg.Auth.Enabled, tt.expected.Auth.Enabled)
			}
			if cfg.Auth.JWTSecret != tt.expected.Auth.JWTSecret {
				t.Errorf("Auth.JWTSecret = %q, want %q", cfg.Auth.JWTSecret, tt.expected.Auth.JWTSecret)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid: domain without TLS cert/key",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Addr:   ":443",
					Domain: "example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "valid: TLS cert/key without domain",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Addr: ":443",
					TLS: config.TLSConfig{
						Cert: "/etc/ssl/cert.pem",
						Key:  "/etc/ssl/key.pem",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid: neither domain nor TLS",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Addr: ":4200",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid: domain + TLS cert",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Addr:   ":443",
					Domain: "example.com",
					TLS: config.TLSConfig{
						Cert: "/etc/ssl/cert.pem",
					},
				},
			},
			wantErr: true,
			errMsg:  "config conflict: 'domain' (auto-TLS) and 'tls.cert'/'tls.key' (manual TLS) cannot both be set",
		},
		{
			name: "invalid: domain + TLS key",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Addr:   ":443",
					Domain: "example.com",
					TLS: config.TLSConfig{
						Key: "/etc/ssl/key.pem",
					},
				},
			},
			wantErr: true,
			errMsg:  "config conflict: 'domain' (auto-TLS) and 'tls.cert'/'tls.key' (manual TLS) cannot both be set",
		},
		{
			name: "invalid: domain + TLS cert + TLS key",
			cfg: &config.Config{
				Server: config.ServerConfig{
					Addr:   ":443",
					Domain: "example.com",
					TLS: config.TLSConfig{
						Cert: "/etc/ssl/cert.pem",
						Key:  "/etc/ssl/key.pem",
					},
				},
			},
			wantErr: true,
			errMsg:  "config conflict: 'domain' (auto-TLS) and 'tls.cert'/'tls.key' (manual TLS) cannot both be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error but got nil")
				} else if err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}
