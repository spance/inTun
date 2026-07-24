package sftp

import "context"

func (s *Client) UploadDirOverwriteReport(ctx context.Context, localDir, targetRemoteDir string) (OverwriteReport, error) {
	plan, err := s.PlanUploadDir(ctx, localDir, targetRemoteDir)
	return plan.Overwrites, err
}

func (s *Client) DownloadDirOverwriteReport(ctx context.Context, sourceRemoteDir, localDir string) (OverwriteReport, error) {
	plan, err := s.PlanDownloadDir(ctx, sourceRemoteDir, localDir)
	return plan.Overwrites, err
}
