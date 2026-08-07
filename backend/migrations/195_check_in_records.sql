-- 签到赠送额度：每用户每天一条签到记录（硬删除，审计型流水）。
CREATE TABLE IF NOT EXISTS check_in_records (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    check_in_date TIMESTAMPTZ NOT NULL,
    streak_days INTEGER NOT NULL DEFAULT 1,
    reward_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT checkinrecord_user_id_check_in_date UNIQUE (user_id, check_in_date)
);

CREATE INDEX IF NOT EXISTS idx_check_in_records_user_id
    ON check_in_records (user_id);
