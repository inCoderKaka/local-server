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
- **Pick the models** you allow, and for each person or app, how long their messages may be (the **context limit**) and how much they may use (**quotas**).
- **Choose when.** Pause sharing at any time with one switch, set **quiet hours**, or allow heavy background work only at night (**background-work hours**).

### Invite people and apps

- **People:** create an **invite** and send them the link. They sign up with a password or a passkey and get their own page.
- **Apps:** create an **access key**. You see it once; copy it into the app's settings.
- **Changing access:** you can suspend a consumer, revoke a key, or change what a consumer may use at any time.

### Requests for more

When a consumer needs more (a bigger model, a higher quota) they send a **resource request**. You get a notification, can discuss it in comments, and approve or reject it.

### See usage

The dashboard shows current use and use over time: per person, per app and per model, by minute, hour and day. Each answer local-server gives is recorded in a tamper-evident **usage ledger**, so the records can't be quietly changed later.

### Backups

Turn on backups to a folder or to a storage bucket you own (Cloudflare R2 or Google Cloud Storage). Backups are encrypted on your computer before they leave it. You choose what goes in: by default, background-job content and login sessions are left out, and you can keep or leave out any kind of data. Keep the **recovery code** you are shown; it is the only way to restore.

## For consumers

### Joining

- **First, reach the host:** the host adds you to their private network, by sharing their machine with you in Tailscale or inviting you as a Tailscale user. Install Tailscale and accept. If the host has turned on public mode, skip this.
- **As a person:** open the host's invite link, choose a password or passkey, and accept the terms. Your page shows your limits, your usage, your keys and your requests.
- **As an app:** paste the access key the host gave you into the app. Developers: see the [developer guide](developer-guide.md).

### What you can see

- Which models you may use, and your limits.
- How much of your quota is left.
- Whether the host is online, busy or paused.

### Asking for more

Send a resource request from your page. You are notified when the host answers.

### Receipts

Every answer comes with a signed **usage receipt**. Your app can keep them. Later, anyone can check a receipt against the host's ledger, and confirm that the host's ledger contains the call.

## Privacy, in plain words

- **Your messages are not saved.** local-server never writes prompts or answers to disk or logs. The only exception is background jobs: those are stored encrypted while they wait, and deleted when you fetch the result (or after a day).
- **The host's computer runs the AI.** Your messages are processed in its memory. Only use hosts you trust, as you would with any computer you send data to.
- **Your personal details are erased** if your account is deleted. The usage records stay, without your personal details, so the host's accounting stays correct.
- **Nothing is shared with anyone else** unless the host approves a specific, time-limited **sharing grant**. Even then, your messages are never part of it. If the host turns on platform sync, an encrypted copy of your account details (name and email) and usage is kept by the platform, which cannot read it without such a grant.

## Host setup

> Filled in when the installers ship. It will cover: install and upgrade commands; setting up Ollama and Tailscale; how apps hosted elsewhere (for example on Fly.io) reach you; public mode and its limits; keeping a headless machine logged in; why closing the lid stops sharing; why your `*.ts.net` name is publicly visible; why renaming the computer breaks consumers' passkeys; building from source; and checking a download's provenance.

## Questions

- **Does it slow my computer down?** Only within the share budget you set. Background work waits when you are busy, and you can pause at any time.
- **Can strangers reach my computer?** No. Only invited people and apps, over your private network. The optional public mode is off unless you turn it on, and turning it on forces extra protections.
- **What does it cost?** local-server is free and open source (Apache-2.0). Ollama and Tailscale's personal plan are free. Tailscale's free plan is for non-commercial use.
