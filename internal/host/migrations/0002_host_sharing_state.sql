-- host module, migration 2: the host's manual sharing state (D-06).
-- Only sharing and paused are stored. Quiet hours are derived from the quiet-hours settings and the
-- clock (host.quiet_hours.*), never written, so a schedule transition leaves no row.

ALTER TABLE hst_host ADD COLUMN sharing_state TEXT NOT NULL DEFAULT 'sharing' CHECK (sharing_state IN ('sharing', 'paused'));
ALTER TABLE hst_host ADD COLUMN sharing_state_changed_at INTEGER;
--@column hst_host.sharing_state classification=internal billable=false | The host's manual switch: sharing (serving) or paused (the host stopped serving by hand). Quiet hours are not stored here.
--@column hst_host.sharing_state_changed_at classification=internal billable=false | When the switch last changed, Unix milliseconds UTC; empty until the first change.
