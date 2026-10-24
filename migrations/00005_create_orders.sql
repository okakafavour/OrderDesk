
BEGIN;

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL
        REFERENCES businesses(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL
        REFERENCES customers(id) ON DELETE RESTRICT,
    order_number VARCHAR(40) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN (
            'pending',
            'confirmed',
            'processing',
            'completed',
            'cancelled'
        )),
    total_kobo BIGINT NOT NULL DEFAULT 0
        CHECK (total_kobo >= 0),
    notes TEXT,
    confirmed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT orders_confirmed_at_consistency CHECK (
        (status = 'pending' AND confirmed_at IS NULL)
        OR
        (status <> 'pending' AND confirmed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX orders_business_number_unique
    ON orders (business_id, order_number);

CREATE INDEX orders_business_created_idx
    ON orders (business_id, created_at DESC);

CREATE INDEX orders_business_status_idx
    ON orders (business_id, status);

CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL
        REFERENCES orders(id) ON DELETE CASCADE,
    product_id UUID NOT NULL
        REFERENCES products(id) ON DELETE RESTRICT,
    product_name VARCHAR(150) NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price_kobo BIGINT NOT NULL CHECK (unit_price_kobo >= 0),
    line_total_kobo BIGINT NOT NULL CHECK (line_total_kobo >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT order_items_line_total_check CHECK (
        line_total_kobo = unit_price_kobo * quantity
    )
);

CREATE INDEX order_items_order_id_idx
    ON order_items (order_id);

COMMIT;
