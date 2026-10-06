import { randomBytes } from "node:crypto";
import { CoreHost } from "./core-host";
import { configurePiRuntime } from "./pi-process";
import { CORE_IPC, type CoreMethod } from "../shared/agent-core";
import { cpSync, existsSync, mkdirSync, renameSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, isAbsolute, join } from "node:path";
import { app, BrowserWindow, dialog, type IpcMainEvent, type IpcMainInvokeEvent, ipcMain, Menu, nativeTheme, session, shell } from "electron";
import { description } from "../../package.json";
import type { AtpHead } from "../shared/atp";
import type { AuthMethod } from "../shared/auth";
import type { BoardOp } from "../shared/board";
import type { BrowserCommand, BrowserLayout } from "../shared/browser";
import type { ViewportRequest } from "../shared/viewport";
import type { GithubFilter, GithubKind } from "../shared/github";
import type { ComputerOp } from "../shared/computer";
import type { LamentOp } from "../shared/laments";
import { type HostEventBatch, IPC, type OpenSessionRequest, type Page } from "../shared/ipc";
import type { ExtensionUiResponse, RpcCommand } from "../shared/protocol";
import { emptySettings, type Feature, type Settings, type SettingsOp } from "../shared/settings";
import { Atp, librarianPath } from "./atp";
import { BrowserAgent, browserRoute } from "./browser/agent";
import { BrowserManager, PARTITION } from "./browser/manager";
import { attachContextMenu } from "./context-menu";
import { APP_ORIGIN, registerAppScheme, serveRenderer } from "./app-protocol";
import { serveVisual } from "./visual-protocol";
import { VISUAL_SCHEME, visualFrameToKill } from "./visual-frame";
import { describePaths, IMAGE_EXTENSIONS } from "./attachments";
import { BoardStore } from "./board";
import { CardImages } from "./card-images";
import { AgentBridge } from "./bridge";
import { listFiles } from "./files";
import { Github, GithubStore } from "./github";
import { kanbanRoute } from "./kanban";
import { ComputerAgent, computerRoute } from "./computer/agent";
import { PortableComputerService } from "./computer/portable";
import { ComputerService, defaultDeps, HELPER_APP } from "./computer/service";
import { ComputerStore } from "./computer/store";
import { LamentStore, lamentRoute } from "./laments";
import { PiAuth } from "./pi-auth";
import { readCompactionSettings, readPiSettings, writePiSettings } from "./pi-settings";
import { onDisk } from "./resources";
import { SettingsStore } from "./settings";
import { cardWorktree, configureWorktrees } from "./worktree";
import { debugRpc, log, logToFile } from "./log";
import { SessionHost } from "./session-host";
import { listSessions, sessionsDir } from "./session-index";
import { loadShellEnv } from "./shell-env";
import { Updater } from "./updater";

// The app's name (menus, About, profile and log folders) is package.json's productName.
// Test instances (scripts/cdp.mjs) get their own profile and logs so they never share a browser profile or history
// with the svolo you are working in.
if (process.env.SVOLO_USER_DATA) {
  app.setPath("userData", process.env.SVOLO_USER_DATA);
  app.setAppLogsPath(join(process.env.SVOLO_USER_DATA, "logs"));
} else {
  adoptProfile("pi studio", "pi-studio-browser");
}
// The browser pane's cookies would otherwise be encrypted with a key in the login keychain ("svolo Safe Storage"),
// Stable signing is required for production. A mock keychain is available ONLY for explicitly opted-in isolated tests.
if (process.platform === "darwin" && process.env.SVOLO_TEST_MOCK_KEYCHAIN === "1") app.commandLine.appendSwitch("use-mock-keychain");
const logFile = join(app.getPath("logs"), "main.log");
mkdirSync(app.getPath("logs"), { recursive: true });
logToFile(logFile);

