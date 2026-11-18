import { api } from './api.js';
import { editorTestcases, hydroExportButton } from './problem-authoring.js';
import { button, confirmAction, el, link, pageHeader, setBusy, toast } from './ui.js';

function caseFence(tag, raw, displayOnly = false) {
  const content = raw || '';
  let length = 3;
  for (const run of content.matchAll(/`+/g)) length = Math.max(length, run[0].length + 1);
  const fence = '`'.repeat(length);
  const missingNewline = content && !content.endsWith('\n');
  const options = `${missingNewline ? ' noeol' : ''}${displayOnly ? ' display-only' : ''}`;
  return `${fence}${tag}${options}\n${content}${missingNewline ? '\n' : ''}${fence}`;
}

function markdownFor(problem) {
  const metadata = {
    title: problem.title,
    difficulty: problem.difficulty,
    tags: problem.tags || [],
    time_limit_ms: problem.time_limit_ms,
    memory_limit_mb: problem.memory_limit_mb,
    checker: problem.checker,
    float_eps: problem.float_eps,
    is_public: Boolean(problem.is_public),
    templates: problem.templates || {},
    lang_limits: problem.lang_limits || {},
    solutions: problem.solutions || [],
  };
  const lines = ['---', ...Object.entries(metadata).map(([key, value]) => `${key}: ${JSON.stringify(value)}`), '---'];
  for (const [heading, text] of [['题目描述', problem.description], ['输入格式', problem.input_format], ['输出格式', problem.output_format], ['提示', problem.hint]]) {
    lines.push('', `## ${heading}`, '', text || '');
  }
  lines.push('', '## 测试用例');
  let sampleNumber = 0, testNumber = 0;
  for (const test of editorTestcases(problem)) {
    if (test.stored) {
      lines.push('', caseFence(`test-ref${test.index}`, ''));
      continue;
    }
    const number = test.is_sample ? ++sampleNumber : ++testNumber;
    const prefix = test.is_sample ? '' : 'test-';
    lines.push('', caseFence(`${prefix}input${number}`, test.input, test.sample_only), '', caseFence(`${prefix}output${number}`, test.expected));
    if (test.is_sample && test.explanation) lines.push('', caseFence(`sample-note${number}`, test.explanation));
  }
  return `${lines.join('\n')}\n`;
}

export function renderMarkdownEditor(host, ctx, id, problem) {
  const route = id ? `/admin/problems/${id}/edit` : '/admin/problems/new';
  const editor = el('textarea', { id: 'problem-markdown', class: 'problem-markdown-input', rows: 28, 'aria-label': '题目 Markdown', value: markdownFor(problem) });
  editor.spellcheck = false;
  const errorBox = el('div', { class: 'form-error', role: 'alert', hidden: true });
  let dirty = false, saving = false;
  const guard = () => !dirty || confirmAction('离开编辑页', '还有未保存的修改，确定离开吗？', { label: '放弃修改', danger: true });
  ctx.setLeaveGuard(guard);
  ctx.cleanup(() => ctx.setLeaveGuard(null));
  editor.addEventListener('input', () => { dirty = true; });

  const save = button(id ? '保存修改' : '创建题目', async () => {
    if (saving) return;
    saving = true;
    setBusy(save, true, '正在保存…');
    errorBox.hidden = true;
    try {
      await api(id ? `/api/admin/problems/${id}?compact=1` : '/api/admin/problems?compact=1', {
        method: id ? 'PUT' : 'POST', body: { markdown: editor.value, version: problem.updated_at || '' }, signal: ctx.signal,
      });
      dirty = false;
      toast(id ? '题目已更新' : '题目已创建');
      await ctx.navigate('/admin/problems');
    } catch (error) {
      if (error.name !== 'AbortError') {
        errorBox.textContent = error.message;
        errorBox.hidden = false;
      }
    } finally {
      saving = false;
      setBusy(save, false);
    }
  }, { variant: 'primary', icon: 'check' });

  const hidden = problem.hidden_testcases || [];
  const guide = el('div', { class: 'problem-markdown-guide' },
    el('p', {}, '文件开头的 YAML 编辑标题、标签、资源限制、模板和参考答案。正文使用 Markdown。'),
    el('p', {}, el('code', {}, 'input1 / output1'), ' 是公开样例，', el('code', {}, 'test-input1 / test-output1'), ' 是隐藏测试。增加编号可以继续添加用例。'),
    el('p', {}, el('code', {}, 'sample-note1'), ' 可补充样例解释。在样例输入代码块的标签后加 ', el('code', {}, 'display-only'), ' 可将它设为只展示的样例。'),
    hidden.length && el('p', {}, `${hidden.length} 组隐藏数据已省略正文。保留 `, el('code', {}, 'test-ref编号'), ' 即可沿用该用例。'));

  host.replaceChildren(
    pageHeader(id ? 'Markdown 编辑题目' : 'Markdown 创建题目', '',
      el('div', { class: 'row wrap' }, link('表单编辑', route, { class: 'btn' }),
        link('返回题目管理', '/admin/problems', { class: 'btn' }), id && hydroExportButton(ctx, id), save)),
    el('section', { class: 'card form-section problem-markdown-editor' }, guide, errorBox, editor));
}
