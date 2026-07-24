package sftp

import "context"

func (s *Client) UploadDir(ctx context.Context, localDir, targetRemoteDir string, progress func(done, total int64, file string)) (TransferReport, error) {
	return s.UploadDirWithOptions(ctx, localDir, targetRemoteDir, TransferOptions{}, progress)
}

func (s *Client) UploadDirWithOptions(ctx context.Context, localDir, targetRemoteDir string, options TransferOptions, progress func(done, total int64, file string)) (TransferReport, error) {
	plan, err := s.PlanUploadDir(ctx, localDir, targetRemoteDir)
	if err != nil {
		return plan.Report, err
	}
	return s.ExecuteSyncPlan(ctx, plan, options, progress)
}
