// The navigation is configuration-driven. Add an item here only; the navbar,
// sidebar, role checks and query-aware active state are rendered from this data.
export const NAV = [
  {
    id: 'problems', label: '题库', icon: 'book', path: '/problems',
    enabled: true, inNavbar: true,
    children: [
      { id: 'problems-all', label: '全部题目', icon: 'list', path: '/problems' },
      { id: 'problems-random', label: '随机一题', icon: 'refresh', path: '/problems?random=1' },
    ],
  },
  {
    id: 'submissions', label: '提交记录', icon: 'activity', path: '/submissions',
    enabled: true, inNavbar: true, requireAuth: true,
    children: [
      { id: 'submissions-all', label: '全部提交', icon: 'activity', path: '/submissions' },
    ],
  },
  {
    id: 'manage', label: '管理', icon: 'edit', path: '/admin/problems',
    enabled: true, inNavbar: true, requireAuth: true, roles: ['admin'],
    children: [
      { id: 'manage-problems', label: '题目列表', icon: 'list', path: '/admin/problems', roles: ['admin'] },
      { id: 'manage-create', label: '创建题目', icon: 'plus', path: '/admin/problems/new', roles: ['admin'] },
      { id: 'manage-users', label: '用户管理', icon: 'users', path: '/admin/users', roles: ['admin'] },
    ],
  },
  {
    id: 'me', label: '个人中心', icon: 'user', path: '/me',
    enabled: true, inNavbar: false, requireAuth: true,
    children: [
      { id: 'me-profile', label: '个人资料', icon: 'chart', path: '/me' },
      { id: 'me-password', label: '修改密码', icon: 'lock', path: '/me/password' },
    ],
  },
  { id: 'contests', label: '比赛', icon: 'trophy', path: '/contests', enabled: false, inNavbar: true },
  { id: 'ranking', label: '排行榜', icon: 'chart', path: '/ranking', enabled: false, inNavbar: true },
  { id: 'discuss', label: '讨论区', icon: 'users', path: '/discuss', enabled: false, inNavbar: true },
];

export function isVisible(item, user) {
  return item?.enabled !== false &&
    (!item.requireAuth || Boolean(user)) &&
    (!item.roles?.length || item.roles.includes(user?.role));
}

export function matchesPath(path, current = location.pathname + location.search) {
  try {
    const target = new URL(path, location.origin);
    const actual = new URL(current, location.origin);
    if (target.pathname.replace(/\/$/, '') !== actual.pathname.replace(/\/$/, '')) return false;
    if (![...target.searchParams.keys()].length) {
      const filters = target.pathname === '/problems' ? ['random'] : ['q', 'difficulty', 'tag', 'random'];
      return filters.every(key => !actual.searchParams.has(key));
    }
    return [...target.searchParams.entries()].every(([key, value]) => actual.searchParams.get(key) === value);
  } catch {
    return false;
  }
}

// A main section can own several URL prefixes (for example 管理 owns both
// /admin/problems and /admin/users). Return the longest matching prefix so the
// most specific configured section wins.
export function sectionMatch(item, current = location.pathname) {
  const actual = new URL(current, location.origin).pathname.replace(/\/$/, '') || '/';
  const paths = [item.path, ...(item.children || []).map(child => child.path)];
  let best = 0;
  for (const value of paths) {
    if (!value) continue;
    const pathname = new URL(value, location.origin).pathname.replace(/\/$/, '') || '/';
    if (actual === pathname || actual.startsWith(pathname + '/')) {
      best = Math.max(best, pathname.length);
    }
  }
  return best;
}
