> **Provenance note.** Verbatim from the implementation-planning chat of 2026-10-30/31, before the first build. Round 1 came from the `/orchestrate-implement platform 1` plan call; round 2 followed the owner's private notes (not in the repo). Recorded as D-57..D-63.

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
