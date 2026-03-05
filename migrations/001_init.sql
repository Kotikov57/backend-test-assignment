CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS balances (
    user_id BIGINT NOT NULL,
    currency TEXT NOT NULL,
    amount_micros BIGINT NOT NULL CHECK (amount_micros >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, currency)
);

CREATE TABLE IF NOT EXISTS withdrawals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL,
    amount_micros BIGINT NOT NULL CHECK (amount_micros > 0),
    currency TEXT NOT NULL CHECK (currency = 'USDT'),
    destination TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'confirmed')),
    idempotency_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    response_status INT,
    response_body JSONB,
    withdrawal_id UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, idempotency_key)
);

INSERT INTO balances (user_id, currency, amount_micros)
VALUES (1, 'USDT', 100000000)
ON CONFLICT (user_id, currency) DO NOTHING;
