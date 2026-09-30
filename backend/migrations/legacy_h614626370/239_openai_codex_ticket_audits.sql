CREATE TABLE IF NOT EXISTS openai_codex_ticket_audits (
  id BIGSERIAL PRIMARY KEY,
  account_id BIGINT NOT NULL,
  model TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  outcome TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  http_status INTEGER,
  ticket_length INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 1,
  ticket_expires_at TIMESTAMPTZ,
  egress_ip TEXT NOT NULL DEFAULT '',
  request_body TEXT NOT NULL DEFAULT '',
  response_body TEXT NOT NULL DEFAULT '',
  response_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  ticket_hash TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_openai_codex_ticket_audits_account_model_created
  ON openai_codex_ticket_audits (account_id, model, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_openai_codex_ticket_audits_created
  ON openai_codex_ticket_audits (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_openai_codex_ticket_audits_outcome_created
  ON openai_codex_ticket_audits (outcome, created_at DESC);