const devUrl = process.env.ELECTRON_RENDERER_URL;
// Launched from a terminal (bin/svolo.mjs, `pi --svolo`), new chats start where you launched it and the
// environment is your shell's. From Finder or the Dock the cwd is / and the environment is launchd's.
const fromTerminal = Boolean(process.env.SVOLO_CWD);
const launchCwd = process.env.SVOLO_CWD || (process.cwd() === "/" ? homedir() : process.cwd());

/**
 * First launch after a rename: copy the profile of the app's previous name (browser cookies and history) into
 * this one. Copied, not moved, so a still-running old build keeps working; Chromium's singleton lock files are
 * left behind, or this instance would think the old one owns the new profile. The profile folder itself already
 * exists by now (Chromium puts Crashpad there before any app code runs), so "Local State" marks a used profile.
 */
function adoptProfile(oldName: string, oldPartition: string): void {
  const profile = app.getPath("userData");
  const old = join(dirname(profile), oldName);
  if (existsSync(join(profile, "Local State")) || !existsSync(join(old, "Local State"))) return;
  cpSync(old, profile, { recursive: true, filter: (path) => !/^(Singleton|DevToolsActivePort|Crashpad)/.test(basename(path)) });
  const partitions = join(profile, "Partitions");
  if (existsSync(join(partitions, oldPartition))) renameSync(join(partitions, oldPartition), join(partitions, PARTITION.replace(/^persist:/, "")));
  console.log(`copied the ${oldName} profile to ${profile}`);
}

let window: BrowserWindow | undefined;
let browser: BrowserManager | undefined;
let agent: BrowserAgent | undefined;
let updater: Updater | undefined;
const bridge = new AgentBridge();
const core = new CoreHost(() => browser, () => agent, (sid,path,params) => bridge.invokeNative(sid,path,params));
configurePiRuntime(core);
configureWorktrees(core);
const send = (channel: string, ...args: unknown[]) => {
  if (window && !window.isDestroyed()) window.webContents.send(channel, ...args);
};
const settings = new SettingsStore(join(app.getPath("userData"), "settings.json"), (next) => {
  send(IPC.settingsChanged, next);
  applySettings(next);
});
const host = new SessionHost((batch: HostEventBatch) => send(IPC.events, batch), bridge, join(app.getPath("userData"), "atp-sessions"), async () => ({
  ...(await settings.get()).features,
  computer: (await computerPolicy.get()).enabled,
  visuals: (await settings.get()).visuals,
}));
const board = new BoardStore(join(app.getPath("userData"), "board.json"), (next) => send(IPC.boardChanged, next), core);
const cardImages = new CardImages(join(app.getPath("userData"), "card-images"));
// Retain the pi request/result contract but serialize it through the Go control gate.
bridge.route("/browser", (handle,body) => core.extensionAction(handle,body));
const computerPolicy = new ComputerStore(join(app.getPath("userData"), "computer-use.json"), (next) => send(IPC.computerChanged, next));
const laments = new LamentStore(join(app.getPath("userData"), "laments.json"), (next) => send(IPC.lamentsChanged, next), core);
bridge.route("/kanban", settings.gate("kanban", kanbanRoute(board, (handle) => host.identify(handle))));
// The helper starts on first use only: the Computer Use page asking for permissions, or a tool.
const computerHelper = process.platform === "darwin" ? new ComputerService(
  defaultDeps(app.isPackaged ? join(process.resourcesPath, "computer-use", HELPER_APP) : join(app.getAppPath(), "build", "computer-use", HELPER_APP), app.getPath("userData")),
) : new PortableComputerService(core);
const computerAgent = new ComputerAgent(
  computerHelper,
  computerPolicy,
  {
    choose: (handle, title, options) => host.requestChoice(handle, title, options),
    chatName: (handle) => host.chatName(handle),
    abort: (handle) => host.command(handle, { type: "abort" }),
  },
  { ownNames: [app.getName(), app.getName().replace(/\.dev$/i, "")] },
);
bridge.route("/computer", computerRoute(() => computerAgent));
host.onRunEnd((handle) => void computerAgent.release(handle));
host.onExit((handle) => browser?.closeWindowsOf(handle));
bridge.route("/lament", settings.gate("laments", lamentRoute(laments, (handle) => host.identify(handle))));
const githubSettings = new GithubStore(join(app.getPath("userData"), "github.json"));
const github = new Github(githubSettings,undefined,undefined,undefined,core);
const atp = new Atp(
  (plans) => send(IPC.atpPlans, plans),
  (held) => send(IPC.atpHeld, held),
  undefined, core,
);
bridge.route("/atp", settings.gate("atp", atp.route()));
const auth = new PiAuth({ script: onDisk("resources", "pi-auth.mts") });

