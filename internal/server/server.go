package server

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/li363849131/shellfb/internal/ai"
	"github.com/li363849131/shellfb/internal/auth"
	"github.com/li363849131/shellfb/internal/config"
	"github.com/li363849131/shellfb/internal/recording"
	"github.com/li363849131/shellfb/internal/terminal"
	"github.com/li363849131/shellfb/internal/transfer"
	"github.com/li363849131/shellfb/web"
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

	transferDir := "/root"
	if cfg.Transfer.Dir != "" {
		transferDir = cfg.Transfer.Dir
	}
	maxSize := int64(50 * 1024 * 1024)
	if cfg.Transfer.MaxSize > 0 {
		maxSize = cfg.Transfer.MaxSize
	}
	transferHandler := transfer.NewHandler(transferDir, maxSize)
	s.mux.HandleFunc("/api/upload", s.authMiddleware(transferHandler.Upload))
	s.mux.HandleFunc("/api/download", s.authMiddleware(transferHandler.Download))
	s.mux.HandleFunc("/api/files", s.authMiddleware(transferHandler.ListFiles))
	s.mux.HandleFunc("/api/convert", s.authMiddleware(transferHandler.Convert))
	s.mux.HandleFunc("/api/delete", s.authMiddleware(transferHandler.Delete))
	s.mux.HandleFunc("/api/links", s.authMiddleware(s.handleLinks))

	voiceURL, _ := url.Parse("http://127.0.0.1:30022")
	s.mux.Handle("/api/voice/", http.StripPrefix("/api/voice", httputil.NewSingleHostReverseProxy(voiceURL)))

	// Recording routes
	recordingDir := "./recordings"
	if cfg.Recording.Dir != "" {
		recordingDir = cfg.Recording.Dir
	}
	player := recording.NewPlayer(recordingDir)
	s.mux.HandleFunc("/api/recordings", s.authMiddleware(player.HandleList))
	s.mux.HandleFunc("/api/recordings/get", s.authMiddleware(player.HandleGet))

	// Natural-language -> shell command. The command is only *typed* into the
	// terminal; the user still presses Enter, so a bad suggestion cannot run
	// on its own.
	aiHandler := ai.NewHandler()
	aiLimiter := newRateLimiter(20, time.Minute)
	s.mux.HandleFunc("/api/ai/status", s.authMiddleware(aiHandler.HandleStatus))
	s.mux.HandleFunc("/api/ai/translate", s.authMiddleware(aiLimiter.Middleware(aiHandler.HandleTranslate)))

	loginLimiter := newRateLimiter(5, time.Minute)
	s.mux.HandleFunc("/api/login", loginLimiter.Middleware(s.handleLogin))
	s.mux.HandleFunc("/api/sessions", s.authMiddleware(s.handleSessions))
	s.mux.HandleFunc("/ws", s.authMiddleware(s.handleWebSocket))
	s.mux.HandleFunc("/ws/ssh", s.authMiddleware(s.handleSSHWebSocket))

	staticFS, _ := fs.Sub(web.StaticFiles, "static")
	fileServer := http.FileServer(http.FS(staticFS))
	// embed.FS reports a zero modtime, so http.FileServer emits no
	// Last-Modified and no ETag. With no validator the browser falls back to
	// heuristic caching and can serve stale JS indefinitely. Emit a
	// content-derived ETag so every rebuild invalidates the cache on its own,
	// independent of the ?v= tag in index.html.
	s.mux.Handle("/static/", http.StripPrefix("/static/", staticETag(staticFS, fileServer)))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := web.StaticFiles.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// index.html carries the ?v= tags for everything else; it must never
		// be served from cache or the tags can't do their job.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.Write(data)
	})

	return s
}

// staticETag hashes the embedded file for the request path and serves a 304
// when the client's If-None-Match already matches. Hashes are computed once
// per path and cached, since embedded content cannot change at runtime.
func staticETag(fsys fs.FS, next http.Handler) http.Handler {
	var mu sync.RWMutex
	cache := map[string]string{}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || strings.Contains(name, "..") {
			next.ServeHTTP(w, r)
			return
		}

		mu.RLock()
		tag, ok := cache[name]
		mu.RUnlock()

		if !ok {
			data, err := fs.ReadFile(fsys, name)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			sum := sha256.Sum256(data)
			tag = `"` + hex.EncodeToString(sum[:16]) + `"`
			mu.Lock()
			cache[name] = tag
			mu.Unlock()
		}

		w.Header().Set("ETag", tag)
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, tag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Handler() http.Handler {
	return securityHeaders(s.mux)
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

func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request) {
	fbURL := getCFTunnelURL("cf-filebrowser")
	vsURL := getCFTunnelURL("cf-vscode")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"filebrowser_url":  fbURL,
		"vscode_url":       vsURL,
		"filebrowser_port": "30010",
		"vscode_port":      "30011",
	})
}

func getCFTunnelURL(service string) string {
	out, err := exec.Command("journalctl", "-u", service, "-n", "50").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)
	matches := re.FindAllString(string(out), -1)
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}
