-- Records the intent to publish a report job. The row is written in the same
-- transaction as the report insert, so a report never exists without a pending
-- publication, and the publisher can retry independently of the request.
--
-- Publishing is split into a short claim statement and a separate SQS send.
-- These columns make a claim durable after the transaction commits.
CREATE TABLE report_outbox (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    report_id UUID NOT NULL,
    report_type VARCHAR NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,
    claim_token UUID,
    claimed_at TIMESTAMPTZ,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    attempts INT NOT NULL DEFAULT 0,
    send_failures INT NOT NULL DEFAULT 0,
    last_error TEXT,
    dead_at TIMESTAMPTZ, -- automatic publishing is stopped after this time, and the row is no longer claimable
    CONSTRAINT report_outbox_report_fk
    FOREIGN KEY (user_id, report_id) REFERENCES reports (user_id, id) ON DELETE CASCADE,
    CONSTRAINT report_outbox_terminal_state_check
    CHECK (NOT (published_at IS NOT NULL AND dead_at IS NOT NULL))
);

CREATE UNIQUE INDEX report_outbox_report_idx
    ON report_outbox (user_id, report_id);

-- Claimable rows are selected by retry time, then creation order. Published
-- and dead rows are excluded from the index entirely.
CREATE INDEX report_outbox_claimable_idx
    ON report_outbox (next_attempt_at, created_at)
    WHERE published_at IS NULL AND dead_at IS NULL;
