CREATE TABLE IF NOT EXISTS notification_preferences (user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, email_notifications BOOLEAN NOT NULL DEFAULT TRUE, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
INSERT INTO notification_preferences(user_id,email_notifications) SELECT id,TRUE FROM users ON CONFLICT(user_id) DO NOTHING;
