import {
  el, icon, iconButton, button, link, select, field, badge, verdict,
  formatTime, formatMemory, dateTime, debounce, toast, setBusy, loading, empty,
  errorState, pageHeader, pagination, confirmAction, markdown,
} from './ui.js';
import { api, query } from './api.js';
import { renderMarkdownEditor } from './admin-markdown.js';
import { editorTestcases, hydroExportButton } from './problem-authoring.js';

const FALLBACK_LANGUAGES = [
  { id: 'cpp17', display: 'C++17', highlight: 'cpp', time_factor: 1, time_extra_ms: 0, memory_factor: 1, memory_extra_mb: 0, template: '' },
  { id: 'python3', display: 'Python 3', highlight: 'python', time_factor: 3, time_extra_ms: 0, memory_factor: 1, memory_extra_mb: 0, template: '' },
  { id: 'java', display: 'Java', highlight: 'java', time_factor: 2, time_extra_ms: 0, memory_factor: 1, memory_extra_mb: 64, template: '' },
];

function defaultProblem() {
  return {
    title: '', difficulty: 'medium', tags: [], description: '', input_format: '',
    output_format: '', hint: '', checker: 'trim', float_eps: 1e-6,
    time_limit_ms: 1000, memory_limit_mb: 256, is_public: true,
    testcases: [
      { input: '', expected: '', explanation: '', is_sample: true },
      { input: '', expected: '', explanation: '', is_sample: false },
      { input: '', expected: '', explanation: '', is_sample: false },
    ],
    templates: {}, lang_limits: {}, solutions: [],
  };
}

function inputControl(attrs = {}) {
  return el('input', attrs);
}

function textarea(attrs = {}) {
  return el('textarea', { spellcheck: false, ...attrs });
}

function section(title, subtitle, ...children) {
  return el('section', { class: 'form-section card' },
    el('div', { class: 'form-section-heading' },
      el('div', {}, el('h2', {}, title), subtitle && el('p', { class: 'muted' }, subtitle))),
    ...children);
}

function setInlineError(control, message) {
  const parent = control.closest('.field, .testcase-editor, .solution-row') || control.parentElement;
  control.toggleAttribute('aria-invalid', !!message);
  let note = parent?.querySelector(`[data-error-for="${control.id}"]`);
  if (!message) { note?.remove(); return; }
  if (!note && parent) {
    note = el('small', { class: 'inline-error', role: 'alert', dataset: { errorFor: control.id } });
    parent.append(note);
  }
  if (note) note.textContent = message;
}

