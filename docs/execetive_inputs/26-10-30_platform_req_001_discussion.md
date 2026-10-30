> **Provenance note.** These questions and answers are reconstructed from `docs/tracks/platform/phase_1/requirements/req_001.decisions.md` (rows D-01..D-19) on 2026-10-30, at the design-close of the discussion stage. They are verbatim only for what the user said in a chat. Answered `Q-NN` rows were deleted from the log when they were decided, so the original question text did not survive. Where no question text exists, it says "question text not recorded". The user's own words from the requirement rounds are in `docs/tracks/platform/phase_1/requirements/req_001.md` (verbatim sources `docs/reqs/phase1/req001.txt`, `req001_q1.txt`, `req001_q2.txt`).

agent questions:
- Q-01 (stack): question text not recorded.
- Q-02 (exposure of admin UI / consumer API): question text not recorded.
- Q-03 (offline queuing): question text not recorded.
- Q-04 (transport): question text not recorded.
- Q-05 (consumer login method): question text not recorded.
- Q-06 (engine install): question text not recorded.
- Q-07 (background-work hours): question text not recorded.
- Q-08 (usage-record integrity: signed vs tamper-evident): question text not recorded.
- Q-09 / Q-13 (how PA plugs in): question text not recorded.
- Q-10 (PA requirement file timing and content): question text not recorded.
- Q-11 (licence): question text not recorded.
- Q-12 [fit] (Phase 1 build vs future seams): question text not recorded.
- Q-14 [bigger-changes] (Phase 1 scope trims): question text not recorded.
- Q-15 [extra-use-cases] (additional use cases): question text not recorded.
- Software-touching? (Level 1 gate): question text not recorded.

user response:
(Decisions as recorded in the log; the log is the source of truth.)
- D-01: Requirement is software-touching: yes (a new app — inference gateway, admin and consumer UIs, installers — plus the PA integration).
- D-02 (answers Q-12): Phase 1 builds only what invited consumers and PA need: the inference gateway, keys/grants, usage, resource requests and the Tailscale transport. The future marketplace/discovery, own relay server, billing/credits ledger and non-inference resource kinds are designed as seams but not built.
- D-03 (answers Q-13, Q-09): PA plugs in through a new local-server adapter implementing PA's existing `LLMProvider` port, speaking local-server's OpenAI-compatible API with auth, quota/"request more" awareness, and PA's existing Gemini/Anthropic adapters as the fallback when the host is offline or declines. PA's Ollama adapter is not extended.
- D-04 (answers Q-14): Phase 1 scope trims. (1) Notifications: only 2–3 of the 5 channels, whichever are cheapest (interpreted as in-app + webhooks + desktop/OS; email and Telegram later). (2) Install: curl/PowerShell scripts first; brew and winget later. (3) Auto-update later. (4) This repo's work never modifies PA's code; here we only write PA's requirement file(s). (5) Everything else in req_001 is built in Phase 1.
- D-05 (answers Q-15): Accepted: host status endpoint `/v1/health` (online, loaded models, queue depth).
- D-06 (answers Q-15): Accepted: a "pause sharing" switch plus quiet hours.
- D-07 (answers Q-15): Accepted: an audit log of admin actions.
- D-08 (answers Q-15): Scoped for future: client-side failover across several hosts.
- D-09 (answers Q-01): Stack: Go, single static binary per OS (macOS/Windows/Linux), SQLite storage, web UI built into the binary; storage behind an interface.
- D-10 (answers Q-02): Consumer API and consumer web page over the transport (Tailscale); admin UI on localhost by default; one setting adds a second listener on the host's private tailnet address behind an admin login; nothing admin-related ever on the public internet.
- D-11 (answers Q-03): The host queues work only while online, interactive ahead of background; while the host is offline the client app keeps its own retry queue for background work and resource requests and resends when `/v1/health` shows the host back; interactive calls go straight to the client's fallback.
- D-12 (answers Q-04): Tailscale is the Phase 1 default behind a transport adapter; a no-plaintext public option is decided in HLD (carried as Q-16).
- D-13 (answers Q-05): Consumer login: both password (hashed) and passkey (WebAuthn); invite links carry a one-time sign-up token; auth behind an adapter; key-only consumers need no login.
- D-14 (answers Q-06): The installer detects Ollama; if missing it asks "Install Ollama now?" and runs Ollama's official installer on yes; never installs silently.
- D-15 (answers Q-07): Background-work time windows as one rule type in the request-properties policy, per grant or globally; separate from pause/quiet hours.
- D-16 (answers Q-08): Both in Phase 1: a tamper-evident, append-only, hash-chained usage ledger on the host, and a host-signed usage receipt in every response; needs a host signing key pair whose public key consumers can fetch.
- D-17 (answers Q-10): PA requirement file written after HLD (and LLD where it fixes the API contract) at `personal-assistant/docs/tracks/platform/phase_2/requirements/req_001.md`; not committed; PA's code never changed from this repo.
- D-18 (answers Q-11): Licence: Apache-2.0.
- D-19: Discussion stage (Level 1 #2) closed 2026-10-30. 0 deferred.
