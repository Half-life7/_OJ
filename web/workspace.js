import {
  el, icon, iconButton, button, select, badge, difficulty, verdict, pending,
  formatTime, formatMemory, dateTime, debounce, toast, setBusy, loading,
  empty, errorState, confirmAction, copyText, markdown, storage,
} from './ui.js';
import { api } from './api.js';
import { createEditor } from './editor.js';
import { resultView } from './results.js';

function templateFor(problem, language) {
  if (!language) return '';
  return problem.templates?.[language.id] || language.template || '';
}

function limitText(problem, language) {
  if (!language) return '判题服务恢复后显示资源限制';
  const limit = problem.effective_limits?.[language.id];
  if (!limit) return '资源限制暂不可用';
  return `${limit.time_limit_ms} ms · ${limit.memory_limit_mb} MB`;
}

function sampleBlock(sample, index) {
  return el('article', { class: 'sample-card' },
    el('div', { class: 'sample-heading' },
      el('div', { class: 'row' }, badge(`样例 ${index + 1}`, 'accent'), sample.is_sample === false && badge('测试数据', 'muted')),
      button('复制', () => copyText(`输入：\n${sample.input}\n输出：\n${sample.expected}`), { variant: 'text small', icon: 'copy' })),
    el('div', { class: 'sample-grid' },
      el('div', {}, el('span', { class: 'sample-label' }, '输入'), el('pre', { class: 'sample-code' }, sample.input || '（空）')),
      el('div', {}, el('span', { class: 'sample-label' }, '输出'), el('pre', { class: 'sample-code' }, sample.expected || '（空）'))),
    sample.explanation && el('p', { class: 'sample-explanation' }, sample.explanation));
}

function historySection(historyHost, ctx, problem, onRestore) {
  const list = el('div', { class: 'workspace-history-list' });
  historyHost.replaceChildren(list);
  list.append(loading(4));
  api(`/api/submissions?problem_id=${problem.id}&page_size=20`, { signal: ctx.signal })
    .then(data => {
      if (!ctx.alive()) return;
      if (!data.items?.length) {
        list.replaceChildren(empty('这道题还没有提交记录', '写好代码后提交一次，结果会出现在这里。'));
        return;
      }
      list.replaceChildren(...data.items.map(sub => el('button', {
        type: 'button', class: 'history-row',
        onclick: () => onRestore(sub),
        'aria-label': `恢复提交 #${sub.id} 的代码`,
      },
      el('div', { class: 'history-main' },
        el('div', { class: 'row' }, verdict(sub.status), el('span', { class: 'mono muted' }, `#${sub.id}`)),
        el('div', { class: 'history-meta' }, `${sub.language} · ${dateTime(sub.created_at)}`)),
      el('div', { class: 'history-metrics' }, el('span', {}, formatTime(sub)), el('span', {}, formatMemory(sub.memory_kb))))));
    })
    .catch(error => { if (error.name !== 'AbortError') list.replaceChildren(errorState(error)); });
}

