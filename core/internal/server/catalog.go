package server

import (
	"svolo.local/core/internal/agent"
	"svolo.local/core/internal/browser"
)

type definition struct {
	name, description string
	required          []string
	props             map[string]string
}

var definitions = []definition{
	{"project-board", "Project-scoped Kanban, implemented in Go. action=get or apply with a validated board operation. No access to another project.", []string{"action"}, map[string]string{"action": "string", "operation": "object"}},
	{"project-laments", "Project-scoped lament reports, fixes and resolution, implemented in Go. action=get or apply; bounded report retention and explicit reopening.", []string{"action"}, map[string]string{"action": "string", "operation": "object"}},
	{"computer-use", "Native application control, off by default. Requires a separate per-app human approval. Select list_apps/get_app_state/screenshot/click/drag/scroll/type_text/press_key/set_value/select_text/perform_secondary_action/paste/end. Foreground actions need an explicit setting and an already focused app.", []string{"action"}, map[string]string{"action": "string", "app": "string", "arguments": "object"}},
	{"pi-kanban", "Kanban operations for a connected desktop runtime session. Requires an existing pi session in Electron; native domain policy remains enforced.", []string{"arguments"}, map[string]string{"arguments": "object"}},
	{"pi-atp", "Pause or resume the workflow associated with a connected desktop runtime session.", []string{"arguments"}, map[string]string{"arguments": "object"}},
	{"pi-lament", "Issue-report operations for a connected desktop runtime session. Requires an existing pi session in Electron.", []string{"arguments"}, map[string]string{"arguments": "object"}},
	{"pi-computer", "Desktop runtime Computer Use adapter. Preserves pi approvals; macOS uses Swift, Windows/Linux use the Go-supervised native provider. Native capabilities and limitations are reported at runtime.", []string{"arguments"}, map[string]string{"arguments": "object"}},
	{"state", "Read active tab URL, title and loading state.", nil, nil},
	{"navigate", "Navigate the owned tab to http(s). Verify page state afterwards.", []string{"url"}, map[string]string{"url": "string"}},
	{"open-browser", "Open a managed browser tab in this session.", nil, map[string]string{"url": "string"}},
	{"close-browser", "Close only this session's managed browser.", nil, nil},
	{"open-tab", "Create another tab in the same browser session.", nil, map[string]string{"url": "string"}},
	{"tabs", "List only the browser targets owned by this session.", nil, nil},
	{"switch-tab", "Activate a tab by exact id, title or URL; ambiguous matches fail.", []string{"query"}, map[string]string{"query": "string"}},
	{"close-tab", "Close the current owned tab.", nil, nil},
	{"tab-history", "Go back or forward in tab history.", []string{"direction"}, map[string]string{"direction": "string"}},
	{"open-in-new-tab", "Open an element's link or image URL in another tab.", []string{"target"}, map[string]string{"target": "string"}},
	{"click", "Dispatch a click on a visible, unambiguous target. This is not a task-success assertion.", []string{"target"}, map[string]string{"target": "string"}},
	{"type-text", "Type text into the current focus or an explicit target.", []string{"text"}, map[string]string{"text": "string", "target": "string"}},
	{"fill", "Replace the text of a visible editable target.", []string{"target", "text"}, map[string]string{"target": "string", "text": "string"}},
	{"press-key", "Send a key, optionally Ctrl/Alt/Shift/Meta modifiers joined with +.", []string{"key"}, map[string]string{"key": "string"}},
	{"select", "Choose a native select option by exact value or label.", []string{"target", "value"}, map[string]string{"target": "string", "value": "string"}},
	{"check", "Check a checkbox and verify its state.", []string{"target"}, map[string]string{"target": "string"}},
	{"dialog", "Accept or dismiss a JavaScript dialog.", []string{"action"}, map[string]string{"action": "string", "text": "string"}},
	{"drag", "Drag between two explicit page element targets.", []string{"source", "destination"}, map[string]string{"source": "string", "destination": "string"}},
	{"wait", "Bounded wait for text, css, visible, url, title, hidden or image-ready.", []string{"condition", "value"}, map[string]string{"condition": "string", "value": "string", "seconds": "number"}},
	{"wait-for", "Wait for an explicit condition for at most 60 seconds.", []string{"condition", "value"}, map[string]string{"condition": "string", "value": "string", "seconds": "number"}},
	{"assert-url", "Verify the current URL; exact match by default.", []string{"expected"}, map[string]string{"expected": "string", "match": "string"}},
	{"assert-title", "Verify the document title.", []string{"expected"}, map[string]string{"expected": "string", "match": "string"}},
	{"assert-visible", "Verify element visibility and return geometry.", []string{"target"}, map[string]string{"target": "string"}},
	{"assert-text", "Verify a target's text, exact or contains.", []string{"target", "text"}, map[string]string{"target": "string", "text": "string", "match": "string"}},
	{"assert-image-ready", "Verify visible image completion and nonzero natural dimensions.", []string{"target"}, map[string]string{"target": "string"}},
	{"snapshot-interactive", "Read semantic interactive elements with snapshot-scoped refs. Refresh refs after takeover or page changes.", nil, map[string]string{"limit": "number", "offset": "number"}},
	{"find-interactive", "Search interactive elements by semantic label.", []string{"query"}, map[string]string{"query": "string", "limit": "number", "offset": "number"}},
	{"read-page", "Read title, URL, headings and bounded main text.", nil, nil},
	{"inspect-inputs", "Inspect visible form controls. Password values are redacted.", nil, nil},
	{"inspect-elements", "Inspect visible CSS matches, bounded to 500.", nil, map[string]string{"css": "string", "limit": "number"}},
	{"element-info", "Inspect state, attributes, HTML, text and geometry of one target.", []string{"target"}, map[string]string{"target": "string"}},
	{"scroll", "Scroll up/down/top/bottom or to a target.", nil, map[string]string{"destination": "string"}},
	{"query-selector", "Inspect CSS matching elements.", []string{"css"}, map[string]string{"css": "string", "limit": "number"}},
	{"get-element", "Read one target's current DOM data.", []string{"target"}, map[string]string{"target": "string", "mode": "string"}},
	{"accessibility-tree", "Read non-ignored accessibility nodes with a hard output bound.", nil, map[string]string{"max": "number"}},
	{"inspect-links", "Read visible links and resolved URLs.", nil, nil},
	{"inspect-images", "Read image sources and load state.", nil, nil},
	{"upload", "Set a file input. File must belong to the session's registered workspace on the browser host.", []string{"target", "file"}, map[string]string{"target": "string", "file": "string"}},
	{"highlight", "Draw a non-interactive box highlight over a target.", []string{"target"}, map[string]string{"target": "string", "label": "string"}},
	{"clear-highlight", "Remove the current owned highlight.", nil, nil},
	{"evaluate-js", "Evaluate JavaScript in the owned page. Requires explicit approval; never runs in application UI.", []string{"expression"}, map[string]string{"expression": "string"}},
	{"inject-js", "Run script now; optionally install it for future documents of this tab.", []string{"script"}, map[string]string{"script": "string", "persistent": "boolean"}},
	{"screenshot", "Capture page pixels, optionally save an immutable hashed artifact.", nil, map[string]string{"target": "string", "fullPage": "boolean", "save": "boolean"}},
	{"viewport", "Set viewport dimensions, device scale, mobile/touch and optional user agent.", nil, map[string]string{"width": "number", "height": "number", "dpr": "number", "mobile": "boolean", "touch": "boolean", "userAgent": "string", "reset": "boolean"}},
	{"inspect-network", "Start/stop/show bounded redacted network metadata capture. Does not capture request bodies or secrets.", nil, map[string]string{"action": "string"}},
	{"console", "Show the console events collected after network/console capture was started.", nil, nil},
	{"downloads", "Enable managed downloads for this session and list completed files.", nil, nil},
	{"wait-download", "Wait for a non-temporary stable download newer than a timestamp, then register a copy as an artifact.", []string{"afterMs"}, map[string]string{"afterMs": "number", "seconds": "number", "nameContains": "string"}},
	{"verify-artifact", "Verify the immutable artifact bytes, size and SHA-256. Does not invent semantic proof.", []string{"id"}, map[string]string{"id": "string"}},
	{"record-browser", "Start or stop bounded page recordings (sampled continuous or action steps); ffmpeg is required for MP4 encoding.", []string{"action"}, map[string]string{"action": "string", "mode": "string", "intervalMs": "number"}},
	{"profile-import", "Copy a CLOSED Chromium profile into a CLOSED managed session. Credentials may not be portable between OS users.", []string{"source"}, map[string]string{"source": "string", "force": "boolean"}},
	{"browser-task", "Run one browser operation in an isolated owned task session and close it unless persist is set.", []string{"url", "tool"}, map[string]string{"url": "string", "tool": "string", "arguments": "object", "persist": "boolean"}},
	{"call-routine", "Run or resume a bounded persisted JSON graph. Human suspension returns a resume id; uncertain actions never replay automatically.", nil, map[string]string{"name": "string", "resume": "string", "variables": "object"}},
	{"hitl", "Request human intervention in the integrated approval UI.", []string{"message"}, map[string]string{"message": "string"}},
	{"workspace-list", "List files in the explicitly selected workspace.", nil, map[string]string{"path": "string"}},
	{"workspace-read", "Read a regular file confined to the workspace; symlinks are refused.", []string{"path"}, map[string]string{"path": "string"}},
	{"workspace-write", "Atomically write a workspace file after approval.", []string{"path", "content"}, map[string]string{"path": "string", "content": "string"}},
	{"workspace-exec", "Run an explicit program and argument vector inside a trusted workspace. Requires allowExec and approval; not an OS sandbox.", []string{"program"}, map[string]string{"program": "string", "args": "array", "seconds": "number"}},
	{"browser-schema", "Discover the argument schema of a named browser operation.", []string{"name"}, map[string]string{"name": "string"}},
	{"browser-operation", "Execute a discovered browser primitive through the same control/policy path.", []string{"operation", "arguments"}, map[string]string{"operation": "string", "arguments": "object"}},
}

func builtins() []agent.Tool {
	out := []agent.Tool{}
	for _, d := range definitions {
		props := map[string]any{"tab": map[string]any{"type": "string", "description": "Optional owned target id"}}
		for k, kind := range d.props {
			p := map[string]any{"type": kind}
			if k == "target" {
				p["description"] = "Snapshot ref @document:index, css:selector, text:exact text or role:role|name"
			}
			if kind == "array" {
				p["items"] = map[string]any{"type": "string"}
			}
			props[k] = p
		}
		schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(d.required) > 0 {
			schema["required"] = d.required
		}
		read := browser.IsRead(d.name) || d.name == "workspace-read" || d.name == "workspace-list" || d.name == "browser-schema"
		out = append(out, agent.Tool{Name: d.name, Description: d.description, InputSchema: schema, ReadOnly: read})
	}
	return out
}
