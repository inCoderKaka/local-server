# local-server user guide

> **Status: built for Phase 1, not yet released.** Everything in this guide that is not marked *planned* is built. Features marked *planned (phase 2)* are not built and have no date. Words in **bold** are defined in the [glossary](glossary.md).
>
> - **Release:** the first release, the pre-release `v0.1.0-rc.1`, is not published yet. The install commands in [Host setup](#host-setup) work once it is; until then, [build from source](#build-from-source).
> - **Tested on a real machine:** one Mac (Apple silicon) with a real Ollama: chat (streamed and not) and embeddings with an access key, with usage recorded, including over that computer's own Tailscale address. The admin page and the consumer page (sign-up from an invite, limits, usage, requests and approval) were exercised in an automated browser test against a real local-server with a stand-in engine, and have not yet been clicked through by a person on a second computer. Automated tests also run on macOS, Linux and Windows.
> - **Not yet run on a real machine:** a second computer reaching the host over Tailscale; the Linux and Windows builds, installers and background services; the install script on a fresh macOS account; backups to a real storage bucket; Ollama versions other than 0.31.1 (the oldest local-server accepts is 0.12.6).
> - **Planned (phase 2):** notifications (in the app, by webhook and on the desktop), a desktop app, public mode through Tailscale Funnel (consumers without Tailscale), an embedded Tailscale, bundles, signed export and import, platform sync and **sharing grants**, a host-issued login reset link, a way to reset a forgotten admin password, and the integration with the interactive learning portal (engine options such as `top_k`, capability discovery and a TypeScript client library).

## What is local-server?

Some computers can run AI models well, like a Mac mini or a gaming PC. local-server lets the owner of such a computer share its AI with people and apps they trust.

- The **host** is the person who owns the computer. They install local-server, choose how much of the computer to share, and invite people.
- A **consumer** is a person or an app the host invited. They use the host's AI as if it were a cloud AI service: any app or library that speaks the OpenAI API works after you change its address and key.

Everything travels over an encrypted private network (Tailscale). Nobody can use the host's computer without an invitation, and the host can see and limit who uses what. Prompts and answers are not logged or stored by default (see [Privacy](#privacy-in-plain-words)).

## For hosts

### What you need

- A Mac, Windows or Linux computer with enough memory for the models you want to share.
- **Ollama** 0.12.6 or newer, the free app that runs the models. The installer finds it, or offers to install it for you; it never installs anything without asking.
- **Tailscale**, the free app that creates the private network (your **tailnet**). The installer offers it too. Without it, local-server still works, but only on your own computer.

### Install

You run one command in a terminal. It downloads local-server, checks it against the checksum published with the release, and can start it in the background whenever you log in. The exact commands, upgrade, uninstall and building from source are in [Host setup](#host-setup).

### First start

1. Start local-server once in a terminal with `local-server serve` (on macOS and Linux the installer offers to do this for you in its window). This first run creates the data folder and prints a one-time **recovery code**. Write it down and keep it safe. Then press Ctrl-C.
2. Start the background service: `local-server service start` (the installer offers to register and start it; if you built from source, register it first with `local-server service install`). If you prefer to keep the terminal open, leave `local-server serve` running instead.
3. Open **`http://127.0.0.1:8485`** in a browser on that computer. The first time, the page asks you to create the one **admin** login: a login name and a password of at least 12 characters (a host setting, `auth.password.min_length`). The first admin can only be created on this computer; it cannot be created from another one.
4. Run `local-server doctor` to check the data folder, Ollama, Tailscale and the ports. It names the fix for anything that is wrong.

The admin page is only reachable on your own computer, unless you turn on [remote admin](#reach-the-admin-page-from-another-computer). A login lasts 12 hours by default (`admin.session_ttl`).

### The admin page

Every page shows the sharing state (Sharing, Paused or Quiet hours), a count of pending resource requests, and a strip with what the computer is using: **Host total**, **Share-budget slice** (what local-server's sharing uses: local-server, Ollama and the loaded models), **Other apps** (everything else) and **Unattributed** (use the operating system will not assign), each as memory and CPU, plus your **memory budget**. A value that cannot be read shows `n/a`. The strip, the sharing state and the count refresh every 30 seconds (`admin.ui_poll_interval`).

| Page | What it is for |
|---|---|
| Dashboard | Host use now and over time, the pause switch, **quiet hours**, and the state of the engine (Ollama) and of Tailscale |
| Consumers | All consumers; create an app consumer; open one to suspend, reactivate or delete it and to issue or revoke its **access keys** |
| Invites | Create and revoke **invite** links for people |
| Grant (from a consumer) | Set a consumer's **grant**: models, limits, **quotas**, rules; see its history |
| Models | Choose which models are shared, which stay loaded |
| Rules | Rules for the whole host about calls by their request types |
| Usage | Calls and tokens per consumer, over a range you choose |
| Audit log | Who did what on the host, newest first |
| Settings | Every host-owned setting, with its default |
| Requests | **Resource requests** from consumers: comment, approve, reject |

### Choose what to share

Your **share budget** is everything you set aside for consumers together. You set it in four places:

- **Memory and concurrency (Settings).** `scheduler.memory_budget_bytes` is the memory set aside for loaded models and running requests together; `0` (the default) means no limit, and a request that can never fit it is refused with `over_budget`. `scheduler.total_concurrency` (default 4) is the most requests run at once for everyone, but it never goes above `scheduler.ollama_num_parallel` (default 1), the value Ollama itself runs with. To run more at once, raise **both** to the same number: set `OLLAMA_NUM_PARALLEL` where Ollama starts, restart Ollama, then set `scheduler.ollama_num_parallel` to match. Until you do, requests run one at a time.
- **Models page.** The page lists the models Ollama has. A model is **Shared** (a grant may include it), **Preload**ed (loaded at start and never unloaded for idleness or to make room) or neither. A model local-server sees for the first time is **not** shared until you tick it (`admin.model_default_allowed` is off by default). A model's keep-alive field is stored, but this version still unloads every idle model after the host default (`scheduler.keep_alive`, 5 minutes).
- **Grants.** For each consumer, the models and limits it may use (below).
- **Pause and quiet hours** (below).

Model names must match exactly as Ollama lists them (Ollama adds `:latest` when no tag is given).

### Pause, quiet hours and background hours

- **Pause sharing:** the switch on the Dashboard. New work stops at once; calls already running finish, queued background jobs wait. Consumers get a `host_paused` error. It works during quiet hours too.
- **Quiet hours:** on the Dashboard, a daily time window in a time zone you choose during which the host stops serving, as if paused, and lets the computer sleep.
- **Background-work hours:** on the Rules page (or a grant's own rules), create a rule that matches calls with a request type such as `background` and tick "Allow these calls only in set hours". Calls outside the hours are refused with `outside_time_window` and told when the next window opens. A rule can also refuse calls, give them a priority, or only narrow their limits; the first matching rule by position decides the priority.

### Invite people and apps

- **People:** on Invites, create an **invite** (an optional note and, if you wish, an expiry; the default is 24 hours, `invites.ttl`). The page shows the link **once**; send it to the person. Before you send links, set the host setting `invites.link_base_url` to your consumer page address (see [Set up Tailscale](#set-up-tailscale)), or the link comes out relative and you must put the address in front yourself. A link works once. You can revoke an unused one.
- **Apps:** on Consumers, create a **key-only consumer** (kind Application, or Individual) and open it.
- **A new consumer has no access yet.** Neither an invited person nor a key-only consumer can call any model until you set a grant (next section) or approve a resource request. The consumer's page says so and offers a request.
- **Access keys:** on a consumer's page, issue an **access key**: an optional name, an optional expiry (the default is never) and an optional tailnet node to **bind** it to (its address, node ID or machine name), so the key works only from that device. The secret (`ls_<prefix>_<secret>`) is shown **once**; copy it into the app's settings. The host keeps only a prefix and a hash, so if it is lost, revoke it and issue another.
- **Changing access:** you can **suspend** a consumer (its keys and sign-ins are refused until you reactivate it), revoke a key, or change the grant at any time. **Delete** erases the consumer's personal details and revokes its keys for good; its usage records stay.
- The consumer page shows the **billing mode** (`none` by default). It is set through the admin API (see the [developer guide](developer-guide.md)). A consumer with `prepaid` or `postpaid` may choose the quota windows of its own keys; for everyone else you choose them. No money, balance or price is handled yet.

### Grants and limits

A **grant** is one consumer's part of your share budget. On a consumer's Grant page you set:

- **Models:** each shared model the consumer may use, with an optional **context limit** (the longest prompt plus answer, in tokens) and the most output tokens one answer may have. Empty means the host's own setting applies.
- **Quotas:** any number of entries, each capping tokens or requests in a repeating window, for example 100000 tokens every 24h. A consumer can have several (an hourly and a daily cap, say), at most one tokens entry and one requests entry per window. Windows are between 1 minute and 30 days by default (`limits.quota_window_min` and `limits.quota_window_max`) and start on fixed boundaries counted in UTC.
- **Requests at once:** how many of the consumer's calls run at the same time (the default for a new grant is `scheduler.default_grant_concurrency`, 1).
- **If a prompt is too long:** refuse the call (the default, `context_too_long`) or cut the oldest messages to fit.
- **Allowed request types:** any, or only a named list, such as `background`.
- **Expiry:** when the consumer's access ends (the default is never).

Each save stores a **new grant version** that applies from the consumer's next call; earlier versions stay in the history, and usage records keep the version they ran under. A key can narrow its consumer's limits but never widen them (narrowing a key is available through the admin API, not on the admin page). Calls are also rate-limited per access key (60 a minute by default, `limits.rate_capacity` and `limits.rate_window`), and a source that repeatedly presents bad keys is locked out for a while (`limits.lockout_*`).

### Requests for more

When a consumer needs more (a model, a bigger context, a higher quota, or access at all) they send a **resource request**. The admin page shows a count of pending requests on every page. On the Requests page you read the request and its comments, add comments, and either:

- **Approve:** the form starts from the consumer's current grant with what the request asks for applied; change any of it, add an optional comment, and approving stores a new grant version; or
- **Reject:** with an optional reason. A rejected request is not reopened; the consumer can send a new one.

A request also ends if the consumer withdraws it or if it expires (`requests.ttl`). Notifications of new requests are *planned (phase 2)*; the count updates when the page polls.

### See usage

- **Usage page:** choose all consumers or one, a bucket size (minute, hour or day) and a range. You see calls, prompt tokens, generated tokens and total tokens per consumer, a chart of calls and tokens over time, and the same data as a table. Buckets are UTC minutes, hours and days and are shown in your browser's time zone. By default the host keeps minute totals for 7 days, hour totals for 90 days and day totals forever; for an older range, choose a larger bucket. New calls appear after the next rollup pass (about a minute).
- **Dashboard:** host use (memory and CPU of the four parts) now and over the last stretch of time, per minute.
- **Audit log:** admin actions, newest first, filterable by action and dates.
- Each answer local-server gives is recorded in a tamper-evident **usage ledger**, so the records cannot be quietly changed later, and consumers receive a signed **usage receipt** for each call.

### Reach the admin page from another computer

By default the admin page opens only on the host computer. To open it from another computer on your tailnet:

1. In Settings, turn on `serve.tailnet_admin_enabled` and restart local-server.
2. Open `https://<name>.ts.net:8486` (the name `local-server doctor` shows) and log in with the same admin login.

The first admin can never be created this way, and the host's `/metrics` is not served there. A tailnet peer that guesses passwords is locked out like a local one.

If the admin password is typed wrongly too many times, the admin page stops accepting logins from your computer for a while. Every program on your computer counts as the same visitor, so another program can cause this too. Restarting local-server lifts it at once.

### Backups

Backups are off until you choose a destination. In Settings, group `backup`:

1. Set `backup.target` to `folder` (and `backup.folder.path`) or to `s3` (a storage bucket you own, such as Cloudflare R2 or Google Cloud Storage: `backup.s3.endpoint`, `backup.s3.bucket`, `backup.s3.region`, `backup.s3.prefix`). For a bucket, store its access key with `local-server backup-credentials` first; it is sealed with the host's master key and never kept in plain text.
2. Turn on `backup.enabled`. A backup is taken every 5 minutes by default (`backup.interval`) and the newest 100 are kept (`backup.retention_count`).

Backups are encrypted on your computer before they leave it. Folder backups and restore are tested; a backup to a real bucket has not yet been run against one.

- **What goes in:** by default, background-job content and login sessions are left out (**backup minimization**; one `backup.minimize.<table>` setting per table). You can leave most kinds of data out, with one limit: you can leave a kind out only if everything that depends on it is left out too. Otherwise local-server refuses the change and names the data involved. Three kinds cannot be left out, because a restore needs them: the host's keys (`hst_host` and `hst_key`) and the record of how the database is built (`ops_schema_migrations`).
- **Job content:** if background-job content was left out of a backup, jobs that were still waiting or running when it was taken come back as failed after a restore, so apps send them again.
- **One folder per host:** give each host its own backup folder or bucket folder: pruning and restore treat every backup file in it as that host's.
- **Restore:** stop the host, then run `local-server restore --from <folder>` or `local-server restore --bucket <name>` (with `--endpoint`, `--region`, `--prefix` as needed; `--snapshot <name>` picks one other than the newest). It asks for the **recovery code** (or reads `LS_RECOVERY_CODE`); for a bucket it reads the bucket access key from `LS_BACKUP_ACCESS_KEY_ID` and `LS_BACKUP_SECRET_ACCESS_KEY`. It keeps the replaced database beside the new one. Restoring an older backup takes the **usage ledger** back to the moment that backup was taken, and local-server warns you that receipts issued after that moment will no longer verify.
- **Recovery code:** keep the one you were shown; it is the only way to restore. `local-server recovery-code` shows it again (it needs a terminal and your admin login, and is written to the audit log).
- **Cut-off uploads:** if a backup to a bucket is cut off (for example, the computer loses power), the bucket can keep the pieces already sent, which your storage provider may charge for. Set a lifecycle rule on the bucket that removes unfinished uploads after a few days.

A backup does not contain the host's **master key**, the key that locks the host's signing keys and other secrets on your computer (it lives in the computer's keychain, or in a file named `master.key` in the data folder). The recovery code restores your data on the same computer, or on any computer where the master key survives. Copy `master.key` (or the keychain entry) somewhere safe and private, and back it up yourself. If you lose the computer and the master key, the restore cannot unlock the host's signing keys: the host cannot start on them until the master key is back. Restarting it on your restored data with new signing keys, which the people you shared with would have to trust again, is *planned (phase 2)*. When you restore, local-server tells you if the master key on that computer does not open the restored keys.

### Upgrade and uninstall

Run the install command again with `upgrade` or `uninstall`; the exact forms are in [Host setup](#host-setup). An upgrade never touches your data. Uninstall asks whether to delete the data folder too (the default is to keep it).

### Troubleshooting

Start with `local-server doctor`. The log is `local-server.log` in `~/Library/Logs/local-server` (macOS), `%LocalAppData%\local-server\logs` (Windows) or `~/.local/state/local-server/logs` (Linux); it never holds prompts or answers.

- **Consumers cannot connect.** Check the Tailnet card on the Dashboard. local-server reads Tailscale's state once, when it starts, so if Tailscale was not logged in or MagicDNS and HTTPS Certificates were not on at that moment, restart local-server after fixing it (`local-server service stop`, then `local-server service start`). If Tailscale is not ready, local-server serves only your own computer and says so in `doctor`.
- **A setting "applies on the next start".** Restart local-server the same way.
- **A model is not in a grant's list or a consumer gets `model_not_granted`.** The model must be pulled in Ollama, **Shared** on the Models page, and ticked in the consumer's grant, spelled exactly as Ollama lists it.
- **Calls run one at a time.** Raise `scheduler.ollama_num_parallel` and Ollama's `OLLAMA_NUM_PARALLEL` together (see [Choose what to share](#choose-what-to-share)).
- **`engine_unavailable`.** Ollama is not running or does not answer at `http://127.0.0.1:11434` (`engine.ollama_base_url`). local-server still starts and answers this error until Ollama is back. An Ollama older than 0.12.6 stops local-server from starting; upgrade it.
- **A consumer gets `rate_limit_exceeded`, `quota_exceeded` or `host_paused`.** Rate limit per key (above), a quota entry used up for its window (they can wait or send a request), or sharing is paused or in quiet hours.
- **Usage looks empty.** New calls show after the next rollup pass, and an older range needs a larger bucket size.
- **The computer slept.** See [Keep the computer available](#keep-the-computer-available).
- **After install, the service restarts over and over at login.** Run `local-server serve` once in a terminal (the first run) and then `local-server service start`.
- **I forgot the admin password.** A reset is *planned (phase 2)*; it is not built yet.
- **A consumer lost its password or passkey.** A host-issued reset link is *planned (phase 2)*. For now, delete the consumer and send a new invite. There is no list of a consumer's passkeys yet.

## For consumers

### Joining

- **First, reach the host:** the host adds you to their private network, by sharing their machine with you in Tailscale or inviting you as a Tailscale user. Install Tailscale on your computer, sign in and accept. A host-side public mode that skips this step is *planned (phase 2)*.
- **As a person:** open the host's invite link. It leads to the host's consumer page (an address like `https://<host-name>.ts.net:8485`). Enter your name and an optional email (only the host sees it), choose a login name and a password (the host sets its minimum length, 12 characters by default) or a **passkey**, and accept the Terms of Use and the Privacy and consent notice. Your account is created and you are signed in. The invite link works once and expires after the time the host chose.
- **As an app:** paste the access key the host gave you into the app, with the host's address. See [Call the host from an app](#call-the-host-from-an-app).
- **Access starts when the host gives it.** Right after you sign up you have no models. Send a resource request ("Ask for access"), or wait for the host to set your limits.

### Signing in

Open the consumer page and log in with your login name and password, or with a passkey (your device's screen lock, fingerprint, face or a security key). Repeated wrong passwords lock the login for a while. If the host renames its computer, its `*.ts.net` name changes and passkeys stop working; log in with your password and add a new passkey.

### Your page

| Page | What you see |
|---|---|
| Home | Who you are, your limits in one line, what you have used now, and how many requests are pending |
| My limits | The models you may use, each with its context limit and its most output tokens; your **quotas**; how many calls run at once; what happens to a too-long prompt; the request types you may send; when your access ends; the version of your **grant** and when it was set. A key can have lower limits of its own |
| My usage | How much of each quota window you have used, when it started and when the count starts again; and your calls, prompt tokens and generated tokens per model over a range, by minute, hour or day, as a chart and a table |
| My keys | Your **access keys** by their start (`ls_<prefix>_…`), status, expiry and the device a key is tied to. You cannot create or see a secret here: ask the host for a key (a consumer-side key route is *planned (phase 2)*) |
| Requests | Your **resource requests**, with their status, and a form to send one |
| Settings | Change your password (every other device is signed out), add a passkey, and the few settings a consumer owns, such as how long a finished background job's result stays fetchable |

### Asking for more

On Requests, describe what you need and why, and optionally what you ask for: models, a context limit, most output tokens, requests at once, tokens or requests per window, request types. The host reads it, can comment, and approves or rejects it. You can comment too, and you can withdraw an open request. If it is approved, your limits change at once and the request links to your new limits. An unanswered request expires after the time the host chose. Notifications are *planned (phase 2)*; reload the page to see answers.

### Call the host from an app

local-server speaks the OpenAI API. In any OpenAI library, change the base URL and the key. The host gives you the address (the host's `*.ts.net` name and port 8484 by default) and the access key:

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://<host-name>.ts.net:8484/v1",  # from the host
    api_key="ls_<prefix>_<secret>",                 # your access key
)
reply = client.chat.completions.create(
    model="llama3.2:3b",                            # a model your grant allows
    messages=[{"role": "user", "content": "Hello"}],
)
print(reply.choices[0].message.content)
```

Streaming (`stream=True`) and embeddings (`client.embeddings.create`) work the same way; `GET /v1/models` lists the models your grant allows. Some OpenAI fields are not supported and are rejected with an error that names the field instead of being ignored. Your app must run on a computer that is on the host's private network, and if the host tied your key to a device, on that device. Errors, retries, background jobs, quotas and every call in detail are in the [developer guide](developer-guide.md).

### Receipts

Every answer comes with a signed **usage receipt**: for a normal answer in the `LS-Receipt` response header, for a streamed one in a comment line after the last chunk. Your app can keep them. Later, anyone can check a receipt against the host's public keys (`/v1/keys`) and ask the host for a proof that the call is in its ledger (`/v1/usage/{id}/proof`), and keep the signed checkpoints to confirm the host only ever added to its ledger. local-server does not ship a verifier; the steps are in the [developer guide](developer-guide.md), in the section on verifying receipts.

## Privacy, in plain words

- **Your messages are not saved.** local-server does not log or store prompts or answers by default. The only exception is background jobs: those are stored encrypted while they wait, and deleted once you have fetched the whole result (or after a day). If a fetch is cut off midway, fetch again.
- **Signing up records your consent.** The host keeps which versions of the Terms of Use and the Privacy and consent notice you accepted, when, which invite you used, and the IP address and browser user agent of your sign-up request, as proof. The [privacy notice](legal/privacy-consent.md) lists this.
- **The host's computer runs the AI.** Your messages are processed in its memory. Only use hosts you trust, as you would with any computer you send data to.
- **Your personal details are erased** when the host deletes your account. You cannot delete it yourself on the consumer page; ask the host. The usage records stay, without your personal details, so the host's accounting stays correct. Your consent records, any saved sign-in sessions and your access keys are removed for good after a retention period the host sets (90 days by default, `consumers.deleted_retention`). What stays is an empty account entry with no personal details, which the host's accounting and audit trail refer to. Copies in backups the host took before then stay until those backups are removed under the host's backup settings.
- **Nothing is shared with anyone else.** The host's data stays on the host's computer and in the backups the host chooses. *Planned (phase 2):* if the host opts in, platform sync keeps an encrypted copy of the account details (name and email) and usage on the platform, which cannot read it without a specific, time-limited **sharing grant** the host approves. Even then, your messages are never part of it.

## Host setup

> The first release is the pre-release `v0.1.0-rc.1`. It is not published yet, so these commands do not work until it is. Only the macOS (Apple silicon) build has run on a real machine; the Linux and Windows builds and installers are tested in CI only.

### Install

**macOS and Linux**, in a terminal:

```sh
curl -fsSL https://github.com/inCoderKaka/local-server/releases/download/v0.1.0-rc.1/install.sh | sh -s -- --version v0.1.0-rc.1
```

**Windows**, in PowerShell:

```powershell
$env:LOCAL_SERVER_VERSION = 'v0.1.0-rc.1'
irm https://github.com/inCoderKaka/local-server/releases/download/v0.1.0-rc.1/install.ps1 | iex
```

Once `v0.1.0` is out, the version can be left out and `releases/latest/download/install.sh` (or `install.ps1`) installs the latest release.

The installer:

1. Downloads the program for your computer and checks it against the release's `SHA256SUMS`. If the check fails, it stops and changes nothing.
2. Puts it in `~/.local/bin` (Windows: `%LOCALAPPDATA%\Programs\local-server`) and offers to add that folder to your PATH.
3. Offers to install Ollama and Tailscale if they are missing. Every question defaults to no, and nothing outside the install folder changes without your yes.
4. First run: on macOS and Linux it offers to run `local-server serve` once in its window, which creates the data folder and prints a one-time **recovery code** (write it down; it is the only way to restore a backup); press Ctrl-C when you have it. On Windows, run `local-server serve` once yourself the same way.
5. Offers to start local-server in the background whenever you log in. On macOS and Linux, `--system-service` starts it at boot instead, for the whole machine (needs `sudo`).

With no terminal (for example in a script) every question counts as no and the installer prints the manual steps instead.

Run `local-server doctor` at any time. It checks the data folder, Ollama, Tailscale and the listeners, and names the fix for anything that is wrong. `local-server --version` prints the version.

**Upgrade:** run the same command with `upgrade` (macOS and Linux: `| sh -s -- upgrade`; Windows: set `$env:LOCAL_SERVER_ACTION = 'upgrade'` first). **Uninstall:** the same with `uninstall`. It removes the program, its background service and its PATH line, then asks whether to delete the data folder too (the default is to keep it). An upgrade never touches your data.

**Where things are:** the data folder is `~/Library/Application Support/local-server` (macOS), `~/.config/local-server` (Linux) or `%AppData%\local-server` (Windows), unless you set `--data-dir` or `LS_DATA_DIR`. It is the only thing configured outside the app; everything else is a setting in the admin page.

**The background service:** `local-server service install|uninstall|start|stop|status` manages it. The default runs when you log in (macOS launchd, Linux systemd user service, Windows logon task); `--mode system` runs it at boot for the whole machine. A system-mode service runs as root unless you give `--run-as <account>`, and it then runs the program file your own account owns, so any process of yours could replace it. User mode is the default.

### Set up Ollama

local-server talks to Ollama on your own computer (`http://127.0.0.1:11434` by default). Pull the models you want to share, for example:

```sh
ollama pull llama3.2:3b
ollama pull nomic-embed-text
```

Model names must match exactly as Ollama lists them (Ollama adds `:latest` when no tag is given). Then share them on the admin page's Models page.

### Set up Tailscale

1. Install the Tailscale app on this computer and log in.
2. In the Tailscale admin console, on the DNS page, turn on **MagicDNS** and **HTTPS Certificates**.
3. Restart local-server. It now also listens on your private network, with HTTPS, at your computer's `*.ts.net` name (`local-server doctor` shows it):
   - `https://<name>.ts.net:8484`: the API that apps call with an access key.
   - `https://<name>.ts.net:8485`: the consumer page, where invited people sign up and log in. Set the host setting `invites.link_base_url` to this address, so invite links are complete.
   - `https://<name>.ts.net:8486`: the admin page from another computer, only when you turn on `serve.tailnet_admin_enabled` and restart.

On this computer, the admin page is at `http://127.0.0.1:8485` and the API at `http://127.0.0.1:8484`. If Tailscale is missing or logged out, local-server still starts, serves only this computer, and says so in its log, in `doctor` and on the admin page. The consumer page is reachable only through Tailscale.

### Let people and apps reach you

- **People with their own computer:** share this machine with them in Tailscale, or invite them into your tailnet as users. Tailscale's free personal plan allows 6 users and is for non-commercial use.
- **Apps hosted elsewhere** (for example on Fly.io): the app's server must join your tailnet as a node that a user owns. A tagged node from another tailnet cannot reach a shared machine. A web page in a browser cannot call the API directly (cross-site calls are refused), so call it from a server.
- **Public mode** (*planned (phase 2)*): an opt-in mode through Tailscale Funnel, so consumers need no Tailscale. It is not built yet. Funnel is a beta feature, works only on ports 443, 8443 and 10000 and on `*.ts.net` names, and Tailscale's free plan is for non-commercial use.

### Keep the computer available

- **Sleep:** while sharing is on (not paused and not in quiet hours), local-server keeps the computer from sleeping when idle. It cannot stop sleep when you close a laptop's lid, so a closed laptop stops sharing.
- **Headless computers:** the default background service starts when you log in. On a computer with no screen, turn on automatic login, or install with `--system-service` so it starts at boot.

### Things to know

- **Your `*.ts.net` name is public.** HTTPS certificates are listed in public Certificate Transparency logs, so anyone can see the name (not reach the computer).
- **Renaming the computer** changes its `*.ts.net` name. Consumers' passkeys are tied to that name and stop working; they log in with their password and add a new passkey.

### Build from source

You need Go and, for the web pages, Node.js with npm.

```sh
git clone https://github.com/inCoderKaka/local-server && cd local-server
make build
```

This builds both web pages and writes `bin/local-server`. A plain `CGO_ENABLED=0 go build ./cmd/local-server` also works without npm, but the admin and consumer pages are then one-page placeholders. Put the built `local-server` in a folder on your PATH, then continue with [First start](#first-start).

### Check a download's provenance

Every release binary and `SHA256SUMS` carry a signed build-provenance attestation from GitHub Actions. With the GitHub CLI:

```sh
gh attestation verify local-server_darwin_arm64 --repo inCoderKaka/local-server
```

The installer checks the checksum; the attestation also proves which workflow built the file. The release pipeline that makes them has not yet run on a real release tag.

## Questions

- **Does it slow my computer down?** Only within the share budget you set. Background work waits when you are busy, and you can pause at any time.
- **Can strangers reach my computer?** No. Only invited people and apps, over your private network. The optional public mode is *planned (phase 2)*, off unless you turn it on, and turning it on will force extra protections.
- **What does it cost?** local-server is free and open source (Apache-2.0). Ollama and Tailscale's personal plan are free. Tailscale's free plan is for non-commercial use.
- **Can I see what people ask the AI?** No. local-server does not store prompts or answers. You see who used how much, never what.
- **Can a consumer use a model I have not shared?** No. A consumer can only use models its grant lists, and a grant can only list shared models.
- **Can I sell access?** Not in this version. No billing, prices or balances exist, and Tailscale's free plan is non-commercial.
- **Which apps work?** Anything that speaks the OpenAI API for chat and embeddings, after you change its address and key. Fields that local-server does not support are refused with an error, not ignored.
