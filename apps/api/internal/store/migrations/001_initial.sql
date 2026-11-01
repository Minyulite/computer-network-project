CREATE TABLE users (
    id uuid CONSTRAINT users_pkey PRIMARY KEY,
    username text NOT NULL CONSTRAINT users_username_key UNIQUE,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_format CHECK (username COLLATE "C" ~ '^[a-z0-9_]{3,24}$')
);

CREATE TABLE sessions (
    id uuid CONSTRAINT sessions_pkey PRIMARY KEY,
    user_id uuid NOT NULL,
    token_hash bytea NOT NULL CONSTRAINT sessions_token_hash_key UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT sessions_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT sessions_expiry_after_creation CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE rooms (
    id uuid CONSTRAINT rooms_pkey PRIMARY KEY,
    kind text NOT NULL,
    name text,
    created_by uuid NOT NULL,
    direct_user_low uuid,
    direct_user_high uuid,
    direct_key text GENERATED ALWAYS AS (direct_user_low::text || ':' || direct_user_high::text) STORED,
    last_seq bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT rooms_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT rooms_direct_user_low_fkey FOREIGN KEY (direct_user_low) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT rooms_direct_user_high_fkey FOREIGN KEY (direct_user_high) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT rooms_direct_key_key UNIQUE (direct_key),
    CONSTRAINT rooms_kind_valid CHECK (kind IN ('direct', 'group')),
    CONSTRAINT rooms_shape_valid CHECK (
        (kind = 'group' AND name IS NOT NULL AND char_length(name) BETWEEN 1 AND 64
            AND btrim(name) <> '' AND direct_user_low IS NULL AND direct_user_high IS NULL)
        OR
        (kind = 'direct' AND name IS NULL AND direct_user_low IS NOT NULL AND direct_user_high IS NOT NULL
            AND direct_user_low < direct_user_high
            AND created_by IN (direct_user_low, direct_user_high))
    ),
    CONSTRAINT rooms_last_seq_range CHECK (last_seq BETWEEN 0 AND 9007199254740991)
);

CREATE TABLE room_members (
    room_id uuid NOT NULL,
    user_id uuid NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now(),
    joined_seq bigint NOT NULL DEFAULT 0,
    CONSTRAINT room_members_pkey PRIMARY KEY (room_id, user_id),
    CONSTRAINT room_members_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms (id) ON DELETE RESTRICT,
    CONSTRAINT room_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT room_members_joined_seq_range CHECK (joined_seq BETWEEN 0 AND 9007199254740991)
);

CREATE INDEX room_members_user_room_idx ON room_members (user_id, room_id);

CREATE TABLE messages (
    id uuid CONSTRAINT messages_pkey PRIMARY KEY,
    room_id uuid NOT NULL,
    sender_id uuid NOT NULL,
    client_message_id uuid NOT NULL,
    seq bigint NOT NULL,
    text text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_room_seq_key UNIQUE (room_id, seq),
    CONSTRAINT messages_sender_client_id_key UNIQUE (sender_id, client_message_id),
    CONSTRAINT messages_sender_membership_fkey FOREIGN KEY (room_id, sender_id)
        REFERENCES room_members (room_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT messages_seq_range CHECK (seq BETWEEN 1 AND 9007199254740991),
    CONSTRAINT messages_text_length CHECK (char_length(text) BETWEEN 1 AND 2000),
    CONSTRAINT messages_text_not_blank CHECK (btrim(text) <> '')
);
