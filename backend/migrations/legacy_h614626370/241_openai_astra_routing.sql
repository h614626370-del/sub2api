INSERT INTO settings (key, value) VALUES ('openai_astra_group_id', '0') ON CONFLICT (key) DO NOTHING;
