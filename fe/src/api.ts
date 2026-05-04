import type { APIKeyMeta, CreateAPIKeyResponse, StatResponse } from './types';

const apiPrefix = '/api/v1';

async function parseJSON<T>(res: Response): Promise<T | null> {
  const text = await res.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text) as T;
  } catch {
    return null;
  }
}

export async function fetchStat(): Promise<{ ok: boolean; status: number; data?: StatResponse; error?: string }> {
  const res = await fetch(`${apiPrefix}/stat`, {
    credentials: 'include',
    headers: { Accept: 'application/json' },
  });
  if (res.status === 401) {
    return { ok: false, status: 401, error: 'Sign in to see your files.' };
  }
  if (!res.ok) {
    const body = await parseJSON<{ error?: string }>(res);
    return { ok: false, status: res.status, error: body?.error ?? `Request failed (${res.status})` };
  }
  const data = await parseJSON<StatResponse>(res);
  if (!data) {
    return { ok: false, status: res.status, error: 'Empty response from server.' };
  }
  return { ok: true, status: res.status, data };
}

export async function uploadLuggage(file: File, ttlMinutes: number): Promise<{ ok: true; key: string } | { ok: false; error: string }> {
  const fd = new FormData();
  fd.set('file', file, file.name);
  if (Number.isFinite(ttlMinutes) && ttlMinutes > 0) {
    fd.set('ttl', String(Math.floor(ttlMinutes)));
  }
  const res = await fetch(`${apiPrefix}/luggage`, {
    method: 'POST',
    body: fd,
    credentials: 'include',
  });
  if (res.status === 401) {
    return { ok: false, error: 'Session expired or missing. Sign in again.' };
  }
  if (!res.ok) {
    const body = await parseJSON<{ error?: string }>(res);
    return { ok: false, error: body?.error ?? `Upload failed (${res.status})` };
  }
  const body = await parseJSON<{ key?: string }>(res);
  if (!body?.key) {
    return { ok: false, error: 'Server did not return a key.' };
  }
  return { ok: true, key: body.key };
}

export async function deleteLuggage(key: string): Promise<{ ok: true } | { ok: false; error: string }> {
  const res = await fetch(`${apiPrefix}/luggage/${encodeURIComponent(key)}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 204) {
    return { ok: true };
  }
  if (res.status === 401) {
    return { ok: false, error: 'Unauthorized.' };
  }
  if (res.status === 403) {
    return { ok: false, error: 'You cannot delete this file.' };
  }
  if (res.status === 404) {
    return { ok: false, error: 'File not found.' };
  }
  const body = await parseJSON<{ error?: string }>(res);
  return { ok: false, error: body?.error ?? `Delete failed (${res.status})` };
}

export async function logout(): Promise<void> {
  await fetch(`${apiPrefix}/auth/logout`, {
    method: 'POST',
    credentials: 'include',
  });
}

export function publicLuggageUrl(key: string): string {
  const path = `${apiPrefix}/luggage/${encodeURIComponent(key)}`;
  return new URL(path, window.location.origin).toString();
}

export async function fetchApiKeys(): Promise<{ ok: boolean; status: number; data?: APIKeyMeta[]; error?: string }> {
  const res = await fetch(`${apiPrefix}/api-keys`, {
    credentials: 'include',
    headers: { Accept: 'application/json' },
  });
  if (res.status === 401) {
    return { ok: false, status: 401, error: 'Sign in required.' };
  }
  if (!res.ok) {
    const body = await parseJSON<{ error?: string }>(res);
    return { ok: false, status: res.status, error: body?.error ?? `Request failed (${res.status})` };
  }
  const data = await parseJSON<APIKeyMeta[]>(res);
  if (!data) {
    return { ok: false, status: res.status, error: 'Empty response from server.' };
  }
  return { ok: true, status: res.status, data };
}

export async function createApiKey(label: string): Promise<{ ok: true; data: CreateAPIKeyResponse } | { ok: false; error: string }> {
  const res = await fetch(`${apiPrefix}/api-keys`, {
    method: 'POST',
    credentials: 'include',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ label: label.trim() || undefined }),
  });
  if (res.status === 401) {
    return { ok: false, error: 'Sign in required.' };
  }
  if (!res.ok) {
    const body = await parseJSON<{ error?: string }>(res);
    return { ok: false, error: body?.error ?? `Create failed (${res.status})` };
  }
  const data = await parseJSON<CreateAPIKeyResponse>(res);
  if (!data?.key) {
    return { ok: false, error: 'Server did not return a key.' };
  }
  return { ok: true, data };
}

export async function deleteApiKey(id: number): Promise<{ ok: true } | { ok: false; error: string }> {
  const res = await fetch(`${apiPrefix}/api-keys/${encodeURIComponent(String(id))}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 204) {
    return { ok: true };
  }
  if (res.status === 401) {
    return { ok: false, error: 'Unauthorized.' };
  }
  if (res.status === 404) {
    return { ok: false, error: 'Key not found.' };
  }
  const body = await parseJSON<{ error?: string }>(res);
  return { ok: false, error: body?.error ?? `Delete failed (${res.status})` };
}
