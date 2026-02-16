import { createFileRoute } from "@tanstack/solid-router";
import { createEffect, createMemo, createResource, createSignal, For, Show } from "solid-js";
import { apiClient } from "../api/client";
import { useSessionStore } from "../state/sessions";
import type {
  ExtractorConfig,
  ExtractorProfile,
  ExtractorRule,
  ExtractorReplayResponse,
  TerminalSnapshot,
} from "../types/api";

export const Route = createFileRoute("/extractors")({
  component: ExtractorRulesView,
});

type NoticeTone = "error" | "success" | "neutral";

function ExtractorRulesView() {
  const [configResponse, { refetch }] = createResource(apiClient.getExtractorConfig);
  const { sessions } = useSessionStore();
  const [draft, setDraft] = createSignal<ExtractorConfig | null>(null);
  const [selectedProfileId, setSelectedProfileId] = createSignal("");
  const [selectedRuleId, setSelectedRuleId] = createSignal("");
  const [selectedSessionId, setSelectedSessionId] = createSignal("");
  const [startOffset, setStartOffset] = createSignal("");
  const [snapshot, setSnapshot] = createSignal<TerminalSnapshot | null>(null);
  const [replayResult, setReplayResult] = createSignal<ExtractorReplayResponse | null>(null);
  const [validationErrors, setValidationErrors] = createSignal<string[]>([]);
  const [notice, setNotice] = createSignal<{ tone: NoticeTone; message: string } | null>(null);
  const [saving, setSaving] = createSignal(false);
  const [validating, setValidating] = createSignal(false);
  const [loadingSnapshot, setLoadingSnapshot] = createSignal(false);
  const [replaying, setReplaying] = createSignal(false);
  const [initialized, setInitialized] = createSignal(false);

  createEffect(() => {
    const response = configResponse();
    if (!response || initialized()) return;
    setInitialized(true);
    setDraft(response.config);
    if (response.config.profiles.length > 0) {
      setSelectedProfileId(response.config.profiles[0].id);
    }
    if (response.errors && response.errors.length > 0) {
      setNotice({ tone: "error", message: response.errors.join(" ") });
    }
  });

  createEffect(() => {
    if (selectedSessionId()) return;
    const list = sessions();
    if (list.length > 0) {
      setSelectedSessionId(list[0].id);
    }
  });

  const currentProfile = createMemo(() => {
    const cfg = draft();
    if (!cfg) return null;
    const selected = cfg.profiles.find((profile) => profile.id === selectedProfileId());
    return selected ?? cfg.profiles[0] ?? null;
  });

  const currentRule = createMemo(() => {
    const profile = currentProfile();
    if (!profile) return null;
    const selected = profile.rules.find((rule) => rule.id === selectedRuleId());
    return selected ?? profile.rules[0] ?? null;
  });

  createEffect(() => {
    const profile = currentProfile();
    if (!profile) {
      setSelectedRuleId("");
      return;
    }
    if (profile.rules.length === 0) {
      setSelectedRuleId("");
      return;
    }
    if (!profile.rules.find((rule) => rule.id === selectedRuleId())) {
      setSelectedRuleId(profile.rules[0].id);
    }
  });

  const updateDraft = (updater: (cfg: ExtractorConfig) => void) => {
    const cfg = draft();
    if (!cfg) return;
    const next = structuredClone(cfg);
    updater(next);
    setDraft(next);
  };

  const updateProfile = (id: string, updater: (profile: ExtractorProfile) => void) => {
    updateDraft((cfg) => {
      const index = cfg.profiles.findIndex((profile) => profile.id === id);
      if (index < 0) return;
      const updated = { ...cfg.profiles[index] };
      updater(updated);
      cfg.profiles[index] = updated;
    });
  };

  const updateRule = (profileId: string, ruleId: string, updater: (rule: ExtractorRule) => void) => {
    updateDraft((cfg) => {
      const profileIndex = cfg.profiles.findIndex((profile) => profile.id === profileId);
      if (profileIndex < 0) return;
      const ruleIndex = cfg.profiles[profileIndex].rules.findIndex((rule) => rule.id === ruleId);
      if (ruleIndex < 0) return;
      const updated = { ...cfg.profiles[profileIndex].rules[ruleIndex] };
      updater(updated);
      cfg.profiles[profileIndex].rules[ruleIndex] = updated;
    });
  };

  const addProfile = () => {
    updateDraft((cfg) => {
      const id = `profile-${cfg.profiles.length + 1}`;
      cfg.profiles.push({
        id,
        enabled: true,
        match: { command_regex: "", args_regex: "" },
        rules: [],
      });
      setSelectedProfileId(id);
    });
  };

  const addRule = () => {
    const profile = currentProfile();
    if (!profile) return;
    updateDraft((cfg) => {
      const profileIndex = cfg.profiles.findIndex((p) => p.id === profile.id);
      if (profileIndex < 0) return;
      const newId = `rule-${cfg.profiles[profileIndex].rules.length + 1}`;
      cfg.profiles[profileIndex].rules.push({
        id: newId,
        enabled: true,
        trigger: { region_changed: { top: 0, bottom: 1 } },
        extract: { type: "region_text", region: { top: 0, bottom: 1 } },
        emit: { kind: "agent_message", update_window: "recent_open" },
      });
      setSelectedRuleId(newId);
    });
  };

  const removeRule = () => {
    const profile = currentProfile();
    const rule = currentRule();
    if (!profile || !rule) return;
    updateDraft((cfg) => {
      const profileIndex = cfg.profiles.findIndex((p) => p.id === profile.id);
      if (profileIndex < 0) return;
      cfg.profiles[profileIndex].rules = cfg.profiles[profileIndex].rules.filter((r) => r.id !== rule.id);
    });
  };

  const handleValidate = async () => {
    if (!draft()) return;
    setValidating(true);
    setNotice(null);
    try {
      const result = await apiClient.validateExtractorConfig(draft()!);
      setValidationErrors(result.errors ?? []);
      if (result.valid) {
        setNotice({ tone: "success", message: "Config passes validation." });
      } else {
        setNotice({ tone: "error", message: "Validation failed. Review the errors below." });
      }
    } catch (error) {
      setNotice({ tone: "error", message: formatError(error) });
    } finally {
      setValidating(false);
    }
  };

  const handleSave = async () => {
    if (!draft()) return;
    setSaving(true);
    setNotice(null);
    try {
      const response = await apiClient.saveExtractorConfig(draft()!);
      setDraft(response.config);
      setValidationErrors([]);
      setNotice({ tone: "success", message: "Extractor config saved." });
      refetch();
    } catch (error) {
      setNotice({ tone: "error", message: formatError(error) });
    } finally {
      setSaving(false);
    }
  };

  const handleLoadSnapshot = async () => {
    const sessionId = selectedSessionId();
    if (!sessionId) {
      setNotice({ tone: "error", message: "Select a session to load a snapshot." });
      return;
    }
    setLoadingSnapshot(true);
    setNotice(null);
    try {
      const result = await apiClient.getTerminalSnapshot(sessionId);
      setSnapshot(result);
      setNotice({ tone: "success", message: "Snapshot loaded." });
    } catch (error) {
      setNotice({ tone: "error", message: formatError(error) });
    } finally {
      setLoadingSnapshot(false);
    }
  };

  const handleReplay = async () => {
    const cfg = draft();
    const profile = currentProfile();
    const sessionId = selectedSessionId();
    if (!cfg || !profile) {
      setNotice({ tone: "error", message: "Select a profile to replay." });
      return;
    }
    if (!sessionId) {
      setNotice({ tone: "error", message: "Select a session to replay." });
      return;
    }
    let offset: number | undefined;
    if (startOffset().trim() !== "") {
      const parsed = Number(startOffset());
      if (Number.isNaN(parsed)) {
        setNotice({ tone: "error", message: "Start offset must be a number." });
        return;
      }
      offset = parsed;
    }
    setReplaying(true);
    setNotice(null);
    try {
      const result = await apiClient.replayExtractor({
        sessionId,
        config: cfg,
        profileId: profile.id,
        startOffset: offset,
      });
      setReplayResult(result);
      setNotice({ tone: "success", message: "Replay finished." });
    } catch (error) {
      setNotice({ tone: "error", message: formatError(error) });
    } finally {
      setReplaying(false);
    }
  };

  const regionPreview = createMemo(() => {
    const rule = currentRule();
    const snap = snapshot();
    if (!rule || !snap) return null;
    const region = rule.extract.region ?? {};
    return resolveRegionBounds(region, snap);
  });

  return (
    <div class="extractor-view">
      <header class="view-header">
        <div>
          <p class="eyebrow">Extraction engine</p>
          <h1>Extractor Rules</h1>
          <p class="dashboard-subtitle">
            Tune terminal extraction profiles, preview regions, and replay PTY logs without redeploys.
          </p>
        </div>
        <div class="header-meta">
          <div class="meta-card">
            <p>Profiles</p>
            <strong>{draft()?.profiles.length ?? 0}</strong>
          </div>
          <div class="meta-card">
            <p>Selected</p>
            <strong>{currentProfile()?.id || "None"}</strong>
          </div>
          <div class="meta-card">
            <p>Replay records</p>
            <strong>{replayResult()?.records.length ?? 0}</strong>
          </div>
        </div>
      </header>

      <Show when={notice()}>
        {(current) => <p class={`extractor-banner ${current().tone}`}>{current().message}</p>}
      </Show>

      <main class="extractor-layout">
        <section class="extractor-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Profiles</p>
              <h2>Profile Library</h2>
            </div>
            <div class="panel-tools">
              <button type="button" class="neutral" onClick={addProfile}>
                Add profile
              </button>
            </div>
          </div>
          <div class="profile-list">
            <Show when={(draft()?.profiles.length ?? 0) > 0} fallback={<p class="empty-state">No profiles yet.</p>}>
              <For each={draft()?.profiles ?? []}>
                {(profile) => (
                  <button
                    type="button"
                    class={`profile-card ${profile.id === currentProfile()?.id ? "active" : ""}`}
                    onClick={() => setSelectedProfileId(profile.id)}
                  >
                    <div>
                      <h3>{profile.id}</h3>
                      <p>{profile.match.command_regex || "No command matcher"}</p>
                    </div>
                    <span class={`profile-status ${profile.enabled === false ? "disabled" : "enabled"}`}>
                      {profile.enabled === false ? "Disabled" : "Enabled"}
                    </span>
                  </button>
                )}
              </For>
            </Show>
          </div>
        </section>

        <section class="extractor-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Rules</p>
              <h2>Rule Set</h2>
            </div>
            <div class="panel-tools">
              <button type="button" class="neutral" onClick={addRule}>
                Add rule
              </button>
              <button type="button" class="neutral" onClick={removeRule} disabled={!currentRule()}>
                Remove rule
              </button>
            </div>
          </div>
          <div class="rule-list">
            <Show when={currentProfile()} fallback={<p class="empty-state">Select a profile to see rules.</p>}>
              <Show
                when={(currentProfile()?.rules.length ?? 0) > 0}
                fallback={<p class="empty-state">No rules in this profile.</p>}
              >
                <For each={currentProfile()?.rules ?? []}>
                  {(rule) => (
                    <button
                      type="button"
                      class={`rule-card ${rule.id === currentRule()?.id ? "active" : ""}`}
                      onClick={() => setSelectedRuleId(rule.id)}
                    >
                      <div>
                        <h3>{rule.id}</h3>
                        <p>{rule.emit.kind}</p>
                      </div>
                      <span class={`rule-status ${rule.enabled ? "enabled" : "disabled"}`}>
                        {rule.enabled ? "Enabled" : "Disabled"}
                      </span>
                    </button>
                  )}
                </For>
              </Show>
            </Show>
          </div>
        </section>

        <section class="extractor-panel wide">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Editor</p>
              <h2>Profile & Rule Editor</h2>
            </div>
            <div class="panel-tools">
              <button type="button" class="neutral" onClick={handleValidate} disabled={validating()}>
                {validating() ? "Validating..." : "Validate"}
              </button>
              <button type="button" class="neutral" onClick={handleSave} disabled={saving()}>
                {saving() ? "Saving..." : "Save"}
              </button>
              <button type="button" class="neutral" onClick={() => refetch()}>
                Reload
              </button>
            </div>
          </div>

          <Show when={currentProfile()} fallback={<p class="empty-state">Select a profile to edit.</p>}>
            {(profile) => (
              <div class="editor-grid">
                <div class="editor-block">
                  <h3>Profile Settings</h3>
                  <label>
                    Profile ID
                    <input
                      type="text"
                      value={profile().id}
                      onInput={(event) => {
                        const nextId = event.currentTarget.value;
                        updateProfile(profile().id, (p) => (p.id = nextId));
                        setSelectedProfileId(nextId);
                      }}
                    />
                  </label>
                  <label>
                    Enabled
                    <input
                      type="checkbox"
                      checked={profile().enabled !== false}
                      onChange={(event) =>
                        updateProfile(profile().id, (p) => (p.enabled = event.currentTarget.checked))
                      }
                    />
                  </label>
                  <label>
                    Command regex
                    <input
                      type="text"
                      placeholder="(^|/)claude$"
                      value={profile().match.command_regex}
                      onInput={(event) =>
                        updateProfile(profile().id, (p) => (p.match.command_regex = event.currentTarget.value))
                      }
                    />
                  </label>
                  <label>
                    Args regex
                    <input
                      type="text"
                      placeholder=".*"
                      value={profile().match.args_regex}
                      onInput={(event) =>
                        updateProfile(profile().id, (p) => (p.match.args_regex = event.currentTarget.value))
                      }
                    />
                  </label>
                </div>

                <Show when={currentRule()} fallback={<p class="empty-state">Select a rule to edit.</p>}>
                  {(rule) => (
                    <div class="editor-block">
                      <h3>Rule Settings</h3>
                      <label>
                        Rule ID
                        <input
                          type="text"
                          value={rule().id}
                          onInput={(event) => {
                            const nextId = event.currentTarget.value;
                            updateRule(profile().id, rule().id, (r) => (r.id = nextId));
                            setSelectedRuleId(nextId);
                          }}
                        />
                      </label>
                      <label>
                        Enabled
                        <input
                          type="checkbox"
                          checked={rule().enabled}
                          onChange={(event) => updateRule(profile().id, rule().id, (r) => (r.enabled = event.currentTarget.checked))}
                        />
                      </label>
                      <label>
                        Emit kind
                        <input
                          type="text"
                          placeholder="agent_message"
                          value={rule().emit.kind}
                          onInput={(event) => updateRule(profile().id, rule().id, (r) => (r.emit.kind = event.currentTarget.value))}
                        />
                      </label>
                      <label>
                        Update window
                        <input
                          type="text"
                          placeholder="recent_open"
                          value={rule().emit.update_window ?? ""}
                          onInput={(event) =>
                            updateRule(profile().id, rule().id, (r) => (r.emit.update_window = event.currentTarget.value))
                          }
                        />
                      </label>
                      <label>
                        Extract type
                        <select
                          value={rule().extract.type}
                          onChange={(event) => updateRule(profile().id, rule().id, (r) => (r.extract.type = event.currentTarget.value))}
                        >
                          <option value="region_text">region_text</option>
                          <option value="region_regex">region_regex</option>
                        </select>
                      </label>
                      <label>
                        Extract pattern
                        <input
                          type="text"
                          placeholder="(?ms)^Assistant: (?P<text>.+)$"
                          value={rule().extract.pattern ?? ""}
                          onInput={(event) =>
                            updateRule(profile().id, rule().id, (r) => (r.extract.pattern = event.currentTarget.value))
                          }
                        />
                      </label>
                      <div class="region-grid">
                        <label>
                          Trigger top
                          <input
                            type="number"
                            value={rule().trigger.region_changed?.top ?? 0}
                            onInput={(event) =>
                              updateRule(profile().id, rule().id, (r) => {
                                if (!r.trigger.region_changed) {
                                  r.trigger.region_changed = { top: 0, bottom: 1 };
                                }
                                r.trigger.region_changed.top = Number(event.currentTarget.value);
                              })
                            }
                          />
                        </label>
                        <label>
                          Trigger bottom
                          <input
                            type="number"
                            value={rule().trigger.region_changed?.bottom ?? 1}
                            onInput={(event) =>
                              updateRule(profile().id, rule().id, (r) => {
                                if (!r.trigger.region_changed) {
                                  r.trigger.region_changed = { top: 0, bottom: 1 };
                                }
                                r.trigger.region_changed.bottom = Number(event.currentTarget.value);
                              })
                            }
                          />
                        </label>
                        <label>
                          Extract top
                          <input
                            type="number"
                            value={rule().extract.region.top ?? 0}
                            onInput={(event) =>
                              updateRule(profile().id, rule().id, (r) => (r.extract.region.top = Number(event.currentTarget.value)))
                            }
                          />
                        </label>
                        <label>
                          Extract bottom
                          <input
                            type="number"
                            value={rule().extract.region.bottom ?? 1}
                            onInput={(event) =>
                              updateRule(profile().id, rule().id, (r) => (r.extract.region.bottom = Number(event.currentTarget.value)))
                            }
                          />
                        </label>
                        <label>
                          Extract left
                          <input
                            type="number"
                            value={rule().extract.region.left ?? 0}
                            onInput={(event) =>
                              updateRule(profile().id, rule().id, (r) => (r.extract.region.left = Number(event.currentTarget.value)))
                            }
                          />
                        </label>
                        <label>
                          Extract right
                          <input
                            type="number"
                            value={rule().extract.region.right ?? 1}
                            onInput={(event) =>
                              updateRule(profile().id, rule().id, (r) => (r.extract.region.right = Number(event.currentTarget.value)))
                            }
                          />
                        </label>
                      </div>
                    </div>
                  )}
                </Show>
              </div>
            )}
          </Show>

          <Show when={validationErrors().length > 0}>
            <div class="validation-list">
              <h3>Validation Errors</h3>
              <ul>
                <For each={validationErrors()}>{(err) => <li>{err}</li>}</For>
              </ul>
            </div>
          </Show>
        </section>

        <section class="extractor-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Snapshots</p>
              <h2>Region Preview</h2>
            </div>
            <div class="panel-tools">
              <select value={selectedSessionId()} onChange={(event) => setSelectedSessionId(event.currentTarget.value)}>
                <option value="">Select session</option>
                <For each={sessions()?.sessions ?? []}>
                  {(session) => (
                    <option value={session.id}>
                      {session.id.slice(0, 8)} · {session.provider_type} · {session.state}
                    </option>
                  )}
                </For>
              </select>
              <button type="button" class="neutral" onClick={handleLoadSnapshot} disabled={loadingSnapshot()}>
                {loadingSnapshot() ? "Loading..." : "Load snapshot"}
              </button>
            </div>
          </div>
          <div class="snapshot-preview">
            <Show when={snapshot()} fallback={<p class="empty-state">Load a snapshot to preview regions.</p>}>
              {(snap) => (
                <div class="snapshot-shell">
                  <div class="snapshot-meta">
                    <span>{snap().rows} rows</span>
                    <span>{snap().cols} cols</span>
                    <span>Region: {formatRegion(regionPreview())}</span>
                  </div>
                  <div class="snapshot-lines">
                    <For each={snap().lines}>
                      {(line, index) => {
                        const region = regionPreview();
                        const row = index();
                        if (!region || row < region.top || row >= region.bottom) {
                          return <div class="snapshot-line">{line}</div>;
                        }
                        const { prefix, highlight, suffix } = splitLine(line, region.left, region.right);
                        return (
                          <div class="snapshot-line">
                            <span>{prefix}</span>
                            <span class="snapshot-highlight">{highlight || " "}</span>
                            <span>{suffix}</span>
                          </div>
                        );
                      }}
                    </For>
                  </div>
                </div>
              )}
            </Show>
          </div>
        </section>

        <section class="extractor-panel">
          <div class="panel-header">
            <div>
              <p class="panel-kicker">Replay</p>
              <h2>Extractor Replay</h2>
            </div>
            <div class="panel-tools">
              <input
                type="text"
                placeholder="Start offset (optional)"
                value={startOffset()}
                onInput={(event) => setStartOffset(event.currentTarget.value)}
              />
              <button type="button" class="neutral" onClick={handleReplay} disabled={replaying()}>
                {replaying() ? "Replaying..." : "Run replay"}
              </button>
            </div>
          </div>
          <div class="replay-results">
            <Show when={replayResult()} fallback={<p class="empty-state">No replay results yet.</p>}>
              {(result) => (
                <div class="replay-shell">
                  <div class="replay-meta">
                    <span>Offset: {result().offset}</span>
                    <span>Frames: {result().diagnostics.frames}</span>
                    <span>Bytes: {result().diagnostics.bytes}</span>
                  </div>
                  <div class="replay-records">
                    <For each={result().records}>
                      {(record) => (
                        <div class="replay-record">
                          <header>
                            <span>{record.type}</span>
                            <span>{record.entry?.kind ?? record.id ?? ""}</span>
                          </header>
                          <p>{formatRecord(record)}</p>
                        </div>
                      )}
                    </For>
                  </div>
                </div>
              )}
            </Show>
          </div>
        </section>
      </main>
    </div>
  );
}

