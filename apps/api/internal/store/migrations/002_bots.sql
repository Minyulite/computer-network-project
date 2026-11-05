-- Shop chatbots. Each bot owns a system user that sends its replies, so bot
-- messages reuse messages, seq, history and retry handling unchanged.
ALTER TABLE users ADD COLUMN is_bot boolean NOT NULL DEFAULT false;

CREATE TABLE bots (
    id uuid CONSTRAINT bots_pkey PRIMARY KEY,
    owner_id uuid NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bots_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT bots_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT bots_user_id_key UNIQUE (user_id),
    CONSTRAINT bots_name_length CHECK (char_length(name) BETWEEN 1 AND 64 AND btrim(name) <> ''),
    CONSTRAINT bots_description_length CHECK (char_length(description) <= 2000)
);

CREATE INDEX bots_owner_id_idx ON bots (owner_id);

CREATE TABLE bot_templates (
    id uuid CONSTRAINT bot_templates_pkey PRIMARY KEY,
    bot_id uuid NOT NULL,
    position integer NOT NULL,
    question text NOT NULL,
    answer text NOT NULL,
    CONSTRAINT bot_templates_bot_id_fkey FOREIGN KEY (bot_id) REFERENCES bots (id) ON DELETE CASCADE,
    CONSTRAINT bot_templates_position_key UNIQUE (bot_id, position),
    CONSTRAINT bot_templates_question_length CHECK (char_length(question) BETWEEN 1 AND 300 AND btrim(question) <> ''),
    CONSTRAINT bot_templates_answer_length CHECK (char_length(answer) BETWEEN 1 AND 1000 AND btrim(answer) <> '')
);

ALTER TABLE rooms DROP CONSTRAINT rooms_kind_valid;
ALTER TABLE rooms DROP CONSTRAINT rooms_shape_valid;
ALTER TABLE rooms ADD COLUMN bot_id uuid;
ALTER TABLE rooms ADD COLUMN bot_customer_id uuid;
ALTER TABLE rooms ADD CONSTRAINT rooms_bot_id_fkey FOREIGN KEY (bot_id) REFERENCES bots (id) ON DELETE RESTRICT;
ALTER TABLE rooms ADD CONSTRAINT rooms_bot_customer_id_fkey FOREIGN KEY (bot_customer_id) REFERENCES users (id) ON DELETE RESTRICT;
ALTER TABLE rooms ADD CONSTRAINT rooms_bot_customer_key UNIQUE (bot_id, bot_customer_id);
ALTER TABLE rooms ADD CONSTRAINT rooms_kind_valid CHECK (kind IN ('direct', 'group', 'bot'));
ALTER TABLE rooms ADD CONSTRAINT rooms_shape_valid CHECK (
    (kind = 'group' AND name IS NOT NULL AND char_length(name) BETWEEN 1 AND 64
        AND btrim(name) <> '' AND direct_user_low IS NULL AND direct_user_high IS NULL
        AND bot_id IS NULL AND bot_customer_id IS NULL)
    OR
    (kind = 'direct' AND name IS NULL AND direct_user_low IS NOT NULL AND direct_user_high IS NOT NULL
        AND direct_user_low < direct_user_high
        AND created_by IN (direct_user_low, direct_user_high)
        AND bot_id IS NULL AND bot_customer_id IS NULL)
    OR
    (kind = 'bot' AND name IS NULL AND direct_user_low IS NULL AND direct_user_high IS NULL
        AND bot_id IS NOT NULL AND bot_customer_id IS NOT NULL AND created_by = bot_customer_id)
);
