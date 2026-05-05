-- Per-key in-flight download counts (shared across Concierge instances).

CREATE TABLE IF NOT EXISTS luggage_active_refs (
    key TEXT PRIMARY KEY REFERENCES luggage_objects (key) ON DELETE CASCADE,
    ref_count INT NOT NULL CHECK (ref_count >= 0)
);