function resolveRegionBounds(region: ExtractorRule["extract"]["region"], snap: TerminalSnapshot) {
  const top = region.top ?? 0;
  const bottom = region.bottom ?? snap.rows;
  const left = region.left ?? 0;
  const right = region.right ?? snap.cols;
  return { top, bottom, left, right };
}

function splitLine(line: string, left: number, right: number) {
  const chars = Array.from(line);
  const safeLeft = Math.max(0, left);
  const safeRight = Math.max(safeLeft, right);
  const prefix = chars.slice(0, safeLeft).join("");
  const highlight = chars.slice(safeLeft, safeRight).join("");
  const suffix = chars.slice(safeRight).join("");
  return { prefix, highlight, suffix };
}

function formatRegion(region: { top: number; bottom: number; left: number; right: number } | null) {
  if (!region) return "-";
  return `${region.top}:${region.bottom} · ${region.left}:${region.right}`;
}

function formatRecord(record: ExtractorReplayResponse["records"][number]) {
  if (record.entry?.data?.text) return record.entry.data.text;
  if (record.entry?.data?.summary) return record.entry.data.summary;
  if (record.entry?.data?.message) return record.entry.data.message;
  if (record.entry?.kind) return record.entry.kind;
  return record.id ?? "";
}

function formatError(error: unknown) {
  if (error instanceof Error) return error.message || "Request failed.";
  return "Request failed.";
}
