package tui

import (
	"context"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/tunnel"
)

type appState struct {
	screen  Screen
	width   int
	height  int
	version string
}

type hostSelectionState struct {
	hosts         []config.Host
	hostCursor    int
	hostScroll    int
	selectedHost  config.Host
	hostFilter    textinput.Model
	hostFiltering bool
}

type tunnelCreationState struct {
	typeCursor          int
	typeScroll          int
	selectedType        tunnel.TunnelType
	selectedProtocol    tunnel.NetworkProtocol
	localPort           string
	remotePort          string
	portInput           string
	inputMode           int
	manualHostInput     textinput.Model
	pendingTunnelCreate *pendingTunnelCreate
}

type tunnelListState struct {
	selectedIndex int
	trafficHist   map[int][]int64
	confirmQuit   bool
}

type noticeState struct {
	err           error
	statusMsg     string
	statusTicks   int
	statusConfirm bool
}

type authState struct {
	authQueue   *AuthPromptQueue
	promptMode  bool
	promptInput string
}

type sftpState struct {
	sftpClient              *sftp.Client
	sftpLocalDir            string
	sftpRemoteDir           string
	sftpLocalFiles          []sftp.FileEntry
	sftpRemoteFiles         []sftp.FileEntry
	sftpFocus               int
	sftpCursor              [2]int
	sftpScroll              [2]int
	sftpTransferring        bool
	sftpProgress            *sftp.ProgressInfo
	sftpPreview             string
	sftpPreviewing          bool
	sftpLoading             bool
	sftpLoadingLabel        string
	sftpOperationID         int
	sftpOperationCancel     context.CancelFunc
	sftpCancel              context.CancelFunc
	sftpTransferID          int
	sftpTunnelID            int
	sftpHostLabel           string
	sftpDirection           string
	sftpPrevDone            int64
	sftpRenaming            bool
	sftpRenameInput         string
	sftpSyncConfirm         bool
	sftpSyncConfirmMsg      string
	sftpOverwriteConfirm    bool
	sftpOverwriteConfirmMsg string
	sftpPendingSync         sftpPendingSync
	sftpPendingDirPlan      *sftp.SyncPlan
}

type pendingTunnelCreate struct {
	localAddr  string
	remoteAddr string
}

type sftpPendingSync struct {
	focus          int
	source         string
	target         string
	name           string
	size           int64
	allowOverwrite bool
}

type sftpTransferResult struct {
	id        int
	err       error
	source    string
	target    string
	direction string
	report    sftp.TransferReport
}

type runtimeState struct {
	authCtx    *platform.AuthContext
	cancelCtx  context.Context
	cancelFunc context.CancelFunc
}

type componentState struct {
	spinner        spinner.Model
	tunnelViewport viewport.Model
	help           help.Model
}
