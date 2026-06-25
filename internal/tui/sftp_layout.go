package tui

import (
	"context"

	"github.com/spance/intun/internal/sftp"
)

const (
	sftpTopBarRows          = 1
	sftpShortcutRows        = 1
	sftpViewPaddingRows     = 1
	sftpPanelGapRows        = 1
	sftpPanelFrameRows      = 2
	sftpPanelHeaderRows     = 2
	sftpPanelRowsAroundList = sftpPanelFrameRows + sftpPanelHeaderRows
	sftpOuterChromeRows     = sftpTopBarRows + sftpShortcutRows + sftpViewPaddingRows + sftpPanelGapRows + sftpPanelRowsAroundList
	sftpMinListRows         = 5
	sftpRenameDrawerRows    = 3
	sftpTransferDrawerRows  = 4
	sftpPreviewFrameRows    = 3
)

func (m Model) currentSFTPFiles() []sftp.FileEntry {
	if m.sftpFocus == 0 {
		return m.sftpLocalFiles
	}
	return m.sftpRemoteFiles
}

func (m Model) sftpContext() context.Context {
	if m.cancelCtx != nil {
		return m.cancelCtx
	}
	return context.Background()
}

func (m Model) sftpListVisibleItems() int {
	visible := m.height - sftpOuterChromeRows - m.sftpDrawerHeight()
	if visible < sftpMinListRows {
		return sftpMinListRows
	}
	return visible
}

func (m Model) sftpPanelHeight() int {
	return m.sftpListVisibleItems() + sftpPanelRowsAroundList
}

func (m Model) normalizeSFTPScroll() Model {
	visibleItems := m.sftpListVisibleItems()
	for panel := 0; panel < 2; panel++ {
		totalItems := len(m.sftpLocalFiles) + 1
		if panel == 1 {
			totalItems = len(m.sftpRemoteFiles) + 1
		}
		if totalItems < 1 {
			totalItems = 1
		}

		maxCursor := totalItems - 1
		if m.sftpCursor[panel] < 0 {
			m.sftpCursor[panel] = 0
		}
		if m.sftpCursor[panel] > maxCursor {
			m.sftpCursor[panel] = maxCursor
		}
		if m.sftpScroll[panel] < 0 {
			m.sftpScroll[panel] = 0
		}
		if m.sftpCursor[panel] < m.sftpScroll[panel] {
			m.sftpScroll[panel] = m.sftpCursor[panel]
		}
		if m.sftpCursor[panel] >= m.sftpScroll[panel]+visibleItems {
			m.sftpScroll[panel] = m.sftpCursor[panel] - visibleItems + 1
		}
		maxScroll := totalItems - visibleItems
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.sftpScroll[panel] > maxScroll {
			m.sftpScroll[panel] = maxScroll
		}
	}
	return m
}

func (m Model) sftpDrawerHeight() int {
	height := 0
	if m.sftpRenaming {
		height += sftpRenameDrawerRows
	}
	if m.sftpTransferring {
		height += sftpTransferDrawerRows
	}
	if m.sftpPreviewing {
		height += m.sftpPreviewHeight() + sftpPreviewFrameRows
	}
	return height
}

func (m Model) sftpPreviewHeight() int {
	height := m.height / 4
	if height < 4 {
		height = 4
	}
	if height > 8 {
		height = 8
	}
	return height
}
