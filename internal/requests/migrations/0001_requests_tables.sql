-- requests module, migration 1: resource requests and their comments (D-38, D-39 F3, D-44,
-- D-157). These tables reference idn_consumer, idn_principal and grt_version, so the identity and
-- grants modules' migrations must run first: register identity and grants before requests with
-- storage. A request is open until it is approved, rejected, withdrawn or expired; every state but
-- open is final (D-38). A request keeps what the consumer wrote, which is personal text; it never
-- keeps a prompt or a response (D-31, D-40).

CREATE TABLE req_resource_request (
  id                   TEXT PRIMARY KEY,
  consumer_id          TEXT NOT NULL REFERENCES idn_consumer(id) ON DELETE RESTRICT,
  status               TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'approved', 'rejected', 'withdrawn', 'expired')),
  message              TEXT NOT NULL CHECK (message <> ''),
  ask                  TEXT CHECK (ask IS NULL OR json_valid(ask)),
  created_at           INTEGER NOT NULL,
  expires_at           INTEGER NOT NULL,
  decided_at           INTEGER,
  decided_by           TEXT REFERENCES idn_principal(id) ON DELETE RESTRICT,
  resulting_version_id TEXT REFERENCES grt_version(id) ON DELETE RESTRICT,
  row_version          INTEGER NOT NULL DEFAULT 1,
  updated_at           INTEGER NOT NULL,
  CHECK ((status = 'open' AND decided_at IS NULL) OR (status <> 'open' AND decided_at IS NOT NULL)),
  CHECK (resulting_version_id IS NULL OR status = 'approved')
) STRICT;
--@table req_resource_request classification=personal billable=false | A consumer asking the host for more (another model, a higher quota, ...). Open until the host approves or rejects it, the consumer withdraws it or its time-to-live passes; every state but open is final.
--@column req_resource_request.id classification=internal billable=false | Request id, UUIDv7.
--@column req_resource_request.consumer_id classification=internal billable=false | Consumer who asked.
--@column req_resource_request.status classification=internal billable=false | open, approved, rejected, withdrawn or expired.
--@column req_resource_request.message classification=personal billable=false | What the consumer needs and why, in the consumer's own words.
--@column req_resource_request.ask classification=personal billable=false | What the consumer asks for as a JSON object (models, quota entries, limits, request properties); empty when it asked for nothing in particular.
--@column req_resource_request.created_at classification=internal billable=false | When the request was opened, Unix milliseconds UTC.
--@column req_resource_request.expires_at classification=internal billable=false | When the request expires if it is still open, Unix milliseconds UTC; set from the host's requests.ttl setting when it was opened.
--@column req_resource_request.decided_at classification=internal billable=false | When the request reached its final state, Unix milliseconds UTC; empty while open.
--@column req_resource_request.decided_by classification=internal billable=false | Principal who made it final (the host admin who decided it or the consumer who withdrew it); empty while open and when it expired.
--@column req_resource_request.resulting_version_id classification=internal billable=false | The grant version an approval made; empty unless approved.
--@column req_resource_request.row_version classification=internal billable=false | 1 at creation, plus 1 for each change of the row; the version of its syn_change rows.
--@column req_resource_request.updated_at classification=internal billable=false | When the row last changed, Unix milliseconds UTC.

CREATE INDEX req_resource_request_consumer ON req_resource_request (consumer_id, created_at DESC, id DESC);
CREATE INDEX req_resource_request_status ON req_resource_request (status, created_at DESC, id DESC);
CREATE INDEX req_resource_request_expiry ON req_resource_request (expires_at) WHERE status = 'open';

CREATE TABLE req_comment (
  id                  TEXT PRIMARY KEY,
  request_id          TEXT NOT NULL REFERENCES req_resource_request(id) ON DELETE CASCADE,
  author_principal_id TEXT NOT NULL REFERENCES idn_principal(id) ON DELETE RESTRICT,
  body                TEXT NOT NULL CHECK (body <> ''),
  created_at          INTEGER NOT NULL
) STRICT;
--@table req_comment classification=personal billable=false | One comment on a resource request, from the consumer or the host. A decision's optional comment is stored here too.
--@column req_comment.id classification=internal billable=false | Comment id, UUIDv7.
--@column req_comment.request_id classification=internal billable=false | The request the comment belongs to.
--@column req_comment.author_principal_id classification=internal billable=false | Principal who wrote it: the consumer or a host admin.
--@column req_comment.body classification=personal billable=false | The comment, plain text.
--@column req_comment.created_at classification=internal billable=false | When it was written, Unix milliseconds UTC.

CREATE INDEX req_comment_request ON req_comment (request_id, created_at, id);
