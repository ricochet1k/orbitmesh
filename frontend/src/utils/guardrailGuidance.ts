import type { GuardrailStatus, PermissionsResponse } from "../types/api";

const HTML_TAGS = /<\/?[^>]+(>|$)/g;
const CONTROL_CHARS = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g;
const BEARER_TOKEN = /\bBearer\s+[A-Za-z0-9\-._~+/]+=*/gi;
const KEY_VALUE_SECRETS =
  /\b(api[_-]?key|token|secret|password|passphrase|access[_-]?key)\b\s*[:=]\s*([^\s,;]+)/gi;
const AWS_ACCESS_KEY = /\bAKIA[0-9A-Z]{16}\b/g;
const GITHUB_TOKEN = /\bgh[pousr]_[A-Za-z0-9]{20,}\b/g;

export function sanitizeGuardrailGuidance(input: unknown): string {
  if (input === null || input === undefined) return "";
  const raw = typeof input === "string" ? input : String(input);
  if (!raw) return "";
  let sanitized = raw.replace(HTML_TAGS, "");
  sanitized = sanitized.replace(CONTROL_CHARS, " ");
  sanitized = sanitized.replace(BEARER_TOKEN, "Bearer [redacted]");
  sanitized = sanitized.replace(KEY_VALUE_SECRETS, (_match, label) => `${label}: [redacted]`);
  sanitized = sanitized.replace(AWS_ACCESS_KEY, "[redacted]");
  sanitized = sanitized.replace(GITHUB_TOKEN, "[redacted]");
  sanitized = sanitized.replace(/\s+/g, " ").trim();
  return sanitized;
}

export function sanitizeGuardrailStatus(guardrail: GuardrailStatus): GuardrailStatus {
  return {
    ...guardrail,
    detail: sanitizeGuardrailGuidance(guardrail.detail),
  };
}

export function sanitizePermissionsResponse(response: PermissionsResponse): PermissionsResponse {
  return {
    ...response,
    guardrails: (response.guardrails ?? []).map(sanitizeGuardrailStatus),
  };
}