function createWindow(): void {
  window = new BrowserWindow({
    width: 1320,
    height: 880,
    minWidth: 760,
    minHeight: 520,
    show: false,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 18, y: 18 },
    backgroundColor: nativeTheme.shouldUseDarkColors ? "#161618" : "#f1f1f0",
    webPreferences: {
      preload: join(import.meta.dirname, "../preload/index.cjs"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      spellcheck: false,
      additionalArguments: [`--studio-home=${homedir()}`, `--studio-launch-cwd=${launchCwd}`, `--svolo-build=${__SVOLO_BUILD__}`, `--svolo-version=${app.getVersion()}`],
    },
  });
  // SVOLO_BACKGROUND=1 (test instances): show without taking focus, so keystrokes meant for the
  // svolo you are working in never land in a test window.
  window.once("ready-to-show", () => {
    log.info("svolo", `window ready ${Math.round(process.uptime() * 1000)} ms after launch`);
    if (process.env.SVOLO_BACKGROUND === "1") window?.showInactive();
    else window?.show();
  });
  // The terminal is the log: surface renderer warnings, errors and crashes there too.
  window.webContents.on("console-message", (details) => {
    if (details.level === "warning" || details.level === "error") {
      log[details.level === "error" ? "error" : "warn"]("renderer", `${details.message}  (${details.sourceId.split("/").at(-1)}:${details.lineNumber})`);
    }
  });
  window.on("focus", () => send(IPC.windowFocus, true));
  window.on("blur", () => send(IPC.windowFocus, false));
  window.webContents.on("render-process-gone", (_event, details) => log.error("renderer", `gone: ${details.reason}`));

  // Model output is untrusted: links never navigate the app window.
  window.webContents.setWindowOpenHandler(({ url }) => {
    openExternal(url);
    return { action: "deny" };
  });
  window.webContents.on("will-navigate", (event, url) => {
    if (url !== window?.webContents.getURL()) {
      event.preventDefault();
      openExternal(url);
    }
  });

  // Inline visual frames (sandboxed iframes) may only ever load their own scheme; any navigation inside them is refused.
  window.webContents.on("will-frame-navigate", (event) => {
    if (!event.isMainFrame && !event.url.startsWith(`${VISUAL_SCHEME}://`)) event.preventDefault();
  });

  browser = new BrowserManager(window, {
    state: (state) => { send(IPC.browserState, state); core.watchTabs(); },
    reveal: () => send(IPC.browserReveal),
    annotation: (annotation) => send(IPC.browserAnnotation, annotation),
  });
  agent = new BrowserAgent(browser);
  attachContextMenu(window.webContents, {
    page: false,
    openTab: (url) => {
      browser?.createTab(url);
      send(IPC.browserReveal);
    },
  });

  void window.loadURL(devUrl ?? `${APP_ORIGIN}/index.html`);
}

function openExternal(url: string): void {
  if (/^(https?|mailto):/i.test(url)) void shell.openExternal(url);
}

/** IPC is only accepted from the app window's own page (Electron security checklist #17). */
function trusted(event: IpcMainEvent | IpcMainInvokeEvent): boolean {
  const url = event.senderFrame?.url ?? "";
  return event.sender === window?.webContents && (url.startsWith(`${APP_ORIGIN}/`) || (devUrl !== undefined && url.startsWith(devUrl)));
}

