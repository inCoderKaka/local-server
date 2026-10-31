# PLT track — bugs (phase_1)

Bug registry for the platform track. Convention: `docs/tracks/README.md`.
Statuses: `reported → confirmed → fixed → shipped`; `dropped` (reason). Default P2.

| ID | Pri | Status | Description / Ref | Source |
|---|---|---|---|---|
| PLT-B-001 | P2 | reported | CI actions in `.github/workflows/ci.yml` and `fuzz.yml` use floating `@v7` tags, no SHA pins, no update bot; a moved tag runs unreviewed code, and PLT-C-003's release job will reuse them. Needs a D-row next to D-62: (a) SHA-pin + dependabot (recommended, before PLT-C-003), (b) pin by hand, (c) keep floating. Gate set A `7454922..3cf4f3f` finding 9; found in PLT-C-001. req_001 D-62 · P1/B1/S1 · verify: `grep -nE "uses: .*@v[0-9]+$" .github/workflows/*.yml` prints nothing | review-gate |
