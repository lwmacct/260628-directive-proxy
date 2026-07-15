import { apiFetch } from "../../app/auth";
import type { DirectiveCodecResponse, DirectiveDocument } from "./types";

export async function directiveCodecRequest(
  action: "encode" | "decode",
  body: DirectiveDocument | { token: string },
  signal?: AbortSignal,
): Promise<DirectiveCodecResponse> {
  const response = await apiFetch(`/api/admin/directives/${action}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });
  if (!response.ok) {
    let message = `Directive ${action} failed (${response.status})`;
    try {
      const errorBody = await response.json() as { detail?: string };
      if (errorBody.detail) message = errorBody.detail;
    } catch {
      // Keep the status-based message when the response is not JSON.
    }
    throw new Error(message);
  }
  return response.json() as Promise<DirectiveCodecResponse>;
}
