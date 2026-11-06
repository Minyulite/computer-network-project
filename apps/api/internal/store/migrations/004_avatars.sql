-- Cartoon avatars for people and shop assistants.
-- Avatar values are identifiers like p01 or b03; the pictures live in the web app.
ALTER TABLE users ADD COLUMN avatar text NOT NULL DEFAULT '';
ALTER TABLE users ADD CONSTRAINT users_avatar_format CHECK (avatar COLLATE "C" ~ '^([pb][0-9]{2})?$');
ALTER TABLE bots ADD COLUMN avatar text NOT NULL DEFAULT '';
ALTER TABLE bots ADD CONSTRAINT bots_avatar_format CHECK (avatar COLLATE "C" ~ '^(b[0-9]{2})?$');

-- Give existing accounts a stable picture so nobody is left with an empty one.
UPDATE bots SET avatar = 'b' || lpad(((hashtext(id::text) & 2147483647) % 8 + 1)::text, 2, '0');
UPDATE users u SET avatar = b.avatar FROM bots b WHERE b.user_id = u.id;
UPDATE users SET avatar = 'p' || lpad(((hashtext(id::text) & 2147483647) % 12 + 1)::text, 2, '0')
    WHERE avatar = '' AND NOT is_bot;
