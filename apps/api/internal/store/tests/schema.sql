\set ON_ERROR_STOP on

-- Run with psql -f tests/schema.sql. The schema, fixtures, and helpers disappear at ROLLBACK.
BEGIN;
CREATE SCHEMA t1_schema_acceptance;
SET LOCAL search_path TO t1_schema_acceptance, public;
\ir ../migrations/001_initial.sql

CREATE FUNCTION pg_temp.require(ok boolean, label text) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    IF ok IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'failed: %', label;
    END IF;
END
$$;

CREATE FUNCTION pg_temp.expect_error(statement text, wanted_state text, wanted_constraint text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    got_state text;
    got_constraint text;
BEGIN
    BEGIN
        EXECUTE statement;
    EXCEPTION WHEN OTHERS THEN
        GET STACKED DIAGNOSTICS got_state = RETURNED_SQLSTATE, got_constraint = CONSTRAINT_NAME;
        IF got_state <> wanted_state OR got_constraint IS DISTINCT FROM wanted_constraint THEN
            RAISE EXCEPTION 'expected % / %, got % / % from %',
                wanted_state, wanted_constraint, got_state, got_constraint, statement;
        END IF;
        RETURN;
    END;
    RAISE EXCEPTION 'expected % / % but statement succeeded: %',
        wanted_state, wanted_constraint, statement;
END
$$;

INSERT INTO users (id, username, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000001', 'alice', 'argon2id-fixture'),
    ('00000000-0000-0000-0000-000000000002', 'bob_2', 'argon2id-fixture'),
    ('00000000-0000-0000-0000-000000000003', 'charlie', 'argon2id-fixture');
INSERT INTO users (id, username, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000005', repeat('a', 24), 'argon2id-fixture');

SELECT pg_temp.expect_error($sql$INSERT INTO users (id, username, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000004', 'alice', 'hash')$sql$,
    '23505', 'users_username_key');
SELECT pg_temp.expect_error($sql$INSERT INTO users (id, username, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000004', 'Alice', 'hash')$sql$,
    '23514', 'users_username_format');
SELECT pg_temp.expect_error($sql$INSERT INTO users (id, username, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000004', 'ab', 'hash')$sql$,
    '23514', 'users_username_format');
SELECT pg_temp.expect_error($sql$INSERT INTO users (id, username, password_hash) VALUES
    ('00000000-0000-0000-0000-000000000004', repeat('a', 25), 'hash')$sql$,
    '23514', 'users_username_format');

INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES
    ('10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001',
     decode(repeat('11', 32), 'hex'), now() + interval '7 days');
SELECT pg_temp.expect_error($sql$INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES
    ('10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000002',
     decode(repeat('11', 32), 'hex'), now() + interval '7 days')$sql$,
    '23505', 'sessions_token_hash_key');
SELECT pg_temp.expect_error($sql$INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES
    ('10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000002',
     decode('11', 'hex'), now() + interval '7 days')$sql$,
    '23514', 'sessions_token_hash_length');
SELECT pg_temp.expect_error($sql$INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES
    ('10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000002',
     decode(repeat('22', 32), 'hex'), now() - interval '1 second')$sql$,
    '23514', 'sessions_expiry_after_creation');

INSERT INTO rooms (id, kind, created_by, direct_user_low, direct_user_high) VALUES
    ('20000000-0000-0000-0000-000000000001', 'direct', '00000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002');
INSERT INTO rooms (id, kind, name, created_by) VALUES
    ('20000000-0000-0000-0000-000000000002', 'group', 'Chat 💬',
     '00000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000003', 'group', 'Other',
     '00000000-0000-0000-0000-000000000001');
INSERT INTO rooms (id, kind, name, created_by, last_seq) VALUES
    ('20000000-0000-0000-0000-000000000005', 'group', repeat('💬', 64),
     '00000000-0000-0000-0000-000000000001', 9007199254740991);
SELECT pg_temp.require(
    (SELECT direct_key = '00000000-0000-0000-0000-000000000001:00000000-0000-0000-0000-000000000002'
     FROM rooms WHERE id = '20000000-0000-0000-0000-000000000001'), 'canonical direct key');
SELECT pg_temp.require(
    (SELECT direct_key IS NULL FROM rooms WHERE id = '20000000-0000-0000-0000-000000000002'),
    'group direct key is null');

SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, created_by, direct_user_low, direct_user_high) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', '00000000-0000-0000-0000-000000000002',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')$sql$,
    '23505', 'rooms_direct_key_key');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, created_by, direct_user_low, direct_user_high) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', '00000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, created_by, direct_user_low, direct_user_high) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', '00000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, created_by, direct_user_low, direct_user_high) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', '00000000-0000-0000-0000-000000000003',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, created_by, direct_user_low, direct_user_high) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', '00000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000099')$sql$,
    '23503', 'rooms_direct_user_high_fkey');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, created_by, direct_user_low) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', '00000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, name, created_by) VALUES
    ('20000000-0000-0000-0000-000000000004', 'group', '   ',
     '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, name, created_by) VALUES
    ('20000000-0000-0000-0000-000000000004', 'group', repeat('x', 65),
     '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, name, created_by, direct_user_low) VALUES
    ('20000000-0000-0000-0000-000000000004', 'group', 'Wrong pair',
     '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, name, created_by) VALUES
    ('20000000-0000-0000-0000-000000000004', 'direct', 'wrong',
     '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_shape_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, name, created_by) VALUES
    ('20000000-0000-0000-0000-000000000004', 'other', 'wrong',
     '00000000-0000-0000-0000-000000000001')$sql$,
    '23514', 'rooms_kind_valid');
SELECT pg_temp.expect_error($sql$INSERT INTO rooms (id, kind, name, created_by, last_seq) VALUES
    ('20000000-0000-0000-0000-000000000004', 'group', 'Limit',
     '00000000-0000-0000-0000-000000000001', 9007199254740992)$sql$,
    '23514', 'rooms_last_seq_range');

INSERT INTO room_members (room_id, user_id) VALUES
    ('20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002'),
    ('20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001');
SELECT pg_temp.expect_error($sql$INSERT INTO room_members (room_id, user_id) VALUES
    ('20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001')$sql$,
    '23505', 'room_members_pkey');
SELECT pg_temp.expect_error($sql$INSERT INTO room_members (room_id, user_id, joined_seq) VALUES
    ('20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000002', -1)$sql$,
    '23514', 'room_members_joined_seq_range');
SELECT pg_temp.expect_error($sql$INSERT INTO room_members (room_id, user_id, joined_seq) VALUES
    ('20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000002', 9007199254740992)$sql$,
    '23514', 'room_members_joined_seq_range');

UPDATE rooms SET last_seq = 1 WHERE id = '20000000-0000-0000-0000-000000000001';
INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 1, 'hello');
INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000005', '20000000-0000-0000-0000-000000000005',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000005',
     9007199254740991, repeat('💬', 2000));
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000003', '40000000-0000-0000-0000-000000000002', 2, 'no')$sql$,
    '23503', 'messages_sender_membership_fkey');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 1, 'retry')$sql$,
    '23505', 'messages_sender_client_id_key');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000002', 1, 'duplicate seq')$sql$,
    '23505', 'messages_room_seq_key');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 0, 'bad seq')$sql$,
    '23514', 'messages_seq_range');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 2, '   ')$sql$,
    '23514', 'messages_text_not_blank');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 2, '')$sql$,
    '23514', 'messages_text_length');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 2, repeat('x', 2001))$sql$,
    '23514', 'messages_text_length');
