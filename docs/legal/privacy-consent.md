# Privacy and consent

**Version:** privacy-2026-11-v1

## How versions are named

Every version of this notice has an id of the form `privacy-<YYYY-MM>-v<N>`. `YYYY-MM` is the year and month the text was written. `N` starts at 1 and goes up by one for each new text written in the same month. When you accept this notice, or a host approves a sharing grant, the consent record stores this id. Once a version is in force, its text never changes. Any change, however small, gets a new id.

## 1. Who this notice is for

- **Consumers.** You accept this notice, together with the [Terms of Use](terms-of-use.md), when you sign up through a host's invite link.
- **Hosts.** You give consent under this notice when you approve a sharing grant (section 6).

local-server runs on the **host's** own computer (the **host machine**), so your data is held by the host who invited you. Questions about your data go to that host. The **platform admin** runs optional platform services and can read a host's data only through a sharing grant that the host approved.

## 2. Your prompts and responses

- **Not logged or stored, by default.** Your prompts and the answers to them travel over an encrypted connection to the host machine. They are processed in memory, passed to the model runtime (the **engine**, Ollama by default) on the same machine, and sent back. local-server never writes them to disk or to its logs.
- **Background jobs are the only exception.**
  - While a background job waits or runs, its content is stored encrypted with the host's data key. It is deleted as soon as the job finishes, fails, is cancelled or expires.
  - The job's result is stored encrypted, and only you can fetch it. It is deleted when you fetch it, or after a time you can set (24 hours by default).
  - Job content and results are never sent to platform sync.
  - Backups leave them out by default. A host who chooses to include them keeps a copy in each backup until that backup is removed.
- **Notifications carry no content.** Webhook notifications about events, such as a finished job, do not include prompts or answers.
- **Content capture is off.** This version of local-server does not store prompts or answers for any other purpose. A capture feature is not built. If one is ever added, it could only run after you give a separate consent for exactly that purpose.
- **The host runs the machine.** These promises describe how local-server behaves. Your messages are processed on a computer the host controls, so only use hosts you trust.

## 3. What the host stores about you

Retention values below are defaults. The host can change the ones it owns, and you can change your own job-result time, notification preferences and webhook settings.

| Data | Why | Kept by default |
|---|---|---|
| Account details: name, email, and whether you are a person or an app | To identify your account | Until your account is deleted, then erased (section 7) |
| Login credentials: a one-way hash of your password, or your passkey's public key. Never the password itself | To let you log in | Until your account is deleted, then removed (section 7). Never sent to platform sync |
| Login sessions | To keep you logged in | Until your account is deleted, then removed 90 days later by default (section 7). Left out of backups by default |
| Access keys: only a short visible prefix and a hash. The full key is shown once and never stored. Optionally, the Tailscale device a key is tied to | To check your requests | Until your account is deleted, then removed 90 days later by default (section 7). Never sent to platform sync |
| Your grant and its versions | To apply your limits | No set period |
| Usage records and the usage ledger: account, access key, model, token counts, timing, grant version | Accounting and receipts you can check | No time limit |
| Usage totals per minute, hour and day | Usage charts | 7 days, 90 days, and no time limit, respectively |
| Resource requests and their comments | To handle your requests | No set period |
| In-app notifications, your notification preferences and your webhook addresses | To notify you | Notifications 90 days. Webhook deliveries 7 days after delivery |
| Activity log entries about you: keys issued or revoked, limits changed, requests decided | A record of admin actions | No time limit |
| Consent records (section 5), with the IP address and browser user agent from sign-up | Proof of what you accepted | Until your account is deleted, then removed 90 days later by default (section 7) |
| Technical logs, with sensitive values hidden and no prompts or answers | Running and fixing local-server | 14 days or 50 MB. Never sent to platform sync |

## 4. Where your data lives

- **On the host machine, by default.** local-server keeps its data in a database on the host machine. It is decentralised: no copy is kept anywhere else unless the host turns on backups or platform sync.
- **Backups, if the host turns them on.** Snapshots are encrypted on the host machine with the host's own key, then sent to a folder or a storage bucket the host owns, for example Cloudflare R2 or Google Cloud Storage. The storage provider holds only encrypted data.
- **Platform sync, only if the host opts in.** Each host decides for itself. If it is on, encrypted copies of three datasets (usage, activity log, and consumer directory, which holds account details) are sent to the platform's database (Supabase Postgres by default). The platform admin cannot read them without a sharing grant. Content, credentials, access-key secrets, technical logs and host machine readings are never sent.
- **Over the network,** traffic is encrypted (Tailscale by default). If the host turns on public access, the connection's encryption still ends on the host machine, not at a relay.
- **Secret keys,** such as the host's data and signing keys, never leave the host machine. The only exception is a dataset key wrapped for the grantee of an approved sharing grant.

## 5. Consent records

A consent record stores:
- who gave the consent,
- what it was for (accepting the terms or this notice at sign-up, or a sharing grant),
- the version id of the text accepted,
- when it was given,
- when it was withdrawn, if it was,
- evidence of the acceptance.

At sign-up, a record is made for the Terms of Use and one for this notice. To withdraw your acceptance, contact the host. The withdrawal time is added to your record. A host withdraws a sharing grant by revoking it (section 6).

At sign-up, the evidence holds the version ids of the Terms of Use and of this notice that you accepted, the time you accepted them, the invite you used, the **IP address** your request came from and your browser's **user agent** (the text a browser sends to say which browser and device it is, cut to a length the host sets, 256 characters by default). The IP address and the user agent are personal data. They are kept to show that you accepted these versions, and they are erased with the rest of your consent records when those are removed (section 7). The IP address is the one your request reaches the host machine from, so it may be a relay's address rather than your own.

## 6. Sharing grants

- **What a sharing grant is.** It is a host's time-limited permission for someone else (the grantee) to read one dataset. It states the purpose, the version of the text it was given under, and when it expires. Each dataset has its own encryption key, and the grant gives that key, wrapped so only the grantee can open it.
- **Who can receive one.** In this version of local-server, the only possible grantee is the platform admin, and only for hosts that have turned on platform sync.
- **Expiry and revocation.** The host can revoke a grant at any time. When a grant expires or is revoked, the dataset's key is changed, so the grantee cannot read data written afterwards. Data the grantee has already seen cannot be taken back.
- **Records.** Every grant, expiry and revocation is written to the activity log, notified, and recorded as a consent record naming this notice's version.
- **What this means for consumers.** If your host gives the platform admin a grant for the consumer directory or the usage dataset, the platform admin can read your account details or your usage records. Your prompts and answers are never part of any dataset.

## 7. When an account is deleted

- **Erasure.** Your personal details, such as your name and email, are erased and your access keys are revoked. The account record stays without those details, so usage records, the ledger, receipts and the activity log keep pointing to a valid account. Those records stay.
- **Removal after a retention period.** 90 days after the deletion by default (the host can change this period), your consent records, login credentials, sessions and access keys are removed for good. The account record stays without personal details, because usage records, receipts and the activity log refer to it.
- **Platform sync.** If it is on, the erasure is sent like any other change.
- **Backups.** Copies of your details in backups taken before the deletion or the removal stay until those backups are removed under the host's backup settings.
- **Sharing grants.** Data a grantee already saw under a sharing grant cannot be taken back.

## 8. Changes to this notice

Each version of this notice has its own id (see [How versions are named](#how-versions-are-named)). Your consent record stores the id of the version you accepted and when you accepted it. A changed text is always published under a new id.
