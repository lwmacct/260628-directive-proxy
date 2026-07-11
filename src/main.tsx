import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AppRoot } from "./app/AppRoot";
import { loadSession } from "./app/session";
import "antd/dist/reset.css";
import "@lwmacct/260627-antd-workbench/global.css";
import "./styles/global.css";

async function bootstrap() {
  const initialSession = await loadSession();
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <AppRoot initialSession={initialSession} />
    </StrictMode>,
  );
}

void bootstrap();
