# identity module

## Purpose

Principals, admin users, consumers, credentials, sessions and consent records. Also defines the content-capture interface, which stays off and can only run behind a matching consent record.

## Owns

- Packages: `internal/identity/` (built by PLT-F-007).
- Tables: `idn_principal`, `idn_admin_user`, `idn_consumer`, `idn_credential`, `idn_invite`, `idn_access_key`, `idn_session`, `idn_consent`; migrations in `internal/identity/migrations/`. `idn_consumer.billing_mode` (migration 2, req_001 D-107) is `none` (the default), `prepaid` or `postpaid`: a label that says whether the consumer may set quota windows on its own keys (`Consumer.Chargeable()`); it holds no balance, invoice or price (D-02). `ConsumerRepo.SetBillingMode` changes it and records the edit for sync like any consumer change. Only consumer changes are captured for sync (dataset `consumer_directory`); credentials and keys never sync.
- Admin-facing reads and erasure helpers (PLT-F-025): `ConsumerRepo.GetAccount`, `GetAccountTx` and `ListAccounts` (a consumer with its principal status and login, listed newest first by `PageKey`), `AccessKeyRepo.ListByConsumerTx`, `InviteRepo.GetTx` and `ListPage`, `CredentialRepo.DeleteByPrincipal` and `InviteRepo.EraseNotesRedeemedBy` (both used when a consumer is deleted, req_001 D-40). Purge (PLT-F-097): `ConsumerRepo.ListPurgeable` (deleted consumers whose principal was deleted before a cutoff and that still hold a consent, credential, session, access key or redeemed invite; in id order, paged by id) and `ConsumerRepo.Purge` (deletes those rows and clears `idn_invite.redeemed_consumer_id`, refuses a consumer whose principal is not `deleted`, returns `PurgeCounts`). The consumer and principal rows stay as a tombstone, so there is no cascade and no migration (req_001 D-136, D-106).
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-42, D-44, D-45, D-107. The module map is in `docs/design_docs/lld.md` section 1.
