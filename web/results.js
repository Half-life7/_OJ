import { el, icon, button, iconButton, badge, verdict, pending, formatTime, formatMemory, dateTime, copyText, empty, errorState, loading } from './ui.js';
import { api } from './api.js';
import { highlight } from './editor.js';

export function codeView(code, language = '') {
  return el('div', { class: 'code-block source-view' }, el('div',{class:'code-caption'},language || '源代码',iconButton('copy','复制源代码',() => copyText(code))),el('pre',{},el('code',{},highlight(code || '',language))));
}

export function resultView(result, { selfTest = false } = {}) {
  const status = result.status || 'pending';
  const completed = status === 'accepted';
  const sample = selfTest && completed ? result.sample : null;
  const sampleMismatch = sample && !sample.matches;
  const tone = pending(status) ? 'is-pending' : !completed || sampleMismatch ? 'is-error' : selfTest ? '' : 'is-success';
  const glyph = !completed || sampleMismatch ? 'alert' : selfTest ? 'terminal' : 'check';
  const title = selfTest && completed
    ? sample ? `样例 ${sample.index} ${sample.matches ? '通过' : '输出不匹配'}` : '运行完成'
    : verdict(status);
  const note = selfTest && completed
    ? sample ? '这里只核对了当前样例。其他测试点以正式提交的评测结果为准。' : '这是自定义输入。请自行核对程序输出，正式评测以提交结果为准。'
    : selfTest ? '可查看下方的输出或错误信息。' : pending(status) ? '评测结果将自动更新，请稍候。' : `通过 ${result.passed || 0} / ${result.total || 0} 个测试点`;
  const summary = el('div',{class:`result-summary ${tone}`},
    el('div',{class:'row'}, pending(status) ? el('span',{class:'spinner'}) : icon(glyph,24),
      el('div',{},el('strong',{}, title),el('p',{class:'muted'}, note))),
    !pending(status) && el('div',{class:'result-metrics'},el('span',{},icon('clock',15),formatTime(result)),el('span',{},icon('chip',15),formatMemory(result.memory_kb))));
  const root = el('div',{class:'result-view'},summary);
  if (selfTest) {
    root.append(outputBlock('标准输出', result.stdout ?? '', true));
    if (sample) root.append(outputBlock('样例预期输出', sample.expected));
    if (result.stderr) root.append(outputBlock('标准错误 / 编译信息',result.stderr));
    if (result.truncated) root.append(el('p',{class:'notice warning'},'输出过长，已截断显示。'));
    if (result.exit_code != null && result.exit_code !== 0) root.append(badge(`退出码 ${result.exit_code}`,'warning'));
  }
  if (result.message) root.append(outputBlock(status === 'compile_error' ? '编译信息' : '评测信息',result.message));
  const cases = result.case_results || [];
  if (cases.length) {
    const detail = el('div',{class:'case-detail'}), tiles = el('div',{class:'case-tiles','aria-label':'测试点结果'});
    cases.forEach((item,index) => {
      const btn = button('', () => { tiles.querySelectorAll('button').forEach(b => { b.classList.remove('selected'); b.setAttribute('aria-pressed','false'); }); btn.classList.add('selected'); btn.setAttribute('aria-pressed','true'); detail.replaceChildren(el('div',{class:'row spread'},el('strong',{},`${item.is_sample ? '样例' : '测试点'} ${item.index || index+1}`),verdict(item.status)),el('div',{class:'case-metrics'},`耗时 ${formatTime(item)} · 内存 ${formatMemory(item.memory_kb)}`));
        if (item.is_sample) detail.append(outputBlock('输入',item.input || ''),outputBlock('预期输出',item.expected || ''),outputBlock('实际输出',item.actual || ''));
        else detail.append(el('p',{class:'muted'},'隐藏测试点仅显示评测状态与资源消耗。'));
      }, {variant:`case-tile ${item.status === 'accepted' ? 'success' : pending(item.status) ? '' : 'danger'}`,'aria-label':`测试点 ${item.index || index+1}，${item.status}`,'aria-pressed':'false'});
      btn.append(el('span',{},`#${item.index || index+1}`),icon(item.status === 'accepted' ? 'check' : pending(item.status) ? 'clock' : 'close',14),el('small',{},formatTime(item))); tiles.append(btn);
    });
    root.append(el('h3',{},'测试点详情'),tiles,detail); tiles.firstChild?.click();
  }
  return root;
}
export function outputBlock(title, text, copy = false) { return el('section',{class:'output-block'},el('div',{class:'code-caption'},title,copy && iconButton('copy',`复制${title}`,() => copyText(text))),el('pre',{},String(text) || '（无输出）')); }

export async function watchSubmission(id, host, ctx, { includeCode = true, onResult, onRestore } = {}) {
  let stopped = false, timer = 0, last = null;
  const started = Date.now();
  const stop = () => { stopped = true; clearTimeout(timer); };
  ctx.cleanup?.(stop); host.replaceChildren(loading(3));
  async function refresh() {
    if (stopped || ctx.signal?.aborted) return;
    try {
      const result = await api(`/api/submissions/${id}`, { signal:ctx.signal });
      if (stopped || ctx.signal?.aborted) return; last = result;
      const heading = el('div',{class:'row spread result-title'},el('div',{},el('h2',{},`提交 #${result.id}`),el('p',{class:'muted'},`${result.language} · ${dateTime(result.created_at)}`)),onRestore && button('恢复到编辑器',() => onRestore(result),{icon:'refresh',variant:'small'}));
      host.replaceChildren(heading,resultView(result)); if (includeCode) host.append(codeView(result.code,result.language));
      onResult?.(result);
      if (pending(result.status)) timer = setTimeout(refresh,Date.now()-started < 20000 ? 500 : 1000);
    } catch(error) { if (stopped || error.name === 'AbortError') return; const retry = errorState(error,refresh); if (last) host.append(retry); else host.replaceChildren(retry); }
  }
  await refresh(); return stop;
}