export async function renderAdminEditor(host, ctx, id) {
  ctx.setTitle(id ? '编辑题目' : '创建题目');
  host.replaceChildren(loading(8));
  let problem;
  try {
    problem = id
      ? await api(`/api/admin/problems/${id}?compact=1`, { signal: ctx.signal })
      : defaultProblem();
  } catch (error) {
    if (error.name !== 'AbortError') host.replaceChildren(errorState(error, () => ctx.navigate(location.href, { replace: true, force: true })));
    return;
  }
  if (!ctx.alive()) return;
  if (new URLSearchParams(location.search).get('mode') === 'markdown') {
    renderMarkdownEditor(host, ctx, id, problem);
    return;
  }

  let dirty = false;
  let saving = false;
  let tags = [...(problem.tags || [])];
  const testcases = editorTestcases(problem);
  let solutions = (problem.solutions || []).map(solution => ({ ...solution }));
  const knownLanguages = new Map([...(ctx.languages?.length ? ctx.languages : FALLBACK_LANGUAGES)].map(item => [item.id, item]));
  for (const languageID of [
    ...Object.keys(problem.templates || {}),
    ...Object.keys(problem.lang_limits || {}),
    ...solutions.map(solution => solution.language),
  ]) {
    if (!knownLanguages.has(languageID)) {
      knownLanguages.set(languageID, { id: languageID, display: languageID, time_factor: 1, memory_factor: 1, time_extra_ms: 0, memory_extra_mb: 0 });
    }
  }
  const languages = [...knownLanguages.values()];
  const templates = { ...(problem.templates || {}) };
  const localLimits = { ...(problem.lang_limits || {}) };

  const title = inputControl({ type: 'text', id: 'problem-title', maxLength: 120, value: problem.title || '', required: true, placeholder: '例如：区间最大子段和' });
  const difficultySelect = select([{ value: 'easy', label: '简单' }, { value: 'medium', label: '中等' }, { value: 'hard', label: '困难' }], problem.difficulty, null);
  const timeLimit = inputControl({ type: 'number', id: 'problem-time', min: 1, max: 30000, step: 1, value: problem.time_limit_ms || 1000 });
  const memoryLimit = inputControl({ type: 'number', id: 'problem-memory', min: 16, max: 2048, step: 1, value: problem.memory_limit_mb || 256 });
  const publicToggle = inputControl({ type: 'checkbox', id: 'problem-public', checked: !!problem.is_public });
  const description = textarea({ id: 'problem-description', rows: 14, value: problem.description || '', placeholder: '支持 Markdown；原始 HTML 不会执行。' });
  const inputFormat = textarea({ id: 'problem-input-format', rows: 4, value: problem.input_format || '' });
  const outputFormat = textarea({ id: 'problem-output-format', rows: 4, value: problem.output_format || '' });
  const hint = textarea({ id: 'problem-hint', rows: 4, value: problem.hint || '' });
  const checker = select([
    { value: 'exact', label: '精确匹配' },
    { value: 'trim', label: '忽略行末空白与文末空行' },
    { value: 'float', label: '浮点容差' },
  ], problem.checker || 'trim', () => { floatWrap.hidden = checker.value !== 'float'; markDirty(); });
  const floatEps = inputControl({ type: 'number', id: 'problem-float-eps', min: 0.000000001, max: 1, step: 'any', value: problem.float_eps || 1e-6 });
  const floatWrap = field('浮点误差', floatEps, '按相对误差或绝对误差比较，推荐 1e-6。');
  floatWrap.hidden = checker.value !== 'float';
  const tagInput = inputControl({ type: 'text', id: 'problem-tag-input', maxLength: 24, placeholder: '输入标签后按回车' });
  const tagList = el('div', { class: 'tag-editor-list', 'aria-live': 'polite' });
  const tagSuggestions = el('datalist', { id: 'tag-suggestions' });
  tagInput.setAttribute('list', 'tag-suggestions');
  const livePreview = el('div', { class: 'markdown-preview markdown' });
  const testcaseHost = el('div', { class: 'testcase-list' });
  const validationSummary = el('div', { class: 'form-error', role: 'alert', hidden: true });
  const importInput = inputControl({ type: 'file', multiple: true, accept: '.in,.out,.ans,text/plain', class: 'visually-hidden', id: 'testcase-import' });
  const limitsBody = el('tbody');
  const limitsPreview = el('div', { class: 'limits-preview-grid' });
  const templatesBody = el('div', { class: 'template-list' });
  const solutionsHost = el('div', { class: 'solution-list' });
  const verifyHost = el('div', { class: 'verify-results' });
  const saveButton = button(id ? '保存修改' : '创建题目', () => saveProblem(), { variant: 'primary', icon: 'check' });
  const verifyButton = button('验证测试数据', verifyProblem, { icon: 'activity' });

  const form = el('form', { class: 'admin-editor', noValidate: true });
  form.append(
    section('基本信息', '标题、难度和资源基准会显示在公开题库中。',
      el('div', { class: 'form-grid two' },
        field('题目标题', title, '1–120 个字符。', true),
        field('难度', difficultySelect, '', true)),
      el('div', { class: 'form-grid two' },
        field('基准时间限制（ms）', timeLimit, '以 C++17 为基准，其他语言由倍率或覆盖值计算。', true),
        field('基准内存限制（MB）', memoryLimit, '范围 16–2048 MB。', true)),
      el('div', { class: 'form-grid two' },
        el('div', { class: 'field' }, el('label', { htmlFor: 'problem-public' }, '公开题目'),
          el('label', { class: 'switch-row' }, publicToggle, el('span', {}, '保存后普通用户可在题库中看到'))),
        el('div', { class: 'field' }, el('label', { htmlFor: 'problem-tag-input' }, '标签'),
          tagInput, tagSuggestions, tagList))),
    section('题目说明', '描述支持安全 Markdown 子集；代码块、表格、列表与链接可直接预览。',
      el('div', { class: 'editor-preview-grid' },
        el('div', { class: 'field' }, el('label', { htmlFor: 'problem-description' }, '题目描述 ', el('span', { class: 'required' }, '*')), description),
        el('div', { class: 'preview-panel' }, el('span', { class: 'preview-label' }, '实时预览'), livePreview)),
      el('div', { class: 'form-grid two' },
        field('输入格式', inputFormat),
        field('输出格式', outputFormat)),
      field('提示', hint, '可留空。')),
    section('测试数据', '至少 1 组参与判题的测试。可添加多个公开样例，单用例输入与输出各不超过 1 MB。',
      el('div', { class: 'testcase-toolbar' },
        el('div', { class: 'row' },
          button('添加样例', () => addTestcase(true), { variant: 'small', icon: 'plus' }),
          button('添加用例', () => addTestcase(false), { variant: 'small', icon: 'plus' }),
          button('导入 .in / .out', () => importInput.click(), { variant: 'small', icon: 'upload' })),
        importInput),
      testcaseHost,
      el('div', { class: 'form-grid two checker-row' },
        field('判题方式', checker),
        floatWrap)),
    section('各语言资源限制', '覆盖值留空时自动使用 languages.yaml 中的语言倍率。',
      el('div', { class: 'table-scroll' }, el('table', { class: 'data-table compact-table' },
        el('thead', {}, el('tr', {}, el('th', {}, '语言'), el('th', {}, '时间上限'), el('th', {}, '内存上限'), el('th', {}, '有效限制预览'))),
        limitsBody)),
      limitsPreview),
    section('语言起始模板', '留空时使用 languages.yaml 的默认模板。',
      templatesBody),
    section('参考答案（仅管理员可见）', '用于验证测试数据，最多 3 份；每种语言一份。',
      solutionsHost,
      button('添加参考答案', addSolution, { variant: 'small', icon: 'plus' })),
    validationSummary,
  );

  host.replaceChildren(
    pageHeader(id ? '编辑题目' : '创建题目', id ? `题目 #${id} · 每次保存都会校验全部测试数据配置。` : '设计数据、设置语言资源上限并验证参考答案。',
      el('div', { class: 'row wrap' }, link('返回题目管理', '/admin/problems', { class: 'btn' }),
        link('Markdown 编辑', `${id ? `/admin/problems/${id}/edit` : '/admin/problems/new'}?mode=markdown`, { class: 'btn' }),
        id && hydroExportButton(ctx, id), verifyButton, saveButton)),
    form,
    el('section', { class: 'card verify-panel' }, el('div', { class: 'card-heading' }, el('h2', {}, '数据验证结果')), verifyHost));

  renderTags();
  renderTestcases();
  renderLimits();
  renderTemplates();
  renderSolutions();
  renderPreview();
  loadTagSuggestions();
  form.addEventListener('input', event => { markDirty(); if (event.target === description) renderPreview(); updateLimitPreview(); });
  form.addEventListener('change', event => { markDirty(); if (event.target === description) renderPreview(); updateLimitPreview(); });
  form.addEventListener('submit', event => { event.preventDefault(); saveProblem(); });
  ctx.setLeaveGuard(unsavedGuard);
  ctx.cleanup(() => ctx.setLeaveGuard(null));

  function unsavedGuard() {
    return !dirty || confirmAction('离开编辑页', '还有未保存的修改，确定离开吗？', { label: '放弃修改', danger: true });
  }

  function markDirty() {
    dirty = true;
    ctx.setLeaveGuard(unsavedGuard);
  }

  async function loadTagSuggestions() {
    try {
      const data = await api('/api/tags', { signal: ctx.signal });
      tagSuggestions.replaceChildren(...(data.items || []).map(tag => el('option', { value: tag })));
    } catch (error) {
      if (error.name !== 'AbortError') console.error(error);
    }
  }

  function renderPreview() {
    livePreview.replaceChildren(markdown(description.value || '*开始输入题目描述…*'));
  }

  function renderTags() {
    tagList.replaceChildren(...tags.map(tag => el('span', { class: 'tag-chip' },
      el('span', {}, tag), iconButton('close', `删除标签 ${tag}`, () => {
        tags = tags.filter(item => item !== tag); renderTags(); markDirty();
      }, { class: 'tag-remove' }))));
    if (!tags.length) tagList.append(el('span', { class: 'muted small-text' }, '还没有标签'));
  }

  function addTag(raw) {
    const value = raw.trim();
    if (!value) return;
    if ([...value].length > 24) { toast('标签最多 24 个字符', 'danger'); return; }
    if (!tags.includes(value) && tags.length < 12) tags.push(value);
    if (tags.length >= 12 && !tags.includes(value)) toast('每道题最多 12 个标签', 'danger');
    markDirty(); renderTags();
  }

  tagInput.addEventListener('keydown', event => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault(); addTag(tagInput.value); tagInput.value = '';
    } else if (event.key === 'Backspace' && !tagInput.value && tags.length) {
      tags.pop(); renderTags(); markDirty();
    }
  });
  tagInput.addEventListener('change', () => { addTag(tagInput.value); tagInput.value = ''; });

  function addTestcase(sample) {
    if (testcases.length >= 200) { toast('每题最多 200 个用例（含样例）', 'danger'); return; }
    testcases.push({ input: '', expected: '', explanation: '', is_sample: sample });
    renderTestcases(); markDirty();
    const number = testcases.filter(test => test.is_sample === sample).length;
    requestAnimationFrame(() => testcaseHost.querySelector(`[aria-label="${sample ? '样例' : '用例'} ${number} 输入"]`)?.focus());
  }

  function renderTestcases() {
    testcaseHost.replaceChildren();
    const samples = testcases.filter(test => test.is_sample);
    const hidden = testcases.filter(test => !test.is_sample);
    if (samples.length) testcaseHost.append(el('h3', { class: 'subsection-title' }, '样例', badge(String(samples.length), 'accent')));
    samples.forEach((test, index) => testcaseHost.append(testcaseCard(test, index)));
    if (hidden.length) testcaseHost.append(el('h3', { class: 'subsection-title' }, '隐藏测试用例', badge(String(hidden.length), 'muted')));
    hidden.forEach((test, index) => testcaseHost.append(testcaseCard(test, index)));
    if (!testcases.length) testcaseHost.append(empty('还没有测试数据', '至少添加一组参与判题的测试。'));
  }

  function testcaseCard(test, groupIndex) {
    const remove = iconButton('trash', '删除此测试数据', () => {
      testcases.splice(testcases.indexOf(test), 1);
      renderTestcases(); markDirty();
    }, { class: 'compact-icon danger-text' });
    const caption = badge(`${test.is_sample ? '样例' : '用例'} ${groupIndex + 1}`, test.is_sample ? 'accent' : 'muted');
    if (test.stored || test.collapsed) {
      const open = button('加载用例', async () => {
        if (test.stored) {
          setBusy(open, true, '正在加载…');
          try {
            const item = await api(`/api/admin/problems/${id}/testcases/${test.index}?version=${encodeURIComponent(problem.updated_at)}`, { signal: ctx.signal });
            Object.assign(test, item, { stored: false });
          } catch (error) {
            if (error.name !== 'AbortError') toast(error.message, 'danger');
            return;
          } finally {
            setBusy(open, false);
          }
        }
        test.collapsed = false;
        renderTestcases();
      }, { variant: 'small' });
      const size = test.input_bytes + test.output_bytes;
      return el('article', { class: 'testcase-editor testcase-summary' },
        el('div', { class: 'row wrap' }, caption, el('span', { class: 'muted small-text' }, `${size.toLocaleString('zh-CN')} 字节`)),
        el('div', { class: 'row' }, open, remove));
    }
    const caseInput = textarea({ rows: 4, value: test.input || '', 'aria-label': `${test.is_sample ? '样例' : '用例'} ${groupIndex + 1} 输入`, placeholder: '输入数据' });
    const expected = textarea({ rows: 4, value: test.expected || '', 'aria-label': `${test.is_sample ? '样例' : '用例'} ${groupIndex + 1} 输出`, placeholder: '期望输出' });
    const explanation = textarea({ rows: 3, value: test.explanation || '', 'aria-label': '样例解释', placeholder: '可选的样例解释' });
    caseInput.addEventListener('input', () => { test.input = caseInput.value; });
    expected.addEventListener('input', () => { test.expected = expected.value; });
    explanation.addEventListener('input', () => { test.explanation = explanation.value; });
    const card = el('article', { class: 'testcase-editor' },
      el('div', { class: 'testcase-heading' },
        el('div', { class: 'row' }, caption,
          el('span', { class: 'muted small-text' }, `${new TextEncoder().encode(caseInput.value).length + new TextEncoder().encode(expected.value).length} 字节`)),
        el('div', { class: 'row wrap' },
          labelCheckbox('作为样例', test.is_sample, checked => {
            test.is_sample = checked;
            if (!checked) test.sample_only = false;
            renderTestcases(); markDirty();
          }),
          test.is_sample && labelCheckbox('仅在题面展示', test.sample_only, checked => { test.sample_only = checked; markDirty(); }), remove)),
      el('div', { class: 'testcase-io-grid' }, field('输入', caseInput), field('期望输出', expected)),
      test.is_sample && field('解释', explanation));
    return card;
  }

  function labelCheckbox(text, checked, onChange) {
    const box = inputControl({ type: 'checkbox', checked });
    box.addEventListener('change', () => onChange(box.checked));
    return el('label', { class: 'checkbox-label' }, box, el('span', {}, text));
  }

  importInput.addEventListener('change', async () => {
    const files = [...importInput.files];
    importInput.value = '';
    if (!files.length) return;
    const byStem = new Map();
    for (const file of files) {
      const match = file.name.match(/^(.*)\.(in|out|ans)$/i);
      if (!match) continue;
      const entry = byStem.get(match[1]) || {};
      if (match[2].toLowerCase() === 'in') entry.inputFile = file; else entry.outputFile = file;
      byStem.set(match[1], entry);
    }
    const imported = [];
    const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
    const encoder = new TextEncoder();
    let total = testcases.reduce((size, test) => size + (test.stored
      ? test.input_bytes + test.output_bytes
      : encoder.encode(test.input).length + encoder.encode(test.expected).length), 0);
    for (const [stem, pair] of [...byStem.entries()].sort((a, b) => a[0].localeCompare(b[0], 'zh-CN', { numeric: true }))) {
      if (!pair.inputFile || !pair.outputFile) continue;
      if (pair.inputFile.size > 1 << 20 || pair.outputFile.size > 1 << 20) {
        toast(`${stem} 超过单用例 1 MB 限制，已跳过`, 'danger');
        continue;
      }
      total += pair.inputFile.size + pair.outputFile.size;
      if (total > 16 << 20) { toast('单题测试数据总量不得超过 16 MB', 'danger'); return; }
      let inputText, outputText;
      try {
        const buffers = await Promise.all([pair.inputFile.arrayBuffer(), pair.outputFile.arrayBuffer()]);
        [inputText, outputText] = buffers.map(buffer => decoder.decode(buffer));
      } catch {
        toast(`${stem} 无法按 UTF-8 读取`, 'danger');
        return;
      }
      imported.push({ input: inputText, expected: outputText, explanation: '', is_sample: false,
        collapsed: true, input_bytes: pair.inputFile.size, output_bytes: pair.outputFile.size });
    }
    if (!imported.length) { toast('没有找到配对的 .in / .out（或 .ans）文件', 'danger'); return; }
    if (testcases.length + imported.length > 200) { toast('每题最多 200 个用例（含样例）', 'danger'); return; }
    testcases.push(...imported); renderTestcases(); markDirty();
    toast(`已导入 ${imported.length} 组测试数据`);
  });

  function renderLimits() {
    limitsBody.replaceChildren();
    for (const language of languages) {
      const limit = localLimits[language.id] || { time_limit_ms: 0, memory_limit_mb: 0 };
      const time = inputControl({ type: 'number', min: 1, max: 120000, step: 1, value: limit.time_limit_ms || '', placeholder: '自动' });
      const memory = inputControl({ type: 'number', min: 16, max: 4096, step: 1, value: limit.memory_limit_mb || '', placeholder: '自动' });
      const effective = el('td', { class: 'mono small-text effective-limit' });
      const sync = () => {
        localLimits[language.id] = {
          time_limit_ms: Number(time.value) || 0,
          memory_limit_mb: Number(memory.value) || 0,
        };
        renderEffectiveCell(effective, language);
        updateLimitPreview();
      };
      time.addEventListener('input', sync); memory.addEventListener('input', sync);
      limitsBody.append(el('tr', {}, el('td', {}, el('strong', {}, language.display), el('div', { class: 'muted mono small-text' }, language.id)),
        el('td', {}, time, el('span', { class: 'input-unit' }, 'ms')),
        el('td', {}, memory, el('span', { class: 'input-unit' }, 'MB')),
        effective));
      renderEffectiveCell(effective, language);
    }
    updateLimitPreview();
  }

  function effectiveLimit(language) {
    const override = localLimits[language.id] || {};
    const baseTime = Math.max(1, Number(timeLimit.value) || 1);
    const baseMemory = Math.max(16, Number(memoryLimit.value) || 16);
    return {
      time_limit_ms: Number(override.time_limit_ms) || Math.min(120000, Math.ceil(baseTime * (Number(language.time_factor) || 1) + (Number(language.time_extra_ms) || 0))),
      memory_limit_mb: Number(override.memory_limit_mb) || Math.min(4096, Math.ceil(baseMemory * (Number(language.memory_factor) || 1) + (Number(language.memory_extra_mb) || 0))),
    };
  }

  function renderEffectiveCell(cell, language) {
    const limit = effectiveLimit(language);
    cell.replaceChildren(el('span', {}, `${limit.time_limit_ms} ms`), el('span', { class: 'muted' }, ` / ${limit.memory_limit_mb} MB`));
  }

  function updateLimitPreview() {
    limitsBody.querySelectorAll('tr').forEach((row, index) => {
      if (languages[index] && row.lastElementChild) renderEffectiveCell(row.lastElementChild, languages[index]);
    });
    limitsPreview.replaceChildren(...languages.map(language => {
      const limit = effectiveLimit(language);
      return el('div', { class: 'limit-preview-card' }, el('strong', {}, language.display),
        el('span', { class: 'mono' }, `${limit.time_limit_ms} ms · ${limit.memory_limit_mb} MB`));
    }));
  }

  function renderTemplates() {
    templatesBody.replaceChildren(...languages.map(language => {
      const code = templates[language.id] || '';
      const editor = textarea({ rows: 8, value: code, spellcheck: false, placeholder: `留空则使用 ${language.display} 默认模板`, 'aria-label': `${language.display} 起始模板` });
      editor.addEventListener('input', () => {
        if (editor.value) templates[language.id] = editor.value; else delete templates[language.id];
      });
      return el('details', { class: 'template-editor' },
        el('summary', {}, el('span', {}, language.display), el('span', { class: 'muted small-text' }, code ? '已自定义' : '使用默认模板')),
        editor);
    }));
  }

  function addSolution() {
    if (solutions.length >= 3) { toast('最多保存 3 份参考答案', 'danger'); return; }
    const used = new Set(solutions.map(solution => solution.language));
    const language = languages.find(item => !used.has(item.id)) || languages[0];
    solutions.push({ language: language?.id || 'cpp17', code: '' });
    renderSolutions(); markDirty();
  }

  function renderSolutions() {
    solutionsHost.replaceChildren();
    if (!solutions.length) {
      solutionsHost.append(empty('还没有参考答案', '添加后可以一键验证全部测试数据。'));
      return;
    }
    solutions.forEach((solution, index) => {
      const languageSelect = select(languages.map(item => ({ value: item.id, label: item.display })), solution.language, () => {
        solution.language = languageSelect.value;
      }, { 'aria-label': `参考答案 ${index + 1} 语言` });
      const code = textarea({ rows: 10, value: solution.code || '', spellcheck: false, placeholder: '参考答案代码', 'aria-label': `参考答案 ${index + 1} 代码` });
      code.addEventListener('input', () => { solution.code = code.value; });
      solutionsHost.append(el('article', { class: 'solution-row' },
        el('div', { class: 'testcase-heading' }, languageSelect,
          iconButton('trash', '删除参考答案', () => { solutions.splice(index, 1); renderSolutions(); markDirty(); }, { class: 'compact-icon danger-text' })),
        code));
    });
  }

  function buildProblem() {
    return {
      title: title.value.trim(),
      difficulty: difficultySelect.value,
      tags,
      description: description.value,
      input_format: inputFormat.value,
      output_format: outputFormat.value,
      hint: hint.value,
      time_limit_ms: Number(timeLimit.value),
      memory_limit_mb: Number(memoryLimit.value),
      checker: checker.value,
      float_eps: checker.value === 'float' ? Number(floatEps.value) : 1e-6,
      is_public: publicToggle.checked,
      version: problem.updated_at || '',
      testcase_refs: Object.fromEntries(testcases.flatMap((test, index) => test.stored ? [[index + 1, test.index]] : [])),
      testcases: testcases.map(test => ({
        input: test.input || '', expected: test.expected || '',
        explanation: test.explanation || '', is_sample: !!test.is_sample, sample_only: !!test.sample_only,
      })),
      templates: { ...templates },
      lang_limits: Object.fromEntries(Object.entries(localLimits).filter(([, value]) => value.time_limit_ms || value.memory_limit_mb)),
      solutions: solutions.map(solution => ({ language: solution.language, code: solution.code })),
    };
  }

  function validateBasic() {
    const errors = [];
    setInlineError(title, '');
    const titleValue = title.value.trim();
    if (titleValue.length < 1 || [...titleValue].length > 120) {
      setInlineError(title, '标题应为 1–120 个字符。'); errors.push(title);
    }
    if (!description.value.trim()) {
      setInlineError(description, '题目描述不能为空。'); errors.push(description);
    }
    if (!Number(timeLimit.value) || Number(timeLimit.value) < 1 || Number(timeLimit.value) > 30000) {
      setInlineError(timeLimit, '基准时间限制应为 1–30000 ms。'); errors.push(timeLimit);
    }
    if (!Number(memoryLimit.value) || Number(memoryLimit.value) < 16 || Number(memoryLimit.value) > 2048) {
      setInlineError(memoryLimit, '基准内存限制应为 16–2048 MB。'); errors.push(memoryLimit);
    }
    if (!testcases.some(test => !test.sample_only)) {
      toast('至少需要一组参与判题的测试', 'danger'); errors.push(title);
    }
    if (testcases.some(test => !test.stored && (test.input.length > 1 << 20 || test.expected.length > 1 << 20))) {
      toast('单个测试用例的输入或输出不能超过 1 MB', 'danger'); errors.push(title);
    }
    if (solutions.some(solution => !solution.code.trim())) {
      toast('参考答案代码不能为空；如不需要可删除该行', 'danger'); errors.push(title);
    }
    const uniqueSolutionLanguages = new Set(solutions.map(solution => solution.language));
    if (uniqueSolutionLanguages.size !== solutions.length) {
      toast('每种语言只能保存一份参考答案', 'danger'); errors.push(title);
    }
    if (errors.length) errors[0]?.focus();
    return errors.length === 0;
  }

  async function saveProblem({ navigate = true } = {}) {
    if (saving || !validateBasic()) return;
    saving = true;
    setBusy(saveButton, true, '正在保存…');
    validationSummary.hidden = true;
    try {
      const body = buildProblem();
      problem = await api(id ? `/api/admin/problems/${id}?compact=1` : '/api/admin/problems?compact=1', {
        method: id ? 'PUT' : 'POST', body, signal: ctx.signal,
      });
      dirty = false;
      toast(id ? '题目已更新' : '题目已创建');
      ctx.setLeaveGuard(null);
      if (navigate) ctx.navigate('/admin/problems');
      return problem;
    } catch (error) {
      if (error.name !== 'AbortError') {
        validationSummary.hidden = false;
        validationSummary.textContent = error.message;
      }
      return null;
    } finally {
      saving = false;
      setBusy(saveButton, false);
    }
  }

  async function verifyProblem() {
    if (!validateBasic()) return;
    if (!solutions.length) { toast('请至少添加一份参考答案后再验证', 'danger'); return; }
    setBusy(verifyButton, true, '正在验证…');
    verifyHost.replaceChildren(loading(4));
    try {
      if (!id) {
        const created = await saveProblem({ navigate: false });
        if (!created) {
          verifyHost.replaceChildren(empty('保存失败', '题目创建成功后才能执行数据验证。'));
          return;
        }
        id = created.id;
      }
      const data = await api(`/api/admin/problems/${id}/verify`, { method: 'POST', body: {}, signal: ctx.signal });
      verifyHost.replaceChildren(...(data.results || []).map(result => el('article', { class: 'verify-row' },
        el('div', { class: 'row spread' }, el('strong', {}, result.language), verdict(result.status)),
        el('div', { class: 'row wrap small-text' },
          el('span', {}, `通过 ${result.passed || 0}/${result.total || 0}`),
          el('span', {}, formatTime(result)),
          el('span', {}, formatMemory(result.memory_kb))),
        result.message && el('pre', { class: 'diagnostic' }, result.message),
        el('div', { class: 'case-tiles compact' }, ...(result.case_results || []).map((item, index) =>
          el('span', { class: `case-dot ${item.status === 'accepted' ? 'success' : item.status === 'time_limit_exceeded' || item.status === 'memory_limit_exceeded' || item.status === 'output_limit_exceeded' ? 'warning' : 'danger'}`, title: `用例 ${item.index || index + 1}: ${item.status}` }, `#${item.index || index + 1}`))))));
      toast('测试数据验证完成');
    } catch (error) {
      if (error.name !== 'AbortError') verifyHost.replaceChildren(errorState(error, verifyProblem));
    } finally {
      setBusy(verifyButton, false);
    }
  }
}

