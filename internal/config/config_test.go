package config

import (
	"os"
	"os/user"
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

Host labeled-host
    #!! GroupLabels VM_debian vm22 vm33
    HostName labeled.example.com
    User labeluser
    Port 2222

Host single-label
    #!! GroupLabels my-server
    HostName single.example.com
`
	configPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	hosts, err := ParseSSHConfig()
	if err != nil {
		t.Fatalf("ParseSSHConfig failed: %v", err)
	}

	var expectedUser string
	if u, err := user.Current(); err == nil {
		expectedUser = u.Username
	} else {
		expectedUser = os.Getenv("USER")
	}

	want := []Host{
		{Name: "simple-host", Hostname: "simple.example.com", User: "simpleuser", Port: "2222"},
		{Name: "full-host", Hostname: "full.example.com", User: "fulluser", Port: "22", IdentityFile: "~/.ssh/id_ed25519", IdentityFiles: []string{"~/.ssh/id_ed25519"}},
		{Name: "minimal-host", Hostname: "minimal-host", User: expectedUser, Port: "22"},
		{Name: "commented-host", Hostname: "commented.example.com", User: "commenteduser", Port: "22"},
		{Name: "labeled-host", Hostname: "labeled.example.com", User: "labeluser", Port: "2222", Labels: []string{"VM_debian", "vm22", "vm33"}},
		{Name: "single-label", Hostname: "single.example.com", User: expectedUser, Port: "22", Labels: []string{"my-server"}},
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

	var expectedUser string
	if u, err := user.Current(); err == nil {
		expectedUser = u.Username
	} else {
		expectedUser = os.Getenv("USER")
	}
	if h.User != expectedUser {
		t.Errorf("User = %q, want %q", h.User, expectedUser)
	}
}

func TestParseSSHConfigResolvesIncludesPatternsAndMultipleAliases(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.Mkdir(sshDir, 0755); err != nil {
		t.Fatal(err)
	}
	includedPath := filepath.Join(sshDir, "included.conf")
	if err := os.WriteFile(includedPath, []byte(`
Host included
    HostName included.example.com
    User include-user
`), 0600); err != nil {
		t.Fatal(err)
	}
	configContent := `
Include ` + includedPath + `

Host alpha beta
    #!! GroupLabels production database
    HostName backend.example.com
    IdentityFile ~/.ssh/service_ed25519
    IdentityFile ~/.ssh/service_rsa
    IdentityAgent ~/.ssh/agent.sock
    IdentitiesOnly yes
    ProxyJump jump-host

Host *
    User default-user
    Port 2200
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", tmpDir)
	hosts, err := ParseSSHConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Fatalf("host count = %d, want 3: %#v", len(hosts), hosts)
	}
	byName := make(map[string]Host)
	for _, host := range hosts {
		byName[host.Name] = host
	}
	included := byName["included"]
	if included.Hostname != "included.example.com" || included.User != "include-user" || included.Port != "2200" {
		t.Fatalf("included host = %#v", included)
	}
	for _, alias := range []string{"alpha", "beta"} {
		host := byName[alias]
		if host.Hostname != "backend.example.com" || host.User != "default-user" || host.Port != "2200" {
			t.Fatalf("%s resolved host = %#v", alias, host)
		}
		if diff := cmp.Diff([]string{"~/.ssh/service_ed25519", "~/.ssh/service_rsa"}, host.IdentityFiles); diff != "" {
			t.Fatalf("%s identities mismatch (-want +got):\n%s", alias, diff)
		}
		if !host.IdentitiesOnly || host.IdentityAgent != "~/.ssh/agent.sock" || host.ProxyJump != "jump-host" {
			t.Fatalf("%s SSH options = %#v", alias, host)
		}
		if diff := cmp.Diff([]string{"production", "database"}, host.Labels); diff != "" {
			t.Fatalf("%s labels mismatch (-want +got):\n%s", alias, diff)
		}
	}
}
