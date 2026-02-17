import type { SessionListResponse, ErrorResponse } from "../types/api";

export const BASE_URL = "/api";
export const CSRF_COOKIE_NAME = "orbitmesh-csrf-token";
export const CSRF_HEADER_NAME = "X-CSRF-Token";
export const DEFAULT_PROVIDER = "adk";
const SESSION_CACHE_KEY = "orbitmesh:sessions";

export function readSessionCache(): SessionListResponse | undefined {
  if (typeof window === "undefined" || !window.localStorage) return undefined;
  try {
    const raw = window.localStorage.getItem(SESSION_CACHE_KEY);
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as SessionListResponse;
    if (!parsed || !Array.isArray(parsed.sessions)) return undefined;
    return { sessions: parsed.sessions };
  } catch {
    return undefined;
  }
}

export function writeSessionCache(list: SessionListResponse): void {
  if (typeof window === "undefined" || !window.localStorage) return;
  try {
    window.localStorage.setItem(SESSION_CACHE_KEY, JSON.stringify(list));
  } catch {
    if (import.meta.env.DEV) {
      console.warn("OrbitMesh: failed to write session cache to localStorage");
    }
  }
}

export function mergeSessionLists(
  primary: SessionListResponse,
  fallback?: SessionListResponse,
): SessionListResponse {
  const merged = [...primary.sessions];
  if (!fallback) return { sessions: merged };
  const seen = new Set(primary.sessions.map((session) => session.id));
  fallback.sessions.forEach((session) => {
    if (!seen.has(session.id)) {
      merged.push(session);
      seen.add(session.id);
    }
  });
  return { sessions: merged };
}

export function getWebSocketBaseUrl(): string {
  if (typeof window === "undefined") return "";
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${window.location.host}`;
}

export function readCookie(name: string): string {
  if (typeof document === "undefined") return "";
  const cookieMap = document.cookie.split(";").map((segment) => segment.trim());
  const match = cookieMap.find((segment) => segment.startsWith(`${name}=`));
  if (!match) return "";
  return decodeURIComponent(match.split("=")[1] || "");
}

export function withCSRFHeaders(extra: Record<string, string> = {}): Record<string, string> {
  const token = readCookie(CSRF_COOKIE_NAME);
  if (!token) {
    throw new Error("Missing CSRF token cookie");
  }
  return {
    ...extra,
    [CSRF_HEADER_NAME]: token,
  };
}

export async function readErrorMessage(resp: Response): Promise<string> {
  const text = await resp.text();
  if (!text) return "Request failed.";
  try {
    const payload = JSON.parse(text) as ErrorResponse;
    if (payload && typeof payload.error === "string" && payload.error.trim().length > 0) {
      return payload.error;
    }
  } catch {
    // fall through to return raw text
  }
  return text;
}
