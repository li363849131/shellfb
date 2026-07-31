package ssh_test

import (
	"testing"

	sshclient "github.com/valorisa/ShellFromBrowser/internal/ssh"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		input    string
		wantUser string
		wantHost string
		wantPort string
	}{
		{"user@host.com", "user", "host.com", "22"},
		{"user@host.com:2222", "user", "host.com", "2222"},
		{"host.com", "", "host.com", "22"},
		{"root@192.168.1.1:22", "root", "192.168.1.1", "22"},
	}

	for _, tt := range tests {
		user, host, port := sshclient.ParseTarget(tt.input)
		if user != tt.wantUser || host != tt.wantHost || port != tt.wantPort {
			t.Errorf("ParseTarget(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tt.input, user, host, port, tt.wantUser, tt.wantHost, tt.wantPort)
		}
	}
}
