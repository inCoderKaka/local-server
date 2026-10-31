-- events module, migration 1: the domain event table (D-29, D-39). An event row is written in the
-- same unit of work as the change it announces, so it exists only if that change committed.
-- Payloads never hold prompts, responses or secrets (D-31, D-40). No foreign keys: an event is
-- history and must outlive a soft-deleted principal.

CREATE TABLE ntf_event (
  id                  TEXT PRIMARY KEY,
  type                TEXT NOT NULL CHECK (type <> ''),
  occurred_at         INTEGER NOT NULL,
  actor_principal_id  TEXT,
  target_principal_id TEXT,
  payload             TEXT NOT NULL DEFAULT '{}'
) STRICT;
--@table ntf_event classification=internal billable=false | A domain event: something that happened (a request created, a host paused, a quota near its limit), written in the unit of work of the change that caused it.
--@column ntf_event.id classification=internal billable=false | Event id, UUIDv7.
--@column ntf_event.type classification=internal billable=false | Event type such as host.paused; a closed set defined in code.
--@column ntf_event.occurred_at classification=internal billable=false | When the event happened, Unix milliseconds UTC.
--@column ntf_event.actor_principal_id classification=internal billable=false | The principal that caused the event; empty when the system did.
--@column ntf_event.target_principal_id classification=internal billable=false | The principal the event is about; empty when none.
--@column ntf_event.payload classification=internal billable=false | JSON object with ids and counters describing the event; never a prompt, a response or a secret.

CREATE INDEX ntf_event_occurred_at ON ntf_event (occurred_at);
CREATE INDEX ntf_event_type_occurred_at ON ntf_event (type, occurred_at);
