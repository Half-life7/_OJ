export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value == null || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key.startsWith('on') && typeof value === 'function') node.addEventListener(key.slice(2).toLowerCase(), value);
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else if (key === 'style' && typeof value === 'object') Object.assign(node.style, value);
    else if (key in node && !key.startsWith('aria-') && key !== 'list') node[key] = value;
    else node.setAttribute(key, value === true ? '' : String(value));
  }
  const append = child => {
    if (Array.isArray(child)) child.forEach(append);
    else if (child != null && child !== false) node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  };
  children.forEach(append);
  return node;
}

const paths = {
  code: ['m8 5-7 7 7 7', 'm16 5 7 7-7 7', 'm14 3-4 18'],
  book: ['M4 3h15v18H4a2 2 0 0 1 0-4h15', 'M4 3a2 2 0 0 0-2 2v14', 'M7 7h8', 'M7 11h5'],
  activity: ['M3 12h4l3-8 4 16 3-8h4'],
  chart: ['M4 3v17h17', 'M8 16v-4', 'M13 16V7', 'M18 16V4'],
  user: ['M20 21v-2a7 7 0 0 0-14 0v2', 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0'],
  users: ['M16 21v-2a6 6 0 0 0-12 0v2', 'M14 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0', 'M17 3a4 4 0 0 1 0 8', 'M20 15a5 5 0 0 1 2 4v2'],
  layers: ['m12 3 10 5-10 5L2 8Z', 'm2 12 10 5 10-5', 'm2 16 10 5 10-5'],
  list: ['M8 6h13', 'M8 12h13', 'M8 18h13', 'M3 6h.01', 'M3 12h.01', 'M3 18h.01'],
  lock: ['M5 10h14v11H5Z', 'M8 10V6a4 4 0 0 1 8 0v4', 'M12 14v3'],
  plus: ['M12 5v14', 'M5 12h14'],
  search: ['M19 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0', 'm16 16 5 5'],
  menu: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
  close: ['m6 6 12 12', 'M6 18 18 6'],
  chevron: ['m9 5 7 7-7 7'],
  down: ['m6 9 6 6 6-6'],
  back: ['m12 5-7 7 7 7', 'M5 12h15'],
  check: ['m5 12 4 4L19 6'],
  clock: ['M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0', 'M12 6v6l4 2'],
  chip: ['M6 6h12v12H6Z', 'M9 1v5m6-5v5M9 18v5m6-5v5M1 9h5m-5 6h5m12-6h5m-5 6h5'],
  play: ['m7 4 14 8-14 8Z'],
  send: ['m22 2-7 20-4-9-9-4Z', 'm22 2-11 11'],
  refresh: ['M20 7a9 9 0 1 0 1 9', 'M20 2v6h-6'],
  copy: ['M9 9h12v12H9Z', 'M15 5V2H2v13h3'],
  sun: ['M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0', 'M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1 1m12 12 1 1M5 19l1-1M18 6l1-1'],
  moon: ['M21 13A9 9 0 0 1 11 3a9 9 0 1 0 10 10'],
  monitor: ['M2 3h20v14H2Z', 'M8 21h8', 'M12 17v4'],
  eye: ['M2 12s3-7 10-7 10 7 10 7-3 7-10 7S2 12 2 12', 'M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0'],
  eyeoff: ['m3 3 18 18', 'M10 5h2c7 0 10 7 10 7a18 18 0 0 1-4 5', 'M6 6a20 20 0 0 0-4 6s3 7 10 7c2 0 3-.5 4-1'],
  logout: ['M9 3H3v18h6', 'M9 12h12', 'm17 8 4 4-4 4'],
  edit: ['m16 3 5 5-13 13H3v-5Z', 'm14 5 5 5'],
  trash: ['M3 6h18', 'M5 6l1 15h12l1-15', 'M9 6V3h6v3', 'M10 10v7m4-7v7'],
  upload: ['M12 16V3', 'm7 8 5-5 5 5', 'M3 16v5h18v-5'],
  download: ['M12 3v13', 'm7 11 5 5 5-5', 'M3 16v5h18v-5'],
  info: ['M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0', 'M12 11v6', 'M12 7h.01'],
  shield: ['M12 3 20 6v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6Z', 'm9 12 2 2 4-5'],
  alert: ['m12 3 11 18H1Z', 'M12 9v5', 'M12 17h.01'],
  trophy: ['M7 3h10v6a5 5 0 0 1-10 0Z', 'M7 5H3v3a4 4 0 0 0 4 4m10-7h4v3a4 4 0 0 1-4 4', 'M12 14v7', 'M8 21h8'],
  terminal: ['m4 6 6 6-6 6', 'M13 18h7'],
  expand: ['M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5'],
  panel: ['M2 3h20v18H2Z', 'M8 3v18'],
  file: ['M14 2H4v20h16V8Z', 'M14 2v6h6', 'M8 13h8', 'M8 17h6'],
};

export function icon(name, size = 18) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  Object.entries({ viewBox: '0 0 24 24', width: size, height: size, fill: 'none', stroke: 'currentColor', 'stroke-width': '1.7', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true', class: 'icon' }).forEach(([k,v]) => svg.setAttribute(k,v));
  (paths[name] || paths.code).forEach(d => { const p = document.createElementNS(svg.namespaceURI, 'path'); p.setAttribute('d', d); svg.append(p); });
  return svg;
}

export function button(label, onClick, { variant = '', icon: glyph, ...attrs } = {}) {
  return el('button', { type: 'button', class: `btn ${variant}`, onclick: onClick, ...attrs }, glyph && icon(glyph), label);
}
export function iconButton(name, label, onclick, attrs = {}) { return button('', onclick, { variant: 'icon-btn', icon: name, title: label, 'aria-label': label, ...attrs }); }
export function link(label, href, attrs = {}) { return el('a', { href, ...attrs }, label); }
export function select(options, value, onChange, attrs = {}) {
  const node = el('select', { onchange: onChange, ...attrs }, options.map(o => el('option', { value: typeof o === 'string' ? o : o.value }, typeof o === 'string' ? o : o.label)));
  node.value = value ?? ''; return node;
}
export function field(label, input, help = '', required = false) {
  if (!input.id) input.id = `field-${++field.count}`;
  return el('div', { class: 'field' }, el('label', { htmlFor: input.id }, label, required && el('span', { class: 'required' }, ' *')), input, help && el('small', { class: 'muted' }, help));
}
field.count = 0;
export function badge(text, kind = '') { return el('span', { class: `badge ${kind}` }, text); }
export const DIFFICULTIES = [{ value: 'easy', label: '简单' }, { value: 'medium', label: '中等' }, { value: 'hard', label: '困难' }];
export const VERDICTS = {
  pending: ['等待评测', 'pending'], queued: ['等待评测', 'pending'], running: ['正在运行', 'judging'], judging: ['正在评测', 'judging'], compiling: ['正在编译', 'judging'],
  accepted: ['通过', 'success'], wrong_answer: ['答案错误', 'danger'], time_limit_exceeded: ['超出时间限制', 'warning'], memory_limit_exceeded: ['超出内存限制', 'warning'], runtime_error: ['运行错误', 'danger'], compile_error: ['编译错误', 'danger'], system_error: ['系统错误', 'danger'], output_limit_exceeded: ['超出输出限制', 'warning'], cancelled: ['已取消', 'muted'],
};
export function verdict(status) { const v = VERDICTS[status] || [status || '等待评测', 'muted']; return badge(v[0], v[1]); }
export function pending(status) { return ['pending', 'queued', 'judging', 'running', 'compiling'].includes(status); }
export function difficulty(value) { return badge(DIFFICULTIES.find(x => x.value === value)?.label || value, value); }
export function formatTime(data) { if (data?.time_us != null && Number(data.time_us) > 0) return `${(data.time_us / 1000).toLocaleString('zh-CN', { maximumFractionDigits: 3 })} ms`; return `${Number(data?.time_ms || 0).toLocaleString('zh-CN', { maximumFractionDigits: 3 })} ms`; }
export function formatMemory(kb) { return `${(Number(kb || 0) / 1024).toLocaleString('zh-CN', { maximumFractionDigits: 2 })} MB`; }
export function dateTime(value) { if (!value) return '—'; const d = new Date(value); return Number.isNaN(d.getTime()) ? String(value) : d.toLocaleString('zh-CN', { hour12: false }); }
export function debounce(fn, ms) { let timer; const run = (...args) => { clearTimeout(timer); timer = setTimeout(() => fn(...args), ms); }; run.cancel = () => clearTimeout(timer); return run; }
export const storage = { get(key, fallback = '') { try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; } }, set(key, value) { try { localStorage.setItem(key, String(value)); return true; } catch { return false; } }, remove(key) { try { localStorage.removeItem(key); } catch {} } };
export function toast(message, kind = 'success') {
  const host = document.getElementById('toasts');
  const node = el('div', { class: `toast ${kind}`, role: kind === 'danger' ? 'alert' : 'status' }, icon(kind === 'danger' ? 'alert' : 'check'), el('span', {}, message));
  node.append(iconButton('close', '关闭提示', () => node.remove())); host.append(node); setTimeout(() => node.remove(), kind === 'danger' ? 7000 : 4000);
}
export async function copyText(value) {
  try {
    if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(String(value));
    else { const input = el('textarea', { value: String(value), class: 'clipboard-fallback' }); document.body.append(input); input.select(); const ok = document.execCommand('copy'); input.remove(); if (!ok) throw new Error(); }
    toast('已复制到剪贴板');
  } catch { toast('复制失败，请手动选择并复制', 'danger'); }
}
export function setBusy(btn, busy, label = '处理中…') {
  if (busy) { btn._content = [...btn.childNodes]; btn.replaceChildren(el('span', { class: 'spinner' }), document.createTextNode(label)); }
  else if (btn._content) btn.replaceChildren(...btn._content);
  btn.disabled = busy;
}
export function loading(lines = 6) { return el('div', { class: 'skeleton-list', role: 'status', 'aria-label': '正在加载' }, Array.from({ length: lines }, () => el('div', { class: 'skeleton' }))); }
export function empty(title = '这里暂时还没有内容', description = '', action) { return el('div', { class: 'empty-state' }, el('div', { class: 'empty-icon' }, icon('file', 30)), el('h3', {}, title), el('p', {}, description), action); }
export function errorState(error, retry) { return el('div', { class: 'error-state', role: 'alert' }, icon('alert', 28), el('h3', {}, '加载遇到了一点问题'), el('p', {}, error?.message || '请检查网络后重试'), retry && button('重新加载', retry, { icon: 'refresh' })); }
export function pageHeader(title, subtitle = '', action) { return el('div', { class: 'page-heading' }, el('div', {}, el('h1', {}, title), subtitle && el('p', { class: 'muted' }, subtitle)), action); }
export function pagination(total, page, size, onPage) {
  const count = Math.max(1, Math.ceil(total / size));
  const pages = [...new Set([1, page - 1, page, page + 1, count])].filter(n => n > 0 && n <= count).sort((a,b) => a-b);
  const controls = [];
  pages.forEach((n,i) => { if (i && n - pages[i-1] > 1) controls.push(el('span', { class: 'muted' }, '…')); controls.push(button(String(n), () => onPage(n), { variant: n === page ? 'page-btn current' : 'page-btn', 'aria-label': `第 ${n} 页`, 'aria-current': n === page ? 'page' : null })); });
  return el('div', { class: 'pagination' }, el('span', { class: 'muted' }, `共 ${total} 项 · ${page} / ${count} 页`), el('div', { class: 'row' }, button('上一页', () => onPage(page - 1), { disabled: page <= 1, variant: 'small' }), controls, button('下一页', () => onPage(page + 1), { disabled: page >= count, variant: 'small' })));
}
export function modal(title, content, actions = [], { wide = false, onClose } = {}) {
  const previous = document.activeElement;
  const dialog = el('dialog', { class: `dialog ${wide ? 'wide' : ''}`, 'aria-label': title });
  const close = () => { onClose?.(); dialog.close(); dialog.remove(); previous?.focus?.(); };
  dialog.append(el('div', { class: 'dialog-header' }, el('h2', {}, title), iconButton('close', '关闭对话框', close)), el('div', { class: 'dialog-body' }, content));
  if (actions.length) dialog.append(el('div', { class: 'dialog-footer' }, actions));
  dialog.addEventListener('cancel', e => { e.preventDefault(); close(); });
  dialog.addEventListener('click', e => { if (e.target === dialog) { const r = dialog.getBoundingClientRect(); if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) close(); } });
  document.getElementById('dialogs').append(dialog); dialog.showModal(); return { dialog, close };
}
export function confirmAction(title, message, { danger = false, label = '确认' } = {}) {
  return new Promise(resolve => {
    let settled = false;
    const finish = value => { settled = true; resolve(value); box.close(); };
    const box = modal(title, el('p', {}, message), [button('取消', () => finish(false)), button(label, () => finish(true), { variant: danger ? 'danger' : 'primary' })], { onClose: () => { if (!settled) resolve(false); } });
  });
}

