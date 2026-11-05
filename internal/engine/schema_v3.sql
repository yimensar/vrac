-- Migration 3. Tasks by status and date: open tasks due before a day.
CREATE INDEX IF NOT EXISTS nodes_status_date ON nodes(status, date_key) WHERE status IS NOT NULL;
