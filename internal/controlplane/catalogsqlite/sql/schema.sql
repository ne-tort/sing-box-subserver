-- Controlplane catalog v2 (SQLite). Read-only at runtime after seed.
PRAGMA foreign_keys = ON;

CREATE TABLE meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE protocols (
  tag TEXT PRIMARY KEY,
  singbox_type TEXT NOT NULL,
  short_name TEXT NOT NULL,
  status TEXT NOT NULL,
  i18n_json TEXT NOT NULL DEFAULT '{}',
  notes_json TEXT NOT NULL DEFAULT '{}',
  default_cred_fields_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE preset_bases (
  protocol TEXT PRIMARY KEY REFERENCES protocols(tag),
  tag TEXT NOT NULL UNIQUE,
  short_name TEXT NOT NULL,
  status TEXT NOT NULL,
  custom_preset INTEGER NOT NULL DEFAULT 1,
  aliases_json TEXT NOT NULL DEFAULT '[]',
  traits_json TEXT NOT NULL DEFAULT '[]',
  scores_json TEXT,
  demux_hints_json TEXT,
  requirements_json TEXT,
  cred_fields_json TEXT NOT NULL DEFAULT '[]',
  cred_generators_json TEXT NOT NULL DEFAULT '{}',
  peer_secret_fields_json TEXT NOT NULL DEFAULT '{}',
  param_fields_json TEXT NOT NULL DEFAULT '[]',
  optional_param_fields_json TEXT NOT NULL DEFAULT '[]',
  param_meta_json TEXT NOT NULL DEFAULT '{}',
  default_user_variants_json TEXT NOT NULL DEFAULT '[]',
  default_client_profiles_json TEXT NOT NULL DEFAULT '[]',
  i18n_json TEXT NOT NULL DEFAULT '{}',
  client_notes_json TEXT NOT NULL DEFAULT '{}',
  inbound_template_json TEXT NOT NULL,
  outbound_template_json TEXT NOT NULL,
  endpoint_template_json TEXT
);

CREATE TABLE ready_presets (
  tag TEXT PRIMARY KEY,
  protocol TEXT NOT NULL REFERENCES protocols(tag),
  short_name TEXT NOT NULL,
  status TEXT NOT NULL,
  aliases_json TEXT NOT NULL DEFAULT '[]',
  traits_json TEXT NOT NULL DEFAULT '[]',
  scores_json TEXT,
  demux_hints_json TEXT,
  requirements_json TEXT,
  cred_fields_json TEXT NOT NULL DEFAULT '[]',
  default_user_variants_json TEXT NOT NULL DEFAULT '[]',
  default_client_profiles_json TEXT NOT NULL DEFAULT '[]',
  i18n_json TEXT NOT NULL DEFAULT '{}',
  client_notes_json TEXT NOT NULL DEFAULT '{}',
  -- NULL => use base templates (constructor path).
  inbound_template_json TEXT,
  outbound_template_json TEXT,
  endpoint_template_json TEXT
);

CREATE TABLE ready_param_values (
  ready_tag TEXT NOT NULL REFERENCES ready_presets(tag) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (ready_tag, key)
);

CREATE TABLE aliases (
  alias TEXT PRIMARY KEY,
  canonical_tag TEXT NOT NULL
);

CREATE INDEX idx_ready_protocol ON ready_presets(protocol);
