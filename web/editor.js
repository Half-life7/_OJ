import { el, storage } from './ui.js';

const words = new Set(('alignas auto bool break case catch char class const constexpr continue default def del do double else elif enum except extends false False final finally float for from if import in include inline int interface is lambda long namespace new None not null or override pass private protected public raise return short signed sizeof static string struct super switch template this throw throws true True try typedef typename unsigned using var virtual void volatile while with yield and as assert async await boolean byte package print range len self System String').split(' '));
const tokenPattern = /(\/\*[\s\S]*?(?:\*\/|$)|\/\/[^\n]*|\#[^\n]*|"(?:\\[\s\S]|[^"\\])*"|'(?:\\[\s\S]|[^'\\])*'|\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b|\b[A-Za-z_]\w*\b)/g;

export function highlight(code, language = '') {
  const frag = document.createDocumentFragment(); let offset = 0;
  // Very large files stay editable without making highlighting block keystrokes.
  if (code.length > 180000) { frag.append(document.createTextNode(code)); return frag; }
  for (const match of code.matchAll(tokenPattern)) {
    frag.append(document.createTextNode(code.slice(offset, match.index)));
    const text = match[0]; let kind = '';
    if (text.startsWith('//') || text.startsWith('/*') || (text.startsWith('#') && /python|py/.test(language))) kind = 'comment';
    else if (/^["']/.test(text)) kind = 'string';
    else if (/^\d/.test(text)) kind = 'number';
    else if (words.has(text) || text.startsWith('#')) kind = 'keyword';
    frag.append(kind ? el('span', { class: `syntax-${kind}` }, text) : document.createTextNode(text)); offset = match.index + text.length;
  }
  frag.append(document.createTextNode(code.slice(offset))); return frag;
}

/** Stable native editor API: element, getValue, setValue, setLanguage, setFontSize,
 * setReadOnly, focus, onChange (unsubscribe), destroy. All code is rendered as text. */
export function createEditor(container, options = {}) {
  let language = options.language || '', destroyed = false, frame = 0;
  const listeners = new Set(options.onChange ? [options.onChange] : []);
  const lines = el('div', { class: 'editor-line-numbers', 'aria-hidden': 'true' });
  const paint = el('pre', { class: 'editor-highlight', 'aria-hidden': 'true' });
  const input = el('textarea', { class: 'editor-input', spellcheck: false, autocapitalize: 'off', autocomplete: 'off', autocorrect: 'off', wrap: 'off', 'aria-label': options.label || '代码编辑器', value: options.value || '', readOnly: !!options.readOnly });
  const body = el('div', { class: 'editor-body' }, lines, el('div', { class: 'editor-viewport' }, paint, input));
  const position = el('span', {}, '行 1，列 1');
  const state = el('span', { class: 'muted' }, options.readOnly ? '只读' : 'UTF-8 · 4 空格');
  const root = el('div', { class: 'code-editor', style: { '--code-font-size': `${options.fontSize || Number(storage.get('oj.fontSize', '14'))}px` } }, body, el('div', { class: 'editor-status' }, position, state));
  container?.append(root);
  function syncScroll() { paint.scrollTop = input.scrollTop; paint.scrollLeft = input.scrollLeft; lines.scrollTop = input.scrollTop; }
  function updatePosition() { const before = input.value.slice(0, input.selectionStart); position.textContent = `行 ${before.split('\n').length}，列 ${before.length - before.lastIndexOf('\n')}`; }
  function render() { if (destroyed) return; paint.replaceChildren(highlight(input.value + '\n', language)); lines.textContent = Array.from({length: input.value.split('\n').length}, (_,i) => i + 1).join('\n'); syncScroll(); updatePosition(); }
  function changed() { cancelAnimationFrame(frame); frame = requestAnimationFrame(render); for (const listener of listeners) listener(input.value); }
  function insert(text, start = input.selectionStart, end = input.selectionEnd, selectStart, selectEnd) {
    input.setSelectionRange(start, end);
    // execCommand preserves the browser's native undo history for indent and pairs.
    const success = document.execCommand('insertText', false, text);
    if (!success) { input.setRangeText(text, start, end, 'end'); changed(); }
    if (selectStart != null) input.setSelectionRange(selectStart, selectEnd ?? selectStart);
    updatePosition();
  }
  function keydown(event) {
    if (input.readOnly || event.isComposing) return;
    const value = input.value, start = input.selectionStart, end = input.selectionEnd;
    if (event.key === 'Tab') {
      event.preventDefault();
      if (start !== end || event.shiftKey) {
        const lineStart = value.lastIndexOf('\n', start - 1) + 1;
        const selected = value.slice(lineStart, end), rows = selected.split('\n');
        const result = rows.map(row => event.shiftKey ? row.replace(/^( {1,4}|\t)/, '') : `    ${row}`).join('\n');
        const firstDelta = result.split('\n')[0].length - rows[0].length;
        insert(result, lineStart, end, Math.max(lineStart, start + firstDelta), Math.max(lineStart, end + result.length - selected.length));
      } else insert('    ');
    } else if (event.key === 'Enter' && !event.ctrlKey && !event.metaKey) {
      event.preventDefault(); const prefix = value.slice(value.lastIndexOf('\n', start - 1) + 1, start); const indent = prefix.match(/^\s*/)[0];
      const extra = /[\[{:({]\s*$/.test(prefix) ? '    ' : '';
      if (extra && /^[\]})]/.test(value.slice(end))) { const middle = `\n${indent}${extra}`; insert(`${middle}\n${indent}`, start, end, start + middle.length); }
      else insert(`\n${indent}${extra}`);
    } else if (!event.ctrlKey && !event.metaKey && !event.altKey && ['(', '[', '{', '"', "'"].includes(event.key)) {
      if ((event.key === '"' || event.key === "'") && value[start-1] === '\\') return;
      if ((event.key === '"' || event.key === "'") && value[start] === event.key && start === end) { event.preventDefault(); input.setSelectionRange(start+1,start+1); updatePosition(); return; }
      const pairs = { '(': ')', '[': ']', '{': '}', '"': '"', "'": "'" }; event.preventDefault();
      const selected = value.slice(start,end); insert(event.key + selected + pairs[event.key], start, end, start+1, start+1+selected.length);
    } else if ([')', ']', '}'].includes(event.key) && value[start] === event.key && start === end) {
      event.preventDefault(); input.setSelectionRange(start+1,start+1); updatePosition();
    } else if (event.key === 'Backspace' && start === end && start > 0 && ['()', '[]', '{}', '""', "''"].includes(value.slice(start-1,start+1))) {
      event.preventDefault(); insert('',start-1,start+1);
    }
  }
  input.addEventListener('input', changed); input.addEventListener('scroll', syncScroll); input.addEventListener('keydown', keydown);
  input.addEventListener('click', updatePosition); input.addEventListener('keyup', updatePosition); input.addEventListener('select', updatePosition);
  render();
  return {
    element: root, textarea: input,
    getValue: () => input.value,
    setValue(value, { silent = false } = {}) { input.value = String(value ?? ''); input.setSelectionRange(0,0); render(); if (!silent) for (const listener of listeners) listener(input.value); },
    setLanguage(value) { language = value || ''; render(); },
    setFontSize(value) { const size = Math.min(24, Math.max(11, Number(value) || 14)); root.style.setProperty('--code-font-size', `${size}px`); storage.set('oj.fontSize',size); syncScroll(); },
    setReadOnly(value) { input.readOnly = !!value; state.textContent = value ? '只读' : 'UTF-8 · 4 空格'; },
    focus() { input.focus(); },
    onChange(listener) { listeners.add(listener); return () => listeners.delete(listener); },
    destroy() { destroyed = true; cancelAnimationFrame(frame); listeners.clear(); root.remove(); },
  };
}
