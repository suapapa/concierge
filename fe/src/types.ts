export type KeyStat = {
  key: string;
  filename: string;
  mimeType: string;
  ownerUserId: number;
  activeRefs: number;
  fileSize: number;
  directory: string;
  /** RFC3339Nano UTC from server; omitted for objects uploaded before sidecar stored `expiresAt`. */
  expiresAt?: string;
};

export type StatResponse = {
  totalKeys: number;
  totalSize: number;
  activeRefs: Record<string, number>;
  keys: KeyStat[];
};

export type APIKeyMeta = {
  id: number;
  prefix: string;
  label?: string;
  createdAt: string;
  lastUsedAt?: string | null;
};

export type CreateAPIKeyResponse = {
  id: number;
  key: string;
  prefix: string;
  label?: string;
  createdAt: string;
};

/** User row from GET /admin/users (includes per-user quotas). */
export type UserRow = {
  id: number;
  googleSub: string;
  email: string;
  displayName?: string;
  pictureUrl?: string;
  role: string;
  maxPoolBytes: number;
  maxSingleFileBytes: number;
  dailyMaxUploads: number;
};
