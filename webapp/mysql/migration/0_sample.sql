-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
ALTER TABLE orders
ADD INDEX idx_user_id (user_id);

ALTER TABLE orders
ADD INDEX idx_shipped_status (shipped_status);

-- 全文検索ではなく通常のインデックスを使用（書き込みパフォーマンス優先）
ALTER TABLE products
ADD INDEX idx_name (name),
ADD INDEX idx_description (description(100));