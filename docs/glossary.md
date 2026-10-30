# Glossary

One word, one meaning. This file is the single place where a term is defined. Other docs link here instead of redefining terms.

**Rules**
- Search here before naming anything new. If a term exists, use it. If it doesn't, add a row in the right section.
- **Users see** is the wording the product shows people. `—` means the term is internal and never shown to users.
- **Source** is the requirement line or decision row (`D-NN` in [`req_001.decisions.md`](tracks/platform/phase_1/requirements/req_001.decisions.md)) the term comes from.
- A rename keeps an old → new row under [Retired names](#retired-names). The old word is not used in new docs, UI or code.

## People and roles

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Host | The person who owns the machine and shares its AI resources. | Host | req_001 §2 glossary |
| Consumer | Someone allowed to use a host's resources. A consumer is an account of kind *individual* (a person) or *application* (an app acting on its own behalf). A *key-only consumer* has access keys but no login. | Consumer | req_001 §2 glossary, A4, D-13 |
| Client app | Software that calls a host on a consumer's behalf, e.g. PA. | App | req_001 §2 glossary |
| PA | The personal-assistant application, the first client app. It lives in a separate repo (`personal-assistant`). | — | req_001 formal §1, D-03 |

## Machines and software

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Host machine / Node | The computer running local-server (e.g. the Mac mini). | Host machine | req_001 §2 glossary |
| local-server | This application: the gateway and admin/consumer web pages that run on the host machine. | local-server | req_001 |
| Engine | The model runtime underneath local-server (Ollama by default; llama.cpp and MLX later). | Engine | req_001 §2 glossary, D-14 |
| Transport | How consumers reach the host machine over an encrypted channel. Phase 1 default: Tailscale. | Connection | D-12 |
| Fallback | The provider a client app uses when the host is offline, paused or declines (for PA: its Gemini/Anthropic adapters). | Fallback | D-03, D-11 |

## Access and limits

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Access key | A credential the host issues to a consumer to call the API. Shown once, can expire or be revoked. "API key" is an accepted synonym in conversation; docs and UI say *access key*. | Access key | req_001 §2 glossary |
| Invite | A one-time sign-up link the host sends so a person can create a consumer account. | Invite link | D-13 |
| Share budget | Everything the host sets aside for all consumers together (models, memory, concurrency, tokens). | Share budget | req_001 §2 glossary |
| Grant | One consumer's slice of the share budget: allowed models, quotas, context limit, output limit, concurrency, expiry and allowed request properties. | Your limits | req_001 §2 glossary |
| Request properties | Tags a consumer or client app attaches to a call (e.g. *background*), which the host's rules use to decide what the host machine does with it. | Request type | Q22, D-15 |
| Background-work hours | A time window in which background requests run (e.g. 00:00–07:00), set per grant or for the whole host. Different from quiet hours. | Background hours | D-15 |
| Pause sharing | A switch that stops the host serving all consumers immediately. Consumers see the host as *paused*. | Paused | D-06 |
| Quiet hours | A schedule during which sharing is paused automatically. | Quiet hours | D-06 |

## Requests and records

| Term | Meaning | Users see | Source |
|---|---|---|---|
| Resource request | A consumer asking the host for more (another model, a higher quota, …). The host approves or rejects it, and both sides can comment. | Request | req_001 §2 glossary |
| Host status | The public health signal a client app checks: online, loaded models, queue depth, paused. | Host status | D-05 |
| Usage ledger | The host's append-only, tamper-evident record of every metered request. | Usage history | D-16 |
| Usage receipt | A record of one request's usage, signed by the host and returned with the response, so a consumer can check a bill independently. | Receipt | D-16 |
| Audit log | The record of admin actions: keys issued or revoked, grants changed, requests decided. | Activity log | D-07 |

## Retired names

| Old | New | Why |
|---|---|---|
| host user | Host | Ambiguous with consumer (req_001). |
| user user | Consumer | Ambiguous with host (req_001). |
| reserved resources | Share budget | req_001 §2 glossary. |