export async function renderUsers(host, ctx) {
  ctx.setTitle('用户管理');
  const params = new URLSearchParams(location.search);
  let page = Math.max(1, Number(params.get('page')) || 1);
  let sequence = 0;
  const search = inputControl({ type: 'search', placeholder: '搜索用户名', value: params.get('q') || '', 'aria-label': '搜索用户' });
  const body = el('tbody');
  const list = el('div', {}, loading(5)), footer = el('div');
  const card = el('section', { class: 'card' },
    el('div', { class: 'filter-bar' }, el('div', { class: 'search-input' }, icon('search', 17), search),
      button('刷新', () => load(), { variant: 'small', icon: 'refresh' })),
    list, footer);
  host.append(pageHeader('用户管理', '调整用户角色和账号状态；系统会阻止封禁自己或最后一名有效管理员。'), card);
  const changed = debounce(() => { page = 1; load(); }, 300);
  search.addEventListener('input', changed);
  ctx.cleanup(changed.cancel);
  await load();

  async function load() {
    const current = ++sequence;
    history.replaceState(null, '', `/admin/users${query({ q: search.value.trim(), page: page > 1 ? page : '' })}`);
    list.replaceChildren(loading(6)); footer.replaceChildren();
    try {
      const data = await api(`/api/admin/users${query({ q: search.value.trim(), page, page_size: 20 })}`, { signal: ctx.signal });
      if (current !== sequence || !ctx.alive()) return;
      if (!data.items?.length) {
        list.replaceChildren(empty('没有匹配的用户', '换一个用户名关键词试试。'));
      } else {
        body.replaceChildren(...data.items.map(user => userRow(user, load, ctx.user.id)));
        list.replaceChildren(el('div', { class: 'table-scroll' }, el('table', { class: 'data-table user-table' },
          el('thead', {}, el('tr', {}, ['用户', '角色', '状态', '最近登录', '注册时间', '操作'].map(heading => el('th', {}, heading)))), body)));
      }
      footer.replaceChildren(pagination(data.total || 0, page, 20, next => { page = next; load(); }));
    } catch (error) {
      if (current === sequence && error.name !== 'AbortError') list.replaceChildren(errorState(error, load));
    }
  }
}

