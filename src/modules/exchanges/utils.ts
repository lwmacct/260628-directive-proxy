export function formatDate(value: string) {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "medium",
  }).format(new Date(value));
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }
  const units = ["B", "KiB", "MiB", "GiB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function methodColor(method: string) {
  switch (method) {
    case "GET":
      return "blue";
    case "POST":
      return "green";
    case "DELETE":
      return "red";
    case "PATCH":
    case "PUT":
      return "orange";
    default:
      return "default";
  }
}

export function statusColor(status: number) {
  if (status >= 500) {
    return "red";
  }
  if (status >= 400) {
    return "orange";
  }
  if (status >= 300) {
    return "blue";
  }
  if (status >= 200) {
    return "green";
  }
  return "default";
}
