-- identity module, migration 2: the consumer's billing mode (req_001 D-107).
-- A consumer is none, prepaid or postpaid. Only a chargeable consumer (prepaid or postpaid) may set
-- quota windows on its own access keys; the host sets them for every other consumer. The column
-- is a label only: Phase 1 keeps no balances, invoices or prices (req_001 D-02).

ALTER TABLE idn_consumer ADD COLUMN billing_mode TEXT NOT NULL DEFAULT 'none' CHECK (billing_mode IN ('none', 'prepaid', 'postpaid'));
--@column idn_consumer.billing_mode classification=internal billable=true | none (default), prepaid or postpaid. Only a prepaid or postpaid consumer may set quota windows on its own keys; the host sets them for the rest. A label only: no balances, invoices or prices.
