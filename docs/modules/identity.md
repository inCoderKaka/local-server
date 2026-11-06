# identity module

## Purpose

Principals, admin users, consumers, credentials, sessions and consent records. Also defines the content-capture interface, which stays off and can only run behind a matching consent record.

## Owns

- Packages: `internal/identity/` (built by PLT-F-007).
- Tables: `idn_principal`, `idn_admin_user`, `idn_consumer`, `idn_credential`, `idn_invite`, `idn_access_key`, `idn_session`, `idn_consent`; migrations in `internal/identity/migrations/`. Only consumer changes are captured for sync (dataset `consumer_directory`); credentials and keys never sync.
- Admin-facing reads and erasure helpers (PLT-F-025): `ConsumerRepo.GetAccount`, `GetAccountTx` and `ListAccounts` (a consumer with its principal status and login, listed newest first by `PageKey`), `AccessKeyRepo.ListByConsumerTx`, `InviteRepo.GetTx` and `ListPage`, `CredentialRepo.DeleteByPrincipal` and `InviteRepo.EraseNotesRedeemedBy` (both used when a consumer is deleted, req_001 D-40).
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-42, D-44, D-45. The module map is in `docs/design_docs/lld.md` section 1.
