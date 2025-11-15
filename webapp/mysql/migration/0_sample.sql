-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
ALTER TABLE orders
ADD INDEX idx_user_id (user_id);

ALTER TABLE orders
ADD INDEX idx_shipped_status (shipped_status);

ALTER TABLE products
ADD FULLTEXT INDEX idx_ft_name_desc (name, description)
WITH PARSER ngram;