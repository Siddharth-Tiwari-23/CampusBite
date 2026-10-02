-- 005_add_image_url_to_menu_items.down.sql
ALTER TABLE menu_items DROP COLUMN IF EXISTS image_url;
