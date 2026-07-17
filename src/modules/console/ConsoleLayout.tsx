import { Navigate, Route, Routes } from "react-router-dom";
import { DirectiveConsolePage } from "../directive-workbench/DirectiveConsolePage";

export function ConsoleLayout() {
  return <Routes>
    <Route element={<DirectiveConsolePage />} index />
    <Route element={<Navigate replace to="/console" />} path="*" />
  </Routes>;
}
