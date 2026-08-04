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
  -- NULL => inherit base. Non-NULL JSON object overrides (e.g. SS2022 key length).
  cred_generators_json TEXT,
  peer_secret_fields_json TEXT,
  -- NULL => inherit base constructor param lists (carrier SFU room vs peer/vk).
  param_fields_json TEXT,
  optional_param_fields_json TEXT,
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

-- Subscription expansion catalogs (protocol-scoped). SoT for VLESS; other
-- protocols may stay in Go until cut over.
CREATE TABLE user_variants (
  protocol TEXT NOT NULL REFERENCES protocols(tag),
  name TEXT NOT NULL,
  scope TEXT NOT NULL,
  credential_field TEXT NOT NULL DEFAULT '',
  flow_value TEXT NOT NULL DEFAULT '',
  requires_user_symmetric_entry INTEGER NOT NULL DEFAULT 0,
  subscription_default INTEGER NOT NULL DEFAULT 0,
  query_tags_json TEXT NOT NULL DEFAULT '[]',
  sort_order INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (protocol, name)
);

CREATE TABLE client_profiles (
  protocol TEXT NOT NULL REFERENCES protocols(tag),
  name TEXT NOT NULL,
  scope TEXT NOT NULL,
  subscription_default INTEGER NOT NULL DEFAULT 0,
  query_tags_json TEXT NOT NULL DEFAULT '[]',
  outbound_overrides_json TEXT NOT NULL DEFAULT '{}',
  sort_order INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (protocol, name)
);

CREATE TABLE aliases (
  alias TEXT PRIMARY KEY,
  canonical_tag TEXT NOT NULL
);

-- Demux installable groups (SoT alongside presets). Slots reference ready/base preset tags.
CREATE TABLE demux_groups (
  tag TEXT PRIMARY KEY,
  short_name TEXT NOT NULL,
  status TEXT NOT NULL,
  suggested_port INTEGER NOT NULL DEFAULT 443,
  networks_json TEXT NOT NULL DEFAULT '[]',
  scores_json TEXT,
  i18n_json TEXT NOT NULL DEFAULT '{}',
  notes TEXT NOT NULL DEFAULT ''
);

CREATE TABLE demux_slots (
  group_tag TEXT NOT NULL REFERENCES demux_groups(tag) ON DELETE CASCADE,
  id TEXT NOT NULL,
  role TEXT NOT NULL,
  default_preset TEXT NOT NULL,
  substitutes_json TEXT NOT NULL DEFAULT '[]',
  match_hint TEXT NOT NULL DEFAULT '',
  preferred_alpn_json TEXT NOT NULL DEFAULT '[]',
  slot_order INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (group_tag, id)
);

CREATE INDEX idx_ready_protocol ON ready_presets(protocol);
CREATE INDEX idx_user_variants_protocol ON user_variants(protocol);
CREATE INDEX idx_client_profiles_protocol ON client_profiles(protocol);
CREATE INDEX idx_demux_slots_group ON demux_slots(group_tag, slot_order);
