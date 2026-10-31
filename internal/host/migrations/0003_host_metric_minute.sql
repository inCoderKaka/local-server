-- host module, migration 3: one host-resource reading per minute (D-31, D-40).
-- The sampler writes the latest reading of each minute here and the retention worker prunes rows
-- older than host.metrics.retention (7 d by default, D-38). Rows hold numbers and measure labels
-- only, never prompts, responses or model names. They are not synced.

CREATE TABLE hst_metric_minute (
  minute                INTEGER PRIMARY KEY,
  sampled_at            INTEGER NOT NULL,
  mem_total_bytes       INTEGER NOT NULL,
  mem_used_bytes        INTEGER NOT NULL,
  cpu_cores             INTEGER NOT NULL,
  cpu_percent           REAL NOT NULL,
  slice_mem_bytes       INTEGER NOT NULL,
  slice_cpu_percent     REAL NOT NULL,
  other_mem_bytes       INTEGER NOT NULL,
  other_cpu_percent     REAL NOT NULL,
  unattributed          INTEGER NOT NULL,
  slice_clamped         INTEGER NOT NULL CHECK (slice_clamped IN (0, 1)),
  measures              TEXT NOT NULL
) STRICT;
--@table hst_metric_minute classification=internal billable=false | The host's resource use, one row per minute: host totals, the share-budget slice (local-server, Ollama and loaded models), other apps and the unattributed process count. Not synced.
--@column hst_metric_minute.minute classification=internal billable=false | The start of the minute, Unix milliseconds UTC.
--@column hst_metric_minute.sampled_at classification=internal billable=false | When the last sample of the minute was taken, Unix milliseconds UTC.
--@column hst_metric_minute.mem_total_bytes classification=internal billable=false | Total memory of the host.
--@column hst_metric_minute.mem_used_bytes classification=internal billable=false | Memory in use on the host, as the OS counts it (this can differ from Activity Monitor on macOS).
--@column hst_metric_minute.cpu_cores classification=internal billable=false | Logical CPU cores of the host.
--@column hst_metric_minute.cpu_percent classification=internal billable=false | CPU use of the whole host, 0 to 100.
--@column hst_metric_minute.slice_mem_bytes classification=internal billable=false | Memory of the share-budget slice: local-server, Ollama and loaded models, never more than the host total.
--@column hst_metric_minute.slice_cpu_percent classification=internal billable=false | CPU of the slice as a share of the whole host, 0 to 100.
--@column hst_metric_minute.other_mem_bytes classification=internal billable=false | Memory used by other apps: the host total in use minus the slice, never below zero.
--@column hst_metric_minute.other_cpu_percent classification=internal billable=false | CPU used by other apps: host CPU minus the slice, never below zero.
--@column hst_metric_minute.unattributed classification=internal billable=false | How many processes the OS would not let local-server read; their use is counted in other apps.
--@column hst_metric_minute.slice_clamped classification=internal billable=false | 1 when the slice figures were cut down to the host total.
--@column hst_metric_minute.measures classification=internal billable=false | JSON object saying how each value was measured (host_total, process_rss, engine_loaded, derived or unavailable).
