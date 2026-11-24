ALTER TABLE tutorials ADD COLUMN IF NOT EXISTS route_slug VARCHAR(80) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS tutorials_route_slug_idx ON tutorials (route_slug) WHERE route_slug <> '';
CREATE INDEX IF NOT EXISTS tutorials_draft_slug_idx ON tutorials ((draft->>'slug'));

CREATE TABLE IF NOT EXISTS tutorial_asset_garbage (
    id VARCHAR(32) PRIMARY KEY
);
