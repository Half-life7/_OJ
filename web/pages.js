import { el, icon, iconButton, button, link, select, field, badge, DIFFICULTIES, VERDICTS, difficulty, verdict, formatTime, formatMemory, dateTime, debounce, toast, setBusy, loading, empty, errorState, pageHeader, pagination, confirmAction } from './ui.js';
import { api, query } from './api.js';
import { watchSubmission, resultView, codeView } from './results.js';
import { matchesPath } from './nav.config.js';

export function passwordInput(name, autocomplete = 'current-password') {
  const input = el('input',{type:'password',name,id:name,autocomplete,required:true,maxLength:256});
  const toggle = iconButton('eye','显示密码',() => { const reveal = input.type === 'password'; input.type = reveal ? 'text' : 'password'; toggle.replaceChildren(icon(reveal ? 'eyeoff' : 'eye')); toggle.setAttribute('aria-label',reveal ? '隐藏密码' : '显示密码'); toggle.setAttribute('aria-pressed',String(reveal)); },{'aria-pressed':'false'});
  return { input, element: el('div',{class:'password-input'},input,toggle) };
}
function inlineError(input, message) {
  const parent = input.closest('.field');
  parent?.querySelector('.field-error')?.remove(); input.setAttribute('aria-invalid',String(!!message));
  if (message) { const id = `${input.id}-error`; input.setAttribute('aria-describedby',id); parent?.append(el('small',{class:'field-error',id,role:'alert'},message)); }
  else input.removeAttribute('aria-describedby');
  return !message;
}
function nextPath() { const next = new URLSearchParams(location.search).get('next') || '/problems'; try { const u = new URL(next,location.origin); if (u.origin === location.origin && !['/login','/register'].includes(u.pathname)) return u.pathname+u.search; } catch {} return '/problems'; }
export async function renderAuth(host, ctx, register) {
  const title = register ? '开启你的算法之旅' : '欢迎回来'; ctx.setTitle(register ? '注册' : '登录');
  const next = new URLSearchParams(location.search).get('next');
  const switchURL = (register ? '/login' : '/register') + (next ? query({next}) : '');
  const story = el('section',{class:'auth-story'},el('span',{class:'eyebrow'},'THINK. CODE. GROW.'),el('h1',{},'把每一个想法，',el('br'),'写成更好的解法。'),el('p',{},'从第一行代码到下一次突破。',el('br'),'在知序，专注思考，让成长自然发生。'),el('div',{class:'auth-code-art','aria-hidden':'true'},el('div',{class:'window-dots'},el('i'),el('i'),el('i')),el('pre',{},el('span',{class:'syntax-comment'},'// 小步前进，终有回响\n'),el('span',{class:'syntax-keyword'},'while '),'(curiosity) {\n    learn();\n    practice();\n    grow();\n}'),el('div',{class:'art-success'},icon('check',16),'Accepted · 下一题，继续。')),el('div',{class:'auth-footnote'},icon('code',16),'C++17 / Python 3 / Java'));
  const card = el('section',{class:'auth-card'},el('div',{class:'auth-mark'},icon('code',26)),el('h2',{},title),el('p',{class:'muted'},register ? '创建账号，记录每一次进步。' : '继续你的思考，与更好的自己相遇。'));
  host.append(el('div',{class:'auth-layout'},story,card));
  if (register && !ctx.config.allow_register) { card.append(empty('注册暂未开放','请联系管理员获取账号。',link('前往登录','/login',{class:'btn primary'}))); return; }
  const username = el('input',{id:'auth-username',name:'username',autocomplete:'username',required:true,maxLength:20,placeholder:'请输入用户名'});
  const password = passwordInput('auth-password',register ? 'new-password' : 'current-password'); password.input.placeholder = register ? '设置 8–128 位密码' : '请输入密码';
  const repeat = register ? passwordInput('auth-confirm','new-password') : null;
  const formError = el('div',{class:'form-error',role:'alert',hidden:true});
  const submit = el('button',{type:'submit',class:'btn primary auth-submit'},register ? '创建账号' : '登录',icon('chevron',17));
  const form = el('form',{class:'auth-form',noValidate:true},field('用户名',username,register ? '3–20 位字母、数字或下划线' : '',true),field('密码',password.element,'',true));
  password.element.previousSibling?.setAttribute('for',password.input.id);
  if (repeat) { repeat.input.placeholder = '再次输入密码'; const f = field('确认密码',repeat.element,'',true); f.querySelector('label').htmlFor = repeat.input.id; form.append(f); }
  form.append(formError,submit); card.append(form);
  if (!register && ctx.config.allow_register || register) card.append(el('p',{class:'auth-switch'},register ? '已有账号？ ' : '还没有账号？ ',link(register ? '直接登录' : '免费注册',switchURL,{class:'text-link'})));
  card.append(link('先逛逛题库', '/problems',{class:'auth-back'}));
  [username,password.input,repeat?.input].filter(Boolean).forEach(input => input.addEventListener('input',() => { inlineError(input,''); formError.hidden = true; }));
  form.addEventListener('submit',async event => {
    event.preventDefault(); if (submit.disabled) return;
    let valid = true;
    if (!username.value.trim() || register && !/^[A-Za-z0-9_]{3,20}$/.test(username.value.trim())) valid = inlineError(username,register ? '请输入 3–20 位字母、数字或下划线' : '请输入用户名');
    const length = [...password.input.value].length;
    if (!password.input.value || register && (length < 8 || length > 128)) { inlineError(password.input,register ? '密码长度应为 8–128 个字符' : '请输入密码'); valid = false; }
    if (repeat && repeat.input.value !== password.input.value) { inlineError(repeat.input,'两次输入的密码不一致'); valid = false; }
    if (!valid) { form.querySelector('[aria-invalid="true"]')?.focus(); return; }
    setBusy(submit,true,register ? '正在创建账号…' : '正在登录…'); formError.hidden = true;
    try { const data = await api(`/api/auth/${register ? 'register' : 'login'}`,{method:'POST',auth:false,body:{username:username.value.trim(),password:password.input.value},signal:ctx.signal}); ctx.updateUser(data.user,{}); toast(register ? '注册成功，欢迎来到知序！' : '登录成功，欢迎回来'); ctx.navigate(nextPath(),{replace:true}); }
    catch(error) { if (error.name !== 'AbortError') { formError.textContent = error.message; formError.hidden = false; if (error.code === 'username_taken') inlineError(username,error.message); } }
    finally { setBusy(submit,false); }
  });
}

