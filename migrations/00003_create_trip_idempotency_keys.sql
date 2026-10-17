-- +goose Up
CREATE TABLE trip_idempotency_keys (
    key uuid PRIMARY KEY,
    response jsonb NOT NULL CHECK (jsonb_typeof(response) = 'object'),
    expires_at timestamptz NOT NULL
);

CREATE INDEX trip_idempotency_keys_expires_at_idx ON trip_idempotency_keys (expires_at);

-- +goose Down
DROP TABLE trip_idempotency_keys;