export async function renderWorkspace(host, ctx, id) {
  ctx.setTitle('题目工作台');
  host.replaceChildren(loading(8));
  const [problem] = await Promise.all([api(`/api/problems/${id}`, { signal: ctx.signal })]);
  if (!ctx.alive()) return;
  ctx.setTitle(problem.title);
  document.documentElement.classList.add('workspace-active');
  ctx.cleanup(() => document.documentElement.classList.remove('workspace-active'));

  const rememberedLanguage = ctx.user ? storage.get(`oj.lastLanguage.${ctx.user.id}.${problem.id}`, '') : '';
  let language = ctx.languages.find(item => item.id === rememberedLanguage) ||
    ctx.languages.find(item => item.id === Object.keys(problem.templates || {})[0]) ||
    ctx.languages[0] || null;
  let activeMobilePane = 'description';
  let currentDraft = '';
  let pollTimer = 0;
  let submissionHost = el('div');

  const mobileTabs = el('div', { class: 'workspace-mobile-tabs', role: 'tablist', 'aria-label': '工作台视图' });
  const descriptionTab = button('题目', () => showMobilePane('description'), { variant: 'mobile-tab active', 'aria-selected': 'true' });
  const codeTab = button('代码', () => showMobilePane('code'), { variant: 'mobile-tab', 'aria-selected': 'false' });
  mobileTabs.append(descriptionTab, codeTab);

  const split = el('div', { class: 'workspace-split' });
  const savedSplit = Math.min(72, Math.max(28, Number(storage.get('oj.splitPosition', '50')) || 50));
  split.style.setProperty('--split-position', `${savedSplit}%`);

  const leftPane = el('section', { class: 'workspace-pane problem-pane', 'aria-label': '题目内容' });
  const descriptionPanel = el('div', { class: 'problem-description-pane' });
  const historyPanel = el('div', { class: 'problem-history-pane', hidden: true });
  const descriptionTabButton = button('题目描述', () => showProblemTab('description'), { variant: 'workspace-tab active', 'aria-selected': 'true' });
  const historyTabButton = button('提交记录', () => showProblemTab('history'), { variant: 'workspace-tab', 'aria-selected': 'false' });
  leftPane.append(el('div', { class: 'workspace-tabbar', role: 'tablist' }, descriptionTabButton, historyTabButton), descriptionPanel, historyPanel);

  const paneScroller = el('div', { class: 'problem-scroll' });
  const tags = el('div', { class: 'tag-list' }, ...(problem.tags || []).map(tag => badge(tag)));
  const currentLimit = el('span', { class: 'limit-pill' }, icon('clock', 14), limitText(problem, language));
  paneScroller.append(
    el('header', { class: 'problem-heading' },
      el('div', { class: 'problem-kicker' }, el('span', { class: 'mono muted' }, `#${problem.id}`), difficulty(problem.difficulty), currentLimit),
      el('h1', {}, problem.title), tags),
    el('div', { class: 'statement-section' }, markdown(problem.description || '暂无题目描述。')),
    el('section', { class: 'statement-section' }, el('h2', {}, '输入格式'), markdown(problem.input_format || '未提供。')),
    el('section', { class: 'statement-section' }, el('h2', {}, '输出格式'), markdown(problem.output_format || '未提供。')),
    el('section', { class: 'statement-section' }, el('h2', {}, '样例'),
      ...(problem.testcases || []).filter(test => test.is_sample).map(sampleBlock)),
    problem.hint && el('details', { class: 'hint-disclosure' }, el('summary', {}, icon('info', 16), '查看提示'), markdown(problem.hint)));
  descriptionPanel.append(paneScroller);

  const rightPane = el('section', { class: 'workspace-pane editor-pane', 'aria-label': '代码编辑与运行' });
  const codeWorkspace = el('div', { class: 'code-workspace' });
  const editorTop = el('div', { class: 'editor-toolbar' });
  const languageSelect = select((ctx.languages || []).map(item => ({ value: item.id, label: item.display })), language?.id || '', changeLanguage, { 'aria-label': '选择编程语言' });
  const resetButton = button('模板', resetTemplate, { variant: 'text small', icon: 'refresh' });
  const fontDown = iconButton('chevron', '减小字号', () => adjustFont(-1), { class: 'compact-icon' });
  const fontUp = iconButton('plus', '增大字号', () => adjustFont(1), { class: 'compact-icon' });
  const languageHint = el('span', { class: 'muted small-text' }, language ? limitText(problem, language) : '暂无可用语言');
  editorTop.append(el('div', { class: 'row' }, icon('code', 17), languageSelect, languageHint), el('div', { class: 'row' }, fontDown, fontUp, resetButton));

  const editorMount = el('div', { class: 'editor-mount' });
  const editor = createEditor(editorMount, {
    language: language?.highlight || '',
    value: '',
    onChange(value) {
      currentDraft = value;
      if (ctx.user && language) persistDraft(draftKey(problem.id, ctx.user.id, language.id), value);
      charCount.textContent = `${new TextEncoder().encode(value).length.toLocaleString('zh-CN')} 字节`;
    },
  });
  const persistDraft = debounce((key, value) => storage.set(key, value), 500);
  ctx.cleanup(() => {
    persistDraft.cancel();
    if (ctx.user && language) storage.set(draftKey(problem.id, ctx.user.id, language.id), currentDraft);
    clearTimeout(pollTimer);
    editor.destroy();
  });

  const charCount = el('span', { class: 'muted mono small-text' }, '0 字节');
  const editorFoot = el('div', { class: 'editor-substatus' }, el('span', {}, 'Ctrl/⌘ + Enter 运行 · Ctrl/⌘ + Shift + Enter 提交'), charCount);
  const consolePanel = el('section', { class: 'console-panel', 'aria-label': '自测控制台' });
  const inputArea = el('textarea', {
    class: 'console-input', spellcheck: false, placeholder: '在这里输入自定义测试数据…',
    value: problem.testcases?.find(test => test.is_sample)?.input || '',
    'aria-label': '自测输入',
  });
  const runOutput = submissionHost;
  const inputTab = button('自测输入', () => showConsoleTab('input'), { variant: 'workspace-tab active', 'aria-selected': 'true' });
  const outputTab = button('运行结果', () => showConsoleTab('output'), { variant: 'workspace-tab', 'aria-selected': 'false' });
  const consoleToggle = iconButton('down', '收起或展开控制台', toggleConsole, { class: 'compact-icon' });
  const consoleBody = el('div', { class: 'console-body' },
    el('div', { class: 'console-input-pane' }, inputArea),
    el('div', { class: 'console-output-pane', hidden: true }, runOutput));
  const consoleResize = el('div', { class: 'console-resize', role: 'separator', 'aria-label': '调整控制台高度', tabIndex: 0 });
  consolePanel.append(
    el('div', { class: 'console-tabbar' },
      el('div', { class: 'workspace-tabbar inline' }, inputTab, outputTab),
      el('div', { class: 'row' }, el('span', { class: 'console-caption' }, icon('terminal', 15), '测试控制台'), consoleToggle)),
    consoleResize, consoleBody);
  codeWorkspace.append(editorTop, editorMount, editorFoot, consolePanel);
  rightPane.append(codeWorkspace);

  const divider = el('div', {
    class: 'split-divider', role: 'separator', tabIndex: 0,
    'aria-label': '拖动调整题目和代码区域宽度', 'aria-orientation': 'vertical',
  });
  split.append(leftPane, divider, rightPane);

  const runButton = button('运行', runCode, { variant: 'btn run-btn', icon: 'play' });
  const submitButton = button('提交', submitCode, { variant: 'btn primary submit-btn', icon: 'send' });
  const actionBar = el('div', { class: 'workspace-actions' },
    el('div', { class: 'workspace-action-note' }, icon('shield', 16), el('span', {}, '代码仅在 go-judge 隔离环境中执行')),
    el('div', { class: 'row' }, runButton, submitButton));

  if (!ctx.languages.length) {
    codeWorkspace.prepend(el('div', { class: 'notice warning workspace-language-warning' },
      icon('alert', 18), el('span', {}, ctx.languageError || '判题服务暂不可用。你仍可阅读题目和编写代码，服务恢复后即可运行。')));
    languageSelect.disabled = true;
    runButton.disabled = true;
    submitButton.disabled = true;
  }

  host.replaceChildren(mobileTabs, split, actionBar);
  configureInitialCode();
  setupSplitDrag();
  setupConsoleDrag();
  loadHistory();

  function draftKey(problemID, userID, languageID) {
    return `oj.draft.${userID}.${problemID}.${languageID}`;
  }

  function configureInitialCode() {
    if (!language) {
      editor.setValue('', { silent: true });
      return;
    }
    const saved = ctx.user ? storage.get(draftKey(problem.id, ctx.user.id, language.id), '') : '';
    const value = saved || templateFor(problem, language);
    editor.setValue(value, { silent: true });
    currentDraft = value;
    charCount.textContent = `${new TextEncoder().encode(value).length.toLocaleString('zh-CN')} 字节`;
  }

  function changeLanguage() {
    const next = ctx.languages.find(item => item.id === languageSelect.value);
    if (!next) return;
    persistDraft.cancel();
    if (ctx.user && language) storage.set(draftKey(problem.id, ctx.user.id, language.id), currentDraft);
    language = next;
    if (ctx.user) storage.set(`oj.lastLanguage.${ctx.user.id}.${problem.id}`, next.id);
    editor.setLanguage(next.highlight || '');
    currentLimit.replaceChildren(icon('clock', 14), document.createTextNode(limitText(problem, next)));
    languageHint.textContent = limitText(problem, next);
    configureInitialCode();
  }

  async function resetTemplate() {
    if (!language) return;
    if (!(await confirmAction('恢复起始模板', '当前编辑器中的代码会被替换，确定继续吗？', { label: '恢复模板' }))) return;
    const value = templateFor(problem, language);
    editor.setValue(value);
  }

  function adjustFont(delta) {
    const current = Number(storage.get('oj.fontSize', '14')) || 14;
    editor.setFontSize(current + delta);
  }

  function showMobilePane(pane) {
    activeMobilePane = pane;
    split.classList.toggle('show-code', pane === 'code');
    descriptionTab.classList.toggle('active', pane === 'description');
    codeTab.classList.toggle('active', pane === 'code');
    descriptionTab.setAttribute('aria-selected', String(pane === 'description'));
    codeTab.setAttribute('aria-selected', String(pane === 'code'));
    if (pane === 'code') requestAnimationFrame(() => editor.focus());
  }

  function showProblemTab(tab) {
    const description = tab === 'description';
    descriptionPanel.hidden = !description;
    historyPanel.hidden = description;
    descriptionTabButton.classList.toggle('active', description);
    historyTabButton.classList.toggle('active', !description);
    descriptionTabButton.setAttribute('aria-selected', String(description));
    historyTabButton.setAttribute('aria-selected', String(!description));
    if (!description) loadHistory();
  }

  function showConsoleTab(tab) {
    const input = tab === 'input';
    inputArea.parentElement.hidden = !input;
    runOutput.parentElement.hidden = input;
    inputTab.classList.toggle('active', input);
    outputTab.classList.toggle('active', !input);
    inputTab.setAttribute('aria-selected', String(input));
    outputTab.setAttribute('aria-selected', String(!input));
    consolePanel.classList.remove('collapsed');
  }

  function toggleConsole() {
    const collapsed = consolePanel.classList.toggle('collapsed');
    consoleToggle.replaceChildren(icon(collapsed ? 'chevron' : 'down', 16));
    consoleToggle.setAttribute('aria-expanded', String(!collapsed));
  }

  function loadHistory() {
    if (!ctx.user) {
      historyPanel.replaceChildren(empty('登录后查看提交记录', '本题的提交历史只对当前用户和本站管理员可见。'));
      return;
    }
    historySection(historyPanel, ctx, problem, restoreSubmission);
  }

  function restoreSubmission(sub) {
    const target = ctx.languages.find(item => item.id === sub.language);
    if (target) {
      language = target;
      if (ctx.user) storage.set(`oj.lastLanguage.${ctx.user.id}.${problem.id}`, target.id);
      languageSelect.value = target.id;
      editor.setLanguage(target.highlight || '');
      currentLimit.replaceChildren(icon('clock', 14), document.createTextNode(limitText(problem, target)));
      languageHint.textContent = limitText(problem, target);
    }
    editor.setValue(sub.code);
    showProblemTab('description');
    showMobilePane('code');
    toast(`已恢复提交 #${sub.id} 的代码`);
  }

  async function runCode() {
    if (!ctx.requireLogin()) return;
    if (!language) { toast('当前没有可用的判题语言', 'danger'); return; }
    if (!currentDraft.trim()) { toast('请先编写代码', 'danger'); return; }
    setExecutionBusy(true);
    showConsoleTab('output');
    submissionHost.replaceChildren(loading(3));
    try {
      const result = await api('/api/run', {
        method: 'POST', signal: ctx.signal,
        body: { language: language.id, code: currentDraft, input: inputArea.value, problem_id: problem.id },
      });
      if (!ctx.alive()) return;
      submissionHost.replaceChildren(resultView({ ...result, total: 1, passed: result.status === 'accepted' ? 1 : 0 }, { selfTest: true }));
    } catch (error) {
      if (error.name !== 'AbortError') submissionHost.replaceChildren(errorState(error, runCode));
    } finally {
      setExecutionBusy(false);
    }
  }

  async function submitCode() {
    if (!ctx.requireLogin()) return;
    if (!language) { toast('当前没有可用的判题语言', 'danger'); return; }
    if (!currentDraft.trim()) { toast('请先编写代码', 'danger'); return; }
    setExecutionBusy(true);
    showConsoleTab('output');
    submissionHost.replaceChildren(loading(4));
    try {
      const response = await api(`/api/problems/${problem.id}/submit`, {
        method: 'POST', signal: ctx.signal,
        body: { language: language.id, code: currentDraft },
      });
      await pollSubmission(response.submission_id, Date.now());
      loadHistory();
    } catch (error) {
      if (error.name !== 'AbortError') submissionHost.replaceChildren(errorState(error, submitCode));
    } finally {
      setExecutionBusy(false);
    }
  }

  async function pollSubmission(submissionID, started) {
    while (ctx.alive() && !ctx.signal.aborted) {
      let sub;
      try {
        sub = await api(`/api/submissions/${submissionID}`, { signal: ctx.signal });
      } catch (error) {
        if (error.name === 'AbortError' || !ctx.alive()) throw error;
        await delay(1500);
        continue;
      }
      if (!ctx.alive()) return sub;
      submissionHost.replaceChildren(resultView(sub));
      if (!pending(sub.status)) {
        if (sub.status === 'accepted') toast(`提交 #${submissionID} 已通过`);
        return sub;
      }
      await delay(Date.now() - started < 20000 ? 500 : 1000);
    }
  }

  function delay(milliseconds) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(finish, milliseconds);
      const abort = () => {
        clearTimeout(timer);
        ctx.signal.removeEventListener('abort', abort);
        reject(new DOMException('Aborted', 'AbortError'));
      };
      function finish() {
        ctx.signal.removeEventListener('abort', abort);
        resolve();
      }
      if (ctx.signal.aborted) abort();
      else ctx.signal.addEventListener('abort', abort, { once: true });
    });
  }

  function setExecutionBusy(busy) {
    for (const control of [runButton, submitButton, languageSelect, resetButton]) control.disabled = busy;
    runButton.classList.toggle('loading', busy);
    submitButton.classList.toggle('loading', busy);
    if (busy) {
      runButton.setAttribute('aria-label', '正在运行');
      submitButton.setAttribute('aria-label', '正在提交');
    } else {
      runButton.removeAttribute('aria-label');
      submitButton.removeAttribute('aria-label');
      if (!ctx.languages.length) {
        languageSelect.disabled = true;
        runButton.disabled = true;
        submitButton.disabled = true;
      }
    }
  }

  function setupSplitDrag() {
    let dragging = false;
    const move = event => {
      if (!dragging) return;
      const rect = split.getBoundingClientRect();
      const percent = Math.min(72, Math.max(28, (event.clientX - rect.left) / rect.width * 100));
      split.style.setProperty('--split-position', `${percent}%`);
    };
    const stop = () => {
      if (!dragging) return;
      dragging = false;
      divider.classList.remove('dragging');
      document.body.classList.remove('resizing');
      const raw = split.style.getPropertyValue('--split-position').replace('%', '');
      storage.set('oj.splitPosition', Math.round(Number(raw) || 50));
    };
    divider.addEventListener('pointerdown', event => {
      if (innerWidth < 1024) return;
      dragging = true;
      divider.setPointerCapture?.(event.pointerId);
      divider.classList.add('dragging');
      document.body.classList.add('resizing');
    });
    divider.addEventListener('pointermove', move);
    divider.addEventListener('pointerup', stop);
    divider.addEventListener('pointercancel', stop);
    divider.addEventListener('keydown', event => {
      if (!['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
      event.preventDefault();
      const raw = Number(split.style.getPropertyValue('--split-position').replace('%', '')) || 50;
      const next = Math.min(72, Math.max(28, raw + (event.key === 'ArrowRight' ? 2 : -2)));
      split.style.setProperty('--split-position', `${next}%`);
      storage.set('oj.splitPosition', Math.round(next));
    });
    ctx.cleanup(() => { document.body.classList.remove('resizing'); });
  }

  function setupConsoleDrag() {
    let dragging = false, startY = 0, startHeight = 0;
    const move = event => {
      if (!dragging) return;
      const height = Math.min(innerHeight * 0.65, Math.max(140, startHeight - (event.clientY - startY)));
      consolePanel.style.setProperty('--console-height', `${height}px`);
    };
    const stop = () => {
      if (!dragging) return;
      dragging = false;
      consoleResize.classList.remove('dragging');
      document.body.classList.remove('resizing');
      storage.set('oj.consoleHeight', Math.round(consolePanel.getBoundingClientRect().height));
    };
    consoleResize.addEventListener('pointerdown', event => {
      dragging = true;
      startY = event.clientY;
      startHeight = consolePanel.getBoundingClientRect().height;
      consoleResize.setPointerCapture?.(event.pointerId);
      consoleResize.classList.add('dragging');
      document.body.classList.add('resizing');
    });
    consoleResize.addEventListener('pointermove', move);
    consoleResize.addEventListener('pointerup', stop);
    consoleResize.addEventListener('pointercancel', stop);
    const saved = Number(storage.get('oj.consoleHeight', '240')) || 240;
    consolePanel.style.setProperty('--console-height', `${saved}px`);
  }

  document.addEventListener('keydown', shortcuts);
  ctx.cleanup(() => document.removeEventListener('keydown', shortcuts));
  function shortcuts(event) {
    if (!(event.ctrlKey || event.metaKey) || event.key !== 'Enter') return;
    event.preventDefault();
    if (event.shiftKey) submitCode(); else runCode();
  }
}
