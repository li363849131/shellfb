package config

import (
	"errors"
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
	Transfer  TransferConfig  `yaml:"transfer"`
}

type TransferConfig struct {
	Dir     string `yaml:"dir"`
	MaxSize int64  `yaml:"max_size"`
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

// GetIdleTimeout returns the parsed idle timeout duration
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
		Server: ServerConfig{Addr: ":4200"},
		Shell: ShellConfig{
			Env: []string{"TERM=xterm-256color"},
		},
		Sessions: SessionsConfig{
			MaxPerUser:     10,
			IdleTimeoutStr: "30m",
		},
		Transfer: TransferConfig{
			Dir:     "/root",
			MaxSize: 50 * 1024 * 1024,
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

// ApplyEnv reads SHELLFB_* environment variables and overrides config values.
// This allows 12-factor config style: env vars override config file values.
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
		if b, err := strconv.ParseBool(v); err == nil {
			c.Auth.Enabled = b
		}
	}
	if v := os.Getenv("SHELLFB_JWT_SECRET"); v != "" {
		c.Auth.JWTSecret = v
	}
}

// Validate checks for conflicting configuration values.
// Returns an error if the configuration is invalid.
func (c *Config) Validate() error {
	// Check for conflict between auto-TLS (domain) and manual TLS (cert/key)
	if c.Server.Domain != "" && (c.Server.TLS.Cert != "" || c.Server.TLS.Key != "") {
		return errors.New("config conflict: 'domain' (auto-TLS) and 'tls.cert'/'tls.key' (manual TLS) cannot both be set")
	}
	return nil
}