function handle<A extends unknown[]>(channel: string, listener: (...args: A) => unknown): void {
  ipcMain.handle(channel, (event, ...args) => {
    if (!trusted(event)) throw new Error(`untrusted sender for ${channel}`);
    return listener(...(args as A));
  });
}

function on<A extends unknown[]>(channel: string, listener: (...args: A) => void): void {
  ipcMain.on(channel, (event, ...args) => {
    if (trusted(event)) listener(...(args as A));
    else log.warn("svolo", `dropped ${channel} from an untrusted sender`);
  });
}

function registerIpc(shellEnv: Promise<void>): void {
  handle(CORE_IPC.request, (path:string,method:CoreMethod,body?:unknown) => core.request(path,method,body));
  handle(CORE_IPC.status, () => core.status());
  handle(CORE_IPC.showBrowser, (sid:string) => core.showBrowser(sid));
  handle(CORE_IPC.saveArtifact, (remote:string,sid:string,id:string,name:string) => core.saveArtifact(remote,sid,id,name));
  // pi, rg and session listing depend on the login-shell environment (PATH, PI_CODING_AGENT_DIR, API keys).
  handle(IPC.listSessions, async () => (await shellEnv, listSessions()));
  handle(IPC.openSession, async (request: OpenSessionRequest) => {
    await shellEnv;
    const result=await host.open(request);
    void core.registerRuntimeSession(request.handle,request.cwd).catch(error=>log.warn("core",String(error)));
    return result;
  });
  handle(IPC.closeSession, (handle: string) => host.close(handle));
  handle(IPC.command, (handle: string, command: RpcCommand) => host.command(handle, command));
  on(IPC.respondUi, (handle: string, response: ExtensionUiResponse) => host.respondUi(handle, response));
  handle(IPC.listFiles, async (cwd: string) => (await shellEnv, listFiles(cwd)));
  handle(IPC.pickFolder, async () => {
    const result = await dialog.showOpenDialog({ properties: ["openDirectory", "createDirectory"] });
    return result.canceled ? null : (result.filePaths[0] ?? null);
  });
  on(IPC.openExternal, (url: string) => openExternal(url));
  on(IPC.visualKill, (frameId: string) => {
    const frames = window?.webContents.mainFrame.framesInSubtree ?? [];
    const pid = visualFrameToKill(frames, frameId, window?.webContents.getOSProcessId() ?? 0);
    if (pid !== undefined) process.kill(pid, "SIGKILL");
  });
  handle(IPC.compactionSettings, async () => (await shellEnv, readCompactionSettings()));
  handle(IPC.windowFocused, () => window?.isFocused() ?? false);
  handle(IPC.describePaths, (paths: string[]) => describePaths(Array.isArray(paths) ? paths : []));
  handle(IPC.pickAttachments, async (kind: "photos" | "files") => {
    const options: Electron.OpenDialogOptions =
      kind === "photos"
        ? { title: "Add photos", properties: ["openFile", "multiSelections"], filters: [{ name: "Images", extensions: IMAGE_EXTENSIONS }] }
        : { title: "Attach files and folders", properties: ["openFile", "openDirectory", "multiSelections"] };
    const result = window ? await dialog.showOpenDialog(window, options) : await dialog.showOpenDialog(options);
    return result.canceled ? [] : describePaths(result.filePaths);
  });

  on(IPC.browserLayout, (layout: BrowserLayout) => browser?.setLayout(layout));
  on(IPC.browserNewTab, (url?: string) => browser?.createTab(url));
  on(IPC.browserCloseTab, (id: string) => browser?.closeTab(id));
  on(IPC.browserActivate, (id: string) => {
    browser?.activate(id);
    browser?.focusWindow(id);
  });
  on(IPC.browserNavigate, (id: string, input: string) => browser?.navigate(id, input));
  on(IPC.browserCommand, (id: string, command: BrowserCommand) => browser?.command(id, command));
  on(IPC.browserAnnotate, (enabled: boolean) => browser?.setAnnotating(enabled));
  on(IPC.browserInspect, (id: string) => browser?.inspect(id));
  handle(IPC.browserViewport, async (id: string, request: unknown) => {
    if (typeof id !== "string" || !browser) throw new Error("Invalid browser tab");
    if (request !== null && (typeof request !== "object" || Array.isArray(request))) throw new Error("Invalid viewport request");
    // Whatever the renderer sends, the user is the source.
    return (await browser.setViewport(id, request ? { ...(request as ViewportRequest), source: "user" } : undefined)) ?? null;
  });
  handle(IPC.browserPopOut, async (id: string) => {
    if (typeof id !== "string" || !browser) throw new Error("Invalid browser tab");
    await browser.popOut(id);
  });
  handle(IPC.browserReturn, async (id: string) => {
    if (typeof id !== "string" || !browser) throw new Error("Invalid browser tab");
    await browser.returnToPane(id);
  });
  handle(IPC.browserHistory, () => browser?.getHistory() ?? []);
  handle(IPC.browserGetState, () => browser?.snapshot());

  handle(IPC.boardGet, () => board.get());
  handle(IPC.computerGet, () => computerPolicy.get());
  handle(IPC.computerApply, (op: ComputerOp) => computerPolicy.apply(op));
  handle(IPC.computerPermissions, () => computerHelper.call("permissions", {}));
  handle(IPC.computerRequest, async (pane?: "accessibility" | "screen_recording") => {
    await computerHelper.call("request_permissions", {});
    const permissions = await computerHelper.call("permissions", {});
    // macOS 26 does not prompt for Screen Recording from the background helper ("does not allow prompting") and does
    // not list it in the pane until it is added, so open the pane and show the app to add with + or drag in.
    if (pane === "screen_recording" && !permissions.screenRecording) {
      await computerHelper.call("open_settings", { pane });
      shell.showItemInFolder(computerHelper.installedApp);
    }
    return permissions;
  });
  handle(IPC.computerOpenSettings, async (pane: "accessibility" | "screen_recording") => {
    if (pane !== "accessibility" && pane !== "screen_recording") throw new Error("Unknown settings pane");
    await computerHelper.call("open_settings", { pane });
  });
  handle(IPC.settingsGet, () => settings.get());
  handle(IPC.settingsApply, (op: SettingsOp) => settings.apply(op));
  // PI_CODING_AGENT_DIR can come from the login shell.
  handle(IPC.piSettingsGet, async () => (await shellEnv, readPiSettings()));
  handle(IPC.piSettingsApply, async (patch: unknown) => (await shellEnv, writePiSettings(patch)));
  handle(IPC.piSettingsReveal, async () => {
    await shellEnv;
    shell.showItemInFolder((await readPiSettings()).path);
  });
  // pi's logins (/login), with the pi found on the login shell's PATH.
  handle(IPC.authList, async () => (await shellEnv, auth.list()));
  handle(IPC.authLogin, async (provider: string, method: AuthMethod) => {
    if (typeof provider !== "string" || (method !== "oauth" && method !== "api_key")) throw new Error("Unknown login");
    await shellEnv;
    return auth.signIn(provider, method, (update) => {
      // As pi's /login does, open the sign-in page in the browser (Claude Code opens its own).
      if (update.kind === "event" && update.event.type === "auth_url" && !update.event.opened) openExternal(update.event.url);
      send(IPC.authUpdate, update);
    });
  });
  on(IPC.authAnswer, (n: number, value: string) => auth.answer(Number(n), String(value)));
  on(IPC.authCancel, () => auth.cancel());
  handle(IPC.authLogout, async (provider: string) => {
    await shellEnv;
    await auth.signOut(String(provider));
  });
  handle(IPC.lamentsGet, () => laments.get());
  handle(IPC.lamentsApply, (op: LamentOp) => laments.apply(op));
  handle(IPC.boardApply, async (op: BoardOp) => {
    const next = await board.apply(op);
    if (op.type === "remove") void cardImages.remove(op.id).catch((error: Error) => log.warn("board", `could not delete the images of card ${op.id}: ${error.message}`));
    return next;
  });
  handle(IPC.boardSaveImage, async (card: string, image: { mimeType: string; data: string }) => {
    if (!(await board.get()).cards.some((other) => other.id === card)) throw new Error(`no card ${String(card)}`);
    return cardImages.save(card, image);
  });
  handle(IPC.cardWorktree, async (id: string) => {
    await shellEnv;
    const card = (await board.get()).cards.find((other) => other.id === id);
    if (!card) throw new Error(`no card ${String(id)}`);
    return cardWorktree(card.cwd, card);
  });
  handle(IPC.lamentWorktree, async (id: string) => {
    await shellEnv;
    const lament = (await laments.get()).laments.find((other) => other.id === id);
    if (!lament) throw new Error(`no lament ${String(id)}`);
    return cardWorktree(lament.cwd, { id: lament.id, title: `fix ${lament.title}` });
  });
  // gh runs with the login shell's PATH; a project is an absolute folder.
  const project = (cwd: unknown) => {
    if (typeof cwd !== "string" || !isAbsolute(cwd)) throw new Error("a project is an absolute path");
    return cwd;
  };
  handle(IPC.githubProject, async (cwd: string, refresh?: boolean) => (await shellEnv, github.project(project(cwd), refresh === true)));
  handle(IPC.githubChoose, async (cwd: string, login: string | null) => (await shellEnv, github.choose(project(cwd), typeof login === "string" ? login : null)));
  handle(IPC.githubList, async (cwd: string, kind: GithubKind, filter: GithubFilter) => {
    if ((kind !== "issue" && kind !== "pr") || (filter !== "open" && filter !== "closed")) throw new Error(`cannot list ${String(filter)} ${String(kind)}s`);
    await shellEnv;
    return github.list(project(cwd), kind, filter);
  });
  handle(IPC.githubLookup, async (cwd: string, input: string) => (await shellEnv, github.lookup(project(cwd), String(input).slice(0, 500))));
  // python3, rg and git come from the login shell's PATH.
  handle(IPC.atpWatch, async (cwd: string | null) => (await shellEnv, atp.watch(cwd === null ? null : project(cwd))));
  handle(IPC.atpRead, (plan: string) => atp.read(plan));
  handle(IPC.atpActivate, async (plan: string) => (await shellEnv, atp.activate(plan)));
  handle(IPC.atpClaim, async (plan: string, agent: string) => (await shellEnv, atp.claim(plan, String(agent))));
  handle(IPC.atpRelease, async (plan: string, node: string, agent: string, reason: string) => (await shellEnv, atp.release(plan, String(node), String(agent), String(reason))));
  handle(IPC.atpHead, async (cwd: string) => (await shellEnv, atp.head(project(cwd))));
  handle(IPC.atpCommit, async (cwd: string, node: string, title: string, before: AtpHead | null) => (await shellEnv, atp.commit(project(cwd), String(node), String(title), before)));
  handle(IPC.atpGetHeld, () => core.request("/v1/atp/held"));
  handle(IPC.atpSetHeld, (plan: string, held: boolean) => atp.setHeld(plan, held === true));
  handle(IPC.atpInfo, () => ({ librarian: librarianPath() }));
  handle(IPC.atpRuns,()=>core.request('/v1/atp/runs'));
  handle(IPC.atpStop,(id:string)=>core.request('/v1/atp/stop','POST',{id}));
  handle(IPC.atpStart,async(cwd:string,plan:string,model?:import('../shared/settings').TaskModel)=>{
    await shellEnv;cwd=project(cwd);const features={...(await settings.get()).features,computer:(await computerPolicy.get()).enabled,visuals:(await settings.get()).visuals};
    if(!features.atp)throw new Error('ATP is disabled in Settings');
    const sid=randomBytes(12).toString('hex');await core.workspaceSession(cwd,true,sid);await core.registerRuntimeSession(sid,cwd);
    const runtime=host.piArgs(sid,undefined,{role:'worker',plan},features);
    return core.request('/v1/atp/start','POST',{session:sid,workspace:cwd,plan,runtime:{...runtime,session:sid,cwd},commitPerNode:true,agentId:'svolo-w1',provider:model?.provider,model:model?.id,thinking:model?.thinking});
  });
  handle(IPC.relaunch, () => { app.relaunch(); app.quit(); });
  handle(IPC.updateGet, () => updater?.get() ?? { phase: "idle" });
  handle(IPC.updateDownload, () => { throw new Error("Signed update channel not configured for this fork; build and install a verified release manually."); });
}