function safeHref(value) { try { const u = new URL(value, location.origin); return ['http:', 'https:', 'mailto:'].includes(u.protocol) ? u.href : null; } catch { return null; } }
function inline(text, depth = 0) {
  const frag = document.createDocumentFragment();
  if (depth > 4) { frag.append(document.createTextNode(text)); return frag; }
  const regex = /(`[^`\n]+`|\*\*[^*\n]+\*\*|__[^_\n]+__|\*[^*\n]+\*|\[[^\]\n]+\]\([^\s)]+\))/g;
  let cursor = 0;
  for (const match of text.matchAll(regex)) {
    frag.append(document.createTextNode(text.slice(cursor, match.index))); const token = match[0];
    if (token.startsWith('`')) frag.append(el('code', {}, token.slice(1,-1)));
    else if (token.startsWith('**') || token.startsWith('__')) frag.append(el('strong', {}, inline(token.slice(2,-2), depth+1)));
    else if (token.startsWith('*')) frag.append(el('em', {}, inline(token.slice(1,-1), depth+1)));
    else { const m = token.match(/^\[([^\]]+)\]\((.+)\)$/); const href = safeHref(m[2]); frag.append(href ? el('a', { href, target: '_blank', rel: 'noopener noreferrer' }, m[1]) : document.createTextNode(m[1])); }
    cursor = match.index + token.length;
  }
  frag.append(document.createTextNode(text.slice(cursor))); return frag;
}

