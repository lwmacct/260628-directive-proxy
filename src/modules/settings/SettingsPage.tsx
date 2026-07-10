import {
  WorkbenchAppearanceSettings,
  WorkbenchPage,
  WorkbenchPanel,
} from "@lwmacct/260627-antd-workbench";

export function SettingsPage() {
  return (
    <WorkbenchPage title="外观设置">
      <WorkbenchPanel title="主题与界面">
        <WorkbenchAppearanceSettings
          labels={{
            accent: "强调色",
            black: "纯黑",
            compact: "紧凑",
            comfortable: "舒适",
            dark: "深色",
            deep: "深色表面",
            density: "密度",
            light: "浅色",
            mode: "主题",
            preview: "预览",
            radius: "圆角",
            reset: "重置",
            scheme: "配色",
            soft: "柔和",
            spacious: "宽松",
            surface: "表面",
            system: "跟随系统",
            tinted: "染色表面",
          }}
        />
      </WorkbenchPanel>
    </WorkbenchPage>
  );
}
