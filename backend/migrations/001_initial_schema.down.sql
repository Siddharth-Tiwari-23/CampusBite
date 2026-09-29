-- 001_initial_schema.down.sql

DROP TABLE IF EXISTS webhook_events;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS inventory_reservations;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS cart_items;
DROP TABLE IF EXISTS carts;
DROP TABLE IF EXISTS inventory;
DROP TABLE IF EXISTS menu_items;
DROP TABLE IF EXISTS users;