SELECT pg_temp.expect_error($sql$INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
    ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
     '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 9007199254740992, 'bad seq')$sql$,
    '23514', 'messages_seq_range');

DO $$
DECLARE
    before_seq bigint;
    before_count bigint;
BEGIN
    SELECT last_seq INTO before_seq FROM rooms WHERE id = '20000000-0000-0000-0000-000000000001';
    SELECT count(*) INTO before_count FROM messages WHERE room_id = '20000000-0000-0000-0000-000000000001';
    BEGIN
        UPDATE rooms SET last_seq = last_seq + 1 WHERE id = '20000000-0000-0000-0000-000000000001';
        INSERT INTO messages (id, room_id, sender_id, client_message_id, seq, text) VALUES
            ('30000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001',
             '00000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 2, 'rolled back');
        RAISE EXCEPTION 'simulate failed send';
    EXCEPTION WHEN raise_exception THEN
        NULL;
    END;
    PERFORM pg_temp.require(
        (SELECT last_seq = before_seq FROM rooms WHERE id = '20000000-0000-0000-0000-000000000001'),
        'counter rolls back with send');
    PERFORM pg_temp.require(
        (SELECT count(*) = before_count FROM messages WHERE room_id = '20000000-0000-0000-0000-000000000001'),
        'message rolls back with counter');
END
$$;

SELECT pg_temp.require((SELECT count(*) = 5 FROM information_schema.tables
    WHERE table_schema = 't1_schema_acceptance' AND table_type = 'BASE TABLE'), 'five tables created');
SELECT current_setting('server_version') AS tested_postgresql_version;
SELECT 'T1 schema acceptance passed' AS result;
ROLLBACK;
