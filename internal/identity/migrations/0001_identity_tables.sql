-- identity module, migration 1: principals, the per-type tables, credentials, invites, access
-- keys, sessions and consent records (D-25, D-42, D-44, D-53). Secrets are never stored: an
-- access key keeps a visible prefix and a hash, a credential keeps a hash or a public key (D-40).
-- Every per-type table holds a UNIQUE principal_id FK to idn_principal; idn_principal has no FK
-- to a type table, so a new user type never alters it (D-53). Rows are soft-deleted through
-- idn_principal.status, so every FK that points at a soft-deleted parent is RESTRICT (D-42).

CREATE TABLE idn_principal (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind <> ''),
  category   TEXT,
  segment    TEXT,
  plan_id    TEXT,
  status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;
--@table idn_principal classification=internal billable=true | Every actor of the system (host admin, consumer, platform admin, future kinds) with the properties they share.
--@column idn_principal.id classification=internal billable=false | Principal id, UUIDv7.
--@column idn_principal.kind classification=internal billable=false | Actor kind: host_admin, consumer, platform_admin or a future kind; no fixed list so a new kind needs no migration.
--@column idn_principal.category classification=internal billable=true | Category tag used to group principals, for example for a subscription strategy; empty when unused.
--@column idn_principal.segment classification=internal billable=true | Segment tag used to group principals; empty when unused.
--@column idn_principal.plan_id classification=internal billable=true | Placeholder for a future plan; always empty in Phase 1.
--@column idn_principal.status classification=internal billable=false | active, suspended or deleted; deleted is the soft delete.
--@column idn_principal.created_at classification=internal billable=false | When the principal was created, Unix milliseconds UTC.
--@column idn_principal.updated_at classification=internal billable=false | When the principal last changed, Unix milliseconds UTC.

CREATE TABLE idn_admin_user (
  id           TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL UNIQUE REFERENCES idn_principal(id) ON DELETE RESTRICT,
  username     TEXT NOT NULL UNIQUE,
  display_name TEXT,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
) STRICT;
--@table idn_admin_user classification=personal billable=false | A person who administers this host; the admin-specific properties of a principal.
--@column idn_admin_user.id classification=internal billable=false | Admin user id, UUIDv7.
--@column idn_admin_user.principal_id classification=internal billable=false | The principal this admin user is, one to one.
--@column idn_admin_user.username classification=personal billable=false | Login name, unique on this host.
--@column idn_admin_user.display_name classification=personal billable=false | Name shown in the admin UI.
--@column idn_admin_user.created_at classification=internal billable=false | When the row was created, Unix milliseconds UTC.
--@column idn_admin_user.updated_at classification=internal billable=false | When the row last changed, Unix milliseconds UTC.

CREATE TABLE idn_consumer (
  id           TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL UNIQUE REFERENCES idn_principal(id) ON DELETE RESTRICT,
  kind         TEXT NOT NULL CHECK (kind IN ('individual', 'application')),
  display_name TEXT,
  email        TEXT,
  row_version  INTEGER NOT NULL DEFAULT 1,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
) STRICT;
--@table idn_consumer classification=personal billable=true | An individual or an application that uses this host; the consumer-specific properties of a principal. Personal fields are erased when it is deleted, the row stays for ledger references.
--@column idn_consumer.id classification=internal billable=false | Consumer id, UUIDv7.
--@column idn_consumer.principal_id classification=internal billable=false | The principal this consumer is, one to one.
--@column idn_consumer.kind classification=internal billable=true | individual or application.
--@column idn_consumer.display_name classification=personal billable=false | Name of the person or application; erased on deletion.
--@column idn_consumer.email classification=personal billable=false | Contact email; erased on deletion.
--@column idn_consumer.row_version classification=internal billable=false | Increases with every change; recorded with the captured change.
--@column idn_consumer.created_at classification=internal billable=false | When the consumer was created, Unix milliseconds UTC.
--@column idn_consumer.updated_at classification=internal billable=false | When the consumer last changed, Unix milliseconds UTC.

CREATE TABLE idn_credential (
  id           TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES idn_principal(id) ON DELETE RESTRICT,
  kind         TEXT NOT NULL CHECK (kind IN ('password', 'passkey')),
  identifier   TEXT NOT NULL,
  secret       TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at   INTEGER,
  UNIQUE (kind, identifier)
) STRICT;
--@table idn_credential classification=sensitive billable=false | A way for a principal to log in: a password hash or a passkey public key. Never synced.
--@column idn_credential.id classification=internal billable=false | Credential id, UUIDv7.
--@column idn_credential.principal_id classification=internal billable=false | Principal the credential logs in; a key-only consumer has none.
--@column idn_credential.kind classification=internal billable=false | password or passkey.
--@column idn_credential.identifier classification=personal billable=false | Login name for a password, credential id for a passkey.
--@column idn_credential.secret classification=sensitive billable=false | Password hash (argon2id) or passkey public key; never the password itself.
--@column idn_credential.created_at classification=internal billable=false | When the credential was added, Unix milliseconds UTC.
--@column idn_credential.last_used_at classification=internal billable=false | When it last logged in, Unix milliseconds UTC; empty if never.
--@column idn_credential.revoked_at classification=internal billable=false | When it was revoked, Unix milliseconds UTC; empty while usable.

