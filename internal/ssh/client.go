package ssh

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

type ConnectOpts struct {
	Target   string // user@host:port
	Password string
	KeyFile  string
	Cols     uint32
	Rows     uint32
}

type SSHSession struct {
	client  *gossh.Client
	session *gossh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func ParseTarget(target string) (user, host, port string) {
	port = "22"

	if at := strings.LastIndex(target, "@"); at >= 0 {
		user = target[:at]
		target = target[at+1:]
	}

	if colon := strings.LastIndex(target, ":"); colon >= 0 {
		host = target[:colon]
		port = target[colon+1:]
	} else {
		host = target
	}
	return
}

func Connect(opts ConnectOpts) (*SSHSession, error) {
	user, host, port := ParseTarget(opts.Target)
	if user == "" {
		user = "root"
	}

	var authMethods []gossh.AuthMethod
	if opts.Password != "" {
		authMethods = append(authMethods, gossh.Password(opts.Password))
	}
	if opts.KeyFile != "" {
		if signer, err := loadKey(opts.KeyFile); err == nil {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}

	config := &gossh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}

	addr := net.JoinHostPort(host, port)
	client, err := gossh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("ssh session: %w", err)
	}

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", int(opts.Rows), int(opts.Cols), modes); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}

	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &SSHSession{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

func (s *SSHSession) Read(p []byte) (int, error) {
	return s.stdout.Read(p)
}

func (s *SSHSession) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

func (s *SSHSession) Resize(cols, rows uint32) error {
	return s.session.WindowChange(int(rows), int(cols))
}

func (s *SSHSession) Close() error {
	s.stdin.Close()
	s.session.Close()
	return s.client.Close()
}

func loadKey(path string) (gossh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return gossh.ParsePrivateKey(key)
}
