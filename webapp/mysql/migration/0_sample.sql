-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
ALTER TABLE orders
ADD INDEX idx_user_id (user_id);

ALTER TABLE orders
ADD INDEX idx_shipped_status (shipped_status);

ALTER TABLE orders
ADD INDEX idx_product_id (product_id);

ALTER TABLE orders
ADD INDEX idx_created_at (created_at);

ALTER TABLE products
ADD INDEX idx_name (name);

ALTER TABLE users
ADD INDEX idx_user_name (user_name);