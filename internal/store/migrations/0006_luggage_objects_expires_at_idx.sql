-- Speed up expiry sweeps (WHERE expires_at < now() ORDER BY expires_at).

CREATE INDEX IF NOT EXISTS luggage_objects_expires_at_idx ON luggage_objects (expires_at);
