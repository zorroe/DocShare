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
};

/* ---------- DOM 引用 ---------- */
const $ = (id) => document.getElementById(id);
const els = {
  tree: $('tree'),
  search: $('searchInput'),
  themeBtn: $('themeBtn'),
  menuBtn: $('menuBtn'),
  menuPop: $('menuPop'),
  menuThemeLabel: $('menuThemeLabel'),
  searchResults: $('searchResults'),
  sidebar: document.querySelector('.sidebar'),
  resizer: $('sidebarResizer'),
  docView: $('docView'),
  crumbs: $('crumbs'),
  docMeta: $('docMeta'),
  toast: $('toast'),
  settingsMask: $('settingsMask'),
  setDocsDir: $('setDocsDir'),
  pickDirBtn: $('pickDirBtn'),
  setPort: $('setPort'),
  setLan: $('setLan'),
  setAutoStart: $('setAutoStart'),
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
   Markdown 渲染
   ============================================================ */
marked.setOptions({ gfm: true, breaks: true });

function renderMd(md, container) {
  const html = marked.parse(md || '');
  container.innerHTML = DOMPurify.sanitize(html);
  container.querySelectorAll('pre code').forEach((el) => {
    try { hljs.highlightElement(el); } catch { /* ignore */ }
  });
}

/* ============================================================
   主题
   ============================================================ */
function applyTheme() {
  document.documentElement.dataset.theme = state.theme;
  els.themeBtn.innerHTML = state.theme === 'dark' ? ICONS.sun : ICONS.moon;
  els.themeBtn.title = state.theme === 'dark' ? '切换到浅色' : '切换到深色';
  if (els.menuThemeLabel) {
    els.menuThemeLabel.textContent = state.theme === 'dark' ? '切换到浅色主题' : '切换到深色主题';
  }
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
    item.addEventListener('click', () => {
      const act = item.dataset.act;
      if (act === 'settings') openSettings();
      else if (act === 'access') openAccess();
      else if (act === 'theme') {
        state.theme = state.theme === 'dark' ? 'light' : 'dark';
        store.setItem('docshare-theme', state.theme);
        applyTheme();
      }
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
   目录树
   ============================================================ */
function renderTree() {
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
  } catch { /* 服务不可用时静默 */ }
}

/* ============================================================
   文档浏览
   ============================================================ */
async function openDoc(path, rowEl) {
  try {
    const doc = await api('/api/doc?path=' + encodeURIComponent(path));
    state.currentDoc = doc;
    document.querySelectorAll('.tree-row.active').forEach((r) => r.classList.remove('active'));
    if (rowEl) rowEl.classList.add('active');
    renderDoc(doc);
  } catch (err) {
    toast(err.message, 'err');
  }
}

function renderDoc(doc) {
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
  renderMd(doc.content, h.querySelector('.md-body'));
  buildToc(h);

  // 全文搜索关键词高亮
  if (state.search) {
    highlightTextNodes(h.querySelector('.md-body'), state.search);
  }
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
      if (en.isIntersecting && (!top || en.boundingClientRect.top < top.boundingClientRect.top)) {
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
async function openSettings() {
  try {
    const info = await window.go.main.App.ServerInfo();
    els.setDocsDir.value = info.docsDir || '';
    els.setPort.value = info.port || 8080;
    els.setLan.checked = !!info.lan;
    els.setBlacklist.value = (info.blacklist || []).join('\n');
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
  const dir = els.setDocsDir.value.trim();
  const port = parseInt(els.setPort.value, 10);
  if (!port || port < 1 || port > 65535) { toast('端口无效', 'err'); return; }
  const blacklist = els.setBlacklist.value.split('\n').map((s) => s.trim()).filter(Boolean);
  els.saveSettingsBtn.disabled = true;
  els.settingsStatus.textContent = '应用配置中…';
  try {
    const info = await window.go.main.App.SaveConfig(dir, port, els.setLan.checked, blacklist);
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

  els.themeBtn.addEventListener('click', () => {
    state.theme = state.theme === 'dark' ? 'light' : 'dark';
    store.setItem('docshare-theme', state.theme);
    applyTheme();
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
  } else {
    // 网页端: 管理按钮与菜单从 DOM 中彻底移除
    if (els.menuBtn) els.menuBtn.remove();
    if (els.menuPop) els.menuPop.remove();
  }

  // 侧边栏拖拽调宽 + 恢复上次宽度
  applySidebarWidth();
  bindSidebarResize();

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

  // Ctrl+K 聚焦搜索
  document.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      els.search.focus();
      els.search.select();
    }
  });

  // 桌面端: 获取服务信息; 壳页(wails://)下 API 走绝对地址
  if (DESKTOP) {
    try {
      state.serverInfo = await window.go.main.App.ServerInfo();
      if (state.serverInfo && state.serverInfo.running) {
        state.apiBase = 'http://127.0.0.1:' + state.serverInfo.port;
      }
    } catch { /* bind 暂不可用, 保持相对路径 */ }
  }

  // 加载目录树 + 启动自动刷新(文档增删改后 ≤3s 更新)
  try {
    const data = await api('/api/tree');
    state.tree = data.node || data;
    state.ready = !!data.ready;
    state.treeSig = JSON.stringify(state.tree);
    renderTree();
  } catch (err) {
    els.tree.innerHTML = DESKTOP
      ? `<div class="tree-empty">本地服务不可用（${esc(err.message)}）<br/><span style="font-size:12px;opacity:.75">请打开「设置」检查服务状态</span></div>`
      : `<div class="tree-empty">${esc(err.message)}</div>`;
  }
  setInterval(pollTree, 3000);
}

document.addEventListener('DOMContentLoaded', init);
