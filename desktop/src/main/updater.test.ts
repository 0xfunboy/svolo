import { describe, expect, it, vi } from "vitest";
const app = vi.hoisted(() => ({ relaunch: vi.fn(), quit: vi.fn() }));
vi.mock("electron", () => ({ app }));
import { Updater } from "./updater";
describe("source-build update policy", () => {
  it("reports an unconfigured channel without network activity", async () => {
    const onState = vi.fn(); const updater = new Updater("unused", onState);
    updater.start(); expect(updater.get()).toEqual({ phase: "idle" });
    expect(await updater.check()).toEqual({ phase: "idle" });
    expect(onState).toHaveBeenCalledWith({ phase: "idle" });
  });
  it("refuses downloads and never swaps an application on quit", async () => {
    const updater = new Updater("unused", vi.fn());
    await expect(updater.download()).rejects.toThrow("No verified Svolo release channel");
    expect(() => updater.installOnQuit()).not.toThrow();
  });
  it("can restart only the currently installed application", () => {
    new Updater("unused", vi.fn()).restart();
    expect(app.relaunch).toHaveBeenCalledOnce(); expect(app.quit).toHaveBeenCalledOnce();
  });
});
