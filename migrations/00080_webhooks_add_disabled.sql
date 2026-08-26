-- A webhook that fails every attempt is taken out of rotation.
--
-- Before this a dead receiver was retried on every event forever, each
-- event parking a goroutine and holding a delivery slot through the
-- retry sleeps, so eight dead hooks stalled every project's deliveries.
-- Now the final failure disables the hook, the project's owners are
-- mailed why, and it is re-enabled by hand once the endpoint is fixed.

-- +goose Up
ALTER TABLE webhooks
    ADD COLUMN disabled_at     TIMESTAMPTZ,
    ADD COLUMN disabled_reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webhooks
    DROP COLUMN IF EXISTS disabled_at,
    DROP COLUMN IF EXISTS disabled_reason;
