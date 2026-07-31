package ai

import "testing"

func TestParseResult(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantCmd     string
		wantExplain string
		wantErr     bool
	}{
		{
			name:        "plain json",
			in:          `{"command":"ls -la","explain":"列出文件"}`,
			wantCmd:     "ls -la",
			wantExplain: "列出文件",
		},
		{
			name:        "wrapped in markdown fence",
			in:          "```json\n{\"command\":\"df -h\",\"explain\":\"磁盘\"}\n```",
			wantCmd:     "df -h",
			wantExplain: "磁盘",
		},
		{
			name:        "prose before json",
			in:          "Here you go:\n{\"command\":\"pwd\",\"explain\":\"当前目录\"}",
			wantCmd:     "pwd",
			wantExplain: "当前目录",
		},
		{
			// A newline would submit the first line and run the rest unreviewed.
			name:        "multiline command is truncated to first line",
			in:          `{"command":"echo a\necho b","explain":"两条"}`,
			wantCmd:     "echo a",
			wantExplain: "两条",
		},
		{
			name:        "strips shell prompt",
			in:          `{"command":"$ whoami","explain":"用户"}`,
			wantCmd:     "whoami",
			wantExplain: "用户",
		},
		{
			name:        "refusal with empty command is not an error",
			in:          `{"command":"","explain":"请求不明确"}`,
			wantCmd:     "",
			wantExplain: "请求不明确",
		},
		{
			name:        "danger prefix preserved",
			in:          `{"command":"rm -rf /tmp/x","explain":"危险: 删除目录"}`,
			wantCmd:     "rm -rf /tmp/x",
			wantExplain: "危险: 删除目录",
		},
		{name: "empty reply", in: "", wantErr: true},
		{name: "no json at all", in: "I cannot help with that", wantErr: true},
		{name: "malformed json", in: `{"command": ls}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, explain, err := parseResult(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got cmd=%q", cmd)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd != tt.wantCmd {
				t.Errorf("command = %q, want %q", cmd, tt.wantCmd)
			}
			if explain != tt.wantExplain {
				t.Errorf("explain = %q, want %q", explain, tt.wantExplain)
			}
		})
	}
}

func TestEnabledRequiresToken(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if NewHandler().Enabled() {
		t.Error("Enabled() = true with no credentials")
	}

	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-token")
	if !NewHandler().Enabled() {
		t.Error("Enabled() = false with ANTHROPIC_AUTH_TOKEN set")
	}
}

func TestBaseURLTrailingSlashStripped(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "t")
	t.Setenv("ANTHROPIC_BASE_URL", "https://example.com/")
	if got := NewHandler().baseURL; got != "https://example.com" {
		t.Errorf("baseURL = %q, want %q", got, "https://example.com")
	}
}