function userRow(user, reload, currentUserID) {
  const self = user.id === currentUserID;
  const role = select([{ value: 'user', label: '普通用户' }, { value: 'admin', label: '管理员' }], user.role, null, { 'aria-label': `${user.username} 的角色` });
  const status = select([{ value: 'active', label: '正常' }, { value: 'banned', label: '封禁' }], user.status, null, { 'aria-label': `${user.username} 的状态` });
  const save = button('保存', async () => {
    setBusy(save, true, '保存中…');
    try {
      await api(`/api/admin/users/${user.id}`, { method: 'PATCH', body: { role: role.value, status: status.value } });
      toast(`已更新用户 ${user.username}`);
      await reload();
    } catch (error) {
      if (error.name !== 'AbortError') toast(error.message, 'danger');
    } finally {
      setBusy(save, false);
    }
  }, { variant: 'small' });
  return el('tr', {},
    el('td', {}, el('div', { class: 'user-cell' }, el('span', { class: 'avatar small' }, user.username.slice(0, 1).toUpperCase()),
      el('div', {}, el('strong', {}, user.username), el('span', { class: 'muted mono small-text' }, `#${user.id}`)))),
    el('td', {}, role),
    el('td', {}, status),
    el('td', { class: 'small-text muted' }, user.last_login_at ? dateTime(user.last_login_at) : '从未登录'),
    el('td', { class: 'small-text muted' }, dateTime(user.created_at)),
    el('td', {}, self ? badge('当前账号', 'accent') : save));
}
