-- The UTC offset scheduled_at was written with, in seconds. A campaign
-- sent at each subscriber's local time delivers at the wall clock as
-- written, and the stored instant alone has lost it. NULL for a campaign
-- scheduled before this column, which reads its wall clock in UTC.

-- +goose Up
ALTER TABLE campaigns ADD COLUMN scheduled_offset INTEGER;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS scheduled_offset;
