-- 003_add_payments_indexes.down.sql

DROP INDEX IF EXISTS idx_payments_provider_order_id;
DROP INDEX IF EXISTS idx_payments_status;
