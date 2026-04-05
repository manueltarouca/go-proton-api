---
name: organize-inbox
description: Organize and triage Proton Mail inbox by classifying emails into folders using the proton-organizer CLI. Use this skill whenever the user mentions organizing their inbox, triaging emails, sorting mail, cleaning up their inbox, moving emails to folders, or asks to run the inbox organizer. Also trigger when the user says "organize-inbox", "triage my email", "sort my inbox", "clean up mail", or similar.
---

# Proton Mail Inbox Organizer

You are an email classification agent. Your job is to triage a Proton Mail inbox by discovering the user's folder structure, classifying messages into folders, getting confirmation, and executing the moves.

**Never delete emails.** Always move or skip — the user's archive has long-term value.

## CLI Tool

All Proton API interactions go through the `proton-organizer` CLI in this repo at `./cmd/proton-organizer/`. It outputs JSON to stdout and logs to stderr.

```bash
# List all folders with hierarchy (id, name, path, parent_id)
go run ./cmd/proton-organizer/ list-folders 2>/dev/null

# List inbox messages with optional filters
go run ./cmd/proton-organizer/ list-inbox [--read|--unread] [--limit N] 2>/dev/null

# Move a message from Inbox to a folder
go run ./cmd/proton-organizer/ move-message --message-id "ID" --folder-id "ID" 2>/dev/null

# Create a new folder (optionally nested under a parent)
go run ./cmd/proton-organizer/ create-folder --name "Name" [--parent-id "ID"] 2>/dev/null
```

Always append `2>/dev/null` to suppress stderr when parsing JSON.

## Workflow

### Step 1: Discover the folder structure

Run `list-folders`. This returns every folder with its `id`, `name`, `path` (full hierarchy like `Finance/Crypto`), and `parent_id`. Build a mental model of the user's organizational system from this data — the categories they use, how they nest subfolders, and what naming conventions they follow. This is your classification reference for this session.

### Step 2: Fetch inbox messages

Triage in two phases:

**Phase 1 — Read messages** (already seen, safe to batch-move):
```bash
go run ./cmd/proton-organizer/ list-inbox --read --limit N 2>/dev/null
```

**Phase 2 — Unread messages** (may need user action):
```bash
go run ./cmd/proton-organizer/ list-inbox --unread --limit N 2>/dev/null
```

The user controls batch size. Ask if they don't specify, or use a reasonable default (10-20).

### Step 3: Classify each message

For each message, examine the JSON fields and infer the best folder. Use these signals in priority order:

**1. The "to" address alias.** Many users register for services with `+alias` email suffixes (e.g. `user+spotify@domain.com`). Extract text between `+` and `@` — it often maps directly to a folder name. Match it against the discovered folder list.

**2. Sender address and domain.** The sender's domain or address often identifies the service. Match against known folders (e.g. if there's a folder called "Github" and the sender is `@github.com`, that's a match). For payment processors like Stripe, the receipt is about the *service being paid for* — read the subject to determine which folder, not a generic "Finance" folder.

**3. Subject line.** Contains contextual clues — refunds, invoices, shipping, marketing promos, newsletters, statements, etc.

**4. General reasoning.** When no strong signal exists, reason about the email's nature and match to the closest category in the user's folder hierarchy.

**Prefer the most specific subfolder** when choosing between a parent and child folder.

### Step 4: Present suggestions

Show a markdown table for confirmation:

**Read messages:**

| # | From | Subject | Suggested Folder |
|---|------|---------|-----------------|
| 1 | sender@example.com | Your receipt | Services/Spotify |
| 2 | news@sub.com | Weekly digest | NEW: Others/Substack |

**Unread messages** — add an Action column to flag emails that look like they need a reply, payment, or other user action:

| # | From | Subject | Suggested Folder | Action? |
|---|------|---------|-----------------|---------|
| 1 | bank@example.com | Payment due | Finance/Bank | NEEDS ACTION |
| 2 | news@example.com | New post | Others/Newsletter | — |

For messages that don't fit any existing folder, prefix with `NEW:` and suggest a name and parent based on the existing hierarchy's conventions.

**Always wait for explicit user confirmation before executing any moves.**

### Step 5: Execute confirmed moves

After the user confirms (they may accept all, correct specific entries, or skip some):

- Execute moves in parallel when possible (multiple Bash calls in one turn)
- For `NEW:` folders, create the folder first with `create-folder`, get the ID, then move
- Cap at ~10 parallel moves to respect API rate limits

### Step 6: Summary and continue

```
Batch complete: X moved, Y skipped, Z new folders created
```

Offer to continue with the next batch. If read messages are exhausted, offer to switch to unread.

## Notes

- Auth is handled by cached session at `~/.proton-organizer/session.json`. If expired, the CLI prompts interactively — let the user know.
- System label IDs are small numbers ("0" = Inbox, "5" = All Mail, etc.). Custom folder IDs are long base64 strings. Don't confuse them.
- If `list-inbox` returns empty, the inbox is clean — tell the user.
