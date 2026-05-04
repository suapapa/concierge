import { useCallback, useEffect, useId, useRef, useState } from 'react';
import {
  createApiKey,
  deleteApiKey,
  deleteLuggage,
  fetchAdminUsers,
  fetchApiKeys,
  fetchStat,
  logout,
  patchAdminUser,
  publicLuggageUrl,
  uploadLuggage,
} from './api';
import type { APIKeyMeta, CreateAPIKeyResponse, KeyStat, StatResponse, UserRow } from './types';

const MIB_BYTES = 1024 * 1024;

function AdminUserQuotaRow({ u, onSaved }: { u: UserRow; onSaved: () => Promise<void> }) {
  const [role, setRole] = useState(u.role);
  const [poolMiB, setPoolMiB] = useState(String(Math.max(1, Math.round(u.maxPoolBytes / MIB_BYTES))));
  const [singleMiB, setSingleMiB] = useState(String(Math.max(1, Math.round(u.maxSingleFileBytes / MIB_BYTES))));
  const [daily, setDaily] = useState(String(Math.max(1, u.dailyMaxUploads)));
  const [busy, setBusy] = useState(false);
  const [localErr, setLocalErr] = useState<string | null>(null);

  useEffect(() => {
    setRole(u.role);
    setPoolMiB(String(Math.max(1, Math.round(u.maxPoolBytes / MIB_BYTES))));
    setSingleMiB(String(Math.max(1, Math.round(u.maxSingleFileBytes / MIB_BYTES))));
    setDaily(String(Math.max(1, u.dailyMaxUploads)));
    setLocalErr(null);
  }, [u]);

  const onSave = async () => {
    setLocalErr(null);
    const p = Number.parseFloat(poolMiB);
    const s = Number.parseFloat(singleMiB);
    const d = Number.parseInt(daily, 10);
    if (!Number.isFinite(p) || p < 1) {
      setLocalErr('Pool size (MiB) must be at least 1.');
      return;
    }
    if (!Number.isFinite(s) || s < 1) {
      setLocalErr('Per-file size (MiB) must be at least 1.');
      return;
    }
    if (!Number.isFinite(d) || d < 1 || !Number.isInteger(d)) {
      setLocalErr('Daily upload count must be a whole number ≥ 1.');
      return;
    }
    const maxPoolBytes = Math.round(p * MIB_BYTES);
    const maxSingleFileBytes = Math.round(s * MIB_BYTES);
    if (maxSingleFileBytes > maxPoolBytes) {
      setLocalErr('Per-file limit cannot exceed pool size.');
      return;
    }
    setBusy(true);
    const res = await patchAdminUser(u.id, {
      role,
      maxPoolBytes,
      maxSingleFileBytes,
      dailyMaxUploads: d,
    });
    setBusy(false);
    if (!res.ok) {
      setLocalErr(res.error);
      return;
    }
    await onSaved();
  };

  return (
    <tr>
      <td className="max-w-[12rem] truncate px-3 py-2 text-xs text-zinc-800 dark:text-zinc-200" title={u.email} translate="no">
        {u.email}
      </td>
      <td className="px-3 py-2">
        <select
          className="w-full min-w-[5.5rem] rounded border border-zinc-300 bg-white px-2 py-1 text-xs dark:border-zinc-600 dark:bg-zinc-950 dark:text-zinc-100"
          value={role}
          onChange={(e) => setRole(e.target.value)}
          disabled={busy}
          aria-label={`Role for ${u.email}`}
        >
          <option value="guest">guest</option>
          <option value="admin">admin</option>
        </select>
      </td>
      <td className="px-3 py-2">
        <input
          type="number"
          min={1}
          step={1}
          className="w-20 rounded border border-zinc-300 bg-white px-2 py-1 text-xs tabular-nums dark:border-zinc-600 dark:bg-zinc-950 dark:text-zinc-100"
          value={poolMiB}
          onChange={(e) => setPoolMiB(e.target.value)}
          disabled={busy}
          aria-label={`Max pool MiB for ${u.email}`}
        />
      </td>
      <td className="px-3 py-2">
        <input
          type="number"
          min={1}
          step={1}
          className="w-20 rounded border border-zinc-300 bg-white px-2 py-1 text-xs tabular-nums dark:border-zinc-600 dark:bg-zinc-950 dark:text-zinc-100"
          value={singleMiB}
          onChange={(e) => setSingleMiB(e.target.value)}
          disabled={busy}
          aria-label={`Max single file MiB for ${u.email}`}
        />
      </td>
      <td className="px-3 py-2">
        <input
          type="number"
          min={1}
          step={1}
          className="w-16 rounded border border-zinc-300 bg-white px-2 py-1 text-xs tabular-nums dark:border-zinc-600 dark:bg-zinc-950 dark:text-zinc-100"
          value={daily}
          onChange={(e) => setDaily(e.target.value)}
          disabled={busy}
          aria-label={`Daily upload cap for ${u.email}`}
        />
      </td>
      <td className="px-3 py-2">
        <button
          type="button"
          className="rounded-md bg-blue-600 px-2 py-1 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-60"
          disabled={busy}
          onClick={() => void onSave()}
        >
          {busy ? 'Saving…' : 'Save'}
        </button>
        {localErr && (
          <p className="mt-1 max-w-[14rem] text-xs text-red-700 dark:text-red-300" role="alert">
            {localErr}
          </p>
        )}
      </td>
    </tr>
  );
}

