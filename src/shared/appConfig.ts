export const VERSION = import.meta.env.VITE_APP_VERSION ?? "0.0.0";
export const DISPLAY_VERSION = /^v/i.test(VERSION) ? VERSION : `v${VERSION}`;
