> **Provenance note.** Verbatim from this chat on 2026-10-30. The questions were asked after research-with-sources resolved the five Unsourced claims (F-06..F-10 in `docs/tracks/platform/phase_1/requirements/req_001.decisions.md`). Recorded as D-48 and D-49.

agent questions:
1. "Ollama silently drops the oldest chat messages when a prompt is longer than the context limit. How should local-server handle it (an LLD addendum to D-22/D-39)?"
   - Refuse, never truncate (Recommended): send truncate:false and shift:false on /api/chat, and truncate:false on /api/embed. Map Ollama's 400 to our `context_too_long`. Pin a minimum Ollama version and add a contract test, since these fields aren't documented. Fits D-32's 'never silently ignored'.
   - Allow, but flag it: let Ollama truncate, and add a response header or receipt field saying the prompt was truncated.
   - Grant setting: default to refuse, and let the host allow truncation per grant.
2. "After the host says yes to 'Install Ollama now?' on Windows, how should the installer run OllamaSetup.exe (D-14)?"
   - /SILENT with progress (Recommended): no wizard, but a progress window shows. Per user, no admin. Ollama starts when it finishes.
   - Full wizard: run the normal installer UI and wait for the host to click through.
   - /VERYSILENT: fully hidden; the script prints progress itself.

user response:
1. "Grant setting"
2. "/SILENT with progress (Recommended)"
