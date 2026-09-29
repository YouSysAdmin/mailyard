-- A hard bounce is one suppression kind, bounce, whichever path saw it.
-- hard was only ever written by a caller choosing it by hand.

-- +goose Up
UPDATE suppressions SET kind = 'bounce' WHERE kind = 'hard';

-- +goose Down
SELECT 1;
