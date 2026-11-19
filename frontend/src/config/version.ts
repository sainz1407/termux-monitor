declare const __APP_VERSION__: string
declare const __APP_VERSION_CODE__: string

// Injected by Vite from module.prop at build time.
export const APP_VERSION = __APP_VERSION__
export const APP_BUILD = __APP_VERSION_CODE__
