-- Records the intent to publish a report job. The row is written in the same
-- transaction as the report insert, so a report never exists without a pending
-- publication, and the publisher can retry independently of the request.
CREATE TABLE report_outbox (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    report_id UUID NOT NULL,
    report_type VARCHAR NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMPTZ,
    CONSTRAINT report_outbox_report_fk
    FOREIGN KEY (user_id, report_id) REFERENCES reports (user_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX report_outbox_report_idx
    ON report_outbox (user_id, report_id);

CREATE INDEX report_outbox_unpublished_idx
    ON report_outbox (created_at)
    WHERE published_at IS NULL;