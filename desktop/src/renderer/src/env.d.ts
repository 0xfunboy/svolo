import type { AgentCoreApi } from "../../shared/agent-core";
import type { StudioApi } from "../../shared/ipc";

declare global {
  interface Window {
    studio: StudioApi;
    agentCore: AgentCoreApi;
  }
}