export async function renderPassword(host,ctx) {
  ctx.setTitle('修改密码'); host.append(pageHeader('账号安全','定期更新密码，保护你的练习记录。'));
  const old = passwordInput('old-password'), next = passwordInput('new-password','new-password'), repeat = passwordInput('confirm-password','new-password');
  const error = el('div',{class:'form-error',role:'alert',hidden:true});
  const submit = el('button',{type:'submit',class:'btn primary'},'更新密码');
  const form = el('form',{class:'card password-form',noValidate:true},el('div',{class:'section-heading'},icon('lock',22),el('h2',{},'修改登录密码')));
  for (const [label,control] of [['原密码',old],['新密码',next],['确认新密码',repeat]]) { const f = field(label,control.element,label === '新密码' ? '8–128 个字符，建议使用与其他网站不同的密码。' : '',true); f.querySelector('label').htmlFor = control.input.id; form.append(f); control.input.addEventListener('input',()=>{inlineError(control.input,'');error.hidden=true;}); }
  form.append(error,el('div',{class:'row'},submit,link('返回个人空间','/me',{class:'btn'})),el('p',{class:'muted small-text'},'修改后，其他设备的登录状态将失效。当前设备保持登录。')); host.append(form);
  form.addEventListener('submit',async event => {
    event.preventDefault(); if (submit.disabled) return; let valid = true;
    if (!old.input.value) { inlineError(old.input,'请输入原密码'); valid = false; }
    const length = [...next.input.value].length;
    if (length < 8 || length > 128) { inlineError(next.input,'新密码长度应为 8–128 个字符'); valid = false; }
    if (repeat.input.value !== next.input.value) { inlineError(repeat.input,'两次输入的新密码不一致'); valid = false; }
    if (!valid) { form.querySelector('[aria-invalid="true"]')?.focus(); return; }
    setBusy(submit,true,'正在更新…');
    try { await api('/api/auth/password',{method:'POST',body:{old_password:old.input.value,new_password:next.input.value},signal:ctx.signal}); form.reset(); toast('密码已更新'); ctx.navigate('/me'); }
    catch(e) { if (e.name !== 'AbortError') { error.hidden = false; error.textContent = e.message; if (e.code === 'invalid_password') inlineError(old.input,e.message); } }
    finally { setBusy(submit,false); }
  });
}

