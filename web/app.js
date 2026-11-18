import { el, icon, iconButton, button, link, storage, toast, errorState, confirmAction } from './ui.js';
import { NAV, isVisible, matchesPath, sectionMatch } from './nav.config.js';
import { api, configureAPI } from './api.js';
import { renderAuth, renderPassword, renderProfile, renderProblems, renderSubmissions } from './pages.js';
import { renderWorkspace } from './workspace.js';
import { renderAdminEditor, renderUsers } from './admin.js';

const state = { user: null, stats: {}, config: { allow_register: false }, languages: [], languageError: '', navigate, refreshSession, refreshLanguages, requireLogin, setLeaveGuard(fn) { leaveGuard = fn; } };
let dispose = () => {}, leaveGuard = null, routeID = 0, currentURL = location.pathname + location.search;
let drawerOpen = false, sidebar, overlay, shell, main, collapseButton;
const themeMedia = matchMedia('(prefers-color-scheme: dark)');
function applyTheme(mode) {
  storage.set('oj.theme', mode);
  const dark = mode === 'system' ? themeMedia.matches : mode === 'dark';
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.querySelector('meta[name="theme-color"]').content = dark ? '#111715' : '#f6f8fa';
}
themeMedia.addEventListener('change', () => { if (storage.get('oj.theme', 'system') === 'system') applyTheme('system'); });
applyTheme(storage.get('oj.theme', 'system'));

configureAPI({
  unauthorized() { if (!state.user && location.pathname === '/login') return; state.user = null; state.stats = {}; leaveGuard = null; navigate(`/login?next=${encodeURIComponent(location.pathname + location.search)}`, { replace: true, force: true }); },
  forbidden(message) { toast(message || '你没有权限执行此操作', 'danger'); },
});

async function refreshSession() {
  try { const response = await api('/api/auth/me', { auth: false, timeout: 15000 }); state.user = response.user; state.stats = response.stats || {}; return state.user; }
  catch (error) { if (error.status === 401) { state.user = null; state.stats = {}; return null; } throw error; }
}
async function refreshLanguages() {
  try { const response = await api('/api/languages', { timeout: 15000 }); state.languages = (response.languages || []).map(l => ({ ...l, id: l.id, display: l.display || l.id })); state.languageError = state.languages.length ? '' : '判题服务暂不可用，当前没有可用语言。你仍可阅读题目，稍后重试。'; }
  catch (error) { state.languages = []; state.languageError = error.message; }
  return state.languages;
}
function requireLogin() { if (state.user) return true; navigate(`/login?next=${encodeURIComponent(location.pathname + location.search)}`); return false; }

