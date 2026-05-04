export type KeyStat = {
  key: string;
  filename: string;
  mimeType: string;
  ownerUserId: number;
  activeRefs: number;
  fileSize: number;
  directory: string;
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
