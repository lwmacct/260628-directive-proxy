import { useRef, useState } from "react";
import type { Text } from "../../../shared/i18n";
import {
  decodeDirective,
  encodeDirective,
  formatDirectiveJSON,
  normalizeDirectiveToken,
  parseDirectiveJSON,
  validateDirective,
} from "../codec";
import { createInitialEditor } from "../constants";
import { buildEnvelope, envelopeSource, envelopeToEditor, errorMessage, sourceTokenKind } from "../utils";
import type { DirectiveEnvelope, DirectiveSource, EditorState } from "../types";

type Artifacts = {
  envelope: DirectiveEnvelope;
  formError: string | null;
  json: string;
};

type Direction = "builder" | "json" | "token";

function createArtifacts(source: DirectiveSource, editor: EditorState, text: Text["authConsole"]): Artifacts {
  const draft = buildEnvelope(source, editor);
  try {
    if (source === "inline" && editor.program.some((item) => !item.configValid)) {
      throw new Error(text.invalidModuleConfig);
    }
    if (source === "inline" && editor.recovery.enabled && !editor.recovery.controllerConfigValid) {
      throw new Error(text.invalidControllerConfig);
    }
    const envelope = validateDirective(draft, text);
    return { envelope, formError: null, json: formatDirectiveJSON(envelope) };
  } catch (error) {
    return { envelope: draft, formError: errorMessage(error, text.invalidForm), json: formatDirectiveJSON(draft) };
  }
}

export function useDirectiveSession(text: Text["authConsole"]) {
  const [initial] = useState(() => {
    const editor = createInitialEditor();
    return { editor, ...createArtifacts("inline", editor, text) };
  });
  const [source, setSourceState] = useState<DirectiveSource>("inline");
  const [editor, setEditor] = useState(initial.editor);
  const [envelope, setEnvelope] = useState(initial.envelope);
  const [formError, setFormError] = useState<string | null>(initial.formError);
  const [jsonInput, setJSONInput] = useState(initial.json);
  const [jsonError, setJSONError] = useState<string | null>(null);
  const [tokenSecret, setTokenSecret] = useState("");
  const [tokenInput, setTokenInput] = useState("");
  const [tokenError, setTokenError] = useState<string | null>(null);
  const [directiveToken, setDirectiveToken] = useState("");
  const direction = useRef<Direction>("builder");
  const generation = useRef(0);

  async function refreshToken(nextEnvelope: DirectiveEnvelope, secret: string, blockingError: string | null) {
    const currentGeneration = ++generation.current;
    if (blockingError) {
      setTokenError(null);
      setDirectiveToken("");
      return;
    }
    if (!secret.trim()) {
      setTokenInput("");
      setTokenError(null);
      setDirectiveToken("");
      return;
    }
    try {
      const token = await encodeDirective(nextEnvelope, secret, text);
      if (currentGeneration !== generation.current) return;
      setTokenInput(token);
      setTokenError(null);
      setDirectiveToken(token);
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      setTokenInput("");
      setTokenError(errorMessage(error, text.tokenAuthenticationFailed));
      setDirectiveToken("");
    }
  }

  function syncBuilder(nextSource: DirectiveSource, nextEditor: EditorState) {
    direction.current = "builder";
    const artifacts = createArtifacts(nextSource, nextEditor, text);
    setSourceState(nextSource);
    setEditor(nextEditor);
    setEnvelope(artifacts.envelope);
    setFormError(artifacts.formError);
    setJSONInput(artifacts.json);
    setJSONError(null);
    setTokenError(null);
    void refreshToken(artifacts.envelope, tokenSecret, artifacts.formError);
  }

  function setSource(nextSource: DirectiveSource) {
    if (nextSource === source) return;
    syncBuilder(nextSource, editor);
  }

  function updateEditor(patch: Partial<EditorState>) {
    syncBuilder(source, { ...editor, ...patch });
  }

  function updateJSON(value: string) {
    direction.current = "json";
    ++generation.current;
    setJSONInput(value);
    setDirectiveToken("");
    try {
      const nextEnvelope = parseDirectiveJSON(sourceTokenKind(source), value, text);
      const nextSource = envelopeSource(nextEnvelope);
      const nextEditor = envelopeToEditor(editor, nextEnvelope);
      setSourceState(nextSource);
      setEditor(nextEditor);
      setEnvelope(nextEnvelope);
      setFormError(null);
      setJSONError(null);
      setTokenError(null);
      void refreshToken(nextEnvelope, tokenSecret, null);
    } catch (error) {
      setJSONError(errorMessage(error, text.jsonParseFailed));
    }
  }

  async function updateToken(value: string, secret = tokenSecret) {
    direction.current = "token";
    const currentGeneration = ++generation.current;
    setTokenInput(value);
    setDirectiveToken("");
    if (!value.trim()) {
      setTokenError(null);
      return;
    }
    if (!secret.trim()) {
      setTokenError(text.tokenSecretRequired);
      return;
    }
    try {
      const nextEnvelope = await decodeDirective(value, secret, text);
      if (currentGeneration !== generation.current) return;
      const nextSource = envelopeSource(nextEnvelope);
      const nextEditor = envelopeToEditor(editor, nextEnvelope);
      setSourceState(nextSource);
      setEditor(nextEditor);
      setEnvelope(nextEnvelope);
      setFormError(null);
      setJSONInput(formatDirectiveJSON(nextEnvelope));
      setJSONError(null);
      setTokenError(null);
      setDirectiveToken(normalizeDirectiveToken(value));
    } catch (error) {
      if (currentGeneration !== generation.current) return;
      setTokenError(errorMessage(error, text.tokenParseFailed));
    }
  }

  function updateTokenSecret(secret: string) {
    setTokenSecret(secret);
    if (direction.current === "token") void updateToken(tokenInput, secret);
    else void refreshToken(envelope, secret, formError ?? jsonError);
  }

  return {
    directiveToken,
    editor,
    envelope,
    formError,
    jsonError,
    jsonInput,
    setSource,
    source,
    tokenError,
    tokenInput,
    tokenSecret,
    updateEditor,
    updateJSON,
    updateToken,
    updateTokenSecret,
  };
}