export async function navigate(path, { replace = false, force = false } = {}) {
  const url = new URL(path, location.origin);
  if (url.origin !== location.origin) return;
  if (!force && leaveGuard && !(await leaveGuard())) return;
  if (replace) history.replaceState(null, '', url.pathname + url.search + url.hash); else history.pushState(null, '', url.pathname + url.search + url.hash);
  currentURL = url.pathname + url.search; await renderRoute();
}
function closeDrawer() { drawerOpen = false; sidebar?.classList.remove('open'); overlay?.classList.remove('open'); document.body.classList.remove('drawer-open'); document.getElementById('menu-toggle')?.setAttribute('aria-expanded', 'false'); }
function toggleDrawer() { drawerOpen = !drawerOpen; sidebar.classList.toggle('open', drawerOpen); overlay.classList.toggle('open', drawerOpen); document.body.classList.toggle('drawer-open', drawerOpen); document.getElementById('menu-toggle')?.setAttribute('aria-expanded', String(drawerOpen)); if (drawerOpen) sidebar.querySelector('a,button')?.focus(); }
function setCollapsed(value, save = true) { shell.classList.toggle('sidebar-collapsed', value); collapseButton?.setAttribute('aria-expanded', String(!value)); if (save) storage.set('oj.sidebarCollapsed', value ? '1' : '0'); }
function navItem(item, child = false) {
  const active = matchesPath(item.path);
  const node = link('', item.path, { class: `side-link ${child ? 'child' : ''} ${active ? 'active' : ''}`, title: item.label, 'aria-label': item.label, 'aria-current': active ? 'page' : null });
  node.append(icon(item.icon), el('span', {}, item.label)); return node;
}
function buildShell(workspace, authPage) {
  const user = state.user;
  const logo = link('', '/problems', { class: 'brand', 'aria-label': '知序 OJ 首页' }); logo.append(el('span', { class: 'brand-symbol' }, icon('code', 22)), el('strong', {}, '知序', el('span', {}, ' OJ')));
  const nav = el('nav', { class: 'top-nav', 'aria-label': '主要导航' });
  NAV.filter(item => isVisible(item,user) && item.inNavbar).forEach(item => {
    const active = sectionMatch(item) > 0;
    nav.append(link(item.label,item.path,{ class: active ? 'active' : '', 'aria-current': active ? 'page' : null }));
  });
  const theme = iconButton('sun', '切换主题', () => { const options = ['system','light','dark']; const next = options[(options.indexOf(storage.get('oj.theme','system')) + 1) % options.length]; applyTheme(next); theme.replaceChildren(icon({ system: 'monitor', light: 'sun', dark: 'moon' }[next])); theme.title = `当前：${{system:'跟随系统',light:'浅色',dark:'深色'}[next]}，点击切换`; theme.setAttribute('aria-label', theme.title); });
  theme.replaceChildren(icon({system:'monitor',light:'sun',dark:'moon'}[storage.get('oj.theme','system')] || 'monitor'));
  const account = el('div', { class: 'header-actions' }, theme);
  if (user) {
    const details = el('details', { class: 'account-menu' });
    const summary = el('summary', { class: 'account-link', 'aria-label': '打开账户菜单' },el('span', { class: 'avatar small' }, user.username.slice(0,1).toUpperCase()),el('span', { class: 'account-name' }, user.username),icon('down',14));
    const logout = button('退出登录', async () => { details.open = false; if (!(await confirmAction('退出登录', '本地草稿将保留，下次登录后可继续编写。', { label: '退出登录' }))) return; if (leaveGuard && !(await leaveGuard())) return; try { await api('/api/auth/logout', { method: 'POST', body: {} }); state.user = null; state.stats = {}; leaveGuard = null; toast('已退出登录'); navigate('/problems', { force: true }); } catch(e) { toast(e.message,'danger'); } },{icon:'logout',variant:'menu-item danger-text'});
    details.append(summary,el('div',{class:'account-dropdown'},el('div',{class:'account-dropdown-title'},el('strong',{},user.username),el('small',{class:'muted'},user.role === 'admin' ? '管理员' : '学习者')),link('个人空间','/me',{class:'menu-item'}),link('修改密码','/me/password',{class:'menu-item'}),logout));account.append(details);
  } else {
    account.append(
      link('登录', `/login?next=${encodeURIComponent(location.pathname + location.search)}`, { class: 'btn small' }),
      link('注册', '/register', { class: 'btn primary small register-link' }),
    );
  }
  const menu = iconButton('menu', '展开导航菜单', toggleDrawer, { id: 'menu-toggle', class: 'btn icon-btn mobile-menu', 'aria-expanded': 'false', 'aria-controls': 'sidebar' });
  const header = el('header', { class: 'topbar' }, el('div', { class: 'header-start' }, menu, logo, nav), account);
  sidebar = el('aside', { class: 'sidebar', id: 'sidebar', 'aria-label': '侧边导航' });
  const sideNav = el('nav', { class: 'side-nav' });
  const visible = NAV.filter(item => isVisible(item,user));
  const current = visible.filter(item => sectionMatch(item) > 0).sort((a,b)=>sectionMatch(b)-sectionMatch(a))[0] || visible[0];
  sideNav.append(el('div', { class: 'nav-label' }, current?.label || 'WORKSPACE'));
  if (current?.children?.length) sideNav.append(el('div',{class:'nav-children'},current.children.filter(item=>isVisible(item,user)).map(item=>navItem(item,true))));
  if (!user) sideNav.append(el('div', { class: 'sidebar-callout' }, icon('trophy',22), el('strong', {}, '让每一次练习都有记录'), el('p', {}, '登录后保存进度，见证成长。'), link('开始你的旅程', '/login', { class: 'text-link' })));
  collapseButton = iconButton('panel', '收起或展开侧边栏', () => setCollapsed(!shell.classList.contains('sidebar-collapsed')), { 'aria-expanded': 'true' });
  sidebar.append(sideNav, el('div', { class: 'sidebar-bottom' }, collapseButton, el('span', {}, '专注每一次思考')));
  overlay = el('button', { class: 'drawer-overlay', 'aria-label': '关闭导航菜单', onclick: closeDrawer, tabIndex: -1 });
  main = el('main', { id: 'main', tabIndex: -1, class: `main-content ${workspace ? 'workspace-main' : ''} ${authPage ? 'auth-main' : ''}` });
  shell = el('div', { class: `app-shell ${workspace ? 'workspace-shell' : ''} ${authPage ? 'auth-shell' : ''}` }, header, overlay, sidebar, main);
  document.getElementById('app').replaceChildren(shell);
  setCollapsed(workspace || storage.get('oj.sidebarCollapsed') === '1',false);
  return main;
}

