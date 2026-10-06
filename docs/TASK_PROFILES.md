# Documents and reusable task profiles

The Linux web client can read JPG/JPEG, PNG, PDF, UTF-8 TXT and CSV documents and
reuse a reviewed procedure across chats. A profile is procedural memory, not model
training. It belongs to one account and survives provider switches and application
restarts. The desktop client does not yet expose this web workflow.

## Create a profile for your task

Every account starts without task profiles. There are no built-in business tasks,
industry-specific templates or automatically assigned profiles. Users choose the
activities they want to repeat and may keep up to 1,000 private profiles.

1. Open **Settings → Task profiles → New task profile**. Choose a name and goal,
   then describe your instructions: workflow rules, required checks and desired
   output. **Verified knowledge** may start empty; it holds reviewed observations
   learned from actual executions. Profiles are independent of the selected model.
2. If the task needs browser actions, add each authorized site's exact HTTPS origin,
   for example `https://portal.example`, without paths, queries or credentials.
   Leave the list empty for document-only tasks. Sign in to websites yourself in
   the shared browser and handle any OTP or CAPTCHA. Browser login is separate
   from reusable memory. Review all fields before saving.
3. Select the profile above the chat composer. Creating, duplicating or editing a
   profile does not automatically assign it to a chat. Only the selected profile's
   goal, instructions and knowledge enter that run; other profiles stay separate.
4. Attach the current case's documents if needed. Check the files to use. Preview
   extracted text with the eye button and check uncertain OCR against the original.
   Ask Svolo to perform your task; it reports missing information and verifies the
   outcome with current tool results instead of inventing a workflow.
5. During the first execution, correct the assistant and verify its steps together.
   Sensitive external changes require approval. File uploads require an explicit
   core approval showing their destination, even with browser autonomy enabled.
6. Ask the agent to remember verified steps, or use **Learn this task**. Review and
   edit the complete knowledge proposal and remove personal case data. Saving
   creates a new revision of this profile's knowledge; it preserves your goal and
   instructions. A proposal never changes another profile or another account.

For the next case, open a new chat, select the appropriate profile and attach that
case's documents. Changing profile/revision or the selected document set starts a
fresh model context on the next run. Visible chat history is preserved. New upload
batches replace the checked selection; page reloads require selecting files again.

Search profiles by name, goal or instructions in Settings. **Duplicate profile**
opens an editable copy; review and save it as an independent profile with revision
1. Subsequent edits or learning in either profile do not update the other.

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
required. Goals (2,000 characters), instructions (10,000) and knowledge (40,000) are reviewed
and stored separately. Existing older revisions have empty goal and instruction
fields until the owner edits them. Version restore includes all profile fields,
requires review and creates a new revision. A profile can be edited or deleted
independently of its chats.

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
qualification or establish that a particular user workflow has already been tested.

See [web API](WEB_API.md), [assistant harness](AGENT_HARNESS.md) and
[deployment dependencies](WEB_DEPLOYMENT.md).
