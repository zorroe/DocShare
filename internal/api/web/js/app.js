/* ============================================================
   DocShare 前端逻辑
   - 文档树浏览 / Markdown 渲染
   - 编辑申请提交
   - 审批中心(管理员)
   ============================================================ */
'use strict';

/* 网页端判定: http(s) 协议即网页(浏览器/局域网访问)。
   注意: Wails 在 Windows 上壳页使用 http://wails.localhost/ (其他平台 wails://),
   因此需排除 wails.localhost 主机。
   桌面端判定: Wails 注入 window.go 且非网页协议。
   任何通过 http(s) 访问的页面(即使环境注入了 window.go)一律视为网页端,
   不提供设置/访问记录/审批等管理功能。
   (window.__DSH_TEST_DESKTOP 仅供自动化测试模拟桌面环境) */
function detectIsWeb() {
  return /^https?:/.test(location.protocol) && location.hostname !== 'wails.localhost';
}
const IS_WEB = detectIsWeb();
const DESKTOP = !!window.go && !!window.go.main && !!window.go.main.App &&
  (!IS_WEB || window.__DSH_TEST_DESKTOP === true);

/* ---------- 安全存储 ----------
   Wails 壳页(wails:// scheme)可能没有 localStorage, 退化为内存存储 */
const store = (() => {
  try {
    const t = '__ds_test__';
    localStorage.setItem(t, '1');
    localStorage.removeItem(t);
    return localStorage;
  } catch {
    const mem = {};
    return {
      getItem: (k) => (k in mem ? mem[k] : null),
      setItem: (k, v) => { mem[k] = String(v); },
      removeItem: (k) => { delete mem[k]; },
    };
  }
})();

/* ---------- 图标 ---------- */
const ICONS = {
  folder: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>',
  file: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>',
  chevron: '<svg class="chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>',
  sun: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>',
  moon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>',
  clock: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><polyline points="12 7 12 12 15 14"/></svg>',
  doc: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>',
  shield: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/></svg>',
  check: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>',
  list: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>',
  chevronRight: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>',
  eye: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>',
  eyeOff: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>',
  comment: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>',
  trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/></svg>',
  undo: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>',
};

/* ---------- 状态 ---------- */
const state = {
  tree: null,
  treeSig: '',
  ready: false, // 文档目录是否已配置
  currentDoc: null, // { path, name, content, modified }
  search: '',
  theme: store.getItem('docshare-theme') || 'dark',
  apiBase: '', // 桌面壳页(wails://)下指向 http://127.0.0.1:端口
  serverInfo: null,
  collapsedDirs: new Set(), // 用户手动折叠的目录(树刷新时恢复)
  authToken: store.getItem('docshare-auth') || '',
  passwordChanged: false,
  authEnabled: false,
  docsDirs: [],
  recent: (() => { try { return JSON.parse(store.getItem('docshare-recent') || '[]'); } catch { return []; } })(),
  scrollTimer: null,
  userScrolled: false, // 用户是否已主动滚动(自动恢复位置时让位)
  annotations: [], // 当前文档批注列表
  annoSig: '',      // 批注列表签名(轮询对比用)
  activeAnno: '',   // 批注面板当前定位的批注 id
};

/* ---------- DOM 引用 ---------- */
const $ = (id) => document.getElementById(id);
const els = {
  tree: $('tree'),
  recentBox: $('recentBox'),
  search: $('searchInput'),
  themeBtn: $('themeBtn'),
  menuBtn: $('menuBtn'),
  menuPop: $('menuPop'),
  searchResults: $('searchResults'),
  loginMask: $('loginMask'),
  loginPassword: $('loginPassword'),
  loginError: $('loginError'),
  loginBtn: $('loginBtn'),
  exportBtn: $('exportBtn'),
  exportMenu: $('exportMenu'),
  sidebar: document.querySelector('.sidebar'),
  resizer: $('sidebarResizer'),
  docView: $('docView'),
  crumbs: $('crumbs'),
  docMeta: $('docMeta'),
  toast: $('toast'),
  settingsMask: $('settingsMask'),
  setDocsDir: $('setDocsDir'),
  pickDirBtn: $('pickDirBtn'),
  addDirBtn: $('addDirBtn'),
  multiDirs: $('multiDirs'),
  setPort: $('setPort'),
  setLan: $('setLan'),
  lanUrlText: $('lanUrlText'),
  copyUrlBtn: $('copyUrlBtn'),
  setAutoStart: $('setAutoStart'),
  setPassword: $('setPassword'),
  pwdToggle: $('pwdToggle'),
  checkUpdateBtn: $('checkUpdateBtn'),
  updateStatus: $('updateStatus'),
  updateBadge: $('updateBadge'),
  updateMask: $('updateMask'),
  updateVersionInfo: $('updateVersionInfo'),
  updateNotes: $('updateNotes'),
  updateLaterBtn: $('updateLaterBtn'),
  updateInstallBtn: $('updateInstallBtn'),
  setBlacklist: $('setBlacklist'),
  saveSettingsBtn: $('saveSettingsBtn'),
  openBrowserBtn: $('openBrowserBtn'),
  settingsStatus: $('settingsStatus'),
  accessMask: $('accessMask'),
  accessList: $('accessList'),
  accessStatus: $('accessStatus'),
  accessRefresh: $('accessRefresh'),
  dirMask: $('dirMask'),
  dirPath: $('dirPath'),
  dirList: $('dirList'),
  dirUpBtn: $('dirUpBtn'),
  dirChooseBtn: $('dirChooseBtn'),
  treeTip: $('treeTip'),
  annoBtn: $('annoBtn'),
  annoCount: $('annoCount'),
  annoListMask: $('annoListMask'),
  annoListSub: $('annoListSub'),
  annoList: $('annoList'),
  annoViewMask: $('annoViewMask'),
  annoViewQuote: $('annoViewQuote'),
  annoViewBody: $('annoViewBody'),
  annoViewHint: $('annoViewHint'),
  annoViewResolve: $('annoViewResolve'),
  annoViewDelete: $('annoViewDelete'),
  annoMask: $('annoMask'),
  annoQuotePreview: $('annoQuotePreview'),
  annoContent: $('annoContent'),
  annoAuthor: $('annoAuthor'),
  annoSubmit: $('annoSubmit'),
};

/* 目录选择器当前浏览路径 */
let dirBrowsePath = '';

/* ---------- 工具 ---------- */
function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

function fmtSize(bytes) {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / 1024 / 1024).toFixed(1) + ' MB';
}

function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function toast(msg, type = 'ok', ms = 2600) {
  els.toast.innerHTML = `${type === 'ok' ? ICONS.check : ICONS.x}<span>${esc(msg)}</span>`;
  els.toast.className = `toast ${type}`;
  els.toast.hidden = false;
  clearTimeout(toast._t);
  toast._t = setTimeout(() => { els.toast.hidden = true; }, ms);
}

/* ---------- API 封装 ---------- */
async function api(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  if (state.authToken && !opts.noAuth) headers['Authorization'] = 'Bearer ' + state.authToken;
  const res = await fetch(state.apiBase + path, { ...opts, headers, body: opts.body ? JSON.stringify(opts.body) : undefined });
  let data = null;
  try { data = await res.json(); } catch { /* ignore */ }
  if (!res.ok) {
    const msg = (data && data.error) || `请求失败 (${res.status})`;
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return data;
}

/* ============================================================
   访问密码认证
   ============================================================ */
async function checkAuth() {
  let status;
  try {
    status = await api('/api/auth/status'); // 携带已有 token, 服务端判断是否已认证
  } catch {
    return true; // 服务不可用时放行(由其他逻辑提示)
  }
  state.authEnabled = !!status.enabled;
  if (!status.enabled) return true; // 未启用密码, 直接放行
  if (status.authed) {
    state.authToken = store.getItem('docshare-auth') || '';
    return true;
  }
  // 未认证: 桌面壳使用后端签发的令牌，绝不接触明文访问密码。
  if (DESKTOP && state.serverInfo && state.serverInfo.authToken) {
    try {
      state.authToken = state.serverInfo.authToken;
      store.setItem('docshare-auth', state.authToken);
      const verified = await api('/api/auth/status');
      if (verified.authed) return true;
    } catch { /* 自动登录失败则走遮罩 */ }
  }
  showLogin();
  return false;
}

function showLogin() {
  els.loginMask.hidden = false;
  setTimeout(() => els.loginPassword.focus(), 100);
}

async function doLogin() {
  const pw = els.loginPassword.value;
  if (!pw) return;
  try {
    const res = await api('/api/auth/login', { noAuth: true, method: 'POST', body: { password: pw } });
    state.authToken = res.token;
    store.setItem('docshare-auth', res.token);
    els.loginMask.hidden = true;
    els.loginError.hidden = true;
    location.reload(); // 重新初始化
  } catch (err) {
    // 服务端可能返回锁定提示(连续失败过多, 请稍后再试)
    els.loginError.textContent = err.message || '密码错误，请重试';
    els.loginError.hidden = false;
    els.loginPassword.value = '';
    els.loginPassword.focus();
  }
}

/* ============================================================
   Markdown 渲染
   ============================================================ */
marked.setOptions({ gfm: true, breaks: true });

// 当前渲染的文档基准路径(供 image renderer 拼接相对图片)
let currentBasePath = '';

// 自定义图片渲染: 本地图片(相对/绝对路径)在净化前转换为 /api/file 安全形式,
// 避免 DOMPurify 将 E:\ 等本地路径判定为危险协议而清空 src。
marked.use({
  renderer: {
    image(href, title, text) {
      let h = href || '';
      if (!/^(https?:|data:)/i.test(h)) {
        if (!/^[A-Za-z]:[\\/]/.test(h) && !h.startsWith('/')) {
          const dir = currentBasePath && currentBasePath.includes('/') ? currentBasePath.slice(0, currentBasePath.lastIndexOf('/')) : '';
          h = dir ? dir + '/' + h : h;
        }
        h = state.apiBase + '/api/file?path=' + encodeURIComponent(h);
      }
      const titleAttr = title ? ` title="${esc(title)}"` : '';
      return `<img src="${h}" alt="${esc(text)}"${titleAttr} class="doc-img">`;
    },
    link(href, title, text) {
      let h = href || '';
      // 外部链接: 新窗口打开
      if (/^(https?:|mailto:)/i.test(h)) {
        return `<a href="${esc(h)}" target="_blank" rel="noopener">${text}</a>`;
      }
      // 页面内锚点
      if (h.startsWith('#')) {
        return `<a href="${esc(h)}">${text}</a>`;
      }
      // 本地 Markdown 文档链接 → 站内导航(点击打开文档)
      if (/\.(md|markdown)$/i.test(h)) {
        let target = h;
        if (!/^[A-Za-z]:[\\/]/.test(target) && !target.startsWith('/')) {
          const dir = currentBasePath && currentBasePath.includes('/') ? currentBasePath.slice(0, currentBasePath.lastIndexOf('/')) : '';
          target = dir ? dir + '/' + target : target;
        }
        const titleAttr = title ? ` title="${esc(title)}"` : '';
        return `<a href="#" class="doc-link" data-doc-path="${esc(target)}"${titleAttr}>${text}</a>`;
      }
      // 其他本地资源链接: 原样保留
      return `<a href="${esc(h)}"${title ? ` title="${esc(title)}"` : ''}>${text}</a>`;
    },
  },
});

let mermaidSeq = 0;
const mermaidSources = new Map(); // id -> 源码(主题切换时重渲染)
let mermaidLoadPromise;

function loadMermaid() {
  if (window.mermaid) return Promise.resolve(window.mermaid);
  if (!mermaidLoadPromise) {
    mermaidLoadPromise = new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = 'vendor/mermaid.min.js?v=1.4.1';
      script.onload = () => window.mermaid
        ? resolve(window.mermaid)
        : reject(new Error('Mermaid 初始化失败'));
      script.onerror = () => reject(new Error('Mermaid 加载失败'));
      document.head.appendChild(script);
    });
  }
  return mermaidLoadPromise;
}

