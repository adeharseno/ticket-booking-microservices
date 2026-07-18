CREATE TABLE transaction_payment (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id VARCHAR(255) NOT NULL UNIQUE,
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    amount NUMERIC(15, 2) NOT NULL,
    status VARCHAR(50) NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
