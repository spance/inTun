package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Host struct {
	Name         string
	Hostname     string
	User         string
	Port         string
	IdentityFile string
}

func ParseSSHConfig() ([]Host, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(home, ".ssh", "config")
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ssh config: %w", err)
	}
	defer file.Close()

	var hosts []Host
	var currentHost *Host

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.ToLower(parts[0])
		value := strings.Join(parts[1:], " ")

		switch key {
		case "host":
			if currentHost != nil {
				hosts = append(hosts, *currentHost)
			}
			hostName := parts[1]
			currentHost = &Host{Name: hostName}
		case "hostname":
			if currentHost != nil {
				currentHost.Hostname = value
			}
		case "user":
			if currentHost != nil {
				currentHost.User = value
			}
		case "port":
			if currentHost != nil {
				currentHost.Port = value
			}
		case "identityfile":
			if currentHost != nil {
				currentHost.IdentityFile = value
			}
		}
	}

	if currentHost != nil {
		hosts = append(hosts, *currentHost)
	}

	var validHosts []Host
	currentUsername := os.Getenv("USER")
	if currentUsername == "" {
		currentUsername = os.Getenv("LOGNAME")
	}

	for _, h := range hosts {
		if strings.Contains(h.Name, "*") {
			continue
		}
		if h.Hostname == "" {
			h.Hostname = h.Name
		}
		if h.Port == "" {
			h.Port = "22"
		}
		if h.User == "" && currentUsername != "" {
			h.User = currentUsername
		}
		validHosts = append(validHosts, h)
	}

	return validHosts, scanner.Err()
}

func GetSSHConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}
