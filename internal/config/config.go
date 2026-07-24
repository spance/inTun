package config

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	sshconfig "github.com/kevinburke/ssh_config"
)

const maxIncludeDepth = 8

type Host struct {
	Name           string
	Hostname       string
	User           string
	Port           string
	IdentityFile   string
	IdentityFiles  []string
	IdentityAgent  string
	IdentitiesOnly bool
	ProxyJump      string
	JumpHosts      []Host
	Labels         []string
}

type hostIndex struct {
	order  []string
	labels map[string][]string
	seen   map[string]struct{}
	files  map[string]struct{}
	home   string
}

func ParseSSHConfig() ([]Host, error) {
	configPath := GetSSHConfigPath()
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open ssh config: %w", err)
	}
	defer file.Close()

	parsed, err := sshconfig.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ssh config: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	index := hostIndex{
		labels: make(map[string][]string),
		seen:   make(map[string]struct{}),
		files:  make(map[string]struct{}),
		home:   home,
	}
	if err := index.collect(configPath, 0); err != nil {
		return nil, err
	}

	defaultUser := currentUsername()
	hosts := make([]Host, 0, len(index.order))
	for _, alias := range index.order {
		host, err := resolveHost(parsed, alias, defaultUser)
		if err != nil {
			return nil, fmt.Errorf("resolve SSH host %q: %w", alias, err)
		}
		host.Labels = append([]string(nil), index.labels[alias]...)
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func resolveHost(parsed *sshconfig.Config, alias, defaultUser string) (Host, error) {
	host, err := resolveHostWithoutJumps(parsed, alias, defaultUser)
	if err != nil {
		return Host{}, err
	}
	proxyJump, err := parsed.Get(alias, "ProxyJump")
	if err != nil {
		return Host{}, err
	}
	host.ProxyJump = strings.TrimSpace(proxyJump)
	host.JumpHosts, err = resolveJumpHosts(parsed, host.ProxyJump, defaultUser)
	if err != nil {
		return Host{}, err
	}
	return host, nil
}

func resolveJumpHosts(parsed *sshconfig.Config, proxyJump, defaultUser string) ([]Host, error) {
	if proxyJump == "" || strings.EqualFold(proxyJump, "none") {
		return nil, nil
	}
	parts := strings.Split(proxyJump, ",")
	jumps := make([]Host, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		alias, userOverride, portOverride := parseJumpToken(part)
		jump, err := resolveHostWithoutJumps(parsed, alias, defaultUser)
		if err != nil {
			return nil, fmt.Errorf("resolve ProxyJump %q: %w", part, err)
		}
		jump.Name = alias
		if userOverride != "" {
			jump.User = userOverride
		}
		if portOverride != "" {
			jump.Port = portOverride
		}
		jump.ProxyJump = ""
		jump.JumpHosts = nil
		jumps = append(jumps, jump)
	}
	return jumps, nil
}

func resolveHostWithoutJumps(parsed *sshconfig.Config, alias, defaultUser string) (Host, error) {
	get := func(key string) (string, error) {
		value, err := parsed.Get(alias, key)
		return strings.TrimSpace(value), err
	}
	hostname, err := get("Hostname")
	if err != nil {
		return Host{}, err
	}
	if hostname == "" {
		hostname = alias
	}
	username, err := get("User")
	if err != nil {
		return Host{}, err
	}
	if username == "" {
		username = defaultUser
	}
	port, err := get("Port")
	if err != nil {
		return Host{}, err
	}
	if port == "" {
		port = "22"
	}
	identityFiles, err := parsed.GetAll(alias, "IdentityFile")
	if err != nil {
		return Host{}, err
	}
	identityFiles = cleanConfigValues(identityFiles)
	identityAgent, err := get("IdentityAgent")
	if err != nil {
		return Host{}, err
	}
	identitiesOnly, err := get("IdentitiesOnly")
	if err != nil {
		return Host{}, err
	}
	host := Host{
		Name:           alias,
		Hostname:       hostname,
		User:           username,
		Port:           port,
		IdentityFiles:  identityFiles,
		IdentityAgent:  identityAgent,
		IdentitiesOnly: strings.EqualFold(identitiesOnly, "yes"),
	}
	if len(identityFiles) > 0 {
		host.IdentityFile = identityFiles[0]
	}
	return host, nil
}

func parseJumpToken(value string) (alias, username, port string) {
	alias = value
	if at := strings.LastIndex(alias, "@"); at >= 0 {
		username = alias[:at]
		alias = alias[at+1:]
	}
	if host, parsedPort, err := net.SplitHostPort(alias); err == nil {
		return host, username, parsedPort
	}
	if strings.Count(alias, ":") == 1 {
		if host, parsedPort, ok := strings.Cut(alias, ":"); ok {
			return host, username, parsedPort
		}
	}
	return strings.Trim(alias, "[]"), username, ""
}

func cleanConfigValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "none") {
			continue
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (i *hostIndex) collect(configPath string, depth int) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("SSH config Include depth exceeds %d", maxIncludeDepth)
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	if _, ok := i.files[absolute]; ok {
		return nil
	}
	i.files[absolute] = struct{}{}

	file, err := os.Open(absolute)
	if err != nil {
		return fmt.Errorf("open included SSH config %q: %w", absolute, err)
	}
	defer file.Close()

	var currentAliases []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if labels, ok := parseGroupLabels(line); ok {
				for _, alias := range currentAliases {
					i.labels[alias] = append([]string(nil), labels...)
				}
			}
			continue
		}
		key, value := splitDirective(line)
		switch strings.ToLower(key) {
		case "host":
			currentAliases = i.addAliases(strings.Fields(value))
		case "match":
			currentAliases = nil
		case "include":
			for _, directive := range strings.Fields(value) {
				directive = strings.Trim(directive, `"'`)
				includePattern := directive
				switch {
				case strings.HasPrefix(directive, "~/"):
					includePattern = filepath.Join(i.home, directive[2:])
				case !filepath.IsAbs(directive):
					includePattern = filepath.Join(i.home, ".ssh", directive)
				}
				matches, globErr := filepath.Glob(includePattern)
				if globErr != nil {
					return fmt.Errorf("invalid SSH Include %q: %w", directive, globErr)
				}
				for _, match := range matches {
					if err := i.collect(match, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read SSH config %q: %w", absolute, err)
	}
	return nil
}

func (i *hostIndex) addAliases(patterns []string) []string {
	aliases := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if strings.ContainsAny(pattern, "*?!") {
			continue
		}
		aliases = append(aliases, pattern)
		if _, ok := i.seen[pattern]; ok {
			continue
		}
		i.seen[pattern] = struct{}{}
		i.order = append(i.order, pattern)
	}
	return aliases
}

func parseGroupLabels(line string) ([]string, bool) {
	content, ok := strings.CutPrefix(strings.TrimSpace(line), "#!!")
	if !ok {
		return nil, false
	}
	content = strings.TrimSpace(content)
	labels, ok := strings.CutPrefix(content, "GroupLabels ")
	if !ok {
		return nil, false
	}
	return strings.Fields(labels), true
}

func splitDirective(line string) (string, string) {
	line = stripInlineComment(line)
	key, value, ok := strings.Cut(line, "=")
	if ok && !strings.ContainsAny(strings.TrimSpace(key), " \t") {
		return strings.TrimSpace(key), strings.TrimSpace(value)
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	return fields[0], strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
}

func stripInlineComment(line string) string {
	var quote rune
	for index, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case quote == 0 && r == '#':
			return strings.TrimSpace(line[:index])
		}
	}
	return strings.TrimSpace(line)
}

func currentUsername() string {
	if current, err := user.Current(); err == nil {
		return current.Username
	}
	if username := os.Getenv("USER"); username != "" {
		return username
	}
	return os.Getenv("LOGNAME")
}

func GetSSHConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}
