-- 002_add_idempotency_status.up.sql

ALTER TABLE idempotency_keys 
ADD COLUMN IF NOT EXISTS status VARCHAR(50) NOT NULL DEFAULT 'PROCESSING' CHECK (status IN ('PROCESSING', 'COMPLETED', 'FAILED'));
