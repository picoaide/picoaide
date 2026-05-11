# Picoclaw Adapter Plan

## Goal

PicoAide must manage Picoclaw upgrades and configuration from local or remote adapter rules, without requiring a PicoAide binary release every time Picoclaw publishes a new image.

The PicoAide binary owns only the generic adapter engine. The adapter package owns Picoclaw version mapping, config schema mapping, UI field metadata, config/security storage paths, and migration rules.

## Key Principles

- Runtime paths are offline-first. Config apply, image upgrade checks, and user/group/global rollout must use local adapter files only.
- Remote adapter refresh is asynchronous and never blocks request handling.
- Remote refresh downloads files one by one and verifies each file against `rules/picoclaw/hash`.
- PicoAide validates the adapter format version, not the Picoclaw config version.
- Adapter rules declare the latest supported Picoclaw config version.
- Unknown fields are preserved unless a migration explicitly deletes or moves them.
- `config.json` and `.security.yml` are both first-class migration and persistence targets.
- Existing specialized APIs, such as DingTalk config, should remain temporarily but must call the adapter path engine internally.
- Administrators manage platform policy only: available models, default model, and allowed/enabled channel types.
- Channel credentials, tokens, client secrets, and per-user channel enablement are owned by the regular user in their personal management page.
- Global config rollout must not overwrite user-owned secrets.

## Adapter Files

Local bundled defaults live under:

```text
rules/picoclaw/
  index.json
  hash
  schemas/
    config-v1.json
    config-v2.json
    config-v3.json
  ui/
    ui-v1.json
    ui-v2.json
    ui-v3.json
  migrations/
    v1-to-v2.json
    v2-to-v3.json
```

The active runtime copy lives in the configured rule cache directory:

```text
<rule_cache_dir>/picoclaw/
  index.json
  hash
  schemas/...
  ui/...
  migrations/...
```

## `index.json`

`index.json` is the manifest for all adapter files.

Required fields:

- `adapter_schema_version`: format version of this adapter package. PicoAide only loads schema versions supported by its adapter engine.
- `adapter_version`: human-readable adapter release version.
- `latest_supported_config_version`: highest Picoclaw config version represented by this adapter package.
- `picoclaw_versions`: Picoclaw image versions and their config versions.
- `config_schemas`: map of config version to schema file path.
- `ui_schemas`: map of config version to UI schema file path.
- `migrations`: ordered config-version migrations.

Example:

```json
{
  "adapter_schema_version": 1,
  "adapter_version": "2026.05.10",
  "latest_supported_config_version": 3,
  "picoclaw_versions": [
    { "version": "0.2.4", "config_version": 1 },
    { "version": "0.2.5", "config_version": 2 },
    { "version": "0.2.6", "config_version": 2 },
    { "version": "0.2.7", "config_version": 3 },
    { "version": "0.2.8", "config_version": 3 }
  ],
  "config_schemas": {
    "1": "schemas/config-v1.json",
    "2": "schemas/config-v2.json",
    "3": "schemas/config-v3.json"
  },
  "ui_schemas": {
    "1": "ui/ui-v1.json",
    "2": "ui/ui-v2.json",
    "3": "ui/ui-v3.json"
  },
  "migrations": [
    { "from_config": 1, "to_config": 2, "path": "migrations/v1-to-v2.json" },
    { "from_config": 2, "to_config": 3, "path": "migrations/v2-to-v3.json" }
  ]
}
```

## `hash`

The remote hash file is downloaded first from:

```text
<remote_base_url>/rules/picoclaw/hash
```

It contains one SHA-256 entry per JSON adapter file:

```text
sha256  index.json
sha256  schemas/config-v1.json
sha256  schemas/config-v2.json
sha256  schemas/config-v3.json
sha256  ui/ui-v1.json
sha256  ui/ui-v2.json
sha256  ui/ui-v3.json
sha256  migrations/v1-to-v2.json
sha256  migrations/v2-to-v3.json
```

Refresh requirements:

- Download `hash` first.
- Reject absolute paths, parent paths, empty paths, non-JSON adapter paths, and duplicate paths.
- Download each listed JSON file separately.
- Verify SHA-256 for each file.
- Validate the complete adapter package.
- Atomically replace the active local adapter only after all checks pass.
- Keep the previous adapter if any download, hash check, or schema validation fails.

## Manual Upload

The system settings page exposes a single upload control for configuration adapter packages.

Upload format:

- The uploaded file must be a zip archive.
- The zip root must contain the contents of `rules/picoclaw/` directly.
- Required root entries include `index.json` and `hash`.
- Valid examples: `index.json`, `hash`, `schemas/config-v3.json`, `ui/ui-v3.json`, `migrations/v2-to-v3.json`.
- Invalid examples: `picoclaw/index.json`, `rules/picoclaw/index.json`, absolute paths, parent paths, duplicate paths, non-JSON adapter files except `hash`.

Upload behavior:

- Extract to a temporary directory.
- Parse `hash`.
- Verify every listed JSON file individually with SHA-256.
- Reject any extracted JSON file that is not listed in `hash`.
- Validate the complete adapter package.
- Atomically replace the active local adapter only after all checks pass.

There is no JSON textarea in the UI because the adapter is a JSON file set, not a single JSON document.

## Config Schemas

Config schema files describe version-specific storage paths and capabilities. They are not full JSON Schema; they are PicoAide adapter metadata.

