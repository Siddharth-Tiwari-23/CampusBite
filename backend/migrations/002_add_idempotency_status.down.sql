-- 002_add_idempotency_status.down.sql

ALTER TABLE idempotency_keys 
DROP COLUMN IF EXISTS status;
