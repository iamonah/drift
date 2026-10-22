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
    claim_token UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    -- lease_expires_at prevents a report from been held indefinitely by a worker that crashed
    lease_expires_at TIMESTAMPTZ,
    processing_limit INT NOT NULL DEFAULT 5,
    processing_attempts INT NOT NULL DEFAULT 0,

    PRIMARY KEY (user_id, id),
    CONSTRAINT reports_status_check CHECK (
        status IN ('pending', 'processing', 'completed', 'failed')
    ),
    CONSTRAINT reports_processing_limit_check CHECK (processing_limit > 0),
    CONSTRAINT reports_processing_attempts_check CHECK (processing_attempts >= 0)
    
);