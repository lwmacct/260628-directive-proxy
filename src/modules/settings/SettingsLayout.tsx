import { BgColorsOutlined } from "@ant-design/icons";
import { WorkbenchSectionLayout } from "@lwmacct/260627-antd-workbench";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { useText } from "../../shared/i18n";

type SettingsSectionKey = "appearance";

const sectionKeys = new Set<SettingsSectionKey>(["appearance"]);

function activeSection(pathname: string): SettingsSectionKey {
  const key = pathname.split("/")[2];
  if (sectionKeys.has(key as SettingsSectionKey)) {
    return key as SettingsSectionKey;
  }
  return "appearance";
}

export function SettingsLayout() {
  const t = useText();
  const location = useLocation();
  const navigate = useNavigate();
  const sectionItems = [
    { key: "appearance", label: t.app.appearance, icon: <BgColorsOutlined /> },
  ] as const;

  return (
    <WorkbenchSectionLayout
      selectedKey={activeSection(location.pathname)}
      nav={[
        {
          type: "group",
          key: "preferences",
          label: t.app.preferences,
          children: [...sectionItems],
        },
      ]}
      onSelect={(key) => navigate(`/settings/${key}`)}
    >
      <Outlet />
    </WorkbenchSectionLayout>
  );
}
