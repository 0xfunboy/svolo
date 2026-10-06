# Documents and reusable task profiles

The Linux web client can read JPG/JPEG, PNG, PDF, UTF-8 TXT and CSV documents and
reuse a reviewed procedure across chats. A profile is procedural memory, not model
training. It belongs to one account and survives provider switches and application
restarts. The desktop client does not yet expose this web workflow.

## First TIM contract

1. Open **Settings → Task profiles** and choose **Start with TIM contracts**.
   This creates a starter procedure; it does not establish a verified TIM portal
   integration. Other repeated jobs can use **New task profile**.
2. Edit the procedure. Add the authorized portal's exact HTTPS origin, for example
   `https://dealer.example`, without a path, query or credentials. Add any additional
   portal origins required by the observed workflow. Review and save the profile.
   With an empty origin list, the agent can read documents but cannot perform
   browser actions for that task.
3. Select the task above the chat composer. Sign into the portal yourself in the
   shared browser and handle any OTP or CAPTCHA. Portal login stays in your private
   browser profile, independently of procedural knowledge.
4. Use the attachment button to upload the quote, signed contract and customer
   documents. Check the files for this case. The eye button previews extracted text;
   download the original to check uncertain OCR. Ask Svolo to inspect the documents,
   identify missing or inconsistent data, and assist with the actual portal form.
5. Verify the first case together: field labels, document types, offer codes,
   validation rules and the portal receipt. The harness instructs the agent to use
   supplied values, avoid invented consents/signatures, and obtain your approval
   before final submission. File uploads require an explicit core approval, showing
   their destination, even when browser autonomy is enabled.
6. Ask the agent to remember verified steps, or use **Learn this task** in the chat
   header. A learning proposal appears in the chat. Review and edit the complete
   proposed procedure, remove customer information, then explicitly save it.

For the next customer, create a new chat, select the same profile and attach the
new case documents. Changing the profile/revision or the selected document set
starts a fresh model context on the next run, avoiding continuation with a previous
case's model history. The visible chat history is preserved. New upload batches
replace the checked selection; after a page reload, files require selection again.

## What is remembered

Keep menu paths, source-field to portal-field mappings, required attachment types,
validation rules, known exceptions and verified checks. The agent can propose
changes; it cannot silently approve or overwrite trusted knowledge. Review creates
a new revision. **Versions** shows the latest 20 revisions; restoring one also
requires review and creates a new revision. Concurrent or stale edits are rejected.

Do not store customer records, identity scans, names, addresses, consent choices,
passwords, cookies or tokens in a task profile. Pattern checks reject common email,
tax-code, IBAN, telephone and credential patterns, including credential-bearing
URLs. These checks cannot identify every name or address, so human review is
required. A profile can be edited or deleted independently of its chats.

## Reading and limits

| Item | Limit or behavior |
| --- | --- |
| Original file | 20 MiB, up to 8 selected files per run |
| Stored originals | 100 files and 200 MiB per account; expire after 7 days |
| PDF | First 20 pages; additional pages are marked as a partial reading |
| Extracted text | Up to 200,000 characters per file; bounded excerpts enter the prompt |
| Full text access | Private document tool reads the selected document in bounded chunks |
| JPG/PNG | Local English/Italian OCR; images also go to a vision-capable provider within image limits |
| PDF scans | Local OCR for pages with insufficient text or raster content; bounded page images for vision |
| TXT/CSV | Strict UTF-8, treated as data; CSV formulas and document instructions are not executed |

OCR is fallible. Verify identity numbers, dates, prices and payment fields against
the originals. Partial or unavailable readings are visible, not reported as complete.

## Privacy and boundaries

Profiles, their revisions, pending proposals, original attachments and extracted
text are stored as AES-256-GCM encrypted fields bound to the account in the app's
own database. They are not committed to Git or imported from external OAuth paths.
Only checked files are made available to the chosen AI provider for a run. That
provider receives their bounded text and supported images; its own retention policy
applies. Changing providers does not delete the app's task memory or credentials.

PDF/OCR decoders run in a separate Bubblewrap namespace without network or host
home access, with time, memory and output limits. Temporary decoder files are private
and removed after decoding; startup also clears interrupted decoder remnants and
stale upload copies. Selected originals are materialized privately inside
the user's workspace for browser upload; copies are cleared after the run by polling
or the periodic sweep. Uploads are restricted to the exact selected files and
authorized HTTPS origins; destination changes while waiting for approval are
rechecked before execution. Task runs expose browser and private document/memory
tools, excluding arbitrary scripts, workspace tools and external MCP tools.

The internal memory MCP uses the user's broker credential over loopback; ordinary
web sessions and public-host requests cannot call it. Authorization derives the
account from the session, and document tools require an active run's selected files.

The SQLite file is not wholly encrypted. Core conversation history, browser profiles
and ordinary workspace files remain private filesystem data and can contain customer
information. Expiring or deleting an attachment does not erase text already retained
in chat history or by the provider. Delete the chat to remove its local history and
attachment records. Protect full backups and keep the master key separately, as
described in [web security](WEB_SECURITY.md). This feature does not change release
qualification or establish that a particular TIM workflow has already been tested.

See [web API](WEB_API.md), [assistant harness](AGENT_HARNESS.md) and
[deployment dependencies](WEB_DEPLOYMENT.md).
