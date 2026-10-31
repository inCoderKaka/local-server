> **Provenance note.** Verbatim from the implementation-planning chat of 2026-10-30/31, before the first build. Round 1 came from the `/orchestrate-implement platform 1` plan call; round 2 followed the owner's private notes (not in the repo). Recorded as D-57..D-72. Rounds 3 to 5 settled the review gates' deferred findings.

agent questions (round 1):
1. "PLT-F-001 needs a code generator and spec linter for the OpenAPI 3.1 spec (D-32 says only 'spec-first, server interfaces generated'; no decision row names a tool). Which should the builder use? It will be recorded as a D-NN row."
   - ogen + vacuum (Recommended): Both pinned as `go tool` entries. The builder first checks that ogen handles OpenAPI 3.1 for this spec. If it doesn't, it stops and reports instead of switching tools on its own.
   - oapi-codegen + other linter: A more widely used generator, but its OpenAPI 3.1 support is less complete.
   - Hand-written handlers: No generator. A contract test checks the handlers against the spec.
2. "PLT-F-003 needs a log-rotation library, and none is named. Which one should it use? It will be recorded as a D-NN row."
   - natefinch/lumberjack (Recommended): Widely used and pure Go. It sits behind the Telemetry port, so it can be swapped later.
   - Write our own: No extra dependency, but more code to maintain.
3. "Start building the B1 plan (steps 1–4: PLT-C-001, then F-001/F-002/F-003, then F-004/F-006/C-002, then F-005)?"
   - Go: full plan / Go: step 1 only / Not now

user response (round 1):
1. "ogen + vacuum (Recommended)"
2. "natefinch/lumberjack (Recommended)"
3. "Not now, i have one more thing to add before the implementation begins."

agent questions (round 2):
1. "Git can't make single files private, and flipping this repo to public would expose its whole history. How should local-server be split into public and private?"
   - Private source + public mirror (Recommended): This repo stays private and keeps everything. A publish script (Copybara-style path filter) pushes only the public paths to a new public repo. All tooling keeps working.
   - Two repos (public code, private notes): Notes live in a separate private repo. This breaks the tracking tools that read docs/tracks git history in the same repo.
   - One public repo with git-crypt: Private paths are stored encrypted. File names and commit messages stay public, and one mistake leaks a file permanently.
   - Keep everything private: Give interviewers access on request. Recruiters can't find the project.
2. "Beyond the code, HLD/LLD, README, user/developer guides and approved legal docs (all public), which of these should also be public?" (multi-select)
   - ADRs (Recommended) · Curated ROADMAP.md (Recommended) · Raw decisions logs · AI workflow files
3. "Where should Google Cloud appear? (The product is a local desktop binary, so GCP fits only in a few genuine places.)"
   - GCS backup + future GCP server (Recommended): PLT-F-044 tests Google Cloud Storage (S3-compatible API) as a backup target. The HLD's future scale-out section proposes Cloud Run, Firestore/Spanner, Pub/Sub, KMS and BigQuery for the later central directory service.
   - Also move platform sync to GCP: Revisit D-41 / PLT-F-046 (Supabase Postgres) in an HLD round. Cloud SQL has no free tier, and the Firestore Go client adds gRPC and binary size.
   - No GCP in this project.
4. "Which engineering-quality additions should become decision rows and go into the tickets' verify lines?" (multi-select)
   - Fuzz tests on untrusted input (Recommended) · Race detector in CI (Recommended) · Signed provenance + govulncheck (Recommended)

user response (round 2):
1. "You know what make everything public, its fine (except the mnp_nt.md)."
2. "ADRs (Recommended), Curated ROADMAP.md (Recommended), Raw decisions logs, AI workflow files, "You know what make everything public, its fine (except the mnp_nt.md).""
3. "GCS backup + future GCP server but keep adapters such that this can be shifted to GCP if at all required. even the platform sync decisions dont change if adapters are the way we're implementing them."
4. "Fuzz tests on untrusted input (Recommended), Race detector in CI (Recommended), Signed provenance + govulncheck (Recommended)"

agent questions (round 3):
1. "Backups vs. 'content never leaves the host': whole-database snapshots (VACUUM INTO → encrypt → your bucket) would also copy encrypted queued-job content, job results and login sessions, and keep them until old snapshots are pruned. That breaks the promise 'deleted when you fetch the result'. What should backups contain? (Concept: data minimisation vs. a byte-exact restore.)"
   - Strip content first (Recommended) · Keep everything, reword docs
2. "Which key signs ledger checkpoints (the signed tree heads consumers use to verify their receipts)? Today only receipts have a key rule. (Concept: key hierarchy, and a stable key so outside witnesses can follow the log, as in Certificate Transparency.)"
   - Dedicated checkpoint key (Recommended) · Host identity key directly · Receipt-signing subkey
3. "Consumers can register webhook URLs. Should webhooks to private addresses (home LAN 192.168.x, 10.x; tailnet 100.64.0.0/10; localhost) be allowed? Allowing them by default lets a consumer use your host to probe your home network: server-side request forgery (SSRF)."
   - Deny by default, host can allow (Recommended) · Always deny · Allow tailnet, deny LAN/loopback
4. "Opt-in platform sync retries forever. If the platform's Supabase project pauses (it does after a week idle on the free plan), sync silently stops. How should failures surface? (Concept: alert on symptoms, not causes, as in Google's SRE guidance.)"
   - Alert the host after a threshold (Recommended) · Status only, no alert

user response (round 3):
1. "this should be selectable by the user as to whether the data should be minimized or all the info is saved. users can select which data to minimize and which to save as is."
2. "Dedicated checkpoint key (Recommended)"
3. "Deny by default, host can allow (Recommended)"
4. "Alert the host after a threshold (Recommended)"

agent questions (round 4):
1. "D-64 leaves job content out of backups by default. So after a restore, jobs that were still queued at backup time come back with no payload, and the job state machine (D-54) has no state for that. How should restore treat them? (Concept: explicit, terminal failure states, like the existing failed(host_restart).)"
   - Fail them: payload_minimized (Recommended) · Leave queued until expiry
2. "The gate added a backup rule: a table can only be left out (minimized) if every table that references it is also left out. Otherwise SQLite's foreign-key CASCADE would silently empty tables you chose to keep. Record this as a decision?"
   - Yes, record it (Recommended) · Auto-include dependents

user response (round 4):
1. "Fail them: payload_minimized (Recommended)"
2. "Yes, record it (Recommended)"

agent questions (round 5):
1. "The settings table is named hst_setting, but in the table-prefix map (D-44) hst_ belongs to the host module, and the settings module has no prefix of its own. (Concept: naming conventions as enforced module ownership.) Which should it be?"
   - Keep hst_setting (Recommended) · New prefix set_
2. "D-69 (minimize a table only if every table referencing it is also minimized) is checked only when the host changes a setting. A later upgrade could add a kept table that references a minimized one, and the next backup's CASCADE would silently empty it. What should a backup do when it finds the rule broken?"
   - Fail the run and alert (Recommended) · Keep the parent, warn
3. "While fixing D-68, the gate wrote a new design sentence into the LLD: 'Restore runs before the host next starts, so the start-up host_restart pass finds those jobs already terminal.' In other words, payload_minimized takes precedence over host_restart for restored jobs. Accept it as a decision?"
   - Accept (Recommended) · Remove it

user response (round 5):
1. "Keep hst_setting (Recommended)"
2. "Fail the run and alert (Recommended)"
3. "Accept (Recommended)"
