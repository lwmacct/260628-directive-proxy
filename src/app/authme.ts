export const authmePath = "/authme";

export function authmeEndpoint(path: string) {
  return `${authmePath}${path}`;
}
