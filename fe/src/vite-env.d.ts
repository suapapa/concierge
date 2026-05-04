/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_DEV_API_PROXY?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
