// Re-export infrastructure helpers for any consumers that need them.
export { readSessionCache, writeSessionCache, mergeSessionLists, withCSRFHeaders, readErrorMessage } from "./_base";

import * as sessionApi from "./sessions";
import * as terminalApi from "./terminals";
import * as providerApi from "./providers";
import * as taskApi from "./tasks";
import * as projectApi from "./projects";

/**
 * Unified API client. All methods are grouped by domain in separate modules
 * (sessions.ts, terminals.ts, providers.ts, tasks.ts) but assembled here
 * so existing call sites (`apiClient.xxx()`) require no changes.
 */
export const apiClient = {
  // Sessions
  listSessions: sessionApi.listSessions,
  getCachedSessions: sessionApi.getCachedSessions,
  createSession: sessionApi.createSession,
  createTaskSession: sessionApi.createTaskSession,
  createDockSession: sessionApi.createDockSession,
  getSession: sessionApi.getSession,
  getActivityEntries: sessionApi.getActivityEntries,
  stopSession: sessionApi.stopSession,
  pauseSession: sessionApi.pauseSession,
  resumeSession: sessionApi.resumeSession,
  cancelSession: sessionApi.cancelSession,
  sendSessionInput: sessionApi.sendSessionInput,
  sendMessage: sessionApi.sendMessage,
  getEventsUrl: sessionApi.getEventsUrl,
  getGlobalSessionEventsUrl: sessionApi.getGlobalSessionEventsUrl,
  pollDockMcp: sessionApi.pollDockMcp,
  respondDockMcp: sessionApi.respondDockMcp,

  // Terminals
  getTerminalSnapshot: terminalApi.getTerminalSnapshot,
  getTerminal: terminalApi.getTerminal,
  getTerminalSnapshotById: terminalApi.getTerminalSnapshotById,
  listTerminals: terminalApi.listTerminals,
  deleteTerminal: terminalApi.deleteTerminal,
  getTerminalWsUrl: terminalApi.getTerminalWsUrl,

  // Providers
  listProviders: providerApi.listProviders,
  getProvider: providerApi.getProvider,
  createProvider: providerApi.createProvider,
  updateProvider: providerApi.updateProvider,
  deleteProvider: providerApi.deleteProvider,

  // Projects
  listProjects: projectApi.listProjects,
  getProject: projectApi.getProject,
  createProject: projectApi.createProject,
  updateProject: projectApi.updateProject,
  deleteProject: projectApi.deleteProject,

  // Tasks, commits, permissions, extractors
  getPermissions: taskApi.getPermissions,
  getTaskTree: taskApi.getTaskTree,
  listCommits: taskApi.listCommits,
  getCommit: taskApi.getCommit,
  getExtractorConfig: taskApi.getExtractorConfig,
  saveExtractorConfig: taskApi.saveExtractorConfig,
  validateExtractorConfig: taskApi.validateExtractorConfig,
  replayExtractor: taskApi.replayExtractor,
};
