/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL?: string
  /** `true` offers the fixed development identity on the login page (profile `marco1` only). */
  readonly VITE_DEV_SIGN_IN?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
