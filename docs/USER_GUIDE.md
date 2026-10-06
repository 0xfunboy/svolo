# Working with Svolo

## Navigation and Control

The desktop offers an interactive browser and an activity view. The core console
shows the browser owned by the session through images of the page. The remote
view does not open a second page at the same URL, but it does not include all native
surfaces of a remote desktop.

Use **Take control / Stop** before intervening. Inputs in the viewer take human
control; **Return to agent** allows a new observation and resumption.
Automatic control of all inputs in the native browser window still requires full
qualification. Stopping the agent does not cancel a submission already executed on a site.

In the web workspace, selecting another tab only changes your view. Resolution
changes also leave the agent running and keep its operative tab selected. The
selector briefly disables while the page is resized; wait for the new frame before
clicking on the page. **Waiting for model** means the browser is waiting for the
selected AI provider's next response. A delayed capture retains the last valid
image and retries automatically; it does not imply that the browser has closed.

When the AI provider is temporarily unavailable, Svolo shows its retry progress.
If those attempts fail, use **Continue task** on the latest failure to resume with
the same provider, model, documents and task profile. Existing actions are checked
against the current page; completed tools are not repeated by the retry mechanism.
Your unsent draft remains in the composer. Login or API-key errors require fixing
the provider connection in Settings.

## Agent Activity

Select host, session, and provider, write a desired outcome, and initially keep
**Ask before changes**. An attached image is transmitted to the selected
model: do not upload information that that service cannot receive. Do not include
API keys or the core admin token in the prompt.

Call results and approval requests are separated from the text response.
Verify the outcome of important operations on the page or in the artifact,
not only through the model's concluding sentence.

## Projects and Workflows

Kanban organizes cards and tasks. Reports gather issues, repetitions, and
resolutions. ATP represents dependencies and node status; starting a plan authorizes
execution in the selected workspace, not an indiscriminate remote publication.
Workers receive a node and dependency reports. Interrupted nodes are not
considered completed.

Git and GitHub use the selected repository and account. Keep personal changes
separate; a dedicated worktree reduces interference but does not replace a review.

## Files and Artifacts

Transfers are tied to host and session. The destination path is relative
to the workspace. A resumable transfer has an ID and an offset; the final checksum
verifies the bytes, not the semantic content of the document. Do not overwrite an
existing file without explicitly indicating the version required by the API.

Saved screenshots, logged downloads, and recordings appear as artifacts.
An MP4 recording requires ffmpeg. Do not record sensitive screens without necessity.

## Application Control

Computer Use is disabled initially. Each application requires separate
authorization; foreground input has an additional enablement. Availability and semantics
depend on the operating system, permissions, and graphics server. Verify the
capabilities returned by the backend before committing a task to this feature.

## Appearance

Light/dark/system themes and background rotation remain selectable. All
distributed backgrounds are variations of the approved Svolo landscape, without gesture images or mascots. The **None** option disables the backdrop.

## Documents and repeated tasks in the web client

Attach JPG, PNG, PDF, UTF-8 TXT or CSV above the chat composer. Check only the
current case's files; preview extracted text and verify important OCR values.
In **Settings → Task profiles**, create your own profile with a goal, instructions
and optionally verified knowledge. Add authorized sites if browser actions are
needed. Search or duplicate profiles to manage different tasks, then select the
appropriate profile in chat. The agent proposes learning for review; saving updates
only that profile’s knowledge and preserves its goal and instructions. See [documents and task profiles](TASK_PROFILES.md) for the supervised first
case, retention and privacy boundaries.
