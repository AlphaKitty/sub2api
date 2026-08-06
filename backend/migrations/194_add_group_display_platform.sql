-- 分组展示平台：仅影响用户侧图标/徽章/标签的展示品牌，不参与功能路由、计费、账号池。
-- 空值（NULL）表示不覆盖，用户侧按真实 platform 展示。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS display_platform VARCHAR(50);
