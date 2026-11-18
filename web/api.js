export class APIError extends Error {
  constructor(message, status = 0, code = '', fields = null) { super(message); this.name = 'APIError'; this.status = status; this.code = code; this.fields = fields; }
}

let unauthorized = () => {}, forbidden = () => {};
export function configureAPI(handlers) { unauthorized = handlers.unauthorized || unauthorized; forbidden = handlers.forbidden || forbidden; }

export async function api(path, { method = 'GET', body, signal, auth = true, timeout = 180000 } = {}) {
  const controller = new AbortController();
  const relay = () => controller.abort(signal?.reason);
  if (signal?.aborted) relay(); else signal?.addEventListener('abort', relay, { once: true });
  let timedOut = false;
  const timer = setTimeout(() => { timedOut = true; controller.abort(); }, timeout);
  try {
    const headers = { Accept: 'application/json' };
    if (!['GET','HEAD'].includes(method)) headers['X-Requested-With'] = 'oj';
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(path, { method, credentials: 'same-origin', headers, body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal });
    let data = null;
    if (response.status !== 204) { const text = await response.text(); if (text) { try { data = JSON.parse(text); } catch { throw new APIError('服务返回了无法识别的内容，请稍后重试', response.status); } } }
    if (!response.ok) {
      const err = new APIError(data?.error?.message || data?.message || `请求失败 (${response.status})`, response.status, data?.error?.code, data?.error?.fields);
      if (response.status === 401 && auth) unauthorized();
      if (response.status === 403) forbidden(err.message);
      throw err;
    }
    return data;
  } catch (error) {
    if (timedOut) throw new APIError('请求超时，请稍后重试。提交结果可在提交记录中查看。');
    if (error.name === 'AbortError' || error instanceof APIError) throw error;
    throw new APIError('无法连接服务器，请检查网络后重试');
  } finally { clearTimeout(timer); signal?.removeEventListener('abort', relay); }
}

export function query(params) { const q = new URLSearchParams(); for (const [key, value] of Object.entries(params)) if (value !== '' && value != null) q.set(key,String(value)); const text = q.toString(); return text ? `?${text}` : ''; }
