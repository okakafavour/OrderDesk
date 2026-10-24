BEGIN;

ALTER TABLE orders
DROP CONSTRAINT orders_confirmed_at_consistency;

ALTER TABLE orders
ADD CONSTRAINT orders_confirmed_at_consistency CHECK (
    (status = 'pending' AND confirmed_at IS NULL)
    OR
    (status IN ('confirmed', 'processing', 'completed')
        AND confirmed_at IS NOT NULL)
    OR
    status = 'cancelled'
);

COMMIT;
