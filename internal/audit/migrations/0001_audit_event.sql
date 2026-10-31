-- audit module, migration 1: the append-only audit trail (D-07, D-42, D-44). It references
-- idn_principal, so the identity module's migrations must run first: register identity before
-- audit with storage. An event is written in the same unit of work as the change it records.
-- Triggers refuse UPDATE and DELETE, so the trail is kept forever (lld section 3).

CREATE TABLE aud_event (
  id           TEXT PRIMARY KEY,
  action       TEXT NOT NULL CHECK (action <> ''),
  actor_id     TEXT REFERENCES idn_principal(id) ON DELETE RESTRICT,
  target_id    TEXT REFERENCES idn_principal(id) ON DELETE RESTRICT,
  detail       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(detail)),
  created_at   INTEGER NOT NULL
) STRICT;
--@table aud_event classification=internal billable=false | One thing that happened on this host, such as a key issued or a grant changed, kept forever.
--@column aud_event.id classification=internal billable=false | Event id, UUIDv7.
--@column aud_event.action classification=internal billable=false | What happened, a dotted name such as grant.changed.
--@column aud_event.actor_id classification=internal billable=false | Principal who did it; empty when the system did it.
--@column aud_event.target_id classification=internal billable=false | Principal it was done to; empty when it has no principal target.
--@column aud_event.detail classification=internal billable=false | Structured JSON object with the facts of the event; never holds prompts, responses or secrets.
--@column aud_event.created_at classification=internal billable=false | When it happened, Unix milliseconds UTC.

CREATE INDEX aud_event_created ON aud_event (created_at, id);
CREATE INDEX aud_event_actor ON aud_event (actor_id, created_at);
CREATE INDEX aud_event_target ON aud_event (target_id, created_at);
CREATE INDEX aud_event_action ON aud_event (action, created_at);

CREATE TRIGGER aud_event_no_update BEFORE UPDATE ON aud_event
BEGIN SELECT RAISE(ABORT, 'aud_event is append-only'); END;
CREATE TRIGGER aud_event_no_delete BEFORE DELETE ON aud_event
BEGIN SELECT RAISE(ABORT, 'aud_event is append-only'); END;