function statCard(label,value,glyph,caption = '') { return el('div',{class:'stat-card card'},el('div',{class:'row spread'},el('span',{class:'muted'},label),el('span',{class:'stat-icon'},icon(glyph,20))),el('strong',{class:'stat-value'},String(value ?? 0)),caption && el('small',{class:'muted'},caption)); }
export async function renderProfile(host,ctx) {
  ctx.setTitle('个人空间'); host.append(loading());
  const data = await api('/api/auth/me',{signal:ctx.signal}); if (!ctx.alive()) return;
  const user = data.user, stats = data.stats || {}; ctx.updateUser(user,stats);
  const rate = Number(stats.acceptance_rate || 0);
  host.replaceChildren(pageHeader('个人空间','每一道认真思考的题，都算数。',link('继续练习','/problems',{class:'btn primary'})),el('section',{class:'profile-banner card'},el('div',{class:'avatar large'},user.username.slice(0,1).toUpperCase()),el('div',{},el('div',{class:'row'},el('h2',{},user.username),badge(user.role === 'admin' ? '管理员' : '学习者',user.role === 'admin' ? 'accent' : '')),el('p',{class:'muted'},`加入于 ${dateTime(user.created_at)}`)),link('账号安全','/me/password',{class:'btn small'})),el('div',{class:'stats-grid'},statCard('累计通过',stats.solved,'trophy','每一次 Accepted 都值得记录'),statCard('累计提交',stats.submissions,'send','不断尝试，持续进步'),statCard('通过率',`${rate.toFixed(1)}%`,'chart','通过提交 / 全部提交'),statCard('通过提交',stats.accepted,'check','所有成功的尝试')));
  const breakdown = el('section',{class:'card difficulty-progress'},el('h2',{},'解题分布'));
  for (const d of DIFFICULTIES) {
    const width = Math.min(100, Number(stats[d.value] || 0) / Math.max(1, Number(stats.solved || 0)) * 100);
    breakdown.append(el('div', { class: 'difficulty-progress-row' },
      difficulty(d.value),
      el('div', { class: 'progress-track' }, el('div', { class: `progress-fill ${d.value}`, style: { width: `${width}%` } })),
      el('strong', {}, String(stats[d.value] || 0))));
  }
  const recent = el('section',{class:'card'},el('div',{class:'card-heading'},el('h2',{},'最近提交'),link('查看全部','/submissions',{class:'text-link'}))); const list = el('div',{},loading(3)); recent.append(list); host.append(el('div',{class:'profile-grid'},breakdown,recent));
  try { const response = await api('/api/submissions?page=1&page_size=20',{signal:ctx.signal}); if (ctx.alive()) list.replaceChildren(response.items?.length ? submissionTable(response.items.slice(0,5),ctx) : empty('从第一题开始','选择一道感兴趣的题，写下你的第一个解法。',link('探索题库','/problems',{class:'btn primary'}))); }
  catch(e) { if(e.name !== 'AbortError') list.replaceChildren(errorState(e,()=>ctx.navigate('/me',{replace:true}))); }
}

