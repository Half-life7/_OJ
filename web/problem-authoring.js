import { api } from './api.js';
import { button, el, setBusy, toast } from './ui.js';

export function hydroImportControl(ctx) {
  const file = el('input', { type: 'file', accept: '.zip,application/zip', hidden: true });
  const trigger = button('导入 Hydro ZIP', () => file.click(), { icon: 'upload' });
  file.addEventListener('change', async () => {
    const selected = file.files[0];
    file.value = '';
    if (!selected) return;
    if (selected.size > 24 * 2 ** 20) {
      toast('ZIP 文件不得超过 24 MB', 'danger');
      return;
    }
    setBusy(trigger, true, '正在导入…');
    try {
      const { items } = await api('/api/admin/problems/hydro', { method: 'POST', body: selected, signal: ctx.signal });
      toast(`已导入 ${items.length} 道私有题目`);
      await ctx.navigate(items.length === 1 ? `/admin/problems/${items[0].id}/edit` : '/admin/problems', { replace: true });
    } catch (error) {
      if (error.name !== 'AbortError') toast(error.message, 'danger');
    } finally {
      setBusy(trigger, false);
    }
  });
  return el('div', {}, trigger, file);
}

export function hydroExportButton(ctx, id) {
  const trigger = button('导出 Hydro ZIP', async () => {
    setBusy(trigger, true, '正在打包…');
    try {
      const data = await api(`/api/admin/problems/${id}/hydro`, { responseType: 'blob', signal: ctx.signal });
      const url = URL.createObjectURL(data);
      const link = el('a', { href: url, download: `P${id}-hydro.zip` });
      link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) {
      if (error.name !== 'AbortError') toast(error.message, 'danger');
    } finally {
      setBusy(trigger, false);
    }
  }, { icon: 'download' });
  return trigger;
}

export function editorTestcases(problem) {
  const visible = (problem.testcases || []).map(test => ({ ...test }));
  const stored = (problem.hidden_testcases || []).map(test => ({ ...test, stored: true, is_sample: false }));
  return [...visible, ...stored].sort((a, b) => (a.index || 0) - (b.index || 0));
}
