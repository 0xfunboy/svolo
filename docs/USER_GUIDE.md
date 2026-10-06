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
In **Settings → Task profiles**, create a task or the TIM contract starter, configure
authorized portal origins and review its reusable procedure. Select it in chat.
The agent proposes learning for review; saving creates a version recalled in future
chats. See [documents and task profiles](TASK_PROFILES.md) for the supervised first
case, retention and privacy boundaries.