function statusCell(problem) {
  const value = problem.status;
  return value === 'solved' || value === 'accepted' ? el('span',{class:'solved-mark',title:'已通过','aria-label':'已通过'},icon('check',18)) : value === 'attempted' || value === 'tried' ? el('span',{class:'attempted-mark',title:'已尝试','aria-label':'已尝试'},icon('clock',17)) : el('span',{class:'unsolved-mark',title:'未尝试','aria-label':'未尝试'},'—');
}
export async function renderProblems(host,ctx,admin = false) {
  ctx.setTitle(admin ? '题目管理' : '探索题库');
  const params = new URLSearchParams(location.search); let page = Math.max(1,Number(params.get('page')) || 1), request = 0;
  const search = el('input',{type:'search',placeholder:'搜索题号、题目名称…',value:params.get('q') || '','aria-label':'搜索题目'});
  const diff = select([{value:'',label:'全部难度'},...DIFFICULTIES],params.get('difficulty') || '',()=>update(),{'aria-label':'按难度筛选'});
  const tags = select([{value:'',label:'全部标签'}],params.get('tag') || '',()=>update(),{'aria-label':'按标签筛选'});
  const list = el('div',{class:'problem-list-body'}), foot = el('div'), summary = el('span',{class:'muted small-text'},'正在加载…');
  const reset = button('清除筛选',()=>{search.value='';diff.value='';tags.value='';update();},{variant:'text small'});
  const debounced = debounce(()=>update(),300); search.addEventListener('input',debounced); ctx.cleanup(debounced.cancel);
  const filters = el('div',{class:'filter-bar'},el('div',{class:'search-input'},icon('search'),search),diff,tags,reset);
  if (admin) host.append(pageHeader('题目管理','把好题目带给每一个认真思考的人。',link('', '/admin/problems/new',{class:'btn primary'}))); else {
    host.append(el('section',{class:'practice-banner'},el('div',{},el('span',{class:'eyebrow'},'YOUR NEXT ACCEPTED STARTS HERE'),el('h1',{},'今天，也向前一步。'),el('p',{},'从一道好题开始，让思考成为习惯。'),el('div',{class:'banner-chips'},el('span',{},icon('code',15),'多语言编程'),el('span',{},icon('activity',15),'即时在线评测'))),el('div',{class:'banner-art','aria-hidden':'true'},el('div',{class:'orbit orbit-one'}),el('div',{class:'orbit orbit-two'}),el('div',{class:'art-code'},icon('code',54)),el('span',{class:'art-badge'},icon('check',15),'Accepted'))));
  }
  if (admin) host.querySelector('.page-heading a').append(icon('plus',17),'创建题目');
  const heading = el('div',{class:'card-heading'},el('div',{class:'row'},el('h2',{},admin ? '全部题目' : '题库'),summary),!admin && button('随机一题',randomProblem,{variant:'small',icon:'refresh'}));
  const card = el('section',{class:'card problem-card'},heading,filters,list,foot); host.append(card);
  if (!admin && !ctx.user) host.append(el('div',{class:'subtle-callout'},icon('info',16),el('span',{},'登录后可查看做题状态、保存代码草稿和提交评测。'),link('立即登录','/login',{class:'text-link'})));
  function url() { return (admin ? '/admin/problems' : '/problems') + query({q:search.value.trim(),difficulty:diff.value,tag:tags.value,page:page > 1 ? page : ''}); }
  function update(resetPage = true) { if (resetPage) page=1; history.replaceState(null,'',url()); refreshNavActive(); load(); }
  function refreshNavActive() { document.querySelectorAll('.side-link').forEach(node=>{const yes=matchesPath(node.href,location.href);node.classList.toggle('active',yes);if(yes)node.setAttribute('aria-current','page');else node.removeAttribute('aria-current');}); }
  async function randomProblem() {
    try { const first = await api('/api/problems?page=1&page_size=1',{signal:ctx.signal}); if (!first.total) { toast('题库暂无公开题目','danger'); return; } const chosen = 1+Math.floor(Math.random()*first.total); const response = await api(`/api/problems?page=${chosen}&page_size=1`,{signal:ctx.signal}); if (response.items?.[0]) ctx.navigate(`/problems/${response.items[0].id}`); }
    catch(e) { if(e.name !== 'AbortError')toast(e.message,'danger'); }
  }
  async function load() {
    const seq = ++request; list.replaceChildren(loading(7)); foot.replaceChildren();
    try {
      const data = await api(`${admin ? '/api/admin/problems' : '/api/problems'}${query({q:search.value.trim(),difficulty:diff.value,tag:tags.value,page,page_size:20})}`,{signal:ctx.signal}); if(seq!==request||!ctx.alive())return;
      if (!data.items?.length && data.total>0 && page>1) { page=Math.max(1,Math.ceil(data.total/20));update(false);return; }
      summary.textContent=`${data.total || 0} 道题目`;
      if (!data.items?.length) list.replaceChildren(empty('没有找到匹配的题目',admin?'试试调整筛选条件，或创建第一道题目。':'换个关键词，或者选择其他难度试试。',button('清除筛选',()=>{search.value='';diff.value='';tags.value='';update();})));
      else {
        const table=el('table',{class:'data-table problem-table'},el('thead',{},el('tr',{},!admin&&ctx.user&&el('th',{class:'status-column'},'状态'),el('th',{class:'id-column'},'题号'),el('th',{},'题目'),el('th',{},'难度'),el('th',{},'通过率'),admin&&el('th',{},'用例数'),admin&&el('th',{},'提交 / 通过'),admin&&el('th',{},'更新时间'),admin&&el('th',{},'可见性'),admin&&el('th',{},'操作'))));
        const body=el('tbody'); data.items.forEach(p=>{
          const acceptance=p.submit_count?`${(p.accepted_count/p.submit_count*100).toFixed(1)}%`:'—';
          const title=el('div',{class:'problem-title-cell'},link(p.title,admin?`/admin/problems/${p.id}/edit`:`/problems/${p.id}`,{class:'problem-title'}),el('div',{class:'tag-list'},(p.tags||[]).map(t=>button(t,()=>{tags.value=t;update();},{variant:'tag',title:`筛选标签：${t}`}))));
          const row=el('tr',{},!admin&&ctx.user&&el('td',{class:'status-column'},statusCell(p)),el('td',{class:'id-column mono'},String(p.id)),el('td',{},title),el('td',{},difficulty(p.difficulty)),el('td',{class:'mono acceptance-cell'},acceptance));
          if(admin){
            const toggle=button(p.is_public?'公开':'私有',async()=>{toggle.disabled=true;try{const full=await api(`/api/admin/problems/${p.id}`,{signal:ctx.signal});await api(`/api/admin/problems/${p.id}`,{method:'PUT',body:{...full,is_public:!full.is_public},signal:ctx.signal});toast(full.is_public?'题目已设为私有':'题目已公开');load();}catch(e){if(e.name!=='AbortError')toast(e.message,'danger');}finally{toggle.disabled=false;}},{variant:`visibility-toggle ${p.is_public?'public':''}`,'aria-label':`${p.title}，${p.is_public?'公开，点击设为私有':'私有，点击公开'}`});
            row.append(el('td',{class:'mono'},String(p.testcase_count||0)),el('td',{class:'mono'},`${p.submit_count||0} / ${p.accepted_count||0}`),el('td',{class:'small-text muted'},dateTime(p.updated_at)),el('td',{},toggle),el('td',{},el('div',{class:'row'},link('编辑',`/admin/problems/${p.id}/edit`,{class:'btn small'}),iconButton('trash',`删除 ${p.title}`,async()=>{if(!(await confirmAction('删除题目',`确定删除「${p.title}」？此操作无法撤销。`,{danger:true,label:'删除题目'})))return;try{await api(`/api/admin/problems/${p.id}`,{method:'DELETE',signal:ctx.signal});toast('题目已删除');load();}catch(e){if(e.name!=='AbortError')toast(e.message,'danger');}},{class:'btn icon-btn danger-text'}))));
          } body.append(row);
        }); table.append(body);
        const cards=el('div',{class:'problem-mobile-cards'},data.items.map(p=>el('article',{class:'problem-mobile-card'},el('div',{class:'row spread'},el('span',{class:'mono muted'},`#${p.id}`),el('div',{class:'row'},!admin&&ctx.user&&statusCell(p),difficulty(p.difficulty))),link(p.title,admin?`/admin/problems/${p.id}/edit`:`/problems/${p.id}`,{class:'problem-title'}),el('div',{class:'row spread'},el('div',{class:'tag-list'},(p.tags||[]).slice(0,3).map(t=>badge(t))),el('small',{class:'muted'},`通过率 ${p.submit_count?(p.accepted_count/p.submit_count*100).toFixed(1)+'%':'—'}`)),admin&&el('div',{class:'admin-mobile-meta small-text muted'},`${p.testcase_count||0} 个用例 · ${p.submit_count||0} 提交 · 更新于 ${dateTime(p.updated_at)}`),admin&&el('div',{class:'row'},badge(p.is_public?'公开':'私有',p.is_public?'success':''),link('管理题目',`/admin/problems/${p.id}/edit`,{class:'text-link'})))));
        list.replaceChildren(el('div',{class:'table-scroll'},table),cards);
      }
      foot.replaceChildren(pagination(data.total||0,page,20,n=>{page=n;update(false);card.scrollIntoView({block:'start'});}));
    } catch(e) { if(seq===request&&e.name!=='AbortError'){summary.textContent='';list.replaceChildren(errorState(e,load));} }
  }
  const tagsLoad=api('/api/tags',{signal:ctx.signal}).then(data=>{const selected=params.get('tag')||'';tags.replaceChildren(el('option',{value:''},'全部标签'),...(data.items||[]).map(t=>el('option',{value:t},t)));if(selected&&![...tags.options].some(o=>o.value===selected))tags.append(el('option',{value:selected},selected));tags.value=selected;}).catch(()=>{});
  await tagsLoad; if(!ctx.alive())return;
  if(params.get('random')==='1'){history.replaceState(null,'','/problems');await randomProblem();if(!ctx.alive())return;}
  await load();
}