// 初始化 Mermaid(跟随页面主题)
function initMermaid(instance = window.mermaid) {
  if (!instance) return;
  const isDark = (document.documentElement.dataset.theme || 'dark') === 'dark';
  instance.initialize({
    startOnLoad: false,
    theme: isDark ? 'dark' : 'default',
    securityLevel: 'strict',
  });
}

// 渲染 Mermaid 图表(替换 ```mermaid 代码块)
async function renderMermaid(container) {
  const blocks = container.querySelectorAll('pre code.language-mermaid');
  if (!blocks.length) return;
  const instance = await loadMermaid();
  initMermaid(instance);
  for (const code of blocks) {
    const pre = code.closest('pre');
    const src = code.textContent;
    const id = 'mmd-' + (++mermaidSeq);
    const div = document.createElement('div');
    div.className = 'mermaid';
    div.dataset.mermaidId = id;
    div.textContent = src;
    pre.replaceWith(div);
    try {
      const { svg } = await instance.render(id, src);
      div.innerHTML = svg;
      mermaidSources.set(id, src);
    } catch (e) {
      div.innerHTML = `<div class="mermaid-error">图表渲染失败: ${esc(String(e && e.message || e))}</div>`;
    }
  }
}

// 主题切换后重渲染当前文档的 Mermaid 图表
async function rerenderMermaid() {
  if (!state.currentDoc || !mermaidSources.size) return;
  const instance = await loadMermaid();
  initMermaid(instance);
  document.querySelectorAll('#docView .mermaid[data-mermaid-id]').forEach(async (div) => {
    const src = mermaidSources.get(div.dataset.mermaidId);
    if (!src) return;
    try {
      const id = div.dataset.mermaidId;
      const { svg } = await instance.render(id, src);
      div.innerHTML = svg;
    } catch { /* 保留旧图 */ }
  });
}

// 复制文本(兼容非安全上下文与剪贴板权限拒绝)
function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text).catch(() => legacyCopy(text));
  }
  return legacyCopy(text);
}

function legacyCopy(text) {
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); } catch { /* ignore */ }
  ta.remove();
  return Promise.resolve();
}

// 给代码块添加"复制"按钮
function addCopyButtons(container) {
  container.querySelectorAll('pre').forEach((pre) => {
    if (pre.querySelector('.copy-btn')) return;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'copy-btn';
    btn.textContent = '复制';
    btn.addEventListener('click', () => {
      const code = pre.querySelector('code');
      const text = code ? code.textContent : pre.textContent;
      copyText(text).then(() => {
        btn.textContent = '已复制';
        btn.classList.add('copied');
        setTimeout(() => {
          btn.textContent = '复制';
          btn.classList.remove('copied');
        }, 1500);
      });
    });
    pre.appendChild(btn);
  });
}

function renderMd(md, container, basePath) {
  currentBasePath = basePath || '';
  const html = marked.parse(md || '');
  // ADD_ATTR: 允许 a[target](外部链接新窗口), 其余默认净化策略不变
  container.innerHTML = DOMPurify.sanitize(html, { ADD_ATTR: ['target'] });
  container.querySelectorAll('pre code').forEach((el) => {
    try { hljs.highlightElement(el); } catch { /* ignore */ }
  });
  addCopyButtons(container);
  return renderMermaid(container); // 返回 Promise: Mermaid 全部渲染完成后 resolve
}

/* ============================================================
   主题: dark / light / auto(跟随系统)
   ============================================================ */
// 当前系统深色偏好(跟随系统时使用)
const systemDark = () => window.matchMedia('(prefers-color-scheme: dark)').matches;

// 解析实际生效主题(auto → 系统偏好)
function resolveTheme() {
  return state.theme === 'auto' ? (systemDark() ? 'dark' : 'light') : state.theme;
}

function applyTheme() {
  document.documentElement.dataset.theme = resolveTheme();
  const isDark = resolveTheme() === 'dark';
  els.themeBtn.innerHTML = isDark ? ICONS.sun : ICONS.moon;
  els.themeBtn.title = state.theme === 'auto'
    ? '当前跟随系统（点击切换）'
    : (isDark ? '切换到浅色' : '切换到深色');
  els.themeBtn.setAttribute('aria-label', els.themeBtn.title);
  // 菜单勾选当前主题项
  document.querySelectorAll('.theme-item').forEach((item) => {
    const on = item.dataset.act === 'theme-' + state.theme;
    item.querySelector('.theme-check').style.display = on ? 'inline' : 'none';
  });
}

function themeOrigin(event, fallback) {
  if (event && event.detail !== 0 && Number.isFinite(event.clientX) && Number.isFinite(event.clientY)) {
    return { x: event.clientX, y: event.clientY };
  }
  const rect = fallback && fallback.getBoundingClientRect();
  return rect
    ? { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }
    : { x: window.innerWidth / 2, y: window.innerHeight / 2 };
}

function canAnimateTheme(origin, previousTheme, nextTheme) {
  return origin && previousTheme !== nextTheme &&
    typeof document.startViewTransition === 'function' &&
    !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

// 设置主题；用户点击时，新配色从点击处向视口外扩散。
function setTheme(t, event, source) {
  if (!['dark', 'light', 'auto'].includes(t)) return;
  const previousTheme = resolveTheme();
  const nextTheme = t === 'auto' ? (systemDark() ? 'dark' : 'light') : t;
  const origin = event ? themeOrigin(event, source) : null;
  const commit = () => {
    state.theme = t;
    store.setItem('docshare-theme', t);
    applyTheme();
    return rerenderMermaid().catch(() => { /* 配色切换不应被图表重渲染失败中断 */ });
  };

  if (!canAnimateTheme(origin, previousTheme, nextTheme)) {
    commit();
    return;
  }

  const x = Math.max(0, Math.min(window.innerWidth, origin.x));
  const y = Math.max(0, Math.min(window.innerHeight, origin.y));
  const radius = Math.hypot(
    Math.max(x, window.innerWidth - x),
    Math.max(y, window.innerHeight - y),
  );
  const root = document.documentElement;
  root.classList.add('theme-transitioning');
  let transition;
  try {
    transition = document.startViewTransition(commit);
  } catch {
    root.classList.remove('theme-transitioning');
    commit();
    return;
  }
  transition.ready.then(() => {
    root.animate(
      {
        clipPath: [
          `circle(0px at ${x}px ${y}px)`,
          `circle(${radius}px at ${x}px ${y}px)`,
        ],
      },
      {
        duration: 520,
        easing: 'cubic-bezier(0.22, 1, 0.36, 1)',
        pseudoElement: '::view-transition-new(root)',
      },
    );
  }).catch(() => { /* 主题已切换，仅跳过动画 */ });
  const finish = () => root.classList.remove('theme-transitioning');
  transition.finished.then(finish, finish);
}

/* ============================================================
   左下角管理菜单(popover)
   ============================================================ */
function toggleMenu() {
  if (els.menuPop.hidden) {
    els.menuPop.hidden = false;
  } else {
    els.menuPop.hidden = true;
  }
}

function closeMenu() {
  els.menuPop.hidden = true;
}

function bindMenu() {
  els.menuBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    toggleMenu();
  });
  els.menuPop.querySelectorAll('.menu-item').forEach((item) => {
    item.addEventListener('click', (event) => {
      const act = item.dataset.act;
      if (act === 'settings') openSettings();
      else if (act === 'access') openAccess();
      else if (act === 'theme-dark') setTheme('dark', event, item);
      else if (act === 'theme-light') setTheme('light', event, item);
      else if (act === 'theme-auto') setTheme('auto', event, item);
      closeMenu();
    });
  });
  document.addEventListener('click', (e) => {
    if (!els.menuPop.hidden && !els.menuPop.contains(e.target) && e.target !== els.menuBtn) {
      closeMenu();
    }
  });
}

/* ============================================================
   侧边栏拖拽调宽
   ============================================================ */
let sidebarDrag = null;

function applySidebarWidth() {
  const saved = parseInt(store.getItem('docshare-sidebar-w') || '', 10);
  if (saved >= 200 && saved <= 420 && els.sidebar) {
    els.sidebar.style.width = saved + 'px';
  }
}

function bindSidebarResize() {
  if (!els.resizer || !els.sidebar) return;
  els.resizer.addEventListener('mousedown', (e) => {
    sidebarDrag = { startX: e.clientX, startW: els.sidebar.offsetWidth };
    document.body.classList.add('resizing');
    document.addEventListener('mousemove', onSidebarMove);
    document.addEventListener('mouseup', endSidebarDrag);
    e.preventDefault();
  });
}

