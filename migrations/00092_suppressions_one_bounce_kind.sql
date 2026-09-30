-- A hard bounce is one suppression kind, bounce, whichever path saw it.

-- +goose Up
UPDATE suppressions SET kind = 'bounce' WHERE kind = 'hard';

-- +goose Down
SELECT 1;
