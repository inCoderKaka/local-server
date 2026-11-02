# Roadmap

Phase 1 is fully designed ([HLD](docs/design_docs/hld.md), [LLD](docs/design_docs/lld.md)) and is being built in the order below. Each batch builds on the ones before it. The detailed tickets live in [`docs/tracks/platform/phase_1/`](docs/tracks/platform/phase_1/next-steps.md).

## Phase 1 — share a host's AI with invited consumers

| Batch | What it delivers | Status |
|---|---|---|
| B1 Foundations | Go module and a 3-OS CI definition (not yet run on GitHub) with boundary, race, fuzz and vulnerability checks; the OpenAPI 3.1 contract; the storage foundation (single writer, migrations, data catalog); telemetry; runtime settings; host identity and sealed secrets | built |
| B2 Core domain | Engine port with the Ollama adapter; identity and grants; audit log; domain events; access keys; policy rules; host metrics; pause sharing and quiet hours | built |
| B3 Inference path | Scheduler with memory admission; usage records, Merkle ledger and signed receipts; checkpoints and proofs; quotas and rate limits; the OpenAI-compatible gateway with streaming; jobs API; usage rollups; host status, capabilities and models endpoints; startup and the `doctor` command | in progress: scheduler, usage records, ledger with receipts, checkpoints and proofs, quotas and rate limits, the chat (with streaming) and embeddings gateway, and the host status, capabilities and models endpoints built; nothing runs end to end until startup is built |
| B4 Control plane and accounts | Admin API; consumers, invites and keys; grants, models and policy; consumer accounts with password and passkey login; resource requests; terms and privacy drafts | planned |
| B5 Notifications | In-app notifications, Standard Webhooks via an outbox, desktop notifications | planned |
| B6 Web UIs | Admin UI and consumer web page | planned |
| B7 Transport | System Tailscale, opt-in public mode (Funnel) with mandatory protections, optional embedded tsnet | planned |
| B8 Packaging | Background service, sleep lock, release pipeline with SLSA provenance, install and upgrade scripts | planned |
| B9 Off-host data | Encrypted backups to a folder, R2 or Google Cloud Storage; signed export and import; opt-in platform sync (Postgres); sharing grants | planned |
| B10 Docs | Host setup in the user guide; README install steps | planned |

## Later phases (designed as seams, not yet scheduled)

- **Discovery and marketplace:** a central directory of hosts with benchmarks and smart host selection. Proposed on Google Cloud ([HLD §9](docs/design_docs/hld.md#9-future-scale-out-and-the-central-service-proposal-not-decided)).
- **Billing:** plans and credits, built on the usage ledger and receipts that Phase 1 already produces. A commercial-ready transport (Cloudflare Tunnel with our own end-to-end encryption, or our own relay) comes first, since Tailscale's free plan is non-commercial.
- **Failover:** client-side failover across several hosts.
- **More engines:** llama.cpp and MLX adapters.
- **More channels:** email and Telegram notifications.
- **Packaging:** brew and winget packages, auto-update.
- **Telemetry:** an OpenTelemetry adapter.
- **More resource kinds:** non-inference resources.
