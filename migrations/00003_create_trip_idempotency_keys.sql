-- +goose Up
CREATE TABLE trip_idempotency_keys (
    key uuid PRIMARY KEY,
    response jsonb NOT NULL CHECK (jsonb_typeof(response) = 'object'),
    expires_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE trip_idempotency_keys;
