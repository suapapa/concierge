-- Luggage object metadata (payload bytes remain on disk under tmpDir/<key>/).

CREATE TABLE IF NOT EXISTS luggage_objects (
    key TEXT PRIMARY KEY,
    owner_user_id BIGINT NOT NULL,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    file_size_bytes BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS luggage_objects_owner_user_id_idx ON luggage_objects (owner_user_id);
