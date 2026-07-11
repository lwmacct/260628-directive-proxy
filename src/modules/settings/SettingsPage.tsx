import {
  WorkbenchAppearanceSettings,
  WorkbenchPage,
  WorkbenchPanel,
} from "@lwmacct/260627-antd-workbench";
import { useText } from "../../shared/i18n";

export function SettingsPage() {
  const t = useText();
  return (
    <WorkbenchPage title={t.app.appearance}>
      <WorkbenchPanel title={t.appearance.panel}>
        <WorkbenchAppearanceSettings />
      </WorkbenchPanel>
    </WorkbenchPage>
  );
}
