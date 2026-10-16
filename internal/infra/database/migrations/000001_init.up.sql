-- Emails are compared case-insensitively.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id UUID PRIMARY KEY,
    email CITEXT NOT NULL UNIQUE,
    hashed_password BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE refresh_tokens (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hashed_token BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP + INTERVAL '1 day',
    token_type VARCHAR NOT NULL DEFAULT 'refresh',
    client_ip VARCHAR NOT NULL,
    user_agent VARCHAR NOT NULL,
    is_blocked BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (user_id, hashed_token)
);

CREATE TABLE reports (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    id UUID NOT NULL,
    report_type VARCHAR NOT NULL,
    status VARCHAR NOT NULL DEFAULT 'pending',
    output_object_key VARCHAR,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, id),
    CONSTRAINT reports_status_check CHECK (status IN ('pending', 'processing', 'completed', 'failed'))
);
