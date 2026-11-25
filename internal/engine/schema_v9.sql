-- Migration 9. reviewed_at records when the owner last confirmed a to-do
-- in a review without changing it; an edit counts as a review too.
ALTER TABLE nodes ADD COLUMN reviewed_at TEXT;
