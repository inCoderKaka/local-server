# local-server user guide

> **Status: planned.** local-server is being built. This guide describes how it will work when Phase 1 ships. Check the [roadmap](../ROADMAP.md) for what exists today. Words in **bold** are defined in the [glossary](glossary.md).

## What is local-server?

Some computers can run AI models well, like a Mac mini or a gaming PC. local-server lets the owner of such a computer share its AI with people and apps they trust.

- The **host** is the person who owns the computer. They install local-server, choose how much of the computer to share, and invite people.
- A **consumer** is a person or an app the host invited. They use the host's AI as if it were a cloud AI service.

Everything travels over an encrypted private network (Tailscale). Nobody can use the host's computer without an invitation, and the host can see and limit who uses what.

## For hosts

### What you need

- A Mac, Windows or Linux computer with enough memory for the models you want to share.
- **Ollama**, the free app that runs the models. The installer finds it, or offers to install it for you; it never installs anything without asking.
- **Tailscale**, the free app that creates the private network. The installer offers it too.

### Install

You run one command in a terminal. It downloads local-server, checks it against the checksum published with the release, and starts it in the background whenever you log in. The exact commands and the full host setup appear in [Host setup](#host-setup) when the first release is out.

### Choose what to share

Open the admin page in your browser. It only opens on your own computer, unless you turn on remote admin for your private network. Then you:
- **Set a share budget,** how much of your computer local-server may use for sharing: memory, how many requests run at once, and the models you allow. Every page shows three numbers: what your other apps use, what local-server uses, and your share budget.
- **Pick the models** you allow, and for each person or app, how long their messages may be (the **context limit**) and how much they may use (**quotas**: a limit on tokens or requests for a time window you choose, such as an hour or a day; a person or app can have several). A person or app marked **prepaid** or **postpaid** may choose the windows of its own keys; for everyone else you choose them. No money is handled yet.
- **Choose when.** Pause sharing at any time with one switch, set **quiet hours**, or allow heavy background work only at night (**background-work hours**).

If the admin password is typed wrongly too many times, the admin page stops accepting logins from your computer for a while. Every program on your computer counts as the same visitor, so another program can cause this too. Restarting local-server lifts it at once.

### Invite people and apps

- **People:** create an **invite** and send them the link. They sign up with a password or a passkey and get their own page.
- **Apps:** create an **access key**. You see it once; copy it into the app's settings.
- **Changing access:** you can suspend a consumer, revoke a key, or change what a consumer may use at any time.

### Requests for more

When a consumer needs more (a bigger model, a higher quota) they send a **resource request**. You get a notification, can discuss it in comments, and approve or reject it.

### See usage

The dashboard shows current use and use over time: per person, per app and per model, by minute, hour and day. Each answer local-server gives is recorded in a tamper-evident **usage ledger**, so the records can't be quietly changed later.

### Backups

Turn on backups to a folder or to a storage bucket you own (Cloudflare R2 or Google Cloud Storage). Backups are encrypted on your computer before they leave it. You choose what goes in: by default, background-job content and login sessions are left out, and you can keep or leave out most kinds of data, with one limit: you can leave a kind of data out only if everything that depends on it is left out too. If you try otherwise, local-server refuses the change and names the data involved. Three kinds cannot be left out, because a restore needs them: the host's keys (`hst_host` and `hst_key`) and the record of how the database is built (`ops_schema_migrations`). If background-job content was left out of a backup, jobs that were still waiting or running when it was taken come back as failed after a restore, so apps send them again. Restoring an older backup takes the **usage ledger** back to the moment that backup was taken. When you restore, local-server warns you that receipts issued after that moment will no longer verify. Keep the **recovery code** you are shown; it is the only way to restore.

A backup does not contain the host's **master key**, the key that locks the host's signing keys and other secrets on your computer (it lives in the computer's keychain, or in a file named `master.key` in the data folder). The recovery code restores your data on the same computer, or on any computer where the master key survives. Copy `master.key` (or the keychain entry) somewhere safe and private, and back it up yourself. If you lose the computer and the master key, the restore cannot unlock the host's signing keys: the host cannot start on them until the master key is back. Restarting it on your restored data with new signing keys, which the people you shared with would have to trust again, is planned and not built yet. When you restore, local-server tells you if the master key on that computer does not open the restored keys.

## For consumers

### Joining

- **First, reach the host:** the host adds you to their private network, by sharing their machine with you in Tailscale or inviting you as a Tailscale user. Install Tailscale and accept. If the host has turned on public mode, skip this.
- **As a person:** open the host's invite link, choose a password or passkey, and accept the terms. Your page shows your limits, your usage, your keys and your requests.
- **As an app:** paste the access key the host gave you into the app. Developers: see the [developer guide](developer-guide.md).

### What you can see

- Which models you may use, and your limits.
- How much of your quota is left, for each time window.
- Whether the host is online, busy or paused.

### Asking for more

Send a resource request from your page. You are notified when the host answers.

### Receipts

Every answer comes with a signed **usage receipt**. Your app can keep them. Later, anyone can check a receipt against the host's ledger, and confirm that the host's ledger contains the call.

## Privacy, in plain words

- **Your messages are not saved.** local-server never writes prompts or answers to disk or logs. The only exception is background jobs: those are stored encrypted while they wait, and deleted once you have fetched the whole result (or after a day). If a fetch is cut off midway, fetch again.
- **The host's computer runs the AI.** Your messages are processed in its memory. Only use hosts you trust, as you would with any computer you send data to.
- **Your personal details are erased** if your account is deleted. The usage records stay, without your personal details, so the host's accounting stays correct. Your consent records, any saved sign-in sessions and your access keys are removed for good after a retention period the host sets (90 days by default). What stays is an empty account entry with no personal details, which the host's accounting and audit trail refer to.
- **Nothing is shared with anyone else** unless the host approves a specific, time-limited **sharing grant**. Even then, your messages are never part of it. If the host turns on platform sync, an encrypted copy of your account details (name and email) and usage is kept by the platform, which cannot read it without such a grant.

## Host setup

> Filled in when the installers ship. It will cover: install and upgrade commands; setting up Ollama and Tailscale; how apps hosted elsewhere (for example on Fly.io) reach you; public mode and its limits; keeping a headless machine logged in; why closing the lid stops sharing; why your `*.ts.net` name is publicly visible; why renaming the computer breaks consumers' passkeys; building from source; and checking a download's provenance.

## Questions

- **Does it slow my computer down?** Only within the share budget you set. Background work waits when you are busy, and you can pause at any time.
- **Can strangers reach my computer?** No. Only invited people and apps, over your private network. The optional public mode is off unless you turn it on, and turning it on forces extra protections.
- **What does it cost?** local-server is free and open source (Apache-2.0). Ollama and Tailscale's personal plan are free. Tailscale's free plan is for non-commercial use.
