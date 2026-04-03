-- Improve query performance for the deployments datatable (sorting + search).
CREATE INDEX IF NOT EXISTS idx_deploy_jobs_queued_at ON deploy_jobs(queued_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_jobs_branch    ON deploy_jobs(branch);