function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) {
    return '—';
  }
  const units = ['B', 'KB', 'MB', 'GB'] as const;
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const digits = i === 0 ? 0 : v < 10 ? 1 : 0;
  const num = new Intl.NumberFormat(undefined, {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
  }).format(v);
  return `${num}\u00A0${units[i]}`;
}

function CopyLinkButton({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      window.prompt('Copy this URL:', url);
    }
  };

  return (
    <button
      type="button"
      className="rounded-md border border-zinc-300 bg-white px-2 py-1 text-sm font-medium text-zinc-800 transition hover:border-zinc-400 hover:bg-zinc-50 active:bg-zinc-100 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:border-zinc-500 dark:hover:bg-zinc-800"
      aria-label={copied ? 'Public link copied' : 'Copy public link to clipboard'}
      aria-live="polite"
      onClick={onCopy}
    >
      {copied ? 'Copied' : 'Copy Link'}
    </button>
  );
}

type DeleteTarget = Pick<KeyStat, 'key' | 'filename'>;

function DeleteConfirmDialog({
  target,
  open,
  busy,
  onDismiss,
  onConfirm,
}: {
  target: DeleteTarget | null;
  open: boolean;
  busy: boolean;
  onDismiss: () => void;
  onConfirm: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (!d) {
      return;
    }
    if (open) {
      if (!d.open) {
        d.showModal();
      }
    } else if (d.open) {
      d.close();
    }
  }, [open]);

  return (
    <dialog
      ref={ref}
      className="max-w-md rounded-lg border border-zinc-200 bg-white p-0 text-zinc-900 shadow-lg backdrop:bg-black/40 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
      style={{ overscrollBehavior: 'contain' }}
      onClose={onDismiss}
      onCancel={(e) => {
        if (busy) {
          e.preventDefault();
        }
      }}
      aria-labelledby="delete-dialog-title"
    >
      <div className="p-6">
        <h2 id="delete-dialog-title" className="text-pretty text-lg font-semibold tracking-tight text-zinc-900 dark:text-zinc-50">
          Delete This File?
        </h2>
        <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-300">
          This removes{' '}
          <span className="font-medium text-zinc-900 dark:text-zinc-100" translate="no">
            {target?.filename ?? 'the file'}
          </span>{' '}
          from Concierge. Anyone with the old link will get a not-found error. This cannot be undone.
        </p>
        <div className="mt-6 flex flex-wrap justify-end gap-2">
          <button
            type="button"
            className="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm font-medium text-zinc-800 transition hover:bg-zinc-50 active:bg-zinc-100 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:bg-zinc-800"
            onClick={onDismiss}
            disabled={busy}
          >
            Cancel
          </button>
          <button
            type="button"
            className="rounded-md bg-red-600 px-3 py-2 text-sm font-semibold text-white transition hover:bg-red-700 active:bg-red-800"
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? 'Deleting…' : 'Delete File'}
          </button>
        </div>
      </div>
    </dialog>
  );
}

