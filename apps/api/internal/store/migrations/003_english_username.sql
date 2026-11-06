-- Usernames keep the case they were typed in and may use any printable ASCII
-- character, 3 to 24 of them, with no spaces at either end. The service also
-- requires at least one letter; that is not checked here so older accounts made
-- under the previous rule, which allowed digits only, still pass.
-- Uniqueness ignores case, so "Mark" and "mark" cannot both exist.
ALTER TABLE users DROP CONSTRAINT users_username_format;
ALTER TABLE users ADD CONSTRAINT users_username_format CHECK (
    username COLLATE "C" ~ '^[ -~]{3,24}$'
    AND username = btrim(username)
);
CREATE UNIQUE INDEX users_username_lower_key ON users (lower(username));
