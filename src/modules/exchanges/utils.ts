export function formatDate(value: string) {
  if (!value) {
    return "-";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "medium",
  }).format(new Date(value));
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