async function renderRoute() {
  const id = ++routeID; dispose(); dispose = () => {}; leaveGuard = null; closeDrawer();
  document.querySelectorAll('#dialogs dialog').forEach(d => { d.close(); d.remove(); });
  const path = location.pathname.replace(/\/$/,'') || '/';
  if (path === '/') { history.replaceState(null,'','/problems'); return renderRoute(); }
  const protectedRoute = /^\/(me|admin|submissions)(\/|$)/.test(path);
  if (protectedRoute && !state.user) { history.replaceState(null,'',`/login?next=${encodeURIComponent(location.pathname+location.search)}`); return renderRoute(); }
  const workspace = /^\/problems\/\d+$/.test(path), authPage = ['/login','/register'].includes(path);
  const host = buildShell(workspace,authPage);
  const controller = new AbortController(), cleaners = [];
  const ctx = { ...state, get user() { return state.user; }, signal: controller.signal, cleanup(fn) { cleaners.push(fn); }, alive: () => id === routeID, updateUser(user, stats) { state.user = user; if (stats) state.stats = stats; }, setLeaveGuard: state.setLeaveGuard, setTitle(title) { document.title = `${title} · 知序 OJ`; } };
  dispose = () => { controller.abort(); cleaners.forEach(fn => { try { fn(); } catch (e) { console.error(e); } }); };
  if (path.startsWith('/admin') && state.user?.role !== 'admin') { toast('请使用管理员账号登录','danger'); navigate(`/login?next=${encodeURIComponent(location.pathname+location.search)}`,{replace:true,force:true}); return; }
  try {
    if (authPage) await renderAuth(host,ctx,path === '/register');
    else if (path === '/me/password') await renderPassword(host,ctx);
    else if (path === '/me') await renderProfile(host,ctx);
    else if (path === '/problems') await renderProblems(host,ctx,false);
    else if (workspace) await renderWorkspace(host,ctx,Number(path.split('/')[2]));
    else if (path === '/admin/problems') await renderProblems(host,ctx,true);
    else if (path === '/admin/problems/new') await renderAdminEditor(host,ctx,null);
    else if (/^\/admin\/problems\/\d+\/edit$/.test(path)) await renderAdminEditor(host,ctx,Number(path.split('/')[3]));
    else if (path === '/admin/users') await renderUsers(host,ctx);
    else if (/^\/submissions(?:\/\d+)?$/.test(path)) await renderSubmissions(host,ctx,path.split('/')[2] ? Number(path.split('/')[2]) : null);
    else { ctx.setTitle('页面未找到'); host.append(el('div', {class:'not-found'},el('span',{class:'eyebrow'},'404 / NOT FOUND'),el('h1',{},'这一页还没有解法'),el('p',{class:'muted'},'链接可能已失效，去题库找一道新题吧。'),link('返回题库','/problems',{class:'btn primary'}))); }
  } catch(error) { if (id === routeID && error.name !== 'AbortError') host.replaceChildren(errorState(error,renderRoute)); }
  if (!workspace) window.scrollTo(0,0);
}

document.addEventListener('click', event => {
  document.querySelectorAll('.account-menu[open]').forEach(menu=>{if(!menu.contains(event.target))menu.open=false;});
  const anchor = event.target.closest('a[href]');
  if (!anchor || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || anchor.target || anchor.hasAttribute('download')) return;
  const url = new URL(anchor.href,location.origin);
  if (url.origin !== location.origin || url.hash || !/^\/(?:problems|login|register|me|admin|submissions)(\/|$)/.test(url.pathname)) return;
  event.preventDefault(); navigate(url.pathname + url.search);
});
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && drawerOpen) { closeDrawer(); document.getElementById('menu-toggle')?.focus(); }
  if (event.key === 'Escape') document.querySelectorAll('.account-menu[open]').forEach(menu=>{menu.open=false;menu.querySelector('summary')?.focus();});
  if (event.key === 'Tab' && drawerOpen) { const nodes = [...sidebar.querySelectorAll('a,button')].filter(x => x.getClientRects().length); if (event.shiftKey && document.activeElement === nodes[0]) { event.preventDefault(); nodes.at(-1)?.focus(); } else if (!event.shiftKey && document.activeElement === nodes.at(-1)) { event.preventDefault(); nodes[0]?.focus(); } }
});
window.addEventListener('popstate', async () => { const nextURL = location.pathname+location.search; if (leaveGuard && !(await leaveGuard())) { history.pushState(null,'',currentURL); return; } currentURL = nextURL; renderRoute(); });
window.addEventListener('beforeunload', event => { if (leaveGuard?.dirty?.()) { event.preventDefault(); event.returnValue = ''; } });

async function start() {
  try {
    await Promise.all([refreshSession(), api('/api/auth/config', { auth: false, timeout:15000 }).then(config => { state.config = config; }), refreshLanguages()]);
    await renderRoute();
  } catch(error) { document.getElementById('app').replaceChildren(el('main',{class:'startup-error'},errorState(error,start))); }
}
start();