function NewApiKeyDialog({
  data,
  open,
  onDismiss,
}: {
  data: CreateAPIKeyResponse | null;
  open: boolean;
  onDismiss: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const d = ref.current;
    if (!d) {
      return;
    }
    if (open && data) {
      if (!d.open) {
        d.showModal();
      }
    } else if (d.open) {
      d.close();
    }
  }, [open, data]);

  const onCopy = async () => {
    if (!data?.key) {
      return;
    }
    try {
      await navigator.clipboard.writeText(data.key);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      window.prompt('Copy this API key:', data.key);
    }
  };

  return (
    <dialog
      ref={ref}
      className="max-w-lg rounded-lg border border-zinc-200 bg-white p-0 text-zinc-900 shadow-lg backdrop:bg-black/40 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"
      style={{ overscrollBehavior: 'contain' }}
      onClose={onDismiss}
      aria-labelledby="new-api-key-title"
    >
      <div className="p-6">
        <h2 id="new-api-key-title" className="text-lg font-semibold tracking-tight text-zinc-900 dark:text-zinc-50">
          API key created
        </h2>
        <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-300">
          Copy it now. For security, the full secret is not shown again. Use{' '}
          <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800" translate="no">
            Authorization: Bearer &lt;key&gt;
          </code>{' '}
          on protected routes.
        </p>
        {data && (
          <div className="mt-4 rounded-md border border-zinc-200 bg-zinc-50 p-3 dark:border-zinc-600 dark:bg-zinc-950">
            <p className="text-xs font-medium uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Secret</p>
            <p className="mt-1 break-all font-mono text-sm text-zinc-900 dark:text-zinc-100" translate="no">
              {data.key}
            </p>
          </div>
        )}
        <div className="mt-6 flex flex-wrap justify-end gap-2">
          <button
            type="button"
            className="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm font-medium text-zinc-800 transition hover:bg-zinc-50 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:bg-zinc-800"
            onClick={() => void onCopy()}
          >
            {copied ? 'Copied' : 'Copy key'}
          </button>
          <button
            type="button"
            className="rounded-md bg-blue-600 px-3 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
            onClick={onDismiss}
          >
            Done
          </button>
        </div>
      </div>
    </dialog>
  );
}