Each schema should declare:

- `config_version`
- major roots, such as `channels_path`, `models_path`, `default_model_path`
- channel path style
- security path style
- singleton channel types
- supported channel types
- model list semantics

Example:

```json
{
  "config_version": 3,
  "channels_path": "channel_list",
  "channel_settings_path": "channel_list.*.settings",
  "models_path": "model_list",
  "default_model_path": "agents.defaults.model_name",
  "security": {
    "channels_path": "channel_list",
    "channel_settings_path": "channel_list.*.settings",
    "models_path": "model_list"
  },
  "singleton_channels": ["pico", "pico_client"],
  "channel_types": [
    "pico",
    "pico_client",
    "telegram",
    "discord",
    "feishu",
    "weixin",
    "wecom",
    "dingtalk",
    "slack",
    "matrix",
    "line",
    "onebot",
    "qq",
    "irc",
    "vk",
    "maixcam",
    "whatsapp",
    "whatsapp_native",
    "teams_webhook"
  ]
}
```

## UI Schemas

UI schema files describe pages, sections, fields, and storage.

Each field must declare:

- `key`
- `label`
- `type`
- `storage`: `config` or `security`
- `path`
- optional `default`
- optional `required`
- optional `options`
- optional `secret`
- optional `visible_when`

Example field:

```json
{
  "key": "client_secret",
  "label": "Client Secret",
  "type": "password",
  "storage": "security",
  "path": "channel_list.dingtalk.settings.client_secret",
  "secret": true
}
```

## Migrations

Migration files are scoped to config-version transitions, not Picoclaw image versions.

Example:

```json
{
  "from_config": 2,
  "to_config": 3,
  "actions": [
    { "op": "set", "storage": "config", "path": "version", "value": 3 },
    { "op": "delete", "storage": "config", "path": "bindings" },
    { "op": "rename", "storage": "config", "from": "channels", "to": "channel_list", "mode": "channels_to_nested" },
    { "op": "rename", "storage": "security", "from": "channels", "to": "channel_list", "mode": "channels_to_nested" }
  ]
}
```

Supported operations:

- `set`
- `delete`
- `move`
- `rename`
- `map`
- `infer_model_enabled`

Migration execution must support chained upgrades, for example:

```text
config v1 -> config v2 -> config v3
```

There should not be a required direct `v1-to-v3` file.

## Rollout Compatibility Rules

Before applying global configuration:

- Determine the global UI/config version currently being edited.
- Determine each target user's Picoclaw image version and config version.
- Resolve the user's supported config version from the adapter.
- Reject global rollout if any target user cannot accept the edited config version.
- Reject group rollout under the same rule for all group members.
- Allow single-user rollout only when that user is compatible.

Error messages must list incompatible users and their current Picoclaw/config versions.

## Pages

Split the admin UI into separate pages:

- System Settings: PicoAide-owned settings and the Picoclaw `配置适配` status/upload.
- Model Config: administrator-managed model list, default model, fallback and routing policy.
- Channel Strategy: administrator-managed allowed channel types and global non-secret defaults only.
- Tools
- Agents
- Advanced JSON

The existing System Settings page keeps PicoAide-owned settings only:

- image registry
- timezone
- users/archive directories
- web listen/container base URL
- adapter refresh/upload/status

## Execution Plan

1. Add this plan document.
2. Create the bundled multi-file adapter package under `rules/picoclaw`.
3. Implement adapter package loader:
   - local active directory
   - bundled fallback/release
   - path validation
   - format validation
   - hash verification helpers
4. Implement remote refresh:
   - fetch `hash`
   - fetch each JSON file separately
   - verify SHA-256
   - validate package
   - atomic activation
   - update status stamp
5. Route migration APIs through the adapter package.
6. Extend migration execution to support `config` and `security` storage.
7. Replace hard-coded config-version support with adapter-declared support and adapter-engine schema-version checks.
8. Add generic config read/write APIs driven by UI schema.
9. Move DingTalk and model save paths to the generic adapter path engine.
10. Add rollout compatibility checks for all, group, and single-user config apply.
11. Split admin pages and migrate the settings UI:
    - `系统配置` keeps `配置适配`, image, directory, web, and gateway settings.
    - `模型配置` owns model list, API base, model keys, default model, token/tool limits.
    - `渠道策略` owns which channel types are allowed/enabled globally.
    - user personal pages own channel credential fields and per-user channel setup.
12. Add tests for loader, hash refresh, chained migration, config/security migration, rollout compatibility, and invalid remote adapters.
13. Deploy to the test server after each completed code phase.

## Ownership Boundary

Admin-owned configuration:

- `model_list`
- `agents.defaults.model_name`
- model fallback/routing policy
- allowed/enabled channel types
- non-secret global defaults that are safe to broadcast
- tool/agent/gateway policy

User-owned configuration:

- channel client secrets
- tokens
- webhook secrets
- per-user channel credentials
- per-user decision to configure and use an allowed channel

The admin UI must not expose user-owned secret fields. The personal user UI can render channel fields from the adapter schema and write them to that user's `config.json` and `.security.yml`.

## Open Follow-Ups

- Decide whether the adapter hash file itself should have a pinned hash in deployments. Current requirement is per-file hash verification only.
- Decide how long to keep old adapter revisions locally for rollback.