/** svolo > Check for Updates…: show what GitHub has, also in a checkout (which updates with git, though). */
async function checkForUpdates(): Promise<void> {
  const box = (options: Electron.MessageBoxOptions) => (window ? dialog.showMessageBox(window, options) : dialog.showMessageBox(options));
  try {
    const state = await updater?.check();
    if (state && state.phase !== "idle") send(IPC.updateReveal);
    else await box({ message: `${app.getName()} is up to date`, detail: `${app.getVersion()} is the newest version.` });
  } catch (error) {
    await box({ type: "warning", message: "Could not check for updates", detail: (error as Error).message });
  }
}

/** The window follows its appearance setting; the menu shows only the pages of features that are on. */
function applySettings(next: Settings): void {
  nativeTheme.themeSource = next.theme;
  buildMenu(next.features);
}

function buildMenu(features: Record<Feature, boolean> = emptySettings().features): void {
  const page = (feature: Feature, label: string, accelerator: string, page: Page): Electron.MenuItemConstructorOptions[] =>
    features[feature] ? [{ label, accelerator, click: () => send(IPC.pageToggle, page) }] : [];
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      {
        // Not role "appMenu", whose own submenu would replace this one.
        label: app.name,
        submenu: [
          { role: "about" },
          { label: "Check for Updates…", click: () => void checkForUpdates() },
          { type: "separator" },
          { label: "Settings…", accelerator: "CmdOrCtrl+,", click: () => send(IPC.pageToggle, "settings") },
          { type: "separator" },
          { role: "services" },
          { type: "separator" },
          { role: "hide" },
          { role: "hideOthers" },
          { role: "unhide" },
          { type: "separator" },
          { role: "quit" },
        ],
      },
      { role: "editMenu" },
      {
        label: "View",
        submenu: [
          { label: "Toggle Sidebar", accelerator: "CmdOrCtrl+Shift+S", click: () => send(IPC.sidebarToggle) },
          { label: "Toggle Browser", accelerator: "CmdOrCtrl+B", click: () => send(IPC.browserToggle) },
          ...page("kanban", "Kanban", "CmdOrCtrl+Shift+K", "kanban"),
          ...page("laments", "Laments", "CmdOrCtrl+Shift+L", "laments"),
          ...page("github", "GitHub", "CmdOrCtrl+Shift+G", "github"),
          ...page("atp", "ATP", "CmdOrCtrl+Shift+A", "atp"),
          { label: "Computer Use", accelerator: "CmdOrCtrl+Shift+U", click: () => send(IPC.pageToggle, "settings", "computer") },
          { type: "separator" },
          { role: "reload" },
          { role: "toggleDevTools" },
          { type: "separator" },
          { role: "resetZoom" },
          { role: "zoomIn" },
          { role: "zoomOut" },
          { type: "separator" },
          { role: "togglefullscreen" },
        ],
      },
      { role: "windowMenu" },
      {
        role: "help",
        submenu: [
          { label: "Show Logs", click: () => void shell.openPath(logFile) },
          { type: "separator" },
          { label: "pi Documentation", click: () => openExternal("https://pi.dev") },
          { label: "Release qualification", click: () => { void shell.openPath(join(app.getAppPath(), "..", "docs", "RELEASE.md")); } },
        ],
      },
    ]),
  );
}