export function submissionTable(items,ctx,{onSelect,expandable=false}={}) {
  const table=el('table',{class:'data-table submission-table'},el('thead',{},el('tr',{},['提交','题目','结果','语言','耗时 / 内存','提交时间',''].map(t=>el('th',{},t)))));
  const body=el('tbody');
  items.forEach(s=>{
    const href=`/submissions/${s.id}`;
    let action;
    if(onSelect) action=button('查看',()=>onSelect(s),{variant:'small'});
    else if(expandable) action=button('展开',event=>toggleDetail(event),{variant:'small','aria-expanded':'false'});
    else action=link('查看',href,{class:'btn small'});
    const row=el('tr',{class:expandable?'expandable-row':''},el('td',{class:'mono'},`#${s.id}`),el('td',{},link(s.problem_title||`题目 #${s.problem_id}`,`/problems/${s.problem_id}`,{class:'table-title'})),el('td',{},verdict(s.status)),el('td',{class:'mono'},s.language),el('td',{class:'small-text'},el('div',{},formatTime(s)),el('div',{class:'muted'},formatMemory(s.memory_kb))),el('td',{class:'small-text muted'},dateTime(s.created_at)),el('td',{},action));
    const detailHost=el('div',{class:'submission-inline-detail'},resultView(s),codeView(s.code,s.language));
    const detailRow=el('tr',{class:'submission-detail-row',hidden:true},el('td',{colSpan:7},detailHost));
    let expanded=false;
    function toggleDetail(event){
      event?.stopPropagation();
      expanded=!expanded;
      detailRow.hidden=!expanded;
      row.classList.toggle('expanded',expanded);
      if(action.tagName==='BUTTON'){action.textContent=expanded?'收起':'展开';action.setAttribute('aria-expanded',String(expanded));}
    }
    if(expandable)row.addEventListener('click',event=>{if(!event.target.closest('a,button'))toggleDetail(event);});
    body.append(row,detailRow);
  });
  table.append(body);return el('div',{class:'table-scroll'},table);
}
export async function renderSubmissions(host,ctx,id=null) {
  ctx.setTitle(id?`提交 #${id}`:'提交记录');
  if(id){host.append(pageHeader(`提交 #${id}`,'每一次尝试，都让解法更进一步。',link('返回提交记录','/submissions',{class:'btn',})));const result=el('section',{class:'card submission-detail'});host.append(result);await watchSubmission(id,result,ctx);return;}
  host.append(pageHeader('提交记录','回顾你的每次尝试，找到下一次突破的方向。'));
  const params=new URLSearchParams(location.search);let page=Math.max(1,Number(params.get('page'))||1),seq=0;
  const pid=el('input',{type:'number',min:1,step:1,placeholder:'按题号筛选',value:params.get('problem_id')||'','aria-label':'按题号筛选'});
  const status=select([{value:'',label:'全部结果'},...Object.entries(VERDICTS).filter(([v])=>!['queued','running','compiling','cancelled'].includes(v)).map(([value,[label]])=>({value,label}))],params.get('status')||'',()=>update(),{'aria-label':'按评测结果筛选'});
  const list=el('div'),foot=el('div'),card=el('section',{class:'card'},el('div',{class:'filter-bar'},pid,status,button('刷新',()=>load(),{icon:'refresh',variant:'small'}),button('清除筛选',()=>{pid.value='';status.value='';update();},{variant:'text small'})),list,foot);host.append(card);
  const changed=debounce(()=>update(),300);pid.addEventListener('input',changed);ctx.cleanup(changed.cancel);
  function update(){page=1;load();}
  async function load(){const current=++seq;const q=query({problem_id:pid.value,status:status.value,page,page_size:20});history.replaceState(null,'',`/submissions${query({problem_id:pid.value,status:status.value,page:page>1?page:''})}`);list.replaceChildren(loading());foot.replaceChildren();try{const data=await api('/api/submissions'+q,{signal:ctx.signal});if(current!==seq||!ctx.alive())return;list.replaceChildren(data.items?.length?submissionTable(data.items,ctx,{expandable:true}):empty('还没有提交记录','试试其他筛选条件，或者从一道新题开始。',link('去题库练习','/problems',{class:'btn primary'})));foot.replaceChildren(pagination(data.total||0,page,20,n=>{page=n;load();}));}catch(e){if(current===seq&&e.name!=='AbortError')list.replaceChildren(errorState(e,load));}}
  await load();
}
