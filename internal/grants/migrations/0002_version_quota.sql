-- grants module, migration 2: the windowed quota entries of a grant version (req_001 D-107).
-- A quota is a list of entries {metric, limit, window}: at most one per metric and window length.
-- Like grt_version_model the rows are immutable, because a grant version is (use records and
-- receipts refer to it forever). The two columns grt_version.quota_tokens_per_day and
-- grt_version.quota_requests_per_day stay in the table (migrations only go forward) as a
-- deprecated, read-only alias: new versions leave them empty, old versions keep what they hold,
-- and the limiter reads them as one entry each with the window limits.quota_window.
-- Numbers only, no content (D-31).

CREATE TABLE grt_version_quota (
  version_id  TEXT NOT NULL REFERENCES grt_version(id) ON DELETE CASCADE,
  metric      TEXT NOT NULL CHECK (metric IN ('tokens', 'requests')),
  quota_limit INTEGER NOT NULL CHECK (quota_limit >= 0),
  window_ms   INTEGER NOT NULL CHECK (window_ms >= 1000 AND window_ms % 1000 = 0),
  PRIMARY KEY (version_id, metric, window_ms)
) STRICT;
--@table grt_version_quota classification=internal billable=true | One quota entry of a grant version: the most tokens or requests in one fixed window. Immutable, with its version.
--@column grt_version_quota.version_id classification=internal billable=false | Grant version the entry belongs to.
--@column grt_version_quota.metric classification=internal billable=true | What is counted: tokens or requests.
--@column grt_version_quota.quota_limit classification=internal billable=true | Most of the metric in one window; 0 allows none.
--@column grt_version_quota.window_ms classification=internal billable=true | Length of the quota window, a whole number of seconds in milliseconds (at least 1000).

CREATE TRIGGER grt_version_quota_no_update BEFORE UPDATE ON grt_version_quota
BEGIN SELECT RAISE(ABORT, 'grt_version_quota is immutable'); END;
CREATE TRIGGER grt_version_quota_no_delete BEFORE DELETE ON grt_version_quota
BEGIN SELECT RAISE(ABORT, 'grt_version_quota is immutable'); END;

--@column grt_version.quota_tokens_per_day classification=internal billable=true | Deprecated, read-only alias (req_001 D-107): a token limit for the window limits.quota_window, kept for versions made before grt_version_quota. New versions leave it empty and use grt_version_quota.
--@column grt_version.quota_requests_per_day classification=internal billable=true | Deprecated, read-only alias (req_001 D-107): a request limit for the window limits.quota_window, kept for versions made before grt_version_quota. New versions leave it empty and use grt_version_quota.
