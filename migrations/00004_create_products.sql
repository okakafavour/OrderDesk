BEGIN;

CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL
        REFERENCES businesses(id) ON DELETE CASCADE,
    name VARCHAR(150) NOT NULL,
    description TEXT,
    sku VARCHAR(80),
    price_kobo BIGINT NOT NULL CHECK (price_kobo >= 0),
    stock_quantity INTEGER NOT NULL DEFAULT 0
        CHECK (stock_quantity >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT products_name_not_blank
        CHECK (LENGTH(TRIM(name)) > 0)
);

CREATE UNIQUE INDEX products_business_sku_unique
    ON products (business_id, LOWER(sku))
    WHERE sku IS NOT NULL;

CREATE INDEX products_business_created_idx
    ON products (business_id, created_at DESC);

CREATE INDEX products_business_active_idx
    ON products (business_id, is_active);

COMMIT;
