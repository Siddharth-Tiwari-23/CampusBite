-- 003_add_payments_indexes.up.sql

CREATE INDEX IF NOT EXISTS idx_payments_provider_order_id ON payments(provider_order_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
