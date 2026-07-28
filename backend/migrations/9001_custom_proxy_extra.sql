-- 定制迁移：为 proxies 增加协议扩展参数列（shadowsocks 插件参数等）
-- 编号使用 9001_ 高位段，与上游迁移编号永久隔离，避免合并上游时撞号。
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS extra JSONB;
