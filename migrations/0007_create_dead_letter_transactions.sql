CREATE TABLE dead_letter_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payload JSONB NOT NULL,
    error_reason TEXT,
    failed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
