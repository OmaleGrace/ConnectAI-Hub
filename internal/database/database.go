package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

func Open(databaseURL string) (*sql.DB, error) {
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is not set; copy .env.example to .env and configure PostgreSQL")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return db, nil
}

func Migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_admin BOOLEAN NOT NULL DEFAULT FALSE
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS purpose TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS organization TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_suspended BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS plan TEXT NOT NULL DEFAULT 'free';
ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_limit BIGINT NOT NULL DEFAULT 100;
ALTER TABLE users ADD COLUMN IF NOT EXISTS plus_expires_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_verified BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE IF NOT EXISTS phone_verification_codes (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'general',
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS notifications_user_created_idx ON notifications(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS plan_payments (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reference TEXT NOT NULL UNIQUE,
    amount BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'NGN',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','paid','failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS plan_payments_user_created_idx ON plan_payments(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO notification_preferences(user_id,email_notifications)
SELECT id,TRUE FROM users ON CONFLICT(user_id) DO NOTHING;
CREATE TABLE IF NOT EXISTS sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id);
CREATE TABLE IF NOT EXISTS password_resets (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 token_hash TEXT NOT NULL UNIQUE,
 expires_at TIMESTAMPTZ NOT NULL,
 used_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS password_resets_user_id_idx ON password_resets(user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS ai_messages (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    module_slug TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('user','model')),
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ai_messages_user_module_created_idx
    ON ai_messages(user_id, module_slug, created_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS saved_prompts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    module_slug TEXT NOT NULL,
    prompt TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS saved_prompts_user_created_idx ON saved_prompts(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS ai_usage (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    module_slug TEXT NOT NULL,
    request_type TEXT NOT NULL CHECK (request_type IN ('chat','file_analysis')),
    input_characters INTEGER NOT NULL DEFAULT 0,
    output_characters INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ai_usage_user_created_idx ON ai_usage(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ai_usage_user_module_idx ON ai_usage(user_id, module_slug);
CREATE INDEX IF NOT EXISTS ai_usage_created_idx ON ai_usage(created_at DESC);
ALTER TABLE ai_usage DROP CONSTRAINT IF EXISTS ai_usage_request_type_check;
ALTER TABLE ai_usage ADD CONSTRAINT ai_usage_request_type_check CHECK (request_type IN ('chat','file_analysis','regenerate','website_build','edit'));
CREATE TABLE IF NOT EXISTS ai_message_feedback (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 message_id BIGINT NOT NULL REFERENCES ai_messages(id) ON DELETE CASCADE,
 rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
 comment TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(user_id,message_id)
);
CREATE TABLE IF NOT EXISTS workspace_preferences (
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 module_slug TEXT NOT NULL,
 custom_instructions TEXT NOT NULL DEFAULT '',
 temperature DOUBLE PRECISION NOT NULL DEFAULT 0.7 CHECK (temperature BETWEEN 0.1 AND 1.0),
 pinned BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(user_id,module_slug)
);
CREATE TABLE IF NOT EXISTS ai_response_versions (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 module_slug TEXT NOT NULL,
 user_message_id BIGINT NOT NULL REFERENCES ai_messages(id) ON DELETE CASCADE,
 response_text TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ai_response_versions_user_idx ON ai_response_versions(user_id,module_slug,created_at DESC);
CREATE TABLE IF NOT EXISTS ai_model_events (
 id BIGSERIAL PRIMARY KEY,
 event_type TEXT NOT NULL,
 model TEXT NOT NULL,
 duration_ms BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ai_model_events_created_idx ON ai_model_events(created_at DESC);
CREATE TABLE IF NOT EXISTS website_projects (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title TEXT NOT NULL,
 prompt TEXT NOT NULL,
 html TEXT NOT NULL,
 css TEXT NOT NULL DEFAULT '',
 js TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS website_projects_user_updated_idx ON website_projects(user_id,updated_at DESC);
CREATE TABLE IF NOT EXISTS website_project_versions (
 id BIGSERIAL PRIMARY KEY,
 project_id BIGINT NOT NULL REFERENCES website_projects(id) ON DELETE CASCADE,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title TEXT NOT NULL,
 html TEXT NOT NULL,
 css TEXT NOT NULL DEFAULT '',
 js TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS website_project_versions_idx ON website_project_versions(project_id,user_id,created_at DESC);
ALTER TABLE ai_usage DROP CONSTRAINT IF EXISTS ai_usage_request_type_check;
ALTER TABLE ai_usage ADD CONSTRAINT ai_usage_request_type_check CHECK (request_type IN ('chat','file_analysis','regenerate','website_build','edit'));
CREATE INDEX IF NOT EXISTS users_created_idx ON users(created_at DESC);
CREATE TABLE IF NOT EXISTS user_memories (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 fact TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS user_memories_user_idx ON user_memories(user_id,id DESC);
ALTER TABLE users ADD COLUMN IF NOT EXISTS memory_enabled BOOLEAN NOT NULL DEFAULT TRUE;
`
	_, err := db.Exec(schema)
	return err
}