// Deliberately creates DOM nodes only: source HTML and image markup remain inert text.
export function markdown(source = '') {
  const root = el('div', { class: 'markdown' });
  const lines = String(source).replace(/\r\n?/g, '\n').split('\n');
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }
    if (/^\s*```/.test(line)) {
      const language = line.trim().slice(3).trim(); const code = []; i++;
      while (i < lines.length && !/^\s*```/.test(lines[i])) code.push(lines[i++]); i++;
      const text = code.join('\n'); root.append(el('div', { class: 'code-block' }, el('div', { class: 'code-caption' }, language || '代码', iconButton('copy', '复制代码', () => copyText(text))), el('pre', {}, el('code', {}, text)))); continue;
    }
    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) { root.append(el(`h${Math.min(6, heading[1].length + 1)}`, {}, inline(heading[2]))); i++; continue; }
    if (/^\s*(---+|\*\*\*+)\s*$/.test(line)) { root.append(el('hr')); i++; continue; }
    if (/^>\s?/.test(line)) { const q = []; while (i < lines.length && /^>\s?/.test(lines[i])) q.push(lines[i++].replace(/^>\s?/, '')); root.append(el('blockquote', {}, markdown(q.join('\n')))); continue; }
    if (/^\s*([-*+] |\d+\. )/.test(line)) {
      const ordered = /^\s*\d+\. /.test(line); const list = el(ordered ? 'ol' : 'ul');
      while (i < lines.length && /^\s*([-*+] |\d+\. )/.test(lines[i])) list.append(el('li', {}, inline(lines[i++].replace(/^\s*([-*+] |\d+\. )/, '')))); root.append(list); continue;
    }
    if (line.includes('|') && i + 1 < lines.length && /^\s*\|?\s*:?-{3,}/.test(lines[i+1])) {
      const cells = row => row.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map(s => s.trim());
      const table = el('table', {}, el('thead', {}, el('tr', {}, cells(line).map(x => el('th', {}, inline(x)))))); const body = el('tbody'); i += 2;
      while (i < lines.length && lines[i].includes('|') && lines[i].trim()) body.append(el('tr', {}, cells(lines[i++]).map(x => el('td', {}, inline(x))))); table.append(body); root.append(el('div', { class: 'table-scroll' }, table)); continue;
    }
    const paragraph = [line]; i++;
    while (i < lines.length && lines[i].trim() && !/^(#{1,6}\s|\s*```|>|\s*([-*+] |\d+\. ))/.test(lines[i])) paragraph.push(lines[i++]);
    const p = el('p'); paragraph.forEach((l, n) => { if (n) p.append(el('br')); p.append(inline(l)); }); root.append(p);
  }
  return root;
}
