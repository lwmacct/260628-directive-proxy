import { BgColorsOutlined } from "@ant-design/icons";
import { WorkbenchSectionLayout } from "@lwmacct/260627-antd-workbench";
import { Outlet, useLocation, useNavigate } from "react-router-dom";

type SettingsSectionKey = "appearance";

const sectionItems = [
  { key: "appearance", label: "外观设置", icon: <BgColorsOutlined /> },
] as const;

const sectionKeys = new Set<SettingsSectionKey>(sectionItems.map((item) => item.key));

function activeSection(pathname: string): SettingsSectionKey {
  const key = pathname.split("/")[2];
  if (sectionKeys.has(key as SettingsSectionKey)) {
    return key as SettingsSectionKey;
  }
  return "appearance";
}

export function SettingsLayout() {
  const location = useLocation();
  const navigate = useNavigate();

  return (
    <WorkbenchSectionLayout
      selectedKey={activeSection(location.pathname)}
      nav={[
        {
          type: "group",
          key: "preferences",
          label: "偏好设置",
          children: [...sectionItems],
        },
      ]}
      onSelect={(key) => navigate(`/settings/${key}`)}
    >
      <Outlet />
    </WorkbenchSectionLayout>
  );
}
