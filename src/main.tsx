import React from "react";
import { createRoot } from "react-dom/client";
import "./styles/global.css";

function App() {
  return (
    <main className="app-shell">
      <section className="hello-panel">
        <p className="eyebrow">LLM Relay DProxy</p>
        <h1>Hello World</h1>
        <p className="summary">
          Empty React frontend scaffold for future control-plane pages.
        </p>
      </section>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