function onSidebarMove(e) {
  if (!sidebarDrag) return;
  const w = Math.min(420, Math.max(200, sidebarDrag.startW + (e.clientX - sidebarDrag.startX)));
  els.sidebar.style.width = w + 'px';
}

function endSidebarDrag() {
  if (!sidebarDrag) return;
  document.removeEventListener('mousemove', onSidebarMove);
  document.removeEventListener('mouseup', endSidebarDrag);
  document.body.classList.remove('resizing');
  store.setItem('docshare-sidebar-w', els.sidebar.style.width);
  sidebarDrag = null;
}

/* ============================================================
   树节点悬浮提示(完整文件路径)
   ============================================================ */
function showTreeTip(text, x, y) {
  if (!els.treeTip) return;
  els.treeTip.textContent = text;
  els.treeTip.hidden = false;
  positionTreeTip(x, y);
}

function positionTreeTip(x, y) {
  if (!els.treeTip || els.treeTip.hidden) return;
  const tw = els.treeTip.offsetWidth;
  const th = els.treeTip.offsetHeight;
  let left = x + 14;
  if (left + tw > window.innerWidth - 8) left = Math.max(8, x - tw - 14);
  let top = y + 16;
  if (top + th > window.innerHeight - 8) top = Math.max(8, y - th - 10);
  els.treeTip.style.left = left + 'px';
  els.treeTip.style.top = top + 'px';
}

function hideTreeTip() {
  if (els.treeTip) els.treeTip.hidden = true;
}

function bindTreeTip(row, path) {
  row.addEventListener('mouseenter', (e) => showTreeTip(path, e.clientX, e.clientY));
  row.addEventListener('mousemove', (e) => positionTreeTip(e.clientX, e.clientY));
  row.addEventListener('mouseleave', hideTreeTip);
}

/* ============================================================
   文档导出
   ============================================================ */
// 导出用内嵌样式(独立 HTML 文件可离线打开)
const EXPORT_CSS = `
  body{font-family:-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;color:#24292f;line-height:1.75;max-width:880px;margin:0 auto;padding:32px 24px}
  h1{font-size:1.8em;border-bottom:1px solid #d8dee4;padding-bottom:.35em}h2{border-bottom:1px solid #d8dee4;padding-bottom:.3em}
  code{font-family:Consolas,Menlo,monospace;background:#f0f2f5;padding:2px 5px;border-radius:4px;font-size:.9em}
  pre{background:#f6f8fa;border:1px solid #d8dee4;border-radius:8px;padding:14px;overflow-x:auto}
  pre code{background:none;padding:0}
  blockquote{border-left:3px solid #6d7cff;background:#f4f5ff;margin:1em 0;padding:8px 16px;color:#57606a}
  table{border-collapse:collapse;width:100%;margin:1em 0}
  th,td{border:1px solid #d8dee4;padding:7px 13px;text-align:left}
  th{background:#f0f2f5}
  img{max-width:100%}
  .mermaid{background:#fff;border:1px solid #d8dee4;border-radius:8px;padding:12px;text-align:center;margin:1em 0}
  .mermaid svg{max-width:100%;height:auto}
`;

// 干净渲染一份正文(去除搜索高亮等交互元素), 图片转 base64 内联(导出自包含)
async function buildExportBody(doc) {
  const wrap = document.createElement('div');
  renderMd(doc.content, wrap, doc.path);
  // 站内文档链接还原为相对链接(导出后同目录文件可互跳)
  wrap.querySelectorAll('a.doc-link').forEach((a) => {
    const p = a.dataset.docPath || '';
    const dir = doc.path.includes('/') ? doc.path.slice(0, doc.path.lastIndexOf('/')) : '';
    let rel = p;
    if (dir && p.startsWith(dir + '/')) rel = p.slice(dir.length + 1);
    a.href = rel.replace(/\.(md|markdown)$/i, '.html'); // 指向导出的 HTML
    a.removeAttribute('data-doc-path');
    a.classList.remove('doc-link');
  });
  const imgs = [...wrap.querySelectorAll('img.doc-img')];
  for (const img of imgs) {
    try {
      const res = await fetch(img.getAttribute('src'));
      const blob = await res.blob();
      const dataUrl = await new Promise((resolve) => {
        const fr = new FileReader();
        fr.onload = () => resolve(fr.result);
        fr.readAsDataURL(blob);
      });
      img.src = dataUrl;
    } catch {
      img.remove(); // 图片不可达时移除, 避免导出文件出现裂图
    }
  }
  return wrap.innerHTML;
}

