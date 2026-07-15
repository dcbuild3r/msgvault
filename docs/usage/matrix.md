---
title: Matrix
description: Archive Matrix rooms with resumable sync, raw events, and media.
---

# Matrix

MsgVault can archive every room visible to a dedicated Matrix account through
the authenticated Client-Server API. Use a read-only archive identity where
room policy allows it; MsgVault does not send events.

## Add the archive identity

Create the Matrix account administratively, join it to the rooms to archive,
then provide a separately revocable access token:

```bash
MSGVAULT_MATRIX_TOKEN="..." msgvault add-matrix \
  --homeserver https://matrix.example.org \
  --user-id @archive:example.org
```

The command verifies `/account/whoami` before storing the credential. For
system services, pass a root-owned token file with `--token-file` or a systemd
credential rather than placing the secret in shell history.

## Synchronize

```bash
msgvault sync-matrix
```

The first run paginates backward through available room history. Later runs
resume from the durable `/sync` cursor. Events deduplicate by Matrix
`event_id`. Raw JSON is preserved as `matrix_json`; normalized bodies,
relations, edits, redaction tombstones, and participants remain searchable.

Media referenced by `mxc://` is downloaded through the authenticated Matrix
v1 media endpoint and stored in the content-addressed attachment store. Failed
or oversized downloads leave retryable marker rows without dropping the event.

## Daemon scheduling

```toml
[matrix]
homeserver = "https://matrix.example.org"
user_id = "@archive:example.org"
enabled = true
schedule = "* * * * *"
media = true
max_media_mb = 100
```

A one-minute cron job performs a roughly 50-second long poll, providing
near-continuous ingestion while keeping SQLite writes inside the daemon's
scheduled work path.

## Encryption limitation

MsgVault does not hold Matrix room encryption keys. Encrypted events are
preserved as raw events but their plaintext and encrypted media cannot be
archived. Rooms intended for complete archival must remain unencrypted.

Archival coverage is bounded by what the account can see and what the
homeserver and bridges retain. Never infer complete platform capture solely
from a successful Matrix sync.
