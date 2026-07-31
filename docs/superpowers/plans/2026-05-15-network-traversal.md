# Network Traversal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ShellFromBrowser production-ready for restricted networks with auto-TLS, 12-factor config, Docker improvements, and deployment documentation.

**Architecture:** Add env var and CLI flag resolution to the existing config system (priority: env > flag > YAML > default). Extend the server listener to support autocert (Let's Encrypt) when a domain is configured. Update Docker artifacts to expose 80+443 and support the new env vars.

**Tech Stack:** Go stdlib, `golang.org/x/crypto/acme/autocert` (already in go.mod via `golang.org/x/crypto`), Docker, Markdown.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/config/config.go` | Config structs, defaults, YAML loading, env var resolution |
| `internal/config/config_test.go` | Tests for config loading, env override, conflict detection |
| `internal/server/server.go` | HTTP(S) listener setup, autocert manager, redirect handler |
| `internal/server/server_test.go` | Tests for listener mode selection |
| `cmd/shellfb/main.go` | CLI flag definitions, flag→config override, startup logging |
| `config.example.yaml` | Updated example with `domain` and `autocert_dir` fields |
| `Dockerfile` | Multi-port expose, non-root with NET_BIND_SERVICE, certs volume |
| `docker-compose.yml` | Production compose with auto-TLS env vars and volumes |
| `docker-compose.reverse-proxy.yml` | Variant with Caddy sidecar |
| `docs/deploiement-reseaux-contraints.md` | Full deployment guide (FR) |
| `README.fr.md` | Short section added with link to guide |

---

### Task 1: Extend config structs and add env var resolution

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for new config fields and env override**

```go
// Add to internal/config/config_test.go

func TestConfigDomainField(t *testing.T) {
	yaml := `
server:
  addr: ":443"
  domain: "shell.example.com"
  autocert_dir: "/tmp/certs"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Domain != "shell.example.com" {
		t.Errorf("domain = %q, want shell.example.com", cfg.Server.Domain)
	}
	if cfg.Server.AutocertDir != "/tmp/certs" {
		t.Errorf("autocert_dir = %q, want /tmp/certs", cfg.Server.AutocertDir)
	}
}

func TestEnvVarOverridesConfig(t *testing.T) {
	yaml := `
server:
  addr: ":8080"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	t.Setenv("SHELLFB_ADDR", ":9999")
	t.Setenv("SHELLFB_DOMAIN", "env.example.com")
	t.Setenv("SHELLFB_AUTOCERT_DIR", "/env/certs")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.ApplyEnv()

	if cfg.Server.Addr != ":9999" {
		t.Errorf("addr = %q, want :9999", cfg.Server.Addr)
	}
	if cfg.Server.Domain != "env.example.com" {
		t.Errorf("domain = %q, want env.example.com", cfg.Server.Domain)
	}
	if cfg.Server.AutocertDir != "/env/certs" {
		t.Errorf("autocert_dir = %q, want /env/certs", cfg.Server.AutocertDir)
	}
}

func TestEnvVarAuthFields(t *testing.T) {
	cfg := config.Default()

	t.Setenv("SHELLFB_AUTH_ENABLED", "true")
	t.Setenv("SHELLFB_JWT_SECRET", "my-secret")

	cfg.ApplyEnv()

	if !cfg.Auth.Enabled {
		t.Error("auth should be enabled via env")
	}
	if cfg.Auth.JWTSecret != "my-secret" {
		t.Errorf("jwt_secret = %q, want my-secret", cfg.Auth.JWTSecret)
	}
}

func TestConflictDomainAndTLSCert(t *testing.T) {
	cfg := config.Default()
	cfg.Server.Domain = "example.com"
	cfg.Server.TLS.Cert = "/path/to/cert.pem"
	cfg.Server.TLS.Key = "/path/to/key.pem"

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error when domain and tls.cert are both set")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v -run "TestConfigDomain|TestEnvVar|TestConflict"`
Expected: FAIL — `Domain` field, `ApplyEnv()`, and `Validate()` don't exist yet.

- [ ] **Step 3: Implement new config fields, ApplyEnv, and Validate**

Replace `internal/config/config.go` with:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	Shell     ShellConfig     `yaml:"shell"`
	Sessions  SessionsConfig  `yaml:"sessions"`
	SSH       SSHConfig       `yaml:"ssh"`
	Recording RecordingConfig `yaml:"recording"`
}

type ServerConfig struct {
	Addr        string    `yaml:"addr"`
	Domain      string    `yaml:"domain"`
	AutocertDir string    `yaml:"autocert_dir"`
	TLS         TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled bool   `yaml:"enabled"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

type AuthConfig struct {
	Enabled   bool      `yaml:"enabled"`
	Users     []UserDef `yaml:"users"`
	JWTSecret string    `yaml:"jwt_secret"`
}

type UserDef struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
}

type ShellConfig struct {
	Command string   `yaml:"command"`
	Env     []string `yaml:"env"`
}

type SessionsConfig struct {
	MaxPerUser       int    `yaml:"max_per_user"`
	IdleTimeoutStr   string `yaml:"idle_timeout"`
	idleTimeoutCache time.Duration
}

func (s *SessionsConfig) GetIdleTimeout() time.Duration {
	if s.idleTimeoutCache != 0 {
		return s.idleTimeoutCache
	}
	if s.IdleTimeoutStr == "" {
		return 30 * time.Minute
	}
	d, err := time.ParseDuration(s.IdleTimeoutStr)
	if err != nil {
		return 30 * time.Minute
	}
	s.idleTimeoutCache = d
	return d
}

type SSHConfig struct {
	Enabled    bool   `yaml:"enabled"`
	KnownHosts string `yaml:"known_hosts"`
}

type RecordingConfig struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{Addr: ":8080"},
		Shell: ShellConfig{
			Env: []string{"TERM=xterm-256color"},
		},
		Sessions: SessionsConfig{
			MaxPerUser:     10,
			IdleTimeoutStr: "30m",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) ApplyEnv() {
	if v := os.Getenv("SHELLFB_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("SHELLFB_DOMAIN"); v != "" {
		c.Server.Domain = v
	}
	if v := os.Getenv("SHELLFB_AUTOCERT_DIR"); v != "" {
		c.Server.AutocertDir = v
	}
	if v := os.Getenv("SHELLFB_TLS_CERT"); v != "" {
		c.Server.TLS.Cert = v
	}
	if v := os.Getenv("SHELLFB_TLS_KEY"); v != "" {
		c.Server.TLS.Key = v
	}
	if v := os.Getenv("SHELLFB_AUTH_ENABLED"); v != "" {
		c.Auth.Enabled, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("SHELLFB_JWT_SECRET"); v != "" {
		c.Auth.JWTSecret = v
	}
}

func (c *Config) Validate() error {
	if c.Server.Domain != "" && (c.Server.TLS.Cert != "" || c.Server.TLS.Key != "") {
		return fmt.Errorf("config conflict: 'domain' (auto-TLS) and 'tls.cert'/'tls.key' (manual TLS) cannot both be set")
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add domain, autocert_dir fields, env var resolution, and validation"
```

---

### Task 2: Add CLI flags for domain and TLS

**Files:**
- Modify: `cmd/shellfb/main.go`

- [ ] **Step 1: Write the updated main.go with new flags and env resolution**

```go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/valorisa/ShellFromBrowser/internal/auth"
	"github.com/valorisa/ShellFromBrowser/internal/config"
	"github.com/valorisa/ShellFromBrowser/internal/server"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		fmt.Print("Enter password: ")
		var password string
		fmt.Scanln(&password)
		hash, err := auth.HashPassword(password)
		if err != nil {
			log.Fatalf("hash error: %v", err)
		}
		fmt.Println(hash)
		os.Exit(0)
	}

	addr := flag.String("addr", "", "listen address (overrides config)")
	domain := flag.String("domain", "", "domain for auto-TLS via Let's Encrypt (overrides config)")
	tlsCert := flag.String("tls-cert", "", "path to TLS certificate (overrides config)")
	tlsKey := flag.String("tls-key", "", "path to TLS private key (overrides config)")
	autocertDir := flag.String("autocert-dir", "", "directory to store auto-TLS certificates (overrides config)")
	configPath := flag.String("config", "", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ShellFromBrowser %s (%s)\n", version, commit)
		os.Exit(0)
	}

	var cfg *config.Config
	var err error
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
	} else {
		cfg = config.Default()
	}

	// Priority: env vars override config file
	cfg.ApplyEnv()

	// Priority: CLI flags override env vars
	if *addr != "" {
		cfg.Server.Addr = *addr
	}
	if *domain != "" {
		cfg.Server.Domain = *domain
	}
	if *tlsCert != "" {
		cfg.Server.TLS.Cert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.Server.TLS.Key = *tlsKey
	}
	if *autocertDir != "" {
		cfg.Server.AutocertDir = *autocertDir
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}

	srv := server.New(cfg.Server.Addr, cfg)
	log.Printf("ShellFromBrowser %s starting on %s", version, cfg.Server.Addr)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down")
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/shellfb`
Expected: Build succeeds with no errors.

- [ ] **Step 3: Verify flag help includes new flags**

Run: `go run ./cmd/shellfb -help 2>&1 | head -20`
Expected: Shows `-addr`, `-domain`, `-tls-cert`, `-tls-key`, `-autocert-dir`, `-config`, `-version`.

- [ ] **Step 4: Commit**

```bash
git add cmd/shellfb/main.go
git commit -m "feat(cli): add --domain, --tls-cert, --tls-key, --autocert-dir flags"
```

---

### Task 3: Implement auto-TLS listener in the server

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

- [ ] **Step 1: Write failing test for TLS mode selection**

Add to `internal/server/server_test.go`:

```go
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

	srv := server.New(":8080", cfg)
	mode := srv.ListenMode()
	if mode != "http" {
		t.Errorf("ListenMode() = %q, want http", mode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -v -run "TestListenMode"`
Expected: FAIL — `ListenMode()` method doesn't exist.

- [ ] **Step 3: Implement auto-TLS server with ListenMode and updated ListenAndServe**

Replace `internal/server/server.go` with:

```go
package server

import (
	"crypto/tls"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/valorisa/ShellFromBrowser/internal/auth"
	"github.com/valorisa/ShellFromBrowser/internal/config"
	"github.com/valorisa/ShellFromBrowser/internal/recording"
	"github.com/valorisa/ShellFromBrowser/internal/terminal"
	"github.com/valorisa/ShellFromBrowser/internal/transfer"
	"github.com/valorisa/ShellFromBrowser/web"
)

type Server struct {
	addr     string
	mux      *http.ServeMux
	cfg      *config.Config
	authProv auth.Provider
	sessions *terminal.Manager
}

func New(addr string, cfg *config.Config) *Server {
	s := &Server{addr: addr, mux: http.NewServeMux(), cfg: cfg}

	if cfg.Auth.Enabled {
		s.authProv = auth.NewLocalProvider(&cfg.Auth)
	}

	s.sessions = terminal.NewManager(cfg.Sessions.MaxPerUser)

	transferHandler := transfer.NewHandler("./transfers", 50*1024*1024)
	s.mux.HandleFunc("/api/upload", s.authMiddleware(transferHandler.Upload))
	s.mux.HandleFunc("/api/download", s.authMiddleware(transferHandler.Download))

	recordingDir := "./recordings"
	if cfg.Recording.Dir != "" {
		recordingDir = cfg.Recording.Dir
	}
	player := recording.NewPlayer(recordingDir)
	s.mux.HandleFunc("/api/recordings", s.authMiddleware(player.HandleList))
	s.mux.HandleFunc("/api/recordings/get", s.authMiddleware(player.HandleGet))

	loginLimiter := newRateLimiter(5, time.Minute)
	s.mux.HandleFunc("/api/login", loginLimiter.Middleware(s.handleLogin))
	s.mux.HandleFunc("/api/sessions", s.authMiddleware(s.handleSessions))
	s.mux.HandleFunc("/ws", s.authMiddleware(s.handleWebSocket))
	s.mux.HandleFunc("/ws/ssh", s.authMiddleware(s.handleSSHWebSocket))

	staticFS, _ := fs.Sub(web.StaticFiles, "static")
	s.mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := web.StaticFiles.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	return s
}

func (s *Server) ListenMode() string {
	if s.cfg.Server.Domain != "" {
		return "autocert"
	}
	if s.cfg.Server.TLS.Cert != "" && s.cfg.Server.TLS.Key != "" {
		return "manual-tls"
	}
	return "http"
}

func (s *Server) Handler() http.Handler {
	return securityHeaders(s.mux)
}

func (s *Server) ListenAndServe() error {
	switch s.ListenMode() {
	case "autocert":
		return s.listenAutoTLS()
	case "manual-tls":
		return http.ListenAndServeTLS(s.addr, s.cfg.Server.TLS.Cert, s.cfg.Server.TLS.Key, securityHeaders(s.mux))
	default:
		return http.ListenAndServe(s.addr, securityHeaders(s.mux))
	}
}

func (s *Server) listenAutoTLS() error {
	cacheDir := s.cfg.Server.AutocertDir
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".shellfb", "certs")
	}
	os.MkdirAll(cacheDir, 0700)

	m := &autocert.Manager{
		Cache:      autocert.DirCache(cacheDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(s.cfg.Server.Domain),
	}

	// HTTP server on :80 for ACME challenge + redirect
	httpSrv := &http.Server{
		Addr:    ":80",
		Handler: m.HTTPHandler(http.HandlerFunc(redirectHTTPS)),
	}
	go func() {
		log.Printf("Listening on :80 (HTTP redirect + ACME challenge)")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP redirect server error: %v", err)
		}
	}()

	// HTTPS server on :443 with autocert
	tlsSrv := &http.Server{
		Addr:    ":443",
		Handler: securityHeaders(s.mux),
		TLSConfig: &tls.Config{
			GetCertificate: m.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		},
	}

	log.Printf("Auto-TLS: obtaining certificate for %s...", s.cfg.Server.Domain)
	log.Printf("Listening on :443 (HTTPS)")
	return tlsSrv.ListenAndServeTLS("", "")
}

func redirectHTTPS(w http.ResponseWriter, r *http.Request) {
	target := "https://" + r.Host + r.URL.RequestURI()
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
	Error string `json:"error,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, loginResponse{Error: "invalid request"})
		return
	}

	if s.authProv == nil {
		writeJSON(w, http.StatusOK, loginResponse{Token: "no-auth"})
		return
	}

	token, err := s.authProv.Authenticate(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, loginResponse{Error: "invalid credentials"})
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authProv == nil {
			next(w, r)
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		_, err := s.authProv.ValidateToken(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	username := "anonymous"
	if s.authProv != nil {
		token := r.URL.Query().Get("token")
		if token == "" {
			h := r.Header.Get("Authorization")
			if len(h) > 7 {
				token = h[7:]
			}
		}
		claims, err := s.authProv.ValidateToken(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		username = claims.Username
	}

	switch r.Method {
	case http.MethodGet:
		sessions := s.sessions.ListByUser(username)
		writeJSON(w, http.StatusOK, sessions)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id != "" {
			s.sessions.Destroy(id)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v -run "TestListenMode|TestWebSocket"`
Expected: ALL PASS

- [ ] **Step 5: Verify full build succeeds**

Run: `go build ./cmd/shellfb`
Expected: Build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): implement auto-TLS with autocert and HTTP redirect"
```

---

### Task 4: Update config.example.yaml

**Files:**
- Modify: `config.example.yaml`

- [ ] **Step 1: Rewrite config.example.yaml with new fields**

```yaml
# ShellFromBrowser configuration
#
# All settings can be overridden via environment variables (SHELLFB_ prefix)
# Priority: env var > CLI flag > this file > defaults

server:
  addr: ":8080"

  # Set a domain to enable automatic TLS via Let's Encrypt.
  # When set, the server listens on :443 (HTTPS) and :80 (redirect).
  # The 'addr' field is ignored in auto-TLS mode.
  domain: ""

  # Directory to cache auto-TLS certificates (default: ~/.shellfb/certs)
  autocert_dir: ""

  # Manual TLS (cannot be combined with 'domain')
  tls:
    cert: ""
    key: ""

auth:
  enabled: false
  jwt_secret: "change-me-to-a-random-string"
  users:
    - username: admin
      # Generate with: shellfb hash-password
      password_hash: ""

shell:
  # Leave empty for system default (SHELL on Unix, COMSPEC on Windows)
  command: ""
  env:
    - "TERM=xterm-256color"

sessions:
  max_per_user: 10
  idle_timeout: "30m"

ssh:
  enabled: true
  known_hosts: "~/.ssh/known_hosts"

recording:
  enabled: false
  dir: "./recordings"
```

- [ ] **Step 2: Verify config loads without error**

Run: `go run ./cmd/shellfb --config config.example.yaml --version`
Expected: Prints version and exits (config loads successfully).

- [ ] **Step 3: Commit**

```bash
git add config.example.yaml
git commit -m "docs(config): update example with domain, autocert_dir, and env var comments"
```

---

### Task 5: Update Dockerfile

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Rewrite Dockerfile for multi-port and non-root with capabilities**

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev) -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo none)" -o /bin/shellfb ./cmd/shellfb

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache \
    bash \
    openssh-client \
    ca-certificates \
    libcap \
    && adduser -D -h /home/shellfb shellfb \
    && mkdir -p /var/lib/shellfb/certs \
    && chown shellfb:shellfb /var/lib/shellfb/certs

COPY --from=builder /bin/shellfb /usr/local/bin/shellfb
RUN setcap 'cap_net_bind_service=+ep' /usr/local/bin/shellfb

COPY config.example.yaml /etc/shellfb/config.yaml

USER shellfb
WORKDIR /home/shellfb

EXPOSE 80 443

ENTRYPOINT ["shellfb"]
CMD ["--config", "/etc/shellfb/config.yaml"]
```

- [ ] **Step 2: Verify Docker build succeeds**

Run: `docker build -t shellfb:test .`
Expected: Build completes successfully.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "feat(docker): expose 80+443, add NET_BIND_SERVICE capability, certs volume"
```

---

### Task 6: Update docker-compose.yml and add reverse proxy variant

**Files:**
- Modify: `docker-compose.yml`
- Create: `docker-compose.reverse-proxy.yml`

- [ ] **Step 1: Rewrite docker-compose.yml for auto-TLS mode**

```yaml
services:
  shellfb:
    build: .
    image: valorisa/shellfb:latest
    container_name: shellfb
    ports:
      - "80:80"
      - "443:443"
    environment:
      - SHELLFB_DOMAIN=shell.example.com
      - SHELLFB_AUTH_ENABLED=true
      - SHELLFB_JWT_SECRET=change-me-to-a-random-string
    volumes:
      - certs:/var/lib/shellfb/certs
      - ./config.yaml:/etc/shellfb/config.yaml:ro
      - shellfb-recordings:/home/shellfb/recordings
    restart: unless-stopped

volumes:
  certs:
  shellfb-recordings:
```

- [ ] **Step 2: Create docker-compose.reverse-proxy.yml**

```yaml
# Use this variant when running behind a reverse proxy (Nginx, Caddy, Traefik)
# that handles TLS termination.
#
# Usage: docker compose -f docker-compose.reverse-proxy.yml up -d

services:
  shellfb:
    build: .
    image: valorisa/shellfb:latest
    container_name: shellfb
    environment:
      - SHELLFB_ADDR=:8080
      - SHELLFB_AUTH_ENABLED=true
      - SHELLFB_JWT_SECRET=change-me-to-a-random-string
    volumes:
      - ./config.yaml:/etc/shellfb/config.yaml:ro
      - shellfb-recordings:/home/shellfb/recordings
    expose:
      - "8080"
    restart: unless-stopped

  caddy:
    image: caddy:2
    container_name: shellfb-caddy
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    restart: unless-stopped

volumes:
  shellfb-recordings:
  caddy-data:
  caddy-config:
```

- [ ] **Step 3: Create Caddyfile example**

Create `Caddyfile.example`:

```text
shell.example.com {
    reverse_proxy shellfb:8080
}
```

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml docker-compose.reverse-proxy.yml Caddyfile.example
git commit -m "feat(docker): add auto-TLS compose and reverse-proxy variant with Caddy"
```

---

### Task 7: Write deployment guide for restricted networks

**Files:**
- Create: `docs/deploiement-reseaux-contraints.md`

- [ ] **Step 1: Write the full guide in French**

```markdown
# Déploiement en réseau contraint

## Le problème

Dans les aéroports, gares, centres commerciaux et entreprises, le réseau
ne laisse passer que les ports 53 (DNS), 80 (HTTP) et 443 (HTTPS).
Les connexions SSH (port 22), VPN, et la plupart des autres protocoles
sont bloqués.

## Pourquoi ShellFromBrowser passe

ShellFromBrowser utilise du **HTTPS standard** (port 443) et du **WebSocket**
(protocole HTTP). Pour un firewall, c'est indistinguable d'une visite sur
un site web ordinaire.

```text
┌─ Réseau contraint ──────────────────────────────────────┐
│                                                          │
│  Navigateur ──HTTPS:443──▶ Firewall ──▶ Internet        │
│              (site web normal pour le firewall)          │
│                                                          │
└──────────────────────────────────────────────────────────┘
                                              │
                                              ▼
                                    ┌─ Ton serveur ────────┐
                                    │                       │
                                    │  ShellFromBrowser     │
                                    │  (déchiffre, exécute) │
                                    │                       │
                                    └───────────────────────┘
```

Le navigateur fait du HTTPS. Le firewall voit du HTTPS. Personne ne sait
qu'il y a un terminal derrière.

## Prérequis

- Un serveur (VPS) accessible sur Internet
- Un nom de domaine pointant vers ce serveur (DNS A record)
- Les ports 80 et 443 ouverts sur le serveur

## Déploiement — Méthode 1 : Auto-TLS (recommandé)

La méthode la plus simple. ShellFromBrowser obtient son certificat tout seul.

### Option A : Binaire standalone

```bash
# Télécharger le binaire
wget https://github.com/valorisa/ShellFromBrowser/releases/latest/download/shellfb-linux-amd64
chmod +x shellfb-linux-amd64

# Lancer avec auto-TLS
./shellfb-linux-amd64 --domain shell.monserveur.com
```

C'est tout. Le certificat est obtenu automatiquement, HTTPS actif sur :443.

### Option B : Docker one-liner

```bash
docker run -d --name shellfb \
  -p 80:80 -p 443:443 \
  -e SHELLFB_DOMAIN=shell.monserveur.com \
  -v shellfb-certs:/var/lib/shellfb/certs \
  valorisa/shellfb
```

### Option C : Docker Compose

```bash
# Éditer docker-compose.yml : remplacer shell.example.com par votre domaine
docker compose up -d
```

## Déploiement — Méthode 2 : Derrière un reverse proxy

Si vous avez déjà Nginx, Caddy ou Traefik sur votre serveur :

```bash
docker compose -f docker-compose.reverse-proxy.yml up -d
```

ShellFromBrowser écoute en HTTP sur :8080, le reverse proxy gère le TLS.

### Exemple Caddy

```text
shell.monserveur.com {
    reverse_proxy shellfb:8080
}
```

### Exemple Nginx

```nginx
server {
    listen 443 ssl;
    server_name shell.monserveur.com;

    ssl_certificate /etc/letsencrypt/live/shell.monserveur.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/shell.monserveur.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

## Ports requis

| Côté | Port | Protocole | Requis |
|------|------|-----------|--------|
| Serveur (entrant) | 443 | HTTPS | Oui |
| Serveur (entrant) | 80 | HTTP | Oui (auto-TLS) ou Non (reverse proxy) |
| Client (sortant) | 443 | HTTPS | Oui — c'est tout ce dont le navigateur a besoin |

## Utilisation depuis un réseau contraint

1. Ouvrir un navigateur (Chrome, Firefox, Safari, Edge)
2. Aller sur `https://shell.monserveur.com`
3. Se connecter avec ses identifiants
4. Utiliser le terminal

Rien à installer côté client. Aucune configuration réseau. Aucun VPN.

## FAQ

### Et si le port 80 est déjà pris sur mon serveur ?

Utilisez la méthode reverse proxy. Votre Nginx/Caddy existant gère déjà
les ports 80/443 — ajoutez simplement un virtual host pour ShellFromBrowser.

### Et sans nom de domaine ?

L'auto-TLS nécessite un domaine. Alternatives :
- Utiliser un sous-domaine gratuit (DuckDNS, FreeDNS)
- Utiliser un certificat auto-signé (TLS manuel) — attention : le navigateur
  affichera un avertissement

### Et derrière un proxy d'entreprise (type Zscaler) ?

Si le proxy fait de l'inspection TLS (MITM), le WebSocket devrait quand
même fonctionner car il utilise le protocole HTTP standard. Si ce n'est
pas le cas, c'est un scénario prévu pour une version future.

### Le trafic est-il détectable ?

Non. ShellFromBrowser utilise :
- Du vrai HTTPS (pas du SSH déguisé)
- Un certificat Let's Encrypt valide
- Le protocole WebSocket standard (RFC 6455)
- Un upgrade HTTP classique

Même l'inspection profonde (DPI) ne voit qu'un site web HTTPS normal.
```

- [ ] **Step 2: Commit**

```bash
git add docs/deploiement-reseaux-contraints.md
git commit -m "docs: add restricted network deployment guide (FR)"
```

---

### Task 8: Add summary section to README.fr.md

**Files:**
- Modify: `README.fr.md`

- [ ] **Step 1: Read current README.fr.md to find insertion point**

Look for the "Utilisation" or features section to insert the network traversal summary after it.

- [ ] **Step 2: Add network section to README.fr.md**

Insert after the features/usage section:

```markdown
## Réseau contraint (aéroport, gare, entreprise)

ShellFromBrowser fonctionne partout où un navigateur peut ouvrir un site web.
Il utilise du HTTPS standard (port 443) et du WebSocket — aucun firewall ne
le distingue d'une visite sur un site web ordinaire.

Configuration minimale pour être accessible depuis n'importe quel réseau :

```bash
shellfb --domain shell.monserveur.com
```

Voir le [guide complet de déploiement en réseau contraint](docs/deploiement-reseaux-contraints.md).
```

- [ ] **Step 3: Commit**

```bash
git add README.fr.md
git commit -m "docs: add restricted network summary to French README"
```

---

### Task 9: Run full test suite and final verification

**Files:** None (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -v -race`
Expected: ALL PASS, no race conditions.

- [ ] **Step 2: Run the linter (if available)**

Run: `go vet ./...`
Expected: No issues.

- [ ] **Step 3: Verify Docker build**

Run: `docker build -t shellfb:test .`
Expected: Build succeeds.

- [ ] **Step 4: Verify the binary starts in HTTP mode**

Run: `go run ./cmd/shellfb --version`
Expected: Prints version string.

- [ ] **Step 5: Commit any remaining fixes if needed**

If tests revealed issues, fix and commit. Otherwise, no action.

---

## Summary of commits

1. `feat(config): add domain, autocert_dir fields, env var resolution, and validation`
2. `feat(cli): add --domain, --tls-cert, --tls-key, --autocert-dir flags`
3. `feat(server): implement auto-TLS with autocert and HTTP redirect`
4. `docs(config): update example with domain, autocert_dir, and env var comments`
5. `feat(docker): expose 80+443, add NET_BIND_SERVICE capability, certs volume`
6. `feat(docker): add auto-TLS compose and reverse-proxy variant with Caddy`
7. `docs: add restricted network deployment guide (FR)`
8. `docs: add restricted network summary to French README`
