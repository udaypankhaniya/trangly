-- Add deploy_mode column to projects table.
-- Defaults to 'rebuild' (full pipeline) for existing and new projects.
ALTER TABLE projects ADD COLUMN deploy_mode TEXT NOT NULL DEFAULT 'rebuild';
