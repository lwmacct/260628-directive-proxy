import {
  WorkbenchAppearanceButton,
  WorkbenchLanguageToggle,
  WorkbenchShell,
} from "@lwmacct/260627-antd-workbench";
import { Navigate, Route, Routes } from "react-router-dom";
import { ConsoleLayout } from "../modules/console/ConsoleLayout";
import { DISPLAY_VERSION } from "../shared/appConfig";

export function AppShell() {
  return (
    <WorkbenchShell
      brand={{
        mark: "D",
        name: "Directive Proxy",
        version: DISPLAY_VERSION,
      }}
      className="single-route-shell"
      flushContent
      nav={[]}
      utilities={
        <>
          <WorkbenchAppearanceButton />
          <WorkbenchLanguageToggle />
        </>
      }
      onSelectNav={() => undefined}
    >
      <Routes>
        <Route element={<Navigate replace to="/console" />} path="/" />
        <Route element={<ConsoleLayout />} path="/console/*" />
        <Route element={<Navigate replace to="/console" />} path="*" />
      </Routes>
    </WorkbenchShell>
  );
}
