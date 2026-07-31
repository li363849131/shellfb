# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-05-15

### Added

- Browser-based terminal emulator with xterm.js (256 colors, Unicode, mouse support)
- WebSocket server with bidirectional PTY communication
- Cross-platform PTY spawning (Unix via creack/pty, Windows via ConPTY/pipes)
- JWT authentication with bcrypt password hashing
- YAML configuration system with sensible defaults
- Multi-session support with tab-based UI
- Session manager with per-user limits
- SSH client integration for connecting to remote hosts
- File upload/download with path traversal protection
- Session recording in asciicast v2 format (asciinema-compatible)
- TLS/HTTPS built-in support
- Security headers (CSP, X-Frame-Options, X-Content-Type-Options)
- Rate limiting on login endpoint
- `hash-password` CLI subcommand for generating bcrypt hashes
- Multi-stage Dockerfile and docker-compose deployment
- GitHub Actions CI with cross-platform testing and binary builds
- Embedded static assets via go:embed (single binary distribution)

[Unreleased]: https://github.com/valorisa/ShellFromBrowser/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/valorisa/ShellFromBrowser/releases/tag/v0.1.0
