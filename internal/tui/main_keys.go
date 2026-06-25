package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	sftplib "github.com/pkg/sftp"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/tunnel"
)

func (m Model) handleMainKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tunnels := m.manager.List()

	switch msg.String() {
	case "c":
		if len(m.hosts) == 0 {
			m.err = fmt.Errorf("no hosts found in ~/.ssh/config")
			return m, nil
		}
		m.err = nil
		m.screen = ScreenSelectHost
		return m, nil
	case "y":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusError && strings.Contains(t.Error, "HOST_KEY_NOT_CACHED") {
				m.manager.Restart(t.ID)
			}
		}
	case "r":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Restart(tunnels[m.selectedIndex].ID)
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "s":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				m.manager.Stop(t.ID)
			} else if t.Status == tunnel.StatusStopped {
				m.manager.Restart(t.ID)
			} else if t.Status == tunnel.StatusConnecting {
				m.setStatusMsg("Cannot stop: tunnel is connecting")
			} else if t.Status == tunnel.StatusError {
				m.setStatusMsg("Use [r] to reconnect")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "d":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Delete(tunnels[m.selectedIndex].ID)
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "f":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				rawClient, err := m.manager.GetSFTPClient(t.ID)
				if err != nil {
					m.err = err
					return m, nil
				}
				sftpRaw, ok := rawClient.(*sftplib.Client)
				if !ok {
					m.err = fmt.Errorf("invalid SFTP client type")
					return m, nil
				}
				m.sftpClient = sftp.NewClient(sftpRaw)
				cwd, _ := os.Getwd()
				m.sftpLocalDir = cwd
				remoteDir, _ := sftpRaw.Getwd()
				if remoteDir == "" {
					remoteDir = "/"
				}
				m.sftpRemoteDir = remoteDir
				localFiles, lerr := sftp.ReadLocalDir(m.sftpLocalDir)
				if lerr != nil {
					m.err = lerr
					m.sftpClient = nil
					return m, nil
				}
				remoteFiles, rerr := m.sftpClient.ReadRemoteDir(m.sftpContext(), m.sftpRemoteDir)
				if rerr != nil {
					m.err = rerr
					m.sftpClient = nil
					return m, nil
				}
				m.sftpLocalFiles = localFiles
				m.sftpRemoteFiles = remoteFiles
				m.sftpFocus = 0
				m.sftpCursor = [2]int{0, 0}
				m.sftpScroll = [2]int{0, 0}
				m = m.normalizeSFTPScroll()
				m.sftpTransferring = false
				m.sftpPreview = ""
				m.sftpPreviewing = false
				m.sftpCancel = nil
				m.sftpTunnelID = t.ID
				m.sftpHostLabel = fmt.Sprintf("%s@%s:%s", t.SSHConfig.User, t.SSHConfig.Host, t.SSHConfig.Port)
				m.sftpDone = nil
				m.sftpDirection = ""
				m.screen = ScreenSFTP
			} else {
				m.setStatusMsg("Tunnel must be running to use SFTP")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
	case "down", "j":
		if m.selectedIndex < len(tunnels)-1 {
			m.selectedIndex++
		}
	case "e", "q", "ctrl+c":
		if m.hasLiveTunnels() {
			m.confirmQuit = true
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) hasLiveTunnels() bool {
	for _, t := range m.manager.List() {
		status := t.GetStatus()
		if status == tunnel.StatusRunning || status == tunnel.StatusConnecting {
			return true
		}
	}
	return false
}
