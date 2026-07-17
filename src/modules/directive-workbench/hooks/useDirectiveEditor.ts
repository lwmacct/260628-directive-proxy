import { App as AntdApp } from "antd";
import { useRef, useState } from "react";
import type { Text } from "../../../shared/i18n";
import { decodeDirective, encodeDirective, formatDirectiveJSON, parseDirectiveJSON, validateDirective } from "../codec";
import { createInitialEditor } from "../constants";
import { buildEnvelope, envelopeSource, envelopeToEditor, errorMessage, sourceTokenKind } from "../utils";
import type { DirectiveEnvelope, DirectiveSource, EditorState } from "../types";

type Artifacts = {
  envelope: DirectiveEnvelope;
  json: string;
  formError: string | null;
};

function createArtifacts(source: DirectiveSource, editor: EditorState, text: Text["authConsole"]): Artifacts {
  const draft = buildEnvelope(source, editor);
  try {
    if (source === "inline" && [...editor.requestProgram, ...editor.attemptProgram].some((item) => !item.configValid)) throw new Error(text.invalidModuleConfig);
    const envelope = validateDirective(draft, text);
    return { envelope, json: formatDirectiveJSON(envelope), formError: null };
  } catch (error) {
    return { envelope: draft, json: formatDirectiveJSON(draft), formError: errorMessage(error, text.invalidForm) };
  }
}

export function useDirectiveEditor(text: Text["authConsole"], source: DirectiveSource) {
  const { message } = AntdApp.useApp();
  const [initial] = useState(() => {
    const editor = createInitialEditor();
    return { editor, ...createArtifacts(source, editor, text) };
  });
  const [editor, setEditor] = useState(initial.editor);
  const [envelope, setEnvelope] = useState(initial.envelope);
  const [jsonInput, setJSONInput] = useState(initial.json);
  const [tokenSecret, setTokenSecret] = useState("");
  const [tokenInput, setTokenInput] = useState("");
  const [directiveToken, setDirectiveToken] = useState("");
  const [formError, setFormError] = useState<string | null>(initial.formError);
  const [activeSource, setActiveSource] = useState<"json" | "token">("json");
  const [error, setError] = useState<string | null>(null);
  const tokenGeneration = useRef(0);

  async function refreshToken(nextEnvelope: DirectiveEnvelope, secret: string, nextFormError: string | null) {
    const generation = ++tokenGeneration.current;
    if (nextFormError || !secret.trim()) {
      setTokenInput("");
      setDirectiveToken("");
      return;
    }
    try {
      const token = await encodeDirective(nextEnvelope, secret, text);
      if (generation !== tokenGeneration.current) return;
      setTokenInput(token);
      setDirectiveToken(token);
    } catch (err) {
      if (generation !== tokenGeneration.current) return;
      setTokenInput("");
      setDirectiveToken("");
      setError(errorMessage(err, text.tokenAuthenticationFailed));
    }
  }

  function syncEditor(next: EditorState) {
    const artifacts = createArtifacts(source, next, text);
    setEditor(next);
    setEnvelope(artifacts.envelope);
    setJSONInput(artifacts.json);
    setFormError(artifacts.formError);
    setError(null);
    void refreshToken(artifacts.envelope, tokenSecret, artifacts.formError);
  }

  function updateEditor(patch: Partial<EditorState>) {
    syncEditor({ ...editor, ...patch });
  }

  function applyEnvelope(nextEnvelope: DirectiveEnvelope) {
    if (envelopeSource(nextEnvelope) !== source) {
      setError(text.sourceMismatch);
      return false;
    }
    const nextEditor = envelopeToEditor(editor, nextEnvelope);
    const artifacts = createArtifacts(source, nextEditor, text);
    setEditor(nextEditor);
    setEnvelope(artifacts.envelope);
    setJSONInput(artifacts.json);
    setFormError(artifacts.formError);
    setError(null);
    void refreshToken(artifacts.envelope, tokenSecret, artifacts.formError);
    return true;
  }

  function updateTokenSecret(secret: string) {
    setTokenSecret(secret);
    void refreshToken(envelope, secret, formError);
  }

  function applyJSONInput() {
    try {
      const nextEnvelope = parseDirectiveJSON(sourceTokenKind(source), jsonInput, text);
      if (applyEnvelope(nextEnvelope)) void message.success(text.jsonApplied);
    } catch (err) {
      setError(errorMessage(err, text.jsonParseFailed));
    }
  }

  async function applyTokenInput() {
    try {
      if (applyEnvelope(await decodeDirective(tokenInput, tokenSecret, text))) void message.success(text.tokenApplied);
    } catch (err) {
      setError(errorMessage(err, text.tokenParseFailed));
    }
  }

  return {
    activeSource,
    applyJSONInput,
    applyTokenInput,
    directiveToken,
    editor,
    envelope,
    error,
    formError,
    jsonInput,
    setActiveSource,
    setError,
    setJSONInput,
    setTokenInput,
    tokenInput,
    tokenSecret,
    setTokenSecret: updateTokenSecret,
    updateEditor,
  };
}
