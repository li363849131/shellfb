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
	// Handle subcommands before flag parsing
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

	// Priority order: 1) Load config, 2) Apply env vars, 3) Apply CLI flags, 4) Validate
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

	// Apply environment variables (override config file)
	cfg.ApplyEnv()

	// Apply CLI flags (override env vars and config file)
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

	// Validate configuration (check for conflicts)
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation: %v", err)
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