function exportHTML() {
  const doc = state.currentDoc;
  if (!doc) return;
  const title = doc.name.replace(/\.(md|markdown)$/i, '');
  buildExportBody(doc).then((bodyHtml) => {
    const html = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>${esc(title)}</title>
<style>${EXPORT_CSS}</style>
</head>
<body>
<h1>${esc(title)}</h1>
<div class="doc-meta" style="color:#8a93a3;font-size:.85em;margin-bottom:24px">来源: DocShare · ${esc(fmtTime(doc.modified))}</div>
${bodyHtml}
</body>
</html>`;
    const blob = new Blob([html], { type: 'text/html;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = title + '.html';
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
    window.__DSH_LAST_EXPORT = html; // 测试钩子
    toast('已导出 ' + title + '.html');
  });
}

function exportPDF() {
  if (!state.currentDoc) return;
  window.print(); // 用户可在打印对话框中选择"另存为 PDF"
}

/* ============================================================
   文档批注: 选区创建 / 行内高亮 / 回复 / 删除
   ============================================================ */
let annoFab = null;      // 选区浮动按钮(惰性创建)
let pendingAnno = null;  // 待提交的 { quote, offset }

/* ---- 数据加载(创建/回复/解决/删除/轮询后统一刷新) ---- */
// forceRender: 用户主动操作后为 true, 跳过输入焦点保护强制刷新
async function loadAnnotations(forceRender) {
  if (!state.currentDoc) return;
  try {
    const list = await api('/api/annotations?path=' + encodeURIComponent(state.currentDoc.path));
    const sig = JSON.stringify(list);
    if (sig === state.annoSig) return;
    state.annotations = list || [];
    state.annoSig = sig;
    renderAnnotationMarks();
    // 批注列表弹窗打开时刷新列表
    if (!els.annoListMask.hidden) renderAnnoList();
    // 详情弹窗打开时刷新当前批注(回复输入中不打断; 主动操作后强制)
    if (!els.annoViewMask.hidden && state.activeAnno &&
      (forceRender || !els.annoViewBody.contains(document.activeElement))) {
      renderAnnoView(state.activeAnno);
    }
  } catch { /* 服务不可用时静默(批注不可用) */ }
}

/* ---- 行内高亮 ----
   在渲染后的正文中定位批注引文并包裹为 <mark class="anno-mark">。
   支持跨相邻文本节点(加粗/行内代码会分割文本节点);
   匹配失败(文档已改动)时批注仅显示在列表中。 */
function renderAnnotationMarks() {
  const root = els.docView.querySelector('.md-body');
  if (!root || !state.currentDoc) return;
  // 先还原上一次的 marks(展开回文本节点), 保证幂等
  root.querySelectorAll('mark.anno-mark').forEach((m) => {
    const frag = document.createDocumentFragment();
    while (m.firstChild) frag.appendChild(m.firstChild);
    m.replaceWith(frag);
  });
  for (const a of state.annotations || []) {
    if (findAndWrapQuote(root, a.quote, a.id)) {
      const m = root.querySelector(`mark.anno-mark[data-anno-id="${CSS.escape(a.id)}"]`);
      if (m && a.resolved) m.classList.add('resolved');
    }
  }
  // 面板定位的批注: mark 同步高亮
  if (state.activeAnno) {
    const am = root.querySelector(`mark.anno-mark[data-anno-id="${CSS.escape(state.activeAnno)}"]`);
    if (am) am.classList.add('active');
  }
  updateAnnoBtn();
}

function updateAnnoBtn() {
  // 计数展示未解决批注数(已解决不再打扰)
  const open = (state.annotations || []).filter((a) => !a.resolved).length;
  els.annoBtn.hidden = !state.currentDoc;
  els.annoCount.textContent = open ? String(open) : '';
}

function findAndWrapQuote(root, quote, annoId) {
  const q = String(quote || '').trim();
  if (!q) return false;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const nodes = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  for (let i = 0; i < nodes.length; i++) {
    if (nodes[i].parentNode.closest('code, pre, a')) continue;
    // 从 i 开始拼接连续文本(跨行内元素), 覆盖引文即可
    let acc = '';
    let j = i;
    const parts = [];
    while (j < nodes.length && acc.length < q.length + 32) {
      const n = nodes[j];
      if (n.parentNode.closest('code, pre, a')) break;
      parts.push(n);
      acc += n.textContent;
      j++;
    }
    const idx = acc.indexOf(q);
    if (idx < 0) continue;
    // 定位起点节点与偏移
    let k = 0;
    let pos = idx;
    while (k < parts.length && pos >= parts[k].textContent.length) {
      pos -= parts[k].textContent.length;
      k++;
    }
    if (k >= parts.length) continue;
    return wrapQuote(parts, k, pos, q.length, annoId);
  }
  return false;
}

// 将 [parts[k] 的 pos 起, len 长度] 的文本移入一个 mark 元素。
// 跨父节点时 mark 插在首节点前, 后续文本节点 appendChild 移入(顺序保持)。
function wrapQuote(parts, startIdx, startOff, len, annoId) {
  const mark = document.createElement('mark');
  mark.className = 'anno-mark';
  mark.dataset.annoId = annoId;
  let remaining = len;
  let i = startIdx;
  let off = startOff;
  let inserted = false;
  while (remaining > 0 && i < parts.length) {
    let piece = parts[i];
    if (off > 0) piece = piece.splitText(off); // 前段留在原地, piece 为 [off:]
    const avail = piece.textContent.length;
    const take = Math.min(remaining, avail);
    if (take < avail) piece.splitText(take); // 后段留在原地
    if (!inserted) {
      piece.parentNode.insertBefore(mark, piece);
      inserted = true;
    }
    mark.appendChild(piece);
    remaining -= take;
    off = 0;
    i++;
  }
  return remaining <= 0;
}

/* ---- 选区交互 ---- */
function bindAnnoSelection() {
  document.addEventListener('mouseup', (e) => {
    // 弹窗/面板/浮动按钮内的操作不触发
    if (e.target.closest('.modal-mask, .anno-fab, .toc-panel, .tree, .search')) {
      hideAnnoFab();
      return;
    }
    const sel = window.getSelection();
    const text = sel && !sel.isCollapsed ? sel.toString().trim() : '';
    if (!text || !state.currentDoc || text.length > 300) { hideAnnoFab(); return; }
    const range = sel.rangeCount ? sel.getRangeAt(0) : null;
    if (!range || !els.docView.contains(range.commonAncestorContainer)) { hideAnnoFab(); return; }
    const rect = range.getBoundingClientRect();
    if (!rect || rect.width === 0) { hideAnnoFab(); return; }
    getAnnoFab().hidden = false;
    getAnnoFab().style.left = Math.max(8, Math.min(rect.right, window.innerWidth - 96)) + 'px';
    getAnnoFab().style.top = Math.max(8, rect.bottom + 6) + 'px';
  });
  // 滚动时隐藏浮动按钮(位置不再准确)
  els.docView.addEventListener('scroll', hideAnnoFab);
  document.addEventListener('keydown', hideAnnoFab);
}

function getAnnoFab() {
  if (annoFab) return annoFab;
  annoFab = document.createElement('button');
  annoFab.type = 'button';
  annoFab.className = 'anno-fab';
  annoFab.innerHTML = `${ICONS.comment}<span>批注</span>`;
  annoFab.addEventListener('click', () => {
    const sel = window.getSelection();
    const text = sel && !sel.isCollapsed ? sel.toString().trim() : '';
    hideAnnoFab();
    if (!text || !state.currentDoc) return;
    pendingAnno = { quote: text, offset: annoOffsetOf(sel) };
    openAnnoDialog(text);
  });
  document.body.appendChild(annoFab);
  return annoFab;
}

function hideAnnoFab() {
  if (annoFab) annoFab.hidden = true;
}

// 计算选区起点在文档纯文本中的偏移(辅助定位; 找不到返回 0)
function annoOffsetOf(sel) {
  const root = els.docView.querySelector('.md-body');
  if (!root || !sel || sel.rangeCount === 0) return 0;
  const range = sel.getRangeAt(0);
  const start = range.startContainer;
  if (start.nodeType === Node.TEXT_NODE) return annoOffsetOfNode(root, start, range.startOffset);
  // 起点是元素: 取其内部第一个文本节点近似
  const w = document.createTreeWalker(start, NodeFilter.SHOW_TEXT);
  const first = w.nextNode();
  return first ? annoOffsetOfNode(root, first, 0) : 0;
}

function annoOffsetOfNode(root, node, extra) {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let off = 0;
  while (walker.nextNode()) {
    const n = walker.currentNode;
    if (n === node) return off + extra;
    off += n.textContent.length;
  }
  return 0;
}

/* ---- 创建弹窗 ---- */
function openAnnoDialog(quote) {
  const preview = quote.length > 80 ? quote.slice(0, 80) + '…' : quote;
  els.annoQuotePreview.textContent = `「${preview}」`;
  els.annoContent.value = '';
  els.annoAuthor.value = store.getItem('docshare-author') || '';
  els.annoMask.hidden = false;
  setTimeout(() => els.annoContent.focus(), 60);
}

async function submitAnno() {
  const content = els.annoContent.value.trim();
  const author = els.annoAuthor.value.trim().slice(0, 50);
  if (!content) { toast('请输入批注内容', 'err'); return; }
  if (!author) { toast('请输入你的名字', 'err'); els.annoAuthor.focus(); return; }
  if (!state.currentDoc || !pendingAnno) return;
  store.setItem('docshare-author', author);
  try {
    const created = await api('/api/annotations', {
      method: 'POST',
      body: {
        doc: state.currentDoc.path,
        quote: pendingAnno.quote,
        offset: pendingAnno.offset,
        author,
        content,
      },
    });
    els.annoMask.hidden = true;
    pendingAnno = null;
    toast('批注已添加');
    await loadAnnotations(true);
    if (created && created.id) openAnnoView(created.id); // 打开新批注的详情弹窗
  } catch (err) {
    toast('批注保存失败：' + (err.message || '未知错误'), 'err');
  }
}

/* ---- 批注列表弹窗(顶栏按钮) ---- */
function openAnnoList() {
  els.annoListMask.hidden = false;
  renderAnnoList();
}

function renderAnnoList() {
  // 未解决在前, 已解决置后(各自按时间升序)
  const list = [...(state.annotations || [])].sort((x, y) => {
    if (!!x.resolved !== !!y.resolved) return x.resolved ? 1 : -1;
    return x.time < y.time ? -1 : x.time > y.time ? 1 : 0;
  });
  const open = list.filter((a) => !a.resolved).length;
  els.annoListSub.textContent = list.length
    ? `共 ${list.length} 条 · 待解决 ${open} 条`
    : '本文档暂无批注';
  if (!list.length) {
    els.annoList.innerHTML = '<div class="anno-empty">本文档暂无批注<br/><span>选中正文文字即可添加</span></div>';
    return;
  }
  els.annoList.innerHTML = list.map(annoListItemHTML).join('');
}

function annoListItemHTML(a) {
  const summary = (a.content || '').replace(/\s+/g, ' ').slice(0, 60);
  return `
    <button type="button" class="anno-list-item${a.resolved ? ' resolved' : ''}" data-id="${esc(a.id)}">
      <span class="anno-author">${esc(a.author || '匿名')}</span>
      <span class="anno-list-text">${esc(summary)}</span>
      <span class="anno-status ${a.resolved ? 'done' : 'open'}">${a.resolved ? '已解决' : '待解决'}</span>
    </button>`;
}

function bindAnnoList() {
  els.annoList.addEventListener('click', (e) => {
    const item = e.target.closest('.anno-list-item');
    if (!item) return;
    const id = item.dataset.id;
    els.annoListMask.hidden = true;
    jumpToAnno(id); // 定位正文 + 打开详情弹窗
  });
}

/* ---- 批注详情弹窗(单条批注) ---- */
function openAnnoView(id) {
  if (!id) return;
  state.activeAnno = id;
  els.annoViewMask.hidden = false;
  renderAnnoView(id);
}

function closeAnnoView() {
  els.annoViewMask.hidden = true;
  state.activeAnno = '';
  renderAnnotationMarks(); // 清除 mark 激活态
}

// 渲染当前打开的批注详情; 批注已被删除时关闭弹窗
function renderAnnoView(id) {
  const a = (state.annotations || []).find((x) => x.id === id);
  if (!a) {
    els.annoViewBody.innerHTML = '<div class="anno-empty">该批注已被删除</div>';
    els.annoViewHint.textContent = '';
    els.annoViewResolve.hidden = true;
    els.annoViewDelete.hidden = true;
    return;
  }
  els.annoViewQuote.textContent = `「${a.quote.length > 60 ? a.quote.slice(0, 60) + '…' : a.quote}」`;
  els.annoViewResolve.hidden = false;
  els.annoViewDelete.hidden = false;
  els.annoViewResolve.textContent = a.resolved ? '重新打开' : '标记已解决';
  els.annoViewHint.textContent = a.resolved ? '该批注已解决' : '';
  // 尚未记忆名字时, 回复需先输入名字
  const myName = (store.getItem('docshare-author') || '').trim();
  const replies = (a.replies || []).map((r) => `
    <div class="anno-reply">
      <div class="anno-reply-head">
        <span class="anno-author">${esc(r.author || '匿名')}</span>
        <span class="anno-time">${esc(fmtTime(r.time))}</span>
      </div>
      <div class="anno-reply-text">${esc(r.content)}</div>
    </div>`).join('');
  els.annoViewBody.innerHTML = `
    <div class="anno-card${a.resolved ? ' resolved' : ''}">
      <div class="anno-card-head">
        <span class="anno-author">${esc(a.author || '匿名')}</span>
        <span class="anno-time">${esc(fmtTime(a.time))}</span>
      </div>
      ${a.resolved ? '<span class="anno-resolved-badge">已解决</span>' : ''}
      <blockquote class="anno-quote">${esc(a.quote)}</blockquote>
      <div class="anno-content">${esc(a.content)}</div>
      ${replies ? `<div class="anno-replies">${replies}</div>` : ''}
      <form class="anno-reply-form">
        ${myName ? '' : '<input type="text" class="anno-reply-input anno-reply-name" placeholder="请输入你的名字" maxlength="50" autocomplete="off" />'}
        <input type="text" class="anno-reply-input" placeholder="回复…" maxlength="2000" autocomplete="off" />
        <button type="submit" class="btn ghost">回复</button>
      </form>
    </div>`;
}

function bindAnnoView() {
  els.annoViewBody.addEventListener('submit', (e) => {
    const form = e.target.closest('.anno-reply-form');
    if (!form) return;
    e.preventDefault();
    replyAnno(form);
  });
  els.annoViewResolve.addEventListener('click', () => {
    if (state.activeAnno) toggleResolve(state.activeAnno);
  });
  els.annoViewDelete.addEventListener('click', () => {
    if (state.activeAnno) deleteAnno(state.activeAnno);
  });
}

// 标记解决 / 重新打开(可逆, 无需确认)
async function toggleResolve(id) {
  if (!id || !state.currentDoc) return;
  const a = (state.annotations || []).find((x) => x.id === id);
  const target = !(a && a.resolved);
  try {
    await api('/api/annotations/' + encodeURIComponent(id) + '/resolve', {
      method: 'POST',
      body: { doc: state.currentDoc.path, resolved: target },
    });
    toast(target ? '批注已标记为已解决' : '批注已重新打开');
    await loadAnnotations(true); // 主动操作: 强制刷新
  } catch (err) {
    toast('操作失败：' + (err.message || '未知错误'), 'err');
  }
}

async function deleteAnno(id) {
  if (!id || !state.currentDoc) return;
  if (!confirm('确定删除这条批注及其全部回复？')) return;
  try {
    await api('/api/annotations/' + encodeURIComponent(id) +
      '?path=' + encodeURIComponent(state.currentDoc.path), { method: 'DELETE' });
    toast('批注已删除');
    if (state.activeAnno === id) closeAnnoView(); // 详情弹窗关闭
    await loadAnnotations(true);
  } catch (err) {
    toast('删除失败：' + (err.message || '未知错误'), 'err');
  }
}

async function replyAnno(form) {
  if (!state.currentDoc || !state.activeAnno) return;
  const nameInput = form.querySelector('.anno-reply-name');
  const input = form.querySelector('.anno-reply-input:not(.anno-reply-name)');
  const content = input.value.trim();
  if (!content) return;
  // 首次回复(无记忆名字): 必须输入名字
  let author = (store.getItem('docshare-author') || '').trim();
  if (nameInput) {
    author = nameInput.value.trim().slice(0, 50);
    if (!author) { toast('请输入你的名字', 'err'); nameInput.focus(); return; }
    store.setItem('docshare-author', author);
  }
  if (!author) { toast('请输入你的名字', 'err'); return; } // 防御
  try {
    await api('/api/annotations/' + encodeURIComponent(state.activeAnno) + '/reply', {
      method: 'POST',
      body: { doc: state.currentDoc.path, author, content },
    });
    input.value = '';
    input.blur(); // 释放焦点, 详情可正常刷新
    await loadAnnotations(true); // 主动操作: 强制刷新
  } catch (err) {
    toast('回复失败：' + (err.message || '未知错误'), 'err');
  }
}

// 定位到批注在正文中的位置并打开详情弹窗
function jumpToAnno(id) {
  if (!state.currentDoc) return;
  state.activeAnno = id;
  renderAnnotationMarks(); // 先重建(确保 active 态), 再定位避免 flash 丢失
  const mark = els.docView.querySelector(`mark.anno-mark[data-anno-id="${CSS.escape(id)}"]`);
  if (mark) {
    mark.scrollIntoView({ behavior: 'smooth', block: 'center' });
    flashMark(mark);
  }
  openAnnoView(id);
}

function flashMark(mark) {
  mark.classList.remove('flash');
  void mark.offsetWidth; // 重启动画
  mark.classList.add('flash');
  clearTimeout(flashMark._t);
  flashMark._t = setTimeout(() => mark.classList.remove('flash'), 1800);
}

// 全局快捷键: Ctrl+K 搜索 / Ctrl+P 打印导出 / Esc 关闭弹窗
function bindShortcuts() {
  document.addEventListener('keydown', (e) => {
    const mod = e.ctrlKey || e.metaKey;
    const key = (e.key || '').toLowerCase();
    if (mod && key === 'k') {
      e.preventDefault();
      els.search.focus();
      els.search.select();
      return;
    }
    if (mod && key === 'p') {
      e.preventDefault();
      exportPDF();
      return;
    }
    if (e.key === 'Escape') {
      // 登录遮罩不允许 Esc 关闭(必须输入密码或刷新)
      if (els.loginMask && !els.loginMask.hidden) return;
      ['updateMask', 'dirMask', 'settingsMask', 'accessMask', 'annoMask', 'annoListMask', 'annoViewMask'].forEach((id) => {
        const el = document.getElementById(id);
        if (el && !el.hidden) el.hidden = true;
      });
      if (!els.exportMenu.hidden) els.exportMenu.hidden = true;
      els.search.blur();
    }
  });
}

function bindExport() {
  els.exportBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    els.exportMenu.hidden = !els.exportMenu.hidden;
  });
  els.exportMenu.querySelectorAll('.export-item').forEach((item) => {
    item.addEventListener('click', () => {
      const act = item.dataset.act;
      if (act === 'html') exportHTML();
      else if (act === 'pdf') exportPDF();
      els.exportMenu.hidden = true;
    });
  });
  document.addEventListener('click', (e) => {
    if (!els.exportMenu.hidden && !els.exportMenu.contains(e.target) && e.target !== els.exportBtn) {
      els.exportMenu.hidden = true;
    }
  });
}

/* ============================================================
   阅读记忆: 最近浏览 + 滚动位置恢复
   ============================================================ */
function rememberDoc(path, name) {
  state.recent = state.recent.filter((r) => r.path !== path);
  state.recent.unshift({ path, name, time: Date.now() });
  state.recent = state.recent.slice(0, 8);
  store.setItem('docshare-recent', JSON.stringify(state.recent));
  renderRecent();
}

function renderRecent() {
  const box = els.recentBox;
  if (!box) return;
  if (!state.recent.length) {
    box.hidden = true;
    box.innerHTML = '';
    return;
  }
  box.hidden = false;
  box.innerHTML = `<div class="recent-head">
      <span>最近浏览</span>
      <button type="button" class="recent-clear" title="清除最近浏览">清除</button>
    </div>` +
    state.recent.slice(0, 5).map((r) => `
      <button type="button" class="recent-item" data-path="${esc(r.path)}">
        ${ICONS.clock}<span class="recent-name">${esc(r.name)}</span>
        <span class="recent-time">${esc(fmtTime(new Date(r.time).toISOString()))}</span>
      </button>`).join('');
  box.querySelectorAll('.recent-item').forEach((btn) => {
    btn.addEventListener('click', () => openDoc(btn.dataset.path, null));
  });
  box.querySelector('.recent-clear').addEventListener('click', clearRecent);
}

// 清除最近浏览(连同各文档的阅读位置记忆)
function clearRecent() {
  if (!state.recent.length) return;
  state.recent.forEach((r) => store.removeItem('docshare-scroll-' + r.path));
  state.recent = [];
  store.setItem('docshare-recent', '[]');
  renderRecent();
  toast('已清除最近浏览');
}

function restoreScroll() {
  if (!state.currentDoc) return;
  // 用户已主动滚动过: 以用户当前位置为准, 不再自动恢复(防止覆盖)
  if (state.userScrolled) return;
  const raw = store.getItem('docshare-scroll-' + state.currentDoc.path);
  if (!raw) return;
  let pos;
  try {
    pos = JSON.parse(raw);
    if (!pos || typeof pos !== 'object') pos = { top: parseInt(raw, 10) || 0 };
  } catch {
    pos = { top: parseInt(raw, 10) || 0 }; // 兼容旧版纯数字存储
  }
  const view = els.docView;
  if (!view.scrollHeight) return;
  // 计算最终目标位置(单次赋值, 只产生一次滚动事件)
  let target = null;
  // 1) 快速恢复: 直接按上次像素位置(内容未变化时一步到位)
  if (pos.top > 0 && pos.top < view.scrollHeight) target = pos.top;
  // 2) 章节锚点校正: 找到上次所在的标题, 按其位置 + 段内偏移恢复(文档被编辑过也能定位)
  if (pos.heading) {
    const heads = [...view.querySelectorAll('.md-body h1, .md-body h2, .md-body h3, .md-body h4, .md-body h5, .md-body h6')];
    const found = heads.find((h) => h.textContent.trim() === pos.heading);
    if (found) target = Math.max(0, Math.round(posInView(found) + (pos.delta || 0)));
  } else if (pos.ratio > 0) {
    // 3) 兜底: 按阅读比例恢复(标题找不到或内容大幅变化)
    target = Math.round(pos.ratio * view.scrollHeight);
  }
  if (target === null) return;
  restoring = true; // 该滚动事件在下一帧到达, 由监听器消费
  clearTimeout(restoreTimer);
  restoreTimer = setTimeout(() => { restoring = false; }, 500); // 兜底: 位置未变化时无事件, 超时解除
  view.scrollTop = Math.round(target);
}

// 元素在 #docView 滚动坐标系中的位置(与 offsetParent 无关)
function posInView(el) {
  return el.getBoundingClientRect().top - els.docView.getBoundingClientRect().top + els.docView.scrollTop;
}

// 当前阅读位置: 视口顶部附近的标题 + 段内偏移 + 像素/比例
function readingPos() {
  if (!state.currentDoc) return null;
  const view = els.docView;
  const top = view.scrollTop;
  let heading = '';
  let delta = top;
  const heads = [...view.querySelectorAll('.md-body h1, .md-body h2, .md-body h3, .md-body h4, .md-body h5, .md-body h6')];
  let prev = null;
  for (const h of heads) {
    if (posInView(h) <= top + 24) prev = h; // 最后一个位于视口顶部(±24px)的标题
    else break;
  }
  if (prev) {
    heading = prev.textContent.trim();
    delta = Math.round(top - posInView(prev));
  }
  return { top: Math.round(top), heading, delta, ratio: view.scrollHeight ? top / view.scrollHeight : 0 };
}

function savePos(path, pos) {
  if (!pos) return;
  store.setItem('docshare-scroll-' + path, JSON.stringify(pos));
}

function bindScrollMemory() {
  els.docView.addEventListener('scroll', () => {
    if (!state.currentDoc) return;
    if (restoring) { restoring = false; return; } // 本次滚动由恢复触发, 忽略并消费标记
    state.userScrolled = true;
    // 在事件时刻就固定 path 与位置, 避免防抖窗口内切换文档导致存错
    const p = state.currentDoc.path;
    const pos = readingPos();
    clearTimeout(state.scrollTimer);
    state.scrollTimer = setTimeout(() => savePos(p, pos), 400);
  });
}

// 等待容器内所有图片加载完成(含加载失败), 3 秒兜底超时
function waitImages(container) {
  const imgs = [...container.querySelectorAll('img')];
  if (!imgs.length) return Promise.resolve();
  return new Promise((resolve) => {
    let left = imgs.length;
    const timer = setTimeout(resolve, 3000);
    const done = () => {
      if (--left <= 0) { clearTimeout(timer); resolve(); }
    };
    imgs.forEach((img) => {
      if (img.complete) { done(); return; }
      img.addEventListener('load', done);
      img.addEventListener('error', done);
    });
  });
}

/* ============================================================
   目录树
   ============================================================ */
function renderTree() {
  renderRecent();
  els.tree.innerHTML = '';
  const root = state.tree;
  if (!root) {
    els.tree.innerHTML = '<div class="tree-empty">目录加载失败，请检查服务端配置</div>';
    return;
  }
  const matches = (node) =>
    node.name.toLowerCase().includes(state.search.toLowerCase()) ||
    (node.children && node.children.some(matches));
  const shown = state.search
    ? (root.children || []).filter(matches)
    : root.children || [];

  if (!shown.length) {
    if (state.search) {
      els.tree.innerHTML = '<div class="tree-empty">没有找到匹配的文档</div>';
    } else if (!state.ready) {
      els.tree.innerHTML = DESKTOP
        ? '<div class="tree-empty">尚未配置文档目录<br/><span style="font-size:12px;opacity:.75">点击左下角「设置」选择目录</span></div>'
        : '<div class="tree-empty">文档目录中暂无 Markdown 文件<br/><span style="font-size:12px;opacity:.75">请联系管理员配置</span></div>';
    } else {
      els.tree.innerHTML = '<div class="tree-empty">文档目录中暂无 Markdown 文件</div>';
    }
    return;
  }
  shown.forEach((node) => els.tree.appendChild(buildNode(node)));
  // 恢复当前打开文档的高亮
  if (state.currentDoc) {
    const row = els.tree.querySelector(`.tree-row[data-path="${CSS.escape(state.currentDoc.path)}"]`);
    if (row) row.classList.add('active');
  }
}

// renderDoc 中启用导出按钮
function enableExport() {
  els.exportBtn.disabled = false;
}

function buildNode(node) {
  const wrap = document.createElement('div');
  wrap.className = 'tree-node';
  wrap.dataset.path = node.path;

  if (node.isDir) {
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'tree-row';
    row.dataset.path = node.path;
    row.innerHTML = `${ICONS.chevron}${ICONS.folder}<span class="row-label">${esc(node.name)}</span>`;
    const children = document.createElement('div');
    children.className = 'tree-children';
    // 恢复用户折叠状态(默认展开)
    const isCollapsed = state.collapsedDirs.has(node.path);
    if (isCollapsed) children.classList.add('collapsed');
    row.querySelector('.chevron').classList.toggle('open', !isCollapsed);
    node.children.forEach((c) => children.appendChild(buildNode(c)));
    bindTreeTip(row, node.path === '.' ? node.name : node.path);

    row.addEventListener('click', () => {
      const chev = row.querySelector('.chevron');
      const nowCollapsed = children.classList.toggle('collapsed');
      chev.classList.toggle('open', !nowCollapsed);
      if (nowCollapsed) state.collapsedDirs.add(node.path);
      else state.collapsedDirs.delete(node.path);
    });
    wrap.appendChild(row);
    wrap.appendChild(children);
  } else {
    const row = document.createElement('button');
    row.type = 'button';
    row.className = 'tree-row';
    row.dataset.path = node.path;
    row.innerHTML = `${ICONS.chevron}${ICONS.file}<span class="row-label">${esc(node.name)}</span><span class="row-name">${esc(fmtSize(node.size))}</span>`;
    row.addEventListener('click', () => openDoc(node.path, row));
    bindTreeTip(row, node.path);
    wrap.appendChild(row);
  }
  return wrap;
}

/* ============================================================
   目录树自动刷新: 每 3 秒静默轮询, 结构变化才重渲染
   ============================================================ */
async function pollTree() {
  if (els.search.value.trim()) return; // 搜索中不打扰
  try {
    const data = await api('/api/tree');
    const node = data.node || data;
    const sig = JSON.stringify(node);
    if (sig === state.treeSig) return;
    state.treeSig = sig;
    state.tree = node;
    state.ready = !!data.ready;
    renderTree();
    checkCurrentDocChanged(node); // 当前文档被外部修改/删除时自动刷新
  } catch { /* 服务不可用时静默 */ }
  loadAnnotations(); // 批注静默轮询(多人协作: 新批注 ≤3s 可见)
}

// 在目录树中按完整路径查找节点(含多根前缀)
function findTreeNode(node, path) {
  if (!node) return null;
  if (node.path === path) return node;
  for (const c of node.children || []) {
    const found = findTreeNode(c, path);
    if (found) return found;
  }
  return null;
}

let docMissingNotified = false; // 当前文档删除提示只弹一次
let restoring = false; // 恢复滚动位置期间, 滚动监听忽略(避免恢复动作被当作"用户滚动")
let restoreTimer = null;

// 当前打开的文档在磁盘上被改动时, 自动拉取并重新渲染最新内容
function checkCurrentDocChanged(treeRoot) {
  if (!state.currentDoc) return;
  const node = findTreeNode(treeRoot, state.currentDoc.path);
  if (!node) {
    if (!docMissingNotified) {
      docMissingNotified = true;
      toast('当前文档已被删除或移动', 'err');
    }
    return;
  }
  docMissingNotified = false;
  // mtime(秒级) 或大小变化即视为内容被修改
  if (node.modified !== state.currentDoc.modified || node.size !== state.currentDoc.size) {
    reloadCurrentDoc();
  }
}

async function reloadCurrentDoc() {
  const doc = state.currentDoc;
  try {
    const fresh = await api('/api/doc?path=' + encodeURIComponent(doc.path));
    if (!state.currentDoc || state.currentDoc.path !== fresh.path) return; // 期间已切换文档
    // 先保存当前阅读位置, 刷新后自动恢复到原章节
    clearTimeout(state.scrollTimer);
    savePos(doc.path, readingPos());
    state.currentDoc = fresh;
    renderDoc(fresh);
    toast('文档已更新，已自动刷新');
  } catch (err) {
    toast('文档刷新失败：' + (err.message || '未知错误'), 'err');
  }
}

// 文档正文内链接交互: 站内文档跳转 + 锚点平滑滚动(事件委托, 绑定一次)
function bindDocNav() {
  els.docView.addEventListener('click', (e) => {
    // 点击批注高亮 → 打开该批注的详情弹窗
    const mark = e.target.closest('mark.anno-mark');
    if (mark) {
      e.preventDefault();
      state.activeAnno = mark.dataset.annoId;
      renderAnnotationMarks(); // 重建(同步 active 态), 再闪烁新 mark
      const nm = els.docView.querySelector(`mark.anno-mark[data-anno-id="${CSS.escape(state.activeAnno)}"]`);
      if (nm) flashMark(nm);
      openAnnoView(mark.dataset.annoId);
      return;
    }
    const link = e.target.closest('a');
    if (!link) return;
    // 站内 md 文档链接
    if (link.classList.contains('doc-link')) {
      e.preventDefault();
      const p = link.dataset.docPath;
      if (p) openDoc(p, null);
      return;
    }
    // 锚点链接: 按标题文本匹配平滑滚动
    if (link.getAttribute('href') && link.getAttribute('href').startsWith('#')) {
      e.preventDefault();
      const text = decodeURIComponent(link.getAttribute('href').slice(1));
      const heads = [...document.querySelectorAll('#docView .md-body h1, #docView .md-body h2, #docView .md-body h3, #docView .md-body h4, #docView .md-body h5, #docView .md-body h6')];
      const target = heads.find((h) => h.textContent.trim() === text) || document.getElementById(text);
      if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  });
}

/* ============================================================
   文档浏览
   ============================================================ */
async function openDoc(path, rowEl) {
  docMissingNotified = false; // 新开文档重置删除提示
  // 切换文档前先落盘当前阅读位置(防止 400ms 防抖窗口内切换导致位置丢失)
  if (state.currentDoc) {
    clearTimeout(state.scrollTimer);
    savePos(state.currentDoc.path, readingPos());
  }
  try {
    const doc = await api('/api/doc?path=' + encodeURIComponent(path));
    state.currentDoc = doc;
    // 切换文档: 重置批注状态(异步加载后重新渲染)
    state.annotations = [];
    state.annoSig = '';
    state.activeAnno = '';
    els.annoListMask.hidden = true;
    els.annoViewMask.hidden = true;
    document.querySelectorAll('.tree-row.active').forEach((r) => r.classList.remove('active'));
    if (rowEl) rowEl.classList.add('active');
    renderDoc(doc);
    rememberDoc(doc.path, doc.name);
    loadAnnotations(); // 拉取批注并渲染行内高亮
  } catch (err) {
    toast(err.message, 'err');
  }
}

function renderDoc(doc) {
  state.userScrolled = false; // 新文档渲染: 重置用户滚动标记(等待自动恢复)
  // 面包屑
  const parts = doc.path.split('/');
  els.crumbs.innerHTML = '<span class="crumb-root">DocShare</span>' +
    parts.slice(0, -1).map((p) => `<span class="crumb-sep">/</span><span>${esc(p)}</span>`).join('') +
    `<span class="crumb-sep">/</span><span class="crumb-current">${esc(doc.name)}</span>`;

  // 元信息
  els.docMeta.textContent = `${fmtTime(doc.modified)} · ${fmtSize(doc.size)}`;

  // 正文
  const h = document.createElement('div');
  h.className = 'doc-inner';
  h.innerHTML = `
    <div class="doc-head">
      <h2>${esc(doc.name.replace(/\.(md|markdown)$/i, ''))}</h2>
      <div class="doc-head-meta">
        <span>${ICONS.clock}${esc(fmtTime(doc.modified))}</span>
        <span>${fmtSize(doc.size)}</span>
      </div>
    </div>
    <div class="md-body"></div>`;
  els.docView.innerHTML = '';
  els.docView.appendChild(h);
  const mdBody = h.querySelector('.md-body');
  const mdReady = renderMd(doc.content, mdBody, doc.path); // 同步完成 innerHTML, 返回 Mermaid 渲染 Promise
  buildToc(h);

  // 全文搜索关键词高亮
  if (state.search) {
    highlightTextNodes(mdBody, state.search);
  }
  els.exportBtn.disabled = false;
  // 快速恢复一次(内容未变化时立刻到位)
  setTimeout(restoreScroll, 60);
  // Mermaid 与图片渲染完成后再精确恢复一次:
  // 此前内容高度尚未定型(图片/图表异步加载), 直接恢复的位置会偏移
  const settled = Promise.allSettled([mdReady, waitImages(mdBody)]);
  settled.then(() => {
    if (state.currentDoc && state.currentDoc.path === doc.path) {
      renderAnnotationMarks(); // 批注高亮(依赖 state.annotations, 异步加载后再次调用)
      restoreScroll();
    }
  });
}

/* ============================================================
   全文搜索
   ============================================================ */
function highlightTextNodes(root, keyword) {
  if (!root || !keyword) return;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const nodes = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  const kw = keyword.trim();
  if (!kw) return;
  const lower = kw.toLowerCase();
  nodes.forEach((node) => {
    const idx = node.textContent.toLowerCase().indexOf(lower);
    if (idx < 0 || node.parentNode.closest('code, pre, a')) return;
    const frag = document.createDocumentFragment();
    frag.appendChild(document.createTextNode(node.textContent.slice(0, idx)));
    const mark = document.createElement('mark');
    mark.className = 'search-hit';
    mark.textContent = node.textContent.slice(idx, idx + kw.length);
    frag.appendChild(mark);
    frag.appendChild(document.createTextNode(node.textContent.slice(idx + kw.length)));
    node.parentNode.replaceChild(frag, node);
  });
}

async function doFulltextSearch(q) {
  const box = els.searchResults;
  if (!box) return;
  if (!q) {
    box.hidden = true;
    box.innerHTML = '';
    return;
  }
  try {
    const results = await api('/api/search?q=' + encodeURIComponent(q));
    if (q !== state.search) return; // 过期响应丢弃
    if (!results || !results.length) {
      box.hidden = true;
      box.innerHTML = '';
      return;
    }
    box.hidden = false;
    box.innerHTML = `
      <div class="sr-head">全文匹配 · ${results.length} 篇</div>
      ${results.map((r) => `
        <button type="button" class="sr-item" data-path="${esc(r.path)}">
          <span class="sr-name">${esc(r.name)}</span>
          <span class="sr-snippet">${esc(r.snippet || '')}</span>
        </button>`).join('')}`;
    box.querySelectorAll('.sr-item').forEach((btn) => {
      btn.addEventListener('click', () => {
        openDoc(btn.dataset.path, null);
      });
    });
  } catch { /* 搜索失败静默(树过滤仍可用) */ }
}

/* ============================================================
   大纲视图(右侧悬浮, 可折叠)
   ============================================================ */
let tocObserver = null;
let tocPanel = null;
let tocFab = null;

function slugify(s) {
  return String(s).trim().toLowerCase()
    .replace(/[^\w\u4e00-\u9fa5-]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60);
}

/* 悬浮「目录」按钮, init 时创建一次 */
function createTocFab() {
  tocFab = document.createElement('button');
  tocFab.type = 'button';
  tocFab.className = 'toc-fab';
  tocFab.hidden = true;
  tocFab.title = '展开大纲';
  tocFab.innerHTML = `${ICONS.list}<span>目录</span>`;
  tocFab.addEventListener('click', () => setTocCollapsed(false));
  document.body.appendChild(tocFab);
}

function setTocCollapsed(collapsed) {
  if (!tocPanel) return;
  tocPanel.classList.toggle('collapsed', collapsed);
  if (tocFab) tocFab.hidden = !collapsed;
  const appEl = document.querySelector('.app');
  if (appEl) appEl.classList.toggle('toc-open', !collapsed); // 展开时正文让位
  store.setItem('docshare-toc', collapsed ? '0' : '1');
}

function buildToc(docEl) {
  // 清理上一次的大纲
  if (tocPanel) { tocPanel.remove(); tocPanel = null; }
  if (tocObserver) { tocObserver.disconnect(); tocObserver = null; }
  if (tocFab) tocFab.hidden = true;
  const appEl = document.querySelector('.app');
  if (appEl) appEl.classList.remove('toc-open');

  const body = docEl.querySelector('.md-body');
  const heads = body ? body.querySelectorAll('h1, h2, h3, h4, h5, h6') : [];
  if (!heads.length) return;

  const used = {};
  const items = [];
  heads.forEach((h) => {
    let id = slugify(h.textContent) || 'section';
    if (used[id]) { let n = 2; while (used[id + '-' + n]) n++; id = id + '-' + n; }
    used[id] = true;
    h.id = id;
    items.push({ id, level: parseInt(h.tagName[1], 10), text: h.textContent.trim() });
  });

  const panel = document.createElement('aside');
  panel.className = 'toc-panel';
  panel.innerHTML = `
    <div class="toc-head">
      <span class="toc-title">${ICONS.list}目录</span>
      <button type="button" class="toc-collapse-btn" title="收起大纲" aria-label="收起大纲">${ICONS.chevronRight}</button>
    </div>
    <div class="toc-scroll">${items.map((it) =>
      `<a class="toc-item lv${it.level}" href="#${it.id}" data-id="${it.id}">${esc(it.text)}</a>`).join('')}
    </div>`;
  document.body.appendChild(panel);
  tocPanel = panel;

  panel.querySelector('.toc-collapse-btn').addEventListener('click', () => setTocCollapsed(true));
  panel.querySelectorAll('.toc-item').forEach((a) => {
    a.addEventListener('click', (e) => {
      e.preventDefault();
      const target = docEl.querySelector('#' + CSS.escape(a.dataset.id));
      if (target) target.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
  });

  // 滚动高亮当前章节
  tocObserver = new IntersectionObserver((entries) => {
    let top = null;
    for (const en of entries) {
      if (en.isIntersecting && (!top || en.boundingClientRect.top < top.getBoundingClientRect().top)) {
        top = en.target;
      }
    }
    if (top) {
      panel.querySelectorAll('.toc-item').forEach((a) =>
        a.classList.toggle('active', a.dataset.id === top.id));
    }
  }, { root: els.docView, rootMargin: '-72px 0px -72% 0px', threshold: 0 });
  heads.forEach((h) => tocObserver.observe(h));

  // 恢复用户上次的折叠状态
  setTocCollapsed(store.getItem('docshare-toc') === '0');
}

/* ============================================================
   设置面板(仅桌面端)
   ============================================================ */
function renderMultiDirs() {
  els.multiDirs.innerHTML = state.docsDirs.map((d, i) => `
    <div class="multi-dir-item">
      <span class="multi-dir-path" title="${esc(d)}">${esc(d)}</span>
      <button type="button" class="multi-dir-del" data-idx="${i}" title="移除">✕</button>
    </div>`).join('');
  els.multiDirs.querySelectorAll('.multi-dir-del').forEach((btn) => {
    btn.addEventListener('click', () => {
      state.docsDirs.splice(parseInt(btn.dataset.idx, 10), 1);
      renderMultiDirs();
    });
  });
}

async function openSettings() {
  try {
    const info = await window.go.main.App.ServerInfo();
    state.docsDirs = (info.docsDirs || []).slice();
    renderMultiDirs();
    els.setDocsDir.value = '';
    els.setPort.value = info.port || 8080;
    els.setLan.checked = !!info.lan;
    els.lanUrlText.textContent = info.lanUrl || '';
    els.setBlacklist.value = (info.blacklist || []).join('\n');
    els.setPassword.value = '';
    els.setPassword.placeholder = info.passwordConfigured
      ? '已设置访问密码；留空保持不变'
      : '留空 = 不启用（所有访客免密访问）';
    state.passwordChanged = false;
    // 版本号由后端下发(设置面板初始显示)
    if (info.version) {
      els.updateStatus.innerHTML = `当前版本 <code>${esc(info.version)}</code>`;
    }
    try {
      els.setAutoStart.checked = !!(await window.go.main.App.AutoStart());
    } catch { els.setAutoStart.checked = false; }
  } catch (err) {
    toast('读取配置失败: ' + err.message, 'err');
  }
  els.settingsStatus.textContent = '';
  els.settingsMask.hidden = false;
}

async function saveSettings() {
  const port = parseInt(els.setPort.value, 10);
  if (!port || port < 1 || port > 65535) { toast('端口无效', 'err'); return; }
  const blacklist = els.setBlacklist.value.split('\n').map((s) => s.trim()).filter(Boolean);
  const password = els.setPassword.value.trim();
  els.saveSettingsBtn.disabled = true;
  els.settingsStatus.textContent = '应用配置中…';
  try {
    const info = await window.go.main.App.SaveConfig(
      state.docsDirs.slice(), port, els.setLan.checked, blacklist, password, state.passwordChanged,
    );
    state.passwordChanged = false;
    els.settingsStatus.textContent = '已保存，服务已重启';
    toast('配置已保存，服务已重启');
    setTimeout(() => { if (info.running) location.reload(); }, 500);
  } catch (err) {
    els.settingsStatus.textContent = '';
    toast('保存失败: ' + err.message, 'err');
  } finally {
    els.saveSettingsBtn.disabled = false;
  }
}

/* ---- 开机自启动(独立即时生效) ---- */
async function toggleAutoStart() {
  const on = els.setAutoStart.checked;
  try {
    await window.go.main.App.SetAutoStart(on);
    toast(on ? '已开启开机自启动' : '已关闭开机自启动');
  } catch (err) {
    els.setAutoStart.checked = !on;
    toast('设置失败: ' + err.message, 'err');
  }
}

/* ---- 访问记录 ---- */
async function openAccess() {
  els.accessMask.hidden = false;
  await loadAccess();
}

async function loadAccess() {
  els.accessList.innerHTML = '<div class="spinner"></div>';
  els.accessStatus.textContent = '';
  try {
    const list = (await window.go.main.App.ListAccessLogs()) || [];
    if (!list.length) {
      els.accessList.innerHTML = `
        <div class="tree-empty" style="padding:40px 0">
          <p style="margin-bottom:6px">暂无访问记录</p>
          <p style="font-size:12px;opacity:.8">局域网用户浏览文档后，会显示在这里</p>
        </div>`;
      return;
    }
    els.accessList.innerHTML = `
      <div class="access-table">
        ${list.map((r) => `
          <div class="access-row">
            <span class="access-time">${esc(fmtTime(r.time))}</span>
            <span class="access-doc" title="${esc(r.doc)}">${esc(r.doc)}</span>
            <span class="access-ip">${esc(r.ip || '-')}</span>
            <span class="access-ua" title="${esc(r.ua || '')}">${esc((r.ua || '').slice(0, 60))}</span>
          </div>`).join('')}
      </div>`;
    els.accessStatus.textContent = `共 ${list.length} 条记录`;
  } catch (err) {
    els.accessList.innerHTML = `<div class="tree-empty">${esc(err.message)}</div>`;
  }
}

/* ---- 软件更新检查与一键更新 ---- */
let lastUpdateInfo = null; // 最近一次检查的更新信息(含更新说明)
let pendingInstaller = ''; // 已下载待安装的安装包路径

// 启动静默检查: 发现新版本只显示徽标, 不打扰; 点击徽标转为完整检查
async function silentUpdateCheck() {
  try {
    const info = await window.go.main.App.CheckUpdate();
    lastUpdateInfo = info;
    if (info.hasUpdate) {
      els.updateBadge.textContent = `新版本 v${info.latest}`;
      els.updateBadge.hidden = false;
    }
  } catch { /* 网络不可用等: 静默忽略 */ }
}

async function checkUpdate() {
  els.checkUpdateBtn.disabled = true;
  els.updateStatus.innerHTML = '正在检查…';
  try {
    const info = await window.go.main.App.CheckUpdate();
    lastUpdateInfo = info;
    if (info.hasUpdate) {
      els.updateStatus.innerHTML =
        `发现新版本 <code>${esc(info.latest)}</code>（当前 ${esc(info.current)}）` +
        ` <button id="dlUpdateBtn" class="btn primary sm" type="button">下载更新</button>` +
        ` <a href="${esc(info.url)}" target="_blank" style="color:var(--accent)">查看更新内容</a>`;
      const dl = document.getElementById('dlUpdateBtn');
      if (dl) dl.addEventListener('click', downloadUpdate);
    } else {
      els.updateStatus.innerHTML = `当前版本 <code>${esc(info.current)}</code> · 已是最新`;
    }
  } catch (err) {
    els.updateStatus.innerHTML = '检查失败：' + esc(err.message || '网络不可用');
  } finally {
    els.checkUpdateBtn.disabled = false;
  }
}

async function downloadUpdate() {
  els.updateStatus.innerHTML = '正在下载新版安装包…（完成后会自动提示）';
  try {
    const installerPath = await window.go.main.App.DownloadUpdate();
    els.updateStatus.innerHTML = `下载完成：<code>${esc(installerPath)}</code>`;
    pendingInstaller = installerPath;
    showUpdateMask();
  } catch (err) {
    els.updateStatus.innerHTML = '下载失败：' + esc(err.message || '未知错误');
  }
}

// 展示更新完成弹窗: 版本变化 + 更新说明(以 Markdown 渲染)
function showUpdateMask() {
  const info = lastUpdateInfo || {};
  els.updateVersionInfo.textContent =
    `当前版本 v${info.current || '?'} → 新版本 v${info.latest || '?'}`;
  const notes = (info.notes || '').trim()
    ? marked.parse(info.notes)
    : '<p>（本次更新未提供说明）</p>';
  els.updateNotes.innerHTML = DOMPurify.sanitize(notes, { ADD_ATTR: ['target'] });
  els.updateMask.hidden = false;
}

/* ---- 目录选择器 ---- */
async function openDirPicker() {
  dirBrowsePath = '';
  els.dirMask.hidden = false;
  await loadDirList();
}

async function loadDirList() {
  els.dirPath.textContent = dirBrowsePath || '我的电脑';
  els.dirUpBtn.disabled = !dirBrowsePath;
  els.dirList.innerHTML = '<div class="spinner"></div>';
  try {
    const entries = await window.go.main.App.ListDir(dirBrowsePath);
    if (!entries || !entries.length) {
      els.dirList.innerHTML = '<div class="dir-empty">此目录下没有子文件夹</div>';
      return;
    }
    els.dirList.innerHTML = entries.map((e) => `
      <div class="dir-item" data-path="${esc(e.path)}">
        ${ICONS.folder}<span class="dir-item-name">${esc(e.name)}</span>
      </div>`).join('');
    els.dirList.querySelectorAll('.dir-item').forEach((item) => {
      item.addEventListener('click', () => {
        dirBrowsePath = item.dataset.path;
        loadDirList();
      });
    });
  } catch (err) {
    els.dirList.innerHTML = `<div class="dir-empty">${esc(err.message)}</div>`;
  }
}

function dirGoUp() {
  if (!dirBrowsePath) return;
  const up = dirBrowsePath.replace(/[\\/][^\\/]*$/, '');
  dirBrowsePath = up === dirBrowsePath ? '' : up;
  loadDirList();
}

/* ============================================================
   弹窗通用行为
   ============================================================ */
function bindModals() {
  document.querySelectorAll('.modal-mask').forEach((mask) => {
    mask.addEventListener('click', (e) => {
      if (e.target === mask) mask.hidden = true;
    });
    mask.querySelectorAll('[data-close]').forEach((btn) => {
      btn.addEventListener('click', () => { mask.hidden = true; });
    });
  });
  // Esc 关闭
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      document.querySelectorAll('.modal-mask').forEach((m) => { m.hidden = true; });
      closeMenu();
    }
  });
}

/* ============================================================
   初始化
   ============================================================ */
async function init() {
  applyTheme();
  bindModals();
  createTocFab();
  bindExport();
  bindDocNav();
  bindAnnoSelection();
  bindAnnoList();
  bindAnnoView();
  els.annoBtn.addEventListener('click', openAnnoList);
  els.annoSubmit.addEventListener('click', submitAnno);
  els.annoContent.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) submitAnno();
  });

  els.themeBtn.addEventListener('click', (event) => {
    // 循环: 深色 → 浅色 → 跟随系统 → 深色
    const next = state.theme === 'dark' ? 'light' : state.theme === 'light' ? 'auto' : 'dark';
    setTheme(next, event, els.themeBtn);
  });

  // 跟随系统: 系统主题切换时自动联动
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (state.theme === 'auto') {
      applyTheme();
      rerenderMermaid();
    }
  });


  if (DESKTOP) {
    // 左下角管理菜单(popover)
    bindMenu();
    els.menuBtn.hidden = false;
    els.menuPop.querySelector('[data-act="settings"]').hidden = false;
    els.menuPop.querySelector('[data-act="access"]').hidden = false;
    els.menuPop.querySelector('.menu-sep').hidden = false;
    els.saveSettingsBtn.addEventListener('click', saveSettings);
    els.pickDirBtn.addEventListener('click', openDirPicker);
    els.addDirBtn.addEventListener('click', () => {
      const d = els.setDocsDir.value.trim();
      if (!d) { toast('请先输入目录路径', 'err'); return; }
      if (state.docsDirs.includes(d)) { toast('该目录已在列表中', 'err'); return; }
      state.docsDirs.push(d);
      renderMultiDirs();
      els.setDocsDir.value = '';
    });
    els.openBrowserBtn.addEventListener('click', () => window.go.main.App.OpenBrowser());
    els.dirUpBtn.addEventListener('click', dirGoUp);
    els.dirChooseBtn.addEventListener('click', () => {
      if (!dirBrowsePath) { toast('请先选择目录', 'err'); return; }
      els.setDocsDir.value = dirBrowsePath;
      els.dirMask.hidden = true;
    });
    els.setDocsDir.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') saveSettings();
    });
    els.setAutoStart.addEventListener('change', toggleAutoStart);
    els.accessRefresh.addEventListener('click', loadAccess);
    els.checkUpdateBtn.addEventListener('click', checkUpdate);
    // 启动静默检查: 有新版本时徽标提示
    setTimeout(silentUpdateCheck, 2500);
    els.updateBadge.addEventListener('click', checkUpdate);
    els.copyUrlBtn.addEventListener('click', () => {
      const url = (els.lanUrlText.textContent || '').trim();
      if (!url) { toast('暂无可复制的访问地址', 'err'); return; }
      copyText(url).then(() => toast(`访问地址已复制：${url}`));
    });
    els.updateLaterBtn.addEventListener('click', () => {
      els.updateMask.hidden = true;
      pendingInstaller = '';
    });
    els.updateInstallBtn.addEventListener('click', async () => {
      els.updateMask.hidden = true;
      if (pendingInstaller) await window.go.main.App.ApplyUpdate(pendingInstaller);
      pendingInstaller = '';
    });
    els.pwdToggle.addEventListener('click', () => {
      const show = els.setPassword.type === 'password';
      els.setPassword.type = show ? 'text' : 'password';
      els.pwdToggle.innerHTML = show ? ICONS.eyeOff : ICONS.eye;
    });
    els.setPassword.addEventListener('input', () => { state.passwordChanged = true; });
  } else {
    // 网页端: 管理按钮与菜单从 DOM 中彻底移除
    if (els.menuBtn) els.menuBtn.remove();
    if (els.menuPop) els.menuPop.remove();
  }

  // 侧边栏拖拽调宽 + 恢复上次宽度
  applySidebarWidth();
  bindSidebarResize();
  bindScrollMemory();
  renderRecent();

  // 搜索(防抖): 文件名过滤 + 全文搜索
  let debounce;
  els.search.addEventListener('input', () => {
    clearTimeout(debounce);
    debounce = setTimeout(async () => {
      state.search = els.search.value.trim();
      renderTree();
      await doFulltextSearch(state.search);
    }, 260);
  });

  // 全局快捷键: Ctrl+K 搜索 / Ctrl+P 打印 / Esc 关闭弹窗
  bindShortcuts();

  // 桌面端: 获取服务信息; 壳页(wails://)下 API 走绝对地址
  if (DESKTOP) {
    try {
      state.serverInfo = await window.go.main.App.ServerInfo();
      if (state.serverInfo && state.serverInfo.running) {
        state.apiBase = 'http://127.0.0.1:' + state.serverInfo.port;
      }
    } catch { /* bind 暂不可用, 保持相对路径 */ }
  }

  // 访问密码认证(未通过则显示登录遮罩)
  els.loginBtn.addEventListener('click', doLogin);
  els.loginPassword.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') doLogin();
  });
  const authed = await checkAuth();
  if (!authed) return; // 登录成功后页面 reload 重新初始化

  // 加载目录树 + 启动自动刷新(文档增删改后 ≤3s 更新)
  try {
    const data = await api('/api/tree');
    state.tree = data.node || data;
    state.ready = !!data.ready;
    state.treeSig = JSON.stringify(state.tree);
    renderTree();
    // 桌面端: 自动打开上次阅读的文档(章节位置随记忆恢复)
    if (DESKTOP && state.recent.length && !state.currentDoc) {
      const last = state.recent[0];
      if (findTreeNode(state.tree, last.path)) {
        openDoc(last.path, null);
      }
    }
  } catch (err) {
    els.tree.innerHTML = DESKTOP
      ? `<div class="tree-empty">本地服务不可用（${esc(err.message)}）<br/><span style="font-size:12px;opacity:.75">请打开「设置」检查服务状态</span></div>`
      : `<div class="tree-empty">${esc(err.message)}</div>`;
  }
  setInterval(pollTree, 3000);
}

document.addEventListener('DOMContentLoaded', init);