export default function App() {
  const fileInputId = useId();
  const ttlInputId = useId();
  const [loading, setLoading] = useState(true);
  const [stat, setStat] = useState<StatResponse | null>(null);
  const [authRequired, setAuthRequired] = useState(false);
  const [banner, setBanner] = useState<{ tone: 'info' | 'error'; text: string } | null>(null);
  const [uploading, setUploading] = useState(false);
  const [ttlInput, setTtlInput] = useState('3');
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [apiKeys, setApiKeys] = useState<APIKeyMeta[]>([]);
  const [apiKeyLabel, setApiKeyLabel] = useState('');
  const [createKeyBusy, setCreateKeyBusy] = useState(false);
  const [revokeKeyBusy, setRevokeKeyBusy] = useState<number | null>(null);
  const [newKeyDialog, setNewKeyDialog] = useState<CreateAPIKeyResponse | null>(null);
  const [adminUsers, setAdminUsers] = useState<UserRow[] | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setBanner(null);
    const r = await fetchStat();
    setLoading(false);
    if (r.status === 401) {
      setAuthRequired(true);
      setStat(null);
      setApiKeys([]);
      setAdminUsers(null);
      return;
    }
    setAuthRequired(false);
    if (!r.ok || !r.data) {
      setStat(null);
      setApiKeys([]);
      setAdminUsers(null);
      setBanner({ tone: 'error', text: r.error ?? 'Could not load your files.' });
      return;
    }
    setStat(r.data);
    const ak = await fetchApiKeys();
    if (ak.ok && ak.data) {
      setApiKeys(ak.data);
    } else {
      setApiKeys([]);
    }
    const adm = await fetchAdminUsers();
    if (adm.ok) {
      setAdminUsers(adm.data);
    } else {
      setAdminUsers(null);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const onUpload = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const input = fileInputRef.current;
    const file = input?.files?.[0];
    if (!file) {
      setBanner({ tone: 'error', text: 'Choose a file to upload.' });
      input?.focus();
      return;
    }
    setUploading(true);
    setBanner(null);
    const ttlParsed = Number.parseInt(ttlInput, 10);
    const ttlMinutes = Number.isFinite(ttlParsed) && ttlParsed > 0 ? ttlParsed : 0;
    const res = await uploadLuggage(file, ttlMinutes);
    setUploading(false);
    if (!res.ok) {
      setBanner({ tone: 'error', text: res.error });
      return;
    }
    setBanner({ tone: 'info', text: `Uploaded. New key ${res.key}.` });
    if (input) {
      input.value = '';
    }
    await load();
  };

  const onLogout = async () => {
    await logout();
    setStat(null);
    setAuthRequired(true);
    setBanner({ tone: 'info', text: 'Signed out.' });
  };

  const openDelete = (row: KeyStat) => {
    setDeleteTarget({ key: row.key, filename: row.filename });
  };

  const closeDelete = () => {
    if (deleteBusy) {
      return;
    }
    setDeleteTarget(null);
  };

  const onCreateApiKey = async () => {
    setCreateKeyBusy(true);
    setBanner(null);
    const res = await createApiKey(apiKeyLabel);
    setCreateKeyBusy(false);
    if (!res.ok) {
      setBanner({ tone: 'error', text: res.error });
      return;
    }
    setNewKeyDialog(res.data);
    setApiKeyLabel('');
    const ak = await fetchApiKeys();
    if (ak.ok && ak.data) {
      setApiKeys(ak.data);
    }
  };

  const onRevokeApiKey = async (id: number) => {
    if (!window.confirm('Revoke this API key? Scripts using it will stop working.')) {
      return;
    }
    setRevokeKeyBusy(id);
    setBanner(null);
    const res = await deleteApiKey(id);
    setRevokeKeyBusy(null);
    if (!res.ok) {
      setBanner({ tone: 'error', text: res.error });
      return;
    }
    setApiKeys((prev) => prev.filter((k) => k.id !== id));
    setBanner({ tone: 'info', text: 'API key revoked.' });
  };

  const confirmDelete = async () => {
    if (!deleteTarget) {
      return;
    }
    setDeleteBusy(true);
    setBanner(null);
    const res = await deleteLuggage(deleteTarget.key);
    setDeleteBusy(false);
    if (!res.ok) {
      setBanner({ tone: 'error', text: res.error });
      return;
    }
    setDeleteTarget(null);
    setBanner({ tone: 'info', text: 'File deleted.' });
    await load();
  };

  const nfCount = new Intl.NumberFormat(undefined);

  return (
    <div className="mx-auto flex min-h-screen max-w-5xl flex-col px-4 pb-16 pt-4 sm:px-6">
      <a
        href="#main-content"
        className="fixed left-4 top-4 z-[100] -translate-y-[200%] rounded-md bg-white px-3 py-2 text-sm font-medium text-zinc-900 opacity-0 shadow transition focus:translate-y-0 focus:opacity-100 dark:bg-zinc-900 dark:text-zinc-50"
      >
        Skip to main content
      </a>

      <header className="flex flex-col gap-4 border-b border-zinc-200 pb-6 dark:border-zinc-800 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-balance text-2xl font-semibold tracking-tight text-zinc-900 dark:text-zinc-50">Your Files</h1>
          <p className="mt-1 max-w-xl text-sm text-zinc-600 dark:text-zinc-400">
            Upload temporary luggage, copy a public link, and remove items you no longer need. Guests only see their own
            uploads; admins see every key on the server.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!authRequired && stat && (
            <button
              type="button"
              className="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm font-medium text-zinc-800 transition hover:bg-zinc-50 active:bg-zinc-100 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:bg-zinc-800"
              onClick={() => void onLogout()}
            >
              Sign Out
            </button>
          )}
        </div>
      </header>

      <div className="mt-4 min-h-6" aria-live="polite" aria-atomic="true">
        {banner && (
          <p
            role="status"
            className={
              banner.tone === 'error'
                ? 'rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-900 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-100'
                : 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-100'
            }
          >
            {banner.text}
          </p>
        )}
      </div>

      <main id="main-content" className="mt-6 flex flex-1 flex-col gap-10 scroll-mt-4">
        {loading && <p className="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>}

        {authRequired && !loading && (
          <section className="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900" aria-labelledby="sign-in-heading">
            <h2 id="sign-in-heading" className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">
              Sign In Required
            </h2>
            <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
              Use Google OAuth on the Concierge server (database mode), or configure a legacy bearer token for API-only
              access. This UI expects a session cookie after login; you can then create per-user API keys for scripts.
            </p>
            <p className="mt-4">
              <a
                href="/api/v1/auth/google"
                className="inline-flex rounded-md bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 active:bg-blue-800"
              >
                Continue With Google
              </a>
            </p>
            <p className="mt-3 text-xs text-zinc-500 dark:text-zinc-500">
              Set <code className="rounded bg-zinc-100 px-1 py-0.5 text-zinc-800 dark:bg-zinc-800 dark:text-zinc-200" translate="no">CONCIERGE_POST_LOGIN_REDIRECT</code>{' '}
              to this app’s URL (for example <span translate="no">http://localhost:5173/</span>) so you return here after
              OAuth.
            </p>
          </section>
        )}

        {!authRequired && !loading && stat && (
          <>
            <section className="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900" aria-labelledby="upload-heading">
              <h2 id="upload-heading" className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">
                Upload A File
              </h2>
              <form className="mt-4 flex max-w-xl flex-col gap-4" onSubmit={(e) => void onUpload(e)} noValidate>
                <div className="min-w-0">
                  <label htmlFor={fileInputId} className="block text-sm font-medium text-zinc-800 dark:text-zinc-200">
                    File
                  </label>
                  <input
                    ref={fileInputRef}
                    id={fileInputId}
                    name="file"
                    type="file"
                    autoComplete="off"
                    className="mt-1 block w-full min-w-0 text-sm text-zinc-800 file:mr-3 file:rounded-md file:border-0 file:bg-zinc-100 file:px-3 file:py-2 file:text-sm file:font-medium file:text-zinc-900 hover:file:bg-zinc-200 dark:text-zinc-200 dark:file:bg-zinc-800 dark:file:text-zinc-100 dark:hover:file:bg-zinc-700"
                    disabled={uploading}
                  />
                </div>
                <div>
                  <label htmlFor={ttlInputId} className="block text-sm font-medium text-zinc-800 dark:text-zinc-200">
                    TTL (minutes)
                  </label>
                  <input
                    id={ttlInputId}
                    name="ttl"
                    type="number"
                    inputMode="numeric"
                    min={1}
                    step={1}
                    spellCheck={false}
                    autoComplete="off"
                    className="mt-1 w-32 rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm tabular-nums text-zinc-900 shadow-sm dark:border-zinc-600 dark:bg-zinc-950 dark:text-zinc-100"
                    value={ttlInput}
                    onChange={(e) => setTtlInput(e.target.value)}
                    disabled={uploading}
                    placeholder="e.g. 5…"
                  />
                </div>
                <div>
                  <button
                    type="submit"
                    className="rounded-md bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 active:bg-blue-800 disabled:cursor-not-allowed disabled:opacity-60"
                    disabled={uploading}
                  >
                    {uploading ? 'Uploading…' : 'Upload File'}
                  </button>
                </div>
              </form>
            </section>

            <section className="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900" aria-labelledby="api-keys-heading">
              <h2 id="api-keys-heading" className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">
                API keys
              </h2>
              <p className="mt-2 max-w-xl text-sm text-zinc-600 dark:text-zinc-400">
                Create keys to call protected endpoints from curl, CI, or other apps with{' '}
                <code className="rounded bg-zinc-100 px-1 py-0.5 text-xs dark:bg-zinc-800" translate="no">
                  Authorization: Bearer concierge_…
                </code>
                . Keys inherit your role (admins may use admin APIs).
              </p>
              <div className="mt-4 flex max-w-xl flex-wrap items-end gap-3">
                <div className="min-w-0 flex-1">
                  <label htmlFor="api-key-label" className="block text-sm font-medium text-zinc-800 dark:text-zinc-200">
                    Label (optional)
                  </label>
                  <input
                    id="api-key-label"
                    type="text"
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="e.g. laptop, CI"
                    className="mt-1 w-full min-w-0 rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm text-zinc-900 shadow-sm dark:border-zinc-600 dark:bg-zinc-950 dark:text-zinc-100"
                    value={apiKeyLabel}
                    onChange={(e) => setApiKeyLabel(e.target.value)}
                    disabled={createKeyBusy}
                  />
                </div>
                <button
                  type="button"
                  className="rounded-md bg-zinc-800 px-4 py-2 text-sm font-semibold text-white transition hover:bg-zinc-900 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-zinc-200 dark:text-zinc-900 dark:hover:bg-white"
                  disabled={createKeyBusy}
                  onClick={() => void onCreateApiKey()}
                >
                  {createKeyBusy ? 'Creating…' : 'Create key'}
                </button>
              </div>
              {apiKeys.length === 0 ? (
                <p className="mt-4 text-sm text-zinc-500 dark:text-zinc-400">No API keys yet.</p>
              ) : (
                <div className="mt-4 overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
                  <table className="min-w-full divide-y divide-zinc-200 text-left text-sm dark:divide-zinc-800">
                    <thead className="bg-zinc-50 dark:bg-zinc-900/80">
                      <tr>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Prefix
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Label
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Created
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Last used
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Actions
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-zinc-200 bg-white dark:divide-zinc-800 dark:bg-zinc-950">
                      {apiKeys.map((row) => (
                        <tr key={row.id}>
                          <td className="px-3 py-2 font-mono text-xs text-zinc-800 dark:text-zinc-200" translate="no">
                            {row.prefix}…
                          </td>
                          <td className="max-w-[10rem] truncate px-3 py-2 text-zinc-800 dark:text-zinc-200" title={row.label}>
                            {row.label || '—'}
                          </td>
                          <td className="whitespace-nowrap px-3 py-2 text-xs text-zinc-600 dark:text-zinc-400">
                            {new Date(row.createdAt).toLocaleString()}
                          </td>
                          <td className="whitespace-nowrap px-3 py-2 text-xs text-zinc-600 dark:text-zinc-400">
                            {row.lastUsedAt ? new Date(row.lastUsedAt).toLocaleString() : '—'}
                          </td>
                          <td className="px-3 py-2">
                            <button
                              type="button"
                              className="rounded-md border border-red-200 bg-red-50 px-2 py-1 text-xs font-semibold text-red-800 transition hover:bg-red-100 disabled:opacity-60 dark:border-red-900/60 dark:bg-red-950/50 dark:text-red-100 dark:hover:bg-red-950"
                              disabled={revokeKeyBusy === row.id}
                              onClick={() => void onRevokeApiKey(row.id)}
                            >
                              {revokeKeyBusy === row.id ? 'Revoking…' : 'Revoke'}
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>

            {adminUsers !== null && (
              <section className="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900" aria-labelledby="admin-users-heading">
                <h2 id="admin-users-heading" className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">
                  Users &amp; quotas
                </h2>
                <p className="mt-2 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
                  Per-user limits: total stored size (pool), largest single upload, and uploads per UTC day. Effective per-file
                  limit is the smaller of the global server cap and the user&apos;s single-file quota. Defaults for new users:
                  100&nbsp;MiB pool, 10&nbsp;MiB per file, 10 uploads/day.
                </p>
                <div className="mt-4 overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
                  <table className="min-w-full divide-y divide-zinc-200 text-left text-sm dark:divide-zinc-800">
                    <thead className="bg-zinc-50 dark:bg-zinc-900/80">
                      <tr>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Email
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Role
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Pool (MiB)
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Max file (MiB)
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Daily uploads
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Actions
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-zinc-200 bg-white dark:divide-zinc-800 dark:bg-zinc-950">
                      {adminUsers.map((row) => (
                        <AdminUserQuotaRow key={row.id} u={row} onSaved={load} />
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>
            )}

            <section aria-labelledby="files-heading">
              <div className="flex flex-wrap items-end justify-between gap-3">
                <h2 id="files-heading" className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">
                  Your Luggage
                </h2>
                <p className="text-sm tabular-nums text-zinc-600 dark:text-zinc-400">
                  {nfCount.format(stat.totalKeys)} files · {formatBytes(stat.totalSize)} total
                </p>
              </div>

              {stat.keys.length === 0 ? (
                <p className="mt-4 rounded-lg border border-dashed border-zinc-300 bg-zinc-50 px-4 py-8 text-center text-sm text-zinc-600 dark:border-zinc-700 dark:bg-zinc-900/40 dark:text-zinc-400">
                  No files yet… Upload above to add your first object.
                </p>
              ) : (
                <div className="mt-4 overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
                  <table className="min-w-full divide-y divide-zinc-200 text-left text-sm dark:divide-zinc-800">
                    <thead className="bg-zinc-50 dark:bg-zinc-900/80">
                      <tr>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Key
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Name
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          MIME
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Size
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Active Refs
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Owner ID
                        </th>
                        <th scope="col" className="px-3 py-2 font-medium text-zinc-700 dark:text-zinc-300">
                          Actions
                        </th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-zinc-200 bg-white dark:divide-zinc-800 dark:bg-zinc-950">
                      {stat.keys.map((row) => {
                        const url = publicLuggageUrl(row.key);
                        return (
                          <tr key={row.key}>
                            <td className="max-w-[14rem] min-w-0 px-3 py-2 font-mono text-xs text-zinc-800 dark:text-zinc-200">
                              <span className="block truncate" translate="no" title={row.key}>
                                {row.key}
                              </span>
                            </td>
                            <td className="max-w-[12rem] min-w-0 px-3 py-2 text-zinc-800 dark:text-zinc-200">
                              <span className="block truncate break-words" title={row.filename}>
                                {row.filename}
                              </span>
                            </td>
                            <td className="max-w-[10rem] min-w-0 px-3 py-2 text-xs text-zinc-600 dark:text-zinc-400">
                              <span className="block truncate" translate="no">
                                {row.mimeType || '—'}
                              </span>
                            </td>
                            <td className="whitespace-nowrap px-3 py-2 tabular-nums text-zinc-800 dark:text-zinc-200">
                              {formatBytes(row.fileSize)}
                            </td>
                            <td className="whitespace-nowrap px-3 py-2 tabular-nums text-zinc-800 dark:text-zinc-200">
                              {nfCount.format(row.activeRefs)}
                            </td>
                            <td className="whitespace-nowrap px-3 py-2 tabular-nums text-zinc-800 dark:text-zinc-200" translate="no">
                              {nfCount.format(row.ownerUserId)}
                            </td>
                            <td className="px-3 py-2">
                              <div className="flex flex-wrap gap-2">
                                <a
                                  href={`${url}?download=true`}
                                  className="inline-flex rounded-md border border-zinc-300 bg-white px-2 py-1 text-xs font-medium text-zinc-800 transition hover:bg-zinc-50 dark:border-zinc-600 dark:bg-zinc-900 dark:text-zinc-100 dark:hover:bg-zinc-800"
                                >
                                  Download
                                </a>
                                <CopyLinkButton url={url} />
                                <button
                                  type="button"
                                  className="rounded-md border border-red-200 bg-red-50 px-2 py-1 text-xs font-semibold text-red-800 transition hover:bg-red-100 dark:border-red-900/60 dark:bg-red-950/50 dark:text-red-100 dark:hover:bg-red-950"
                                  onClick={() => openDelete(row)}
                                >
                                  Delete…
                                </button>
                              </div>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </>
        )}
      </main>

      <DeleteConfirmDialog
        target={deleteTarget}
        open={deleteTarget !== null}
        busy={deleteBusy}
        onDismiss={closeDelete}
        onConfirm={() => void confirmDelete()}
      />

      <NewApiKeyDialog
        data={newKeyDialog}
        open={newKeyDialog !== null}
        onDismiss={() => setNewKeyDialog(null)}
      />
    </div>
  );
}
