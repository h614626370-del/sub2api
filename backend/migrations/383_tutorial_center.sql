CREATE TABLE IF NOT EXISTS tutorial_categories (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(80) NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tutorials (
    id BIGSERIAL PRIMARY KEY,
    draft JSONB NOT NULL,
    published JSONB,
    version BIGINT NOT NULL DEFAULT 1,
    published_version BIGINT,
    author_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS tutorials_published_idx ON tutorials (published_at DESC) WHERE published IS NOT NULL;

CREATE TABLE IF NOT EXISTS tutorial_assets (
    id VARCHAR(32) PRIMARY KEY,
    tutorial_id BIGINT NOT NULL REFERENCES tutorials(id) ON DELETE CASCADE,
    mime VARCHAR(32) NOT NULL,
    size BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS tutorial_assets_article_idx ON tutorial_assets (tutorial_id);
