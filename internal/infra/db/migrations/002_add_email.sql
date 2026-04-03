-- Add email column to users table (nullable for backward compat).
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
