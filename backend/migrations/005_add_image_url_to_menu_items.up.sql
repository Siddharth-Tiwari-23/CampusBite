-- 005_add_image_url_to_menu_items.up.sql
ALTER TABLE menu_items ADD COLUMN IF NOT EXISTS image_url TEXT NOT NULL DEFAULT '';
