DROP FUNCTION IF EXISTS claim_invitation_deliveries(int, int);
DROP TABLE IF EXISTS invitation_outbox;

-- Restore NOT NULL. Any not-yet-delivered invitations (token_hash IS NULL) have
-- no usable token, so drop them before reinstating the constraint.
DELETE FROM invitations WHERE token_hash IS NULL;
ALTER TABLE invitations ALTER COLUMN token_hash SET NOT NULL;
