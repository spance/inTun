package sftp

import "fmt"

type SkippedItem struct {
	Path   string
	Reason string
}

const maxSkippedDetails = 5

type TransferReport struct {
	Skipped      []SkippedItem
	SkippedCount int
}

type ExistingItem struct {
	Path string
	Kind string
}

const maxExistingDetails = 5

type OverwriteReport struct {
	Items []ExistingItem
	Count int
}

func (r *TransferReport) addSkipped(path string, reason interface{}) {
	r.SkippedCount++
	if len(r.Skipped) >= maxSkippedDetails {
		return
	}

	reasonText := fmt.Sprint(reason)
	if reasonText == "" {
		reasonText = "skipped"
	}
	r.Skipped = append(r.Skipped, SkippedItem{
		Path:   path,
		Reason: reasonText,
	})
}

func (r TransferReport) HasSkipped() bool {
	return r.SkippedCount > 0
}

func (r *OverwriteReport) addExisting(path, kind string) {
	r.Count++
	if len(r.Items) >= maxExistingDetails {
		return
	}
	if kind == "" {
		kind = "existing target"
	}
	r.Items = append(r.Items, ExistingItem{
		Path: path,
		Kind: kind,
	})
}

func (r OverwriteReport) HasOverwrites() bool {
	return r.Count > 0
}

func (r *OverwriteReport) AddExisting(path, kind string) {
	r.addExisting(path, kind)
}
