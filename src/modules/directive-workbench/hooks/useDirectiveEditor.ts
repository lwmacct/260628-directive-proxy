import { App as AntdApp } from "antd";
import { useEffect, useMemo, useState } from "react";
import type { Text } from "../../../shared/i18n";
import { directiveCodecRequest } from "../codec";
import { initialEditor } from "../constants";
import { buildPayload, encodeDocument, errorMessage, formatPayload, payloadToEditor, remoteDocumentToEditor } from "../utils";
import type { DirectivePayload, EditorState } from "../types";

export function useDirectiveEditor(text: Text["authConsole"]) {
  const { message } = AntdApp.useApp();
  const [editor, setEditor] = useState(initialEditor);
  const payload = useMemo(() => buildPayload(editor), [editor]);
  const [payloadInput, setPayloadInput] = useState(() => formatPayload(payload));
  const [tokenInput, setTokenInput] = useState("");
  const [directiveToken, setDirectiveToken] = useState("");
  const [activeSource, setActiveSource] = useState<"payload" | "token">("payload");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void directiveCodecRequest("encode", encodeDocument(editor, payload), controller.signal)
        .then((result) => { setDirectiveToken(result.token); setTokenInput(result.token); })
        .catch((err: unknown) => {
          if (!(err instanceof DOMException && err.name === "AbortError")) setDirectiveToken("");
        });
    }, 200);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [editor, payload]);

  function updateEditor(patch: Partial<EditorState>) {
    const next = { ...editor, ...patch };
    setEditor(next);
    setDirectiveToken("");
    setPayloadInput(formatPayload(buildPayload(next)));
    setError(null);
    if (patch.source) setActiveSource(patch.source === "inline" ? "payload" : "token");
  }

  function applyPayload(nextPayload: DirectivePayload, recovery = editor.recovery) {
    const next = { ...editor, source: "inline" as const, recovery, ...payloadToEditor(nextPayload) };
    setEditor(next);
    setPayloadInput(formatPayload(nextPayload));
    setError(null);
    setActiveSource("payload");
  }

  async function applyPayloadInput() {
    try {
      const parsed = JSON.parse(payloadInput) as DirectivePayload;
      const result = await directiveCodecRequest("encode", { kind: "inline", payload: parsed, ...(editor.recovery ? { recovery: editor.recovery } : {}) });
      if (result.document.kind !== "inline") throw new Error(text.payloadParseFailed);
      applyPayload(result.document.payload, result.document.recovery);
      setDirectiveToken(result.token);
      setTokenInput(result.token);
      void message.success(text.payloadApplied);
    } catch (err) { setError(errorMessage(err, text.payloadParseFailed)); }
  }

  async function applyTokenInput() {
    try {
      const decoded = await directiveCodecRequest("decode", { token: tokenInput });
      if (decoded.document.kind === "inline") {
        applyPayload(decoded.document.payload, decoded.document.recovery);
      } else {
        setEditor({ ...remoteDocumentToEditor(editor, decoded.document.remote), recovery: decoded.document.recovery });
        setActiveSource("token");
        setError(null);
      }
      setDirectiveToken(decoded.token);
      setTokenInput(decoded.token);
      void message.success(text.tokenApplied);
    } catch (err) { setError(errorMessage(err, text.tokenParseFailed)); }
  }

  return {
    activeSource,
    applyPayloadInput,
    applyTokenInput,
    directiveToken,
    editor,
    error,
    payload,
    payloadInput,
    setActiveSource,
    setError,
    setPayloadInput,
    setTokenInput,
    tokenInput,
    updateEditor,
  };
}
