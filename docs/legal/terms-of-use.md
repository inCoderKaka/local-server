# Terms of Use

> **Review note for the project owner. This note is not part of the versioned text and is removed when the version is approved.**
> This draft was written by an AI assistant from the decisions log (req_001 D-06, D-13, D-15, D-16, D-24, D-25, D-26, D-27, D-40, D-43, D-44, D-45, D-48, D-51, D-52, D-54). It is not legal advice. Have a qualified lawyer review it before approving it. It describes only what the decisions say. These points are deliberately left out because the decisions log does not settle them; each one needs your answer (and a `D-NN` row) before approval:
> - **Who the parties are.** The text names the host as your counterparty and says contact goes through the host. It does not say whether hosts may edit or replace this text, or what the project owner's role is beyond writing it.
> - **Legal facts.** No operator legal name, contact address, governing law, jurisdiction or minimum age is given.
> - **Liability.** There are no warranty, liability or indemnity clauses. The Apache-2.0 licence covers only the software.
> - **Acceptable use of content.** Beyond "inference only", there are no rules on what prompts are allowed.
> - **Key-only consumers.** They never sign up, so no consent record is made for them (D-45 records sign-up acceptance only). How they accept these terms is open.
> - **New versions.** It is open how a consumer is asked to accept a newer version, and what happens if they do not.
> - **Suspension.** The decisions do not say what a suspended consumer can still do, so the text says only that the host can suspend and reactivate an account.
> - **Self-service deletion.** It is open whether a consumer can delete their own account. The text says they can ask the host.

**Version:** terms-2026-10-v1

**Status:** Draft v1 for owner review. These terms are not in force. Nobody is asked to accept them until the project owner approves this text.

## How versions are named

Every version of these terms has an id of the form `terms-<YYYY-MM>-v<N>`. `YYYY-MM` is the year and month the text was written. `N` starts at 1 and goes up by one for each new text written in the same month. When you accept the terms, your consent record stores this id. Once a version is in force, its text never changes. Any change, however small, gets a new id. A draft keeps its id while it is being reviewed.

## 1. Who these terms are between

local-server is software that a **host** runs on their own computer (the **host machine**) to share its AI with people and apps they invite. You are a **consumer**. You might be a person with an account, or an app with an access key.

These terms cover your use of the host's AI through local-server. Your arrangement is with the host who invited you, and any question about these terms or your account goes to that host. The software itself is open source under the Apache-2.0 licence. That licence covers the software, and these terms cover using a host's resources.

How your data is handled is described in the [Privacy and consent notice](privacy-consent.md), version `privacy-2026-10-v1`.

## 2. What the host offers, and what you may do

- **The host offers AI inference.** That means running AI models on the host machine through its model runtime (the **engine**, Ollama by default), for chat and embeddings, within the limits the host sets for you.
- **You may only send inference requests.** local-server gives you no way to run your own code, programs or commands on the host machine. Do not try to.
- **The host decides what is shared.** That covers which models, how much and when, and the host can change it at any time.
- **The host machine is not always available.** It can be offline, busy or paused. You can check its **host status**, which says whether it is online, which models are loaded, how many requests are waiting, and whether sharing is paused. Apps should have a fallback for when the host cannot answer.
- **Keep your access keys and your password secret.** An access key is shown only once. If you think one has leaked, tell the host so they can revoke it.

## 3. Limits

- **Your grant.** The host gives you a grant, shown to you as *Your limits*. It sets which models you may use, your quotas, the context limit, the output limit, how many requests you may run at once, when the grant expires, and which request types you may send. Each change makes a new grant version, and every usage record names the version it ran under.
- **Access keys** can be limited more narrowly than your grant.
- **Quotas.** You get a notice when you are close to a quota. A request over a quota is refused with `quota_exceeded`.
- **Rate limits.** The host may limit how many requests a key can send in a period. If the host has turned on public access, rate limits are always on.
- **Prompts that are too long.** Your grant decides what happens when a prompt is longer than the context limit. It is either refused (the default) or shortened by dropping the oldest messages.
- **When the host is busy,** a request may be refused with a "try again later" answer that says when to retry.
- **Background-work hours.** The host may allow background requests only at certain times.
- **Asking for more.** You can send a **resource request**, for example for another model or a higher quota. You and the host can both comment on it. The host approves or rejects it and has no duty to approve. Approval gives you a new grant version. You can withdraw a request, and an open request expires after a time the host sets.
- Do not try to get around your limits.

## 4. Usage records and receipts

- **Usage records.** Every metered request makes a usage record. It says which account and access key made the request, the model, the token counts, the timing and the grant version. The record goes into the host's **usage ledger** (*Usage history*). The ledger is append-only and tamper-evident: records can be added, but changing or removing one would show.
- **Receipts.** Every response carries a usage receipt signed by the host. It comes in a response header, or in the final event of a streamed answer. Keep your receipts. With the host's public keys and the proof the host publishes for each record, anyone can check that a receipt is genuine and that the host's ledger contains it.
- **Unmetered receipts.** If the host answers but cannot record the usage, the receipt is marked *unmetered*. That is signed evidence that the call cannot be billed later.
- **Partial use counts.** If you disconnect during an answer, the tokens generated so far are recorded. If a background job fails, the tokens it used are recorded.
- **Retention.** By default, usage records and the ledger are kept with no time limit. Retention is a host setting.
- **No payments.** This version of local-server has no billing, and these terms involve no payment.

## 5. Pausing and quiet hours

- The host can **pause sharing** at any time, and can set **quiet hours** during which sharing pauses automatically.
- While sharing is paused, new interactive requests are refused with `host_paused`. Requests that are already running finish. Background jobs wait in the queue and run when sharing resumes, unless their deadline passes first. Your host status shows *paused*.

## 6. Suspension, ending and deletion

- Access keys can expire and the host can revoke them. Invite links work once and can expire or be revoked. A grant can expire.
- The host can **suspend** your account and later make it active again.
- The host can **delete** your account. Deletion revokes your access keys and erases your personal details, such as your name and email. The account record itself stays without them, so usage records, the ledger, receipts and the activity log keep pointing to a valid account. Deletion cannot be undone.
- You can stop using the host at any time, and you can ask the host to delete your account.

## 7. Changes to these terms

Each version of these terms has its own id (see [How versions are named](#how-versions-are-named)). Your consent record stores the id of the version you accepted and when you accepted it. A changed text is always published under a new id.
