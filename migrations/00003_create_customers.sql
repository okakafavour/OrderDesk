
BEGIN;

CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL
        REFERENCES businesses(id) ON DELETE CASCADE,
    full_name VARCHAR(120) NOT NULL,
    email VARCHAR(255),
    phone VARCHAR(30),
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX customers_business_created_idx
    ON customers (business_id, created_at DESC);

CREATE INDEX customers_business_email_idx
    ON customers (business_id, email);

CREATE INDEX customers_business_phone_idx
    ON customers (business_id, phone);

COMMIT;
