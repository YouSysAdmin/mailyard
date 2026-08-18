-- Invitation tokens move to hashed-at-rest, hex(sha256(plaintext)).
--
-- 00013 stored the token in the clear, reasoning that an invitation is
-- redeemable only by a caller whose account email matches the invited
-- address. That still holds, but password-reset and signup-verification
-- tokens are hashed, and the asymmetry meant a read-only copy of this
-- table - a dump, a backup, a replica - handed out live invitation
-- links where the other two token tables would not. The plaintext still
-- leaves exactly once, in the create response and the invitation mail.
-- It is simply no longer what the table holds.
--
-- A data migration, which this repository otherwise avoids: the ones
-- that were removed rewrote rows that cannot exist on a fresh install
-- AND had a lazy code path covering existing ones. This has no lazy
-- path - without the rewrite every invitation outstanding at upgrade
-- time would stop redeeming, since the lookup now hashes what the link
-- presents. Rewriting keeps them working.

-- +goose Up
UPDATE project_invitations SET token = encode(sha256(convert_to(token, 'UTF8')), 'hex');

-- +goose Down
-- Irreversible and does not need reversing: the plaintext cannot be
-- recovered from the hash, a binary old enough to compare plaintext
-- simply finds no invitation, and an invitation is re-issuable.
SELECT 1;
