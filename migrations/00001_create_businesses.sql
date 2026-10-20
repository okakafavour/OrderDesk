
-- Create the businesses table.
CREATE TABLE IF NOT EXISTS businesses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(120) NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'NGN',
    timezone VARCHAR(64) NOT NULL DEFAULT 'Africa/Lagos',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
