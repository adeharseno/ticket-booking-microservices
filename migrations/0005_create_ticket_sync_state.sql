CREATE TABLE ticket_sync_state (
    ticket_id UUID PRIMARY KEY,
    quantity INT NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