function init(): void {
  registerAppScheme();
  // Set before ready so Electron never builds its default menu (performance checklist).
  buildMenu();
  const shellEnv = app.isPackaged && !fromTerminal ? loadShellEnv() : Promise.resolve();

  // No publisher channel is configured. See docs/RELEASE.md before enabling updates.
  updater = undefined;
  // Keep the lifecycle hook for a future verified updater; no updater is active.
  app.on("will-quit", () => updater?.installOnQuit());

  let quitting = false;
  app.on("before-quit", (event) => {
    bridge.stop();
    auth.close();
    if (quitting) return;
    event.preventDefault();
    quitting = true;
    if (host.size) log.info("svolo", `stopping ${host.size} pi session(s)`);
    void Promise.allSettled([core.close(), host.closeAll(), board.flushed(), laments.flushed(), computerPolicy.flushed(), settings.flushed(), githubSettings.flushed(), computerAgent.releaseAll().finally(() => computerHelper.stop())]).finally(() => app.quit());
  });
  app.on("window-all-closed", () => app.quit());
  for (const signal of ["SIGINT", "SIGTERM"] as const) process.on(signal, () => app.quit());
  // A second launch on this profile (say `pi --svolo` in another project) opens a chat here instead.
  app.on("second-instance", (_event, _argv, _cwd, data) => {
    if (!window) return;
    if (window.isMinimized()) window.restore();
    window.show();
    app.focus({ steal: true });
    const cwd = (data as { cwd?: unknown } | null)?.cwd;
    if (typeof cwd === "string") send(IPC.openProject, cwd);
  });
  // No <webview> tags anywhere (security checklist #12); the browser pane uses WebContentsView.
  app.on("web-contents-created", (_event, contents) => contents.on("will-attach-webview", (event) => event.preventDefault()));

  void app.whenReady().then(async () => {
    log.info("svolo", `${app.getName()} ${app.getVersion()} build ${__SVOLO_BUILD__}  electron ${process.versions.electron}  sessions ${sessionsDir()}  log ${logFile}`);
    log.info("svolo", `launch cwd ${launchCwd}${debugRpc ? "  (RPC debug on)" : "  (SVOLO_DEBUG=1 logs RPC traffic)"}`);
    if (!app.isPackaged && process.platform === "darwin") app.dock?.setIcon(join(app.getAppPath(), "resources", "icon.png"));
    app.setAboutPanelOptions({
      applicationName: app.getName(),
      applicationVersion: app.getVersion(),
      credits: `${description}\n\nCopyright © 2026 0xfunboy.\nApplicable terms: LICENSE.md and legal/NOTICE.md.`,
    });
    // The window only ever asks for clipboard writes (copy buttons); the browser pane's partition has its own handler.
    session.defaultSession.setPermissionRequestHandler((_contents, permission, callback) => callback(permission === "clipboard-sanitized-write"));
    serveVisual();
    if (!devUrl) serveRenderer(join(import.meta.dirname, "../renderer"));
    registerIpc(shellEnv);
    // Before the window, so it opens in its appearance (and with its background color).
    applySettings(await settings.get());
    await bridge.start();
    createWindow();
    updater?.start();
  });
}

// One instance per profile: a second one hands its launch directory to the first and quits. Test instances use
// their own SVOLO_USER_DATA profile, so they run beside the app you work in.
if (app.requestSingleInstanceLock({ cwd: process.env.SVOLO_CWD })) init();
else {
  log.info("svolo", `already running with this profile${process.env.SVOLO_CWD ? `; opening a new chat in ${process.env.SVOLO_CWD} there` : ""}`);
  app.quit();
}
