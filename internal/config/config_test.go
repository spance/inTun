package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseSSHConfig(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.Mkdir(sshDir, 0755); err != nil {
		t.Fatalf("failed to create .ssh dir: %v", err)
	}

	configContent := `
Host simple-host
    HostName simple.example.com
    User simpleuser
    Port 2222

Host full-host
    HostName full.example.com
    User fulluser
    Port 22
    IdentityFile ~/.ssh/id_ed25519

Host minimal-host
    # No HostName, uses host name

Host wildcard-*
    HostName wildcard.example.com
    User wildcarduser

# Comment line
Host commented-host
    HostName commented.example.com
    User commenteduser
`
	configPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	origUser := os.Getenv("USER")
	os.Setenv("USER", "testuser")
	defer os.Setenv("USER", origUser)

	hosts, err := ParseSSHConfig()
	if err != nil {
		t.Fatalf("ParseSSHConfig failed: %v", err)
	}

	want := []Host{
		{Name: "simple-host", Hostname: "simple.example.com", User: "simpleuser", Port: "2222"},
		{Name: "full-host", Hostname: "full.example.com", User: "fulluser", Port: "22", IdentityFile: "~/.ssh/id_ed25519"},
		{Name: "minimal-host", Hostname: "minimal-host", User: "testuser", Port: "22"},
		{Name: "commented-host", Hostname: "commented.example.com", User: "commenteduser", Port: "22"},
	}

	if len(hosts) != len(want) {
		t.Errorf("got %d hosts, want %d", len(hosts), len(want))
	}

	for i, h := range hosts {
		if i >= len(want) {
			break
		}
		if diff := cmp.Diff(h, want[i]); diff != "" {
			t.Errorf("host[%d] mismatch (-want +got):\n%s", i, diff)
		}
	}
}

func TestParseSSHConfigNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	_, err := ParseSSHConfig()
	if err == nil {
		t.Error("expected error when config file doesn't exist")
	}
}

func TestGetSSHConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	path := GetSSHConfigPath()
	want := filepath.Join(tmpDir, ".ssh", "config")
	if path != want {
		t.Errorf("GetSSHConfigPath() = %q, want %q", path, want)
	}
}

func TestHostDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	os.Mkdir(sshDir, 0755)

	configContent := `Host no-port-no-user
    HostName example.com
`
	configPath := filepath.Join(sshDir, "config")
	os.WriteFile(configPath, []byte(configContent), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	origUser := os.Getenv("USER")
	os.Setenv("USER", "defaultuser")
	defer os.Setenv("USER", origUser)

	hosts, err := ParseSSHConfig()
	if err != nil {
		t.Fatalf("ParseSSHConfig failed: %v", err)
	}

	if len(hosts) != 1 {
		t.Fatalf("got %d hosts, want 1", len(hosts))
	}

	h := hosts[0]
	if h.Port != "22" {
		t.Errorf("Port = %q, want %q", h.Port, "22")
	}
	if h.User != "defaultuser" {
		t.Errorf("User = %q, want %q", h.User, "defaultuser")
	}
}
