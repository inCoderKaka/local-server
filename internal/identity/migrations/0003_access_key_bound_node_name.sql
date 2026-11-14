-- identity module, migration 3: the machine name of the tailnet node an access key is bound to
-- (req_001 D-154, D-159). Additive only: a nullable column; a key bound before this migration has
-- the node id and no name, and is checked by the id as before. bound_node holds Tailscale's stable
-- node id, which survives a rename; the name is kept so the host can recognise the machine.

ALTER TABLE idn_access_key ADD COLUMN bound_node_name TEXT;
--@column idn_access_key.bound_node classification=internal billable=false | Stable id of the tailnet node the key is bound to, taken from Tailscale when the host binds it; empty means not bound. The key works only from that node.
--@column idn_access_key.bound_node_name classification=personal billable=false | Machine name of the bound node when the host bound it, for the host to recognise it; empty when not bound. Often a person's name; cleared when the consumer is deleted. Not checked on use: the node id is.
