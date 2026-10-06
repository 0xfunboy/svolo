// A JSON file that main owns and both the window and agents (through the bridge) change: the Kanban board and the
// laments. Every change is an op applied by a pure function that checks it; the window gets the whole value after
// each one.
import { copyFile, readFile, rename, writeFile } from "node:fs/promises";
import { log } from "./log";
import { randomUUID } from "node:crypto";
import type { CoreHost } from "./core-host";

/** How a store's value starts, changes and is read back from disk. */
export interface StoreModel<T, Op> {
  /** For the log: "board". */
  name: string;
  /** For the log: "card", "lament". */
  item: string;
  empty(): T;
  /** Returns the same value when the op changes nothing; throws for an invalid op. */
  apply(value: T, op: Op, now: number): T;
  /** Throws when the file is not this kind of value; `dropped` counts malformed items it skipped. */
  parse(raw: unknown): { value: T; dropped: number };
}

export class JsonStore<T, Op> {
  private value: T;
  private readonly loaded: Promise<void>;
  private dirty = false;
  private writing?: Promise<void>;
  private migration?: Promise<void>;
  private revision = 0;
  private remoteTimer?: ReturnType<typeof setInterval>;
  private refreshing = false;

  constructor(
    private readonly file: string,
    private readonly model: StoreModel<T, Op>,
    private readonly changed: (value: T) => void,
    private readonly core?: CoreHost,
  ) {
    this.value = model.empty();
    this.loaded = core ? Promise.resolve() : this.load();
  }

  async get(): Promise<T> {
    if (this.core) { await this.remoteReady(); await this.refreshRemote(); return this.value; }
    await this.loaded;
    return this.value;
  }

  /** Apply a change, save and broadcast it. Throws the model's error for an invalid op. */
  async apply(op: Op): Promise<T> {
    if (this.core) {
      await this.get();
      const next = await this.core.request(`/v1/projects/${this.model.name}`, 'POST', {op, expectedRevision:this.revision, operationId:randomUUID()}) as {value:T;revision:number};
      this.adoptRemote(next); return this.value;
    }
    await this.loaded;
    const next = this.model.apply(this.value, op, Date.now());
    if (next === this.value) return next;
    this.value = next;
    this.changed(next);
    this.dirty = true;
    this.writing ??= this.write();
    return next;
  }

  /** Resolves once the latest value is on disk (before quitting). */
  async flushed(): Promise<void> {
    clearInterval(this.remoteTimer);
    if (this.core) { await this.migration; return; }
    while (this.writing) await this.writing;
  }

  private adoptRemote(next: {value:T; revision:number}): void {
    if (next.revision < this.revision) return; // a slow poll cannot roll back a completed mutation
    if (next.revision !== this.revision) { this.value=next.value; this.revision=next.revision; this.changed(this.value); }
    else this.value=next.value;
  }

  private async remoteReady(): Promise<void> {
    if (!this.core) return;
    this.migration ??= (async () => {
      let next = await this.core!.request(`/v1/projects/${this.model.name}`) as {value:T;revision:number};
      if (next.revision === 0) {
        let raw:string|undefined;
        try { raw=await readFile(this.file,'utf8'); } catch(e) { if ((e as NodeJS.ErrnoException).code!=='ENOENT') throw e; }
        if (raw !== undefined) {
          const parsed=this.model.parse(JSON.parse(raw));
          if(parsed.dropped) throw new Error(`Refusing partial ${this.model.name} migration: ${parsed.dropped} malformed records. Original file is unchanged.`);
          next=await this.core!.request(`/v1/projects/${this.model.name}`, 'POST', {op:{type:'import',value:parsed.value},expectedRevision:0,operationId:randomUUID()});
          // Original JSON remains byte-for-byte unchanged as a migration backup.
        }
      }
      this.adoptRemote(next);
      this.remoteTimer=setInterval(() => {
        if(this.refreshing) return; this.refreshing=true;
        void this.refreshRemote().catch(() => undefined).finally(() => {this.refreshing=false;});
      }, 2000);
      this.remoteTimer.unref();
    })().catch(error => {this.migration=undefined; throw error;});
    return this.migration;
  }

  private async refreshRemote(): Promise<void> {
    if(!this.core) return;
    this.adoptRemote(await this.core.request(`/v1/projects/${this.model.name}`));
  }

  /** One write at a time, always of the latest value; tmp + rename, so a crash never leaves half a file. */
  private async write(): Promise<void> {
    try {
      while (this.dirty) {
        this.dirty = false;
        await writeFile(`${this.file}.tmp`, JSON.stringify(this.value, null, 2));
        await rename(`${this.file}.tmp`, this.file);
      }
    } catch (error) {
      log.error(this.model.name, `could not save ${this.file}: ${(error as Error).message}`);
    } finally {
      this.writing = undefined;
    }
  }

  private async load(): Promise<void> {
    const { name, item } = this.model;
    let raw: string;
    try {
      raw = await readFile(this.file, "utf8");
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") log.error(name, `could not read ${this.file}: ${(error as Error).message}`);
      return;
    }
    // Never overwrite what we could not read: keep a copy beside the file.
    const aside = this.file.replace(/\.json$/, `.corrupt-${Date.now()}.json`);
    try {
      const { value, dropped } = this.model.parse(JSON.parse(raw));
      this.value = value;
      if (dropped) {
        await copyFile(this.file, aside);
        log.warn(name, `skipped ${dropped} malformed ${item}(s); the original is in ${aside}`);
      }
    } catch (error) {
      await rename(this.file, aside).catch(() => undefined);
      log.error(name, `${this.file} is not a ${name} file (${(error as Error).message}); moved it to ${aside} and started empty`);
    }
  }
}
