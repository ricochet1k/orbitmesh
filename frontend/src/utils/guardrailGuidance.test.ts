import { describe, expect, it } from "vitest";
import {
  sanitizeGuardrailGuidance,
  sanitizeGuardrailStatus,
  sanitizePermissionsResponse,
} from "./guardrailGuidance";

describe("sanitizeGuardrailGuidance", () => {
  it("strips HTML and redacts secrets", () => {
    const input = "Use <strong>token</strong>: abc123 and Bearer abc.def.ghi";
    expect(sanitizeGuardrailGuidance(input)).toBe("Use token: [redacted] and Bearer [redacted]");
  });

  it("removes control characters and redacts access keys", () => {
    const input = "Key\u0000 AKIA1234567890ABCD12 and api_key=shh";
    expect(sanitizeGuardrailGuidance(input)).toBe("Key [redacted] and api_key: [redacted]");
  });

  it("defaults nullish input to an empty string", () => {
    expect(sanitizeGuardrailGuidance(null)).toBe("");
    expect(sanitizeGuardrailGuidance(undefined)).toBe("");
  });

  it("coerces non-string input before sanitizing", () => {
    expect(sanitizeGuardrailGuidance(0)).toBe("0");
    expect(sanitizeGuardrailGuidance(false)).toBe("false");
    expect(sanitizeGuardrailGuidance({ token: "abc123" })).toBe("[object Object]");
  });
});

describe("sanitizeGuardrailStatus", () => {
  it("sanitizes detail while preserving other fields", () => {
    const input = {
      id: "role-escalation",
      title: "Role escalations",
      allowed: false,
      detail: "Use <em>token</em>: abc123 and ghp_abcdefghijklmnopqrstuvwxyz123456",
    };

    expect(sanitizeGuardrailStatus(input)).toEqual({
      ...input,
      detail: "Use token: [redacted] and [redacted]",
    });
  });

  it("handles non-string detail values", () => {
    const input = {
      id: "role-escalation",
      title: "Role escalations",
      allowed: false,
      detail: null,
    };

    expect(sanitizeGuardrailStatus(input as any)).toEqual({
      ...input,
      detail: "",
    });
  });
});

describe("sanitizePermissionsResponse", () => {
  it("sanitizes guardrail guidance within permissions", () => {
    const input = {
      role: "developer",
      can_inspect_sessions: true,
      can_manage_roles: false,
      can_manage_templates: true,
      can_initiate_bulk_actions: false,
      requires_owner_approval_for_role_changes: true,
      guardrails: [
        {
          id: "bulk-operations",
          title: "Bulk operations",
          allowed: false,
          detail: "Bearer abc.def and access_key=sekret",
        },
      ],
    };

    expect(sanitizePermissionsResponse(input)).toEqual({
      ...input,
      guardrails: [
        {
          id: "bulk-operations",
          title: "Bulk operations",
          allowed: false,
          detail: "Bearer [redacted] and access_key: [redacted]",
        },
      ],
    });
  });

  it("defaults missing guardrails to an empty array", () => {
    const input = {
      role: "developer",
      can_inspect_sessions: true,
      can_manage_roles: false,
      can_manage_templates: true,
      can_initiate_bulk_actions: false,
      requires_owner_approval_for_role_changes: true,
    };

    expect(sanitizePermissionsResponse(input as any).guardrails).toEqual([]);
  });

  it("coerces guardrail detail values in permissions", () => {
    const input = {
      role: "developer",
      can_inspect_sessions: true,
      can_manage_roles: false,
      can_manage_templates: true,
      can_initiate_bulk_actions: false,
      requires_owner_approval_for_role_changes: true,
      guardrails: [
        {
          id: "bulk-operations",
          title: "Bulk operations",
          allowed: false,
          detail: undefined,
        },
      ],
    };

    expect(sanitizePermissionsResponse(input as any)).toEqual({
      ...input,
      guardrails: [
        {
          id: "bulk-operations",
          title: "Bulk operations",
          allowed: false,
          detail: "",
        },
      ],
    });
  });
});