CREATE TABLE idn_invite (
  id                   TEXT PRIMARY KEY,
  prefix               TEXT NOT NULL UNIQUE,
  token_hash           TEXT NOT NULL,
  created_by           TEXT NOT NULL REFERENCES idn_principal(id) ON DELETE RESTRICT,
  note                 TEXT,
  created_at           INTEGER NOT NULL,
  expires_at           INTEGER NOT NULL,
  redeemed_at          INTEGER,
  redeemed_consumer_id TEXT UNIQUE REFERENCES idn_consumer(id) ON DELETE RESTRICT,
  revoked_at           INTEGER
) STRICT;
--@table idn_invite classification=sensitive billable=false | An invitation for a new consumer. Pending until redeemed, revoked or expired.
--@column idn_invite.id classification=internal billable=false | Invite id, UUIDv7.
--@column idn_invite.prefix classification=internal billable=false | Visible start of the invite token, unique; used to find the invite.
--@column idn_invite.token_hash classification=sensitive billable=false | Hash of the invite token; the token is shown once and not stored.
--@column idn_invite.created_by classification=internal billable=false | Principal that issued the invite.
--@column idn_invite.note classification=personal billable=false | Free-text note by the host, for example who it is for.
--@column idn_invite.created_at classification=internal billable=false | When the invite was issued, Unix milliseconds UTC.
--@column idn_invite.expires_at classification=internal billable=false | When the invite stops working, Unix milliseconds UTC.
--@column idn_invite.redeemed_at classification=internal billable=false | When it was redeemed, Unix milliseconds UTC; empty while pending.
--@column idn_invite.redeemed_consumer_id classification=internal billable=false | Consumer created by redeeming it.
--@column idn_invite.revoked_at classification=internal billable=false | When the host revoked it, Unix milliseconds UTC; empty otherwise.

CREATE TABLE idn_access_key (
  id             TEXT PRIMARY KEY,
  consumer_id    TEXT NOT NULL REFERENCES idn_consumer(id) ON DELETE RESTRICT,
  prefix         TEXT NOT NULL UNIQUE,
  secret_hash    TEXT NOT NULL,
  label          TEXT,
  narrowing_json TEXT,
  bound_node     TEXT,
  created_at     INTEGER NOT NULL,
  expires_at     INTEGER,
  revoked_at     INTEGER
) STRICT;
--@table idn_access_key classification=sensitive billable=true | An access key of a consumer. Only a visible prefix and a hash are stored; the secret is shown once and never synced.
--@column idn_access_key.id classification=internal billable=false | Access key id, UUIDv7.
--@column idn_access_key.consumer_id classification=internal billable=false | Consumer that holds the key.
--@column idn_access_key.prefix classification=internal billable=false | Visible start of the key, unique; used to find the key.
--@column idn_access_key.secret_hash classification=sensitive billable=false | Fast hash of the key secret.
--@column idn_access_key.label classification=personal billable=false | Name the consumer gave the key.
--@column idn_access_key.narrowing_json classification=internal billable=false | Optional JSON that narrows the consumer's grant for this key; empty means no narrowing.
--@column idn_access_key.bound_node classification=internal billable=false | Transport node the key is bound to; empty means not bound.
--@column idn_access_key.created_at classification=internal billable=false | When the key was created, Unix milliseconds UTC.
--@column idn_access_key.expires_at classification=internal billable=false | When the key expires, Unix milliseconds UTC; empty means never.
--@column idn_access_key.revoked_at classification=internal billable=false | When the key was revoked, Unix milliseconds UTC; empty while active.

CREATE TABLE idn_session (
  id           TEXT PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES idn_principal(id) ON DELETE RESTRICT,
  token_hash   TEXT NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  revoked_at   INTEGER
) STRICT;
--@table idn_session classification=sensitive billable=false | A logged-in web session of a principal. Only a hash of the session token is stored.
--@column idn_session.id classification=internal billable=false | Session id, UUIDv7.
--@column idn_session.principal_id classification=internal billable=false | Principal that is logged in.
--@column idn_session.token_hash classification=sensitive billable=false | Hash of the session token.
--@column idn_session.created_at classification=internal billable=false | When the session started, Unix milliseconds UTC.
--@column idn_session.expires_at classification=internal billable=false | When the session expires, Unix milliseconds UTC.
--@column idn_session.revoked_at classification=internal billable=false | When it was ended early, Unix milliseconds UTC; empty otherwise.

CREATE TABLE idn_consent (
  id             TEXT PRIMARY KEY,
  principal_id   TEXT NOT NULL REFERENCES idn_principal(id) ON DELETE RESTRICT,
  purpose        TEXT NOT NULL,
  policy_version TEXT NOT NULL,
  granted_at     INTEGER NOT NULL,
  withdrawn_at   INTEGER,
  evidence       TEXT
) STRICT;
--@table idn_consent classification=personal billable=false | A consent a principal gave for one purpose under one version of the terms. Content capture may only run behind a matching, unwithdrawn record.
--@column idn_consent.id classification=internal billable=false | Consent id, UUIDv7.
--@column idn_consent.principal_id classification=internal billable=false | Principal that gave the consent.
--@column idn_consent.purpose classification=internal billable=false | What the consent covers, for example accepting the terms.
--@column idn_consent.policy_version classification=internal billable=false | Version of the terms or policy that was accepted.
--@column idn_consent.granted_at classification=internal billable=false | When it was given, Unix milliseconds UTC.
--@column idn_consent.withdrawn_at classification=internal billable=false | When it was withdrawn, Unix milliseconds UTC; empty while in force.
--@column idn_consent.evidence classification=personal billable=false | How the consent was given, for example the screen or request it came from; never prompts or responses.
