-- Response bodies are not retained for Codex ticket audits. Keep the column
-- for schema compatibility, but remove any values written by earlier builds.
UPDATE openai_codex_ticket_audits
SET response_body = ''
WHERE response_body <> '';
