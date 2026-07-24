package sftp

import "context"

func (s *Client) DownloadDir(ctx context.Context, sourceRemoteDir, localDir string, progress func(done, total int64, file string)) (TransferReport, error) {
	return s.DownloadDirWithOptions(ctx, sourceRemoteDir, localDir, TransferOptions{}, progress)
}

func (s *Client) DownloadDirWithOptions(ctx context.Context, sourceRemoteDir, localDir string, options TransferOptions, progress func(done, total int64, file string)) (TransferReport, error) {
	plan, err := s.PlanDownloadDir(ctx, sourceRemoteDir, localDir)
	if err != nil {
		return plan.Report, err
	}
	return s.ExecuteSyncPlan(ctx, plan, options, progress)
}
