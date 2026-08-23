/* DocShare 前端自动化冒烟测试 (puppeteer-core + 本机 Edge) */
const puppeteer = require('puppeteer-core');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');

const SERVER = process.env.DS_SERVER || path.join(__dirname, '..', 'release', 'DocShare-Server.exe');

const BASE = process.env.DS_BASE || 'http://127.0.0.1:18080';
const EDGE = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';

(async () => {
  const browser = await puppeteer.launch({
    executablePath: EDGE,
    headless: true,
    args: ['--no-sandbox', '--disable-gpu'],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900 });
  page.on('pageerror', (e) => console.log('[pageerror]', e.message));
  let nativeDialogs = 0; // 原生对话框出现次数(更新流程应使用自定义弹窗)
  page.on('dialog', async (d) => { nativeDialogs++; await d.dismiss(); });

  const results = [];
  const check = (name, ok, extra = '') => {
    results.push({ name, ok });
    console.log(`${ok ? 'PASS' : 'FAIL'} ${name} ${extra}`);
  };

  // ---- 1. 页面加载 + 文档树 ----
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' });
  const treeText = await page.$eval('#tree', (el) => el.textContent);
  check('文档树渲染', treeText.includes('README.md') && treeText.includes('使用说明'), `tree=${treeText.slice(0, 40)}`);

  // ---- 1.5 悬浮显示文件全名 ----
  // 确保「指南」目录展开(初始即展开时不做操作)
  await page.evaluate(() => {
    const row = [...document.querySelectorAll('.tree-row')].find((x) => x.textContent.includes('指南'));
    const ch = row.parentElement.querySelector('.tree-children');
    if (ch.classList.contains('collapsed')) row.click();
  });
  await new Promise((r) => setTimeout(r, 200));
  const rows2 = await page.$$('.tree-row');
  let tipRow = null;
  for (const r of rows2) {
    const t = await r.evaluate((el) => el.textContent);
    if (t.includes('使用说明')) { tipRow = r; break; }
  }
  if (!tipRow) throw new Error('未找到「使用说明」行');
  const tipBox = await tipRow.boundingBox();
  if (!tipBox) throw new Error('「使用说明」行不可见(展开未生效?)');
  await page.mouse.move(tipBox.x + tipBox.width / 2, tipBox.y + tipBox.height / 2);
  await page.waitForSelector('#treeTip:not([hidden])', { timeout: 3000 });
  const tipText = await page.$eval('#treeTip', (el) => el.textContent);
  check('悬浮显示文件全名', tipText.includes('使用说明') && tipText.includes('指南/'), tipText);
  await page.mouse.move(8, 8);
  await new Promise((r) => setTimeout(r, 250));
  const tipHidden = await page.$eval('#treeTip', (el) => el.hidden);
  check('移开隐藏提示', tipHidden, '');
  // 收起文件夹, 恢复初始状态
  await page.evaluate(() => {
    const row = [...document.querySelectorAll('.tree-row')].find((x) => x.textContent.includes('指南'));
    row.click();
  });
  await new Promise((r) => setTimeout(r, 200));

  // ---- 2. 打开文档渲染 ----
  await page.$$eval('.tree-row', (rows) => {
    const row = rows.find((r) => r.textContent.includes('README.md'));
    row.click();
  });
  await page.waitForFunction(
    () => document.querySelector('#docView .md-body') && document.querySelector('#docView .md-body').textContent.length > 50,
    { timeout: 6000 });
  const docText = await page.$eval('#docView .md-body', (el) => el.textContent);
  check('文档正文渲染', docText.includes('DocShare'), '');
  const editBtnGone = await page.evaluate(() => !document.querySelector('#editBtn'));
  check('无申请编辑按钮', editBtnGone, '');

  // ---- 2.3 Mermaid 图表渲染 ----
  await page.waitForSelector('#docView .mermaid svg', { timeout: 10000 });
  const mermaidOk = await page.$eval('#docView .mermaid svg', (el) => el.textContent.includes('局域网用户'));
  check('Mermaid 图表渲染', mermaidOk, '');

  // ---- 2.4 代码块复制按钮 ----
  const copyBtnExists = await page.$eval('#docView .md-body pre .copy-btn', (el) => !!el);
  check('代码复制按钮', copyBtnExists, '');
  await page.hover('#docView .md-body pre');
  await new Promise((r) => setTimeout(r, 300));
  await page.click('#docView .md-body pre .copy-btn');
  await new Promise((r) => setTimeout(r, 300));
  const copyLabel = await page.$eval('#docView .md-body pre .copy-btn', (el) => el.textContent);
  check('复制按钮反馈', copyLabel === '已复制', copyLabel);

  // ---- 2.5 文档导出 ----
  const exportEnabled = await page.$eval('#exportBtn', (el) => !el.disabled);
  check('导出按钮可用', exportEnabled, '');
  // 导出 HTML 文件(通过测试钩子验证生成内容)
  await page.click('#exportBtn');
  await page.waitForSelector('#exportMenu:not([hidden])');
  await new Promise((r) => setTimeout(r, 300)); // 等待弹出动画结束
  await page.click('#exportMenu [data-act="html"]');
  await page.waitForFunction(() => window.__DSH_LAST_EXPORT && window.__DSH_LAST_EXPORT.includes('<!DOCTYPE html>'), { timeout: 6000 });
  const dlContent = await page.evaluate(() => window.__DSH_LAST_EXPORT);
  check('导出 HTML 文件', dlContent.includes('<!DOCTYPE html>') && dlContent.includes('DocShare') && dlContent.includes('title'), `len=${dlContent.length}`);
  // 导出 PDF(打印): mock window.print
  await page.evaluate(() => { window.__printed = 0; window.print = () => { window.__printed++; }; });
  await page.click('#exportBtn');
  await new Promise((r) => setTimeout(r, 300)); // 等待弹出动画结束
  await page.click('#exportMenu [data-act="pdf"]');
  await new Promise((r) => setTimeout(r, 400));
  const printed = await page.evaluate(() => window.__printed);
  check('打印为 PDF 调用', printed === 1, `printed=${printed}`);

  // ---- 2.6.5 快捷键 ----
  // Ctrl+P 触发打印导出
  await page.keyboard.down('Control');
  await page.keyboard.press('p');
  await page.keyboard.up('Control');
  await new Promise((r) => setTimeout(r, 300));
  const printed2 = await page.evaluate(() => window.__printed);
  check('Ctrl+P 打印', printed2 === 2, `printed=${printed2}`);
  // Ctrl+K 聚焦搜索框
  await page.keyboard.down('Control');
  await page.keyboard.press('k');
  await page.keyboard.up('Control');
  const focused = await page.evaluate(() => document.activeElement && document.activeElement.id === 'searchInput');
  check('Ctrl+K 聚焦搜索', focused, '');
  // Esc 关闭导出菜单
  await page.click('#exportBtn');
  await page.waitForSelector('#exportMenu:not([hidden])');
  await page.keyboard.press('Escape');
  const escClosed = await page.$eval('#exportMenu', (el) => el.hidden);
  check('Esc 关闭弹出层', escClosed, '');

  // ---- 2.7 文档互链 ----
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('链接测试A')).click();
  });
  await page.waitForFunction(() => document.querySelector('#docView .md-body').textContent.includes('文档 A'), { timeout: 6000 });
  const extLink = await page.$eval('#docView .md-body a[target="_blank"]', (el) => !!el);
  check('外部链接新窗口', extLink, '');
  // 点击站内链接 → 打开 B(页内同步派发, 避免 puppeteer click 的 scrollIntoView 与重渲染竞态)
  await page.$eval('#docView .md-body a.doc-link', (el) => el.click());
  await page.waitForFunction(() => document.querySelector('#docView .md-body').textContent.includes('文档 B'), { timeout: 6000 });
  check('站内文档跳转', true, '');
  // 面包屑确认切到 B
  await page.waitForFunction(() => document.querySelector('#crumbs').textContent.includes('链接测试B'), { timeout: 6000 });
  check('跳转后面包屑', true, '');
  // 从 B 点回 A
  await page.$eval('#docView .md-body a.doc-link', (el) => el.click());
  await page.waitForFunction(() => document.querySelector('#docView .md-body').textContent.includes('文档 A'), { timeout: 6000 });
  check('返回跳转', true, '');

  // ---- 2.6 文档内图片渲染 ----
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('图片示例')).click();
  });
  await page.waitForFunction(() => document.querySelector('#docView .md-body img.doc-img'), { timeout: 15000 });
  await page.waitForFunction(() => {
    const imgs = [...document.querySelectorAll('#docView .md-body img.doc-img')];
    return imgs.length >= 2 && imgs.every((i) => i.complete && i.naturalWidth > 0);
  }, { timeout: 15000 });
  const imgSrcOk = await page.$$eval('#docView .md-body img.doc-img', (els) => els.every((i) => i.src.includes('/api/file?path=')));
  check('文档图片渲染', imgSrcOk, '');
  // 导出含图片文档 → 图片内联 base64
  await page.click('#exportBtn');
  await new Promise((r) => setTimeout(r, 300));
  await page.click('#exportMenu [data-act="html"]');
  await page.waitForFunction(() => window.__DSH_LAST_EXPORT && window.__DSH_LAST_EXPORT.includes('data:image/'), { timeout: 10000 });
  check('导出图片内联 base64', true, '');

  // ---- 2.5 大纲视图 ----
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('README.md')).click();
  });
  await page.waitForFunction(() => document.querySelector('#docView .md-body').textContent.includes('DocShare'), { timeout: 6000 });
  await page.waitForSelector('.toc-panel', { timeout: 4000 });
  const tocText = await page.$eval('.toc-panel', (el) => el.textContent);
  check('大纲面板渲染', tocText.includes('快速开始') && tocText.includes('功能特性'), tocText.slice(0, 40));
  const tocCount = await page.$$eval('.toc-item', (els) => els.length);
  check('大纲条目数量', tocCount >= 5, `count=${tocCount}`);
  // 点击大纲跳转: 记录点击前的 scrollTop, 点击后应变化
  await page.$eval('#docView', (el) => { el.scrollTop = 0; });
  await new Promise((r) => setTimeout(r, 300));
  const before = await page.$eval('#docView', (el) => el.scrollTop);
  await page.click('.toc-item[data-id="功能特性"]');
  await new Promise((r) => setTimeout(r, 900));
  const after = await page.$eval('#docView', (el) => el.scrollTop);
  check('大纲点击跳转', after > before, `scroll ${before} -> ${after}`);
  // 跳转后高亮
  const active = await page.$$eval('.toc-item.active', (els) => els.map((e) => e.dataset.id));
  check('大纲滚动高亮', active.includes('功能特性'), active.join(','));
  // 回顶部
  await page.$eval('#docView', (el) => { el.scrollTop = 0; });

  // 大纲折叠/展开
  await page.click('.toc-collapse-btn');
  await new Promise((r) => setTimeout(r, 400));
  const collapsed = await page.$eval('.toc-panel', (el) => el.classList.contains('collapsed'));
  const fabVisible = await page.$eval('.toc-fab', (el) => !el.hidden);
  check('大纲折叠', collapsed && fabVisible, '');
  await page.click('.toc-fab');
  await new Promise((r) => setTimeout(r, 400));
  const expanded = await page.$eval('.toc-panel', (el) => !el.classList.contains('collapsed'));
  const fabHidden = await page.$eval('.toc-fab', (el) => el.hidden);
  check('大纲展开', expanded && fabHidden, '');
  // 展开时不遮盖正文: 面板右边缘在正文右边缘之外(正文右侧让位)
  const noOverlap = await page.evaluate(() => {
    const panel = document.querySelector('.toc-panel');
    const body = document.querySelector('#docView .md-body');
    const pr = panel.getBoundingClientRect();
    const br = body.getBoundingClientRect();
    return pr.left >= br.right - 1; // 面板完全在正文右侧
  });
  check('大纲不遮盖正文', noOverlap, '');

  // ---- 3. 搜索过滤 ----
  await page.type('#searchInput', '最佳');
  await new Promise((r) => setTimeout(r, 400));
  const searchText = await page.$eval('#tree', (el) => el.textContent);
  check('搜索过滤', searchText.includes('最佳实践') && !searchText.includes('README.md'), '');

  // ---- 3.5 全文搜索 ----
  await page.$eval('#searchInput', (el) => {
    el.value = '';
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await new Promise((r) => setTimeout(r, 350));
  await page.type('#searchInput', '欢迎使用');
  await page.waitForFunction(() => {
    const box = document.querySelector('#searchResults');
    return !box.hidden && box.textContent.includes('欢迎使用');
  }, { timeout: 8000 });
  const srText = await page.$eval('#searchResults', (el) => el.textContent);
  check('全文搜索结果', srText.includes('使用说明') && srText.includes('欢迎使用'), srText.slice(0, 40));
  // 点击结果 → 打开文档并高亮关键词
  await page.click('#searchResults .sr-item');
  await page.waitForFunction(() => document.querySelectorAll('mark.search-hit').length > 0, { timeout: 6000 });
  const hitCount = await page.$$eval('mark.search-hit', (els) => els.length);
  check('关键词高亮', hitCount > 0, `hits=${hitCount}`);
  // 清空搜索 → 结果面板隐藏 + 目录树恢复
  await page.$eval('#searchInput', (el) => {
    el.value = '';
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await new Promise((r) => setTimeout(r, 450));
  const srHidden = await page.$eval('#searchResults', (el) => el.hidden);
  check('清空搜索隐藏结果', srHidden, '');
  const treeRestored = await page.$eval('#tree', (el) => el.textContent.includes('README.md'));
  check('清空后目录树恢复', treeRestored, '');

  // ---- 3.6 目录树自动刷新(增删文件 ≤3s 自动更新) ----
  const tmpDoc = path.join(__dirname, '..', 'docs', '_auto_refresh_test.md');
  fs.writeFileSync(tmpDoc, '# 自动刷新测试\n\n临时文档, 测试后删除。');
  await page.waitForFunction(() =>
    document.querySelector('#tree').textContent.includes('_auto_refresh_test'),
    { timeout: 12000 });
  check('目录树自动刷新(新增文件)', true, '');
  fs.unlinkSync(tmpDoc);
  await page.waitForFunction(() =>
    !document.querySelector('#tree').textContent.includes('_auto_refresh_test'),
    { timeout: 12000 });
  check('目录树自动刷新(删除文件)', true, '');

  // ---- 3.6.5 当前文档自动刷新(磁盘内容被修改/删除时) ----
  const autoDoc = path.join(__dirname, '..', 'docs', '_auto_reload_test.md');
  fs.writeFileSync(autoDoc, '# 自动刷新测试\n\n版本一\n');
  await page.waitForFunction(() =>
    document.querySelector('#tree').textContent.includes('_auto_reload_test'),
    { timeout: 12000 });
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('_auto_reload_test')).click();
  });
  await page.waitForFunction(() =>
    document.querySelector('#docView .md-body').textContent.includes('版本一'),
    { timeout: 6000 });
  // 外部修改文件 → 轮询检测到 mtime/size 变化后自动重新渲染
  fs.writeFileSync(autoDoc, '# 自动刷新测试\n\n版本二\n');
  await page.waitForFunction(() =>
    document.querySelector('#docView .md-body').textContent.includes('版本二'),
    { timeout: 15000 });
  check('文档自动刷新(内容变更)', true, '');
  const refreshTip = await page.$eval('#toast', (el) => el.textContent);
  check('自动刷新提示', refreshTip.includes('已自动刷新'), refreshTip);
  // 删除当前文档 → 提示已被删除(内容保留)
  fs.unlinkSync(autoDoc);
  await page.waitForFunction(() =>
    document.querySelector('#toast').textContent.includes('已被删除'),
    { timeout: 15000 });
  check('文档删除提示', true, '');
  // 回到 README 继续后续用例
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('README.md')).click();
  });
  await page.waitForFunction(() =>
    document.querySelector('#docView .md-body').textContent.includes('DocShare'),
    { timeout: 6000 });

  // ---- 3.7 阅读记忆: 最近浏览 + 滚动位置恢复 ----
  // 打开 README 并滚动到 300
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('README.md')).click();
  });
  await page.waitForFunction(() => document.querySelector('#docView .md-body'), { timeout: 6000 });
  await page.$eval('#docView', (el) => { el.scrollTop = 300; });
  await new Promise((r) => setTimeout(r, 800)); // 等滚动保存防抖
  // 打开另一篇文档
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('使用说明')).click();
  });
  await page.waitForFunction(() => document.querySelector('#docView .md-body').textContent.includes('使用说明'), { timeout: 6000 });
  // 最近浏览区块出现
  const recentShown = await page.$eval('#recentBox', (el) => !el.hidden && el.textContent.includes('README.md'));
  check('最近浏览区块', recentShown, '');
  // 点击最近浏览回到 README, 滚动位置应恢复
  await page.click('#recentBox .recent-item');
  await page.waitForFunction(() => document.querySelector('#docView .md-body').textContent.includes('DocShare'), { timeout: 6000 });
  await new Promise((r) => setTimeout(r, 800));
  const restored = await page.$eval('#docView', (el) => el.scrollTop);
  check('阅读位置恢复', Math.abs(restored - 300) <= 2, `scroll=${restored}`);
  // 清除最近浏览
  await page.click('#recentBox .recent-clear');
  await new Promise((r) => setTimeout(r, 300));
  const recentCleared = await page.$eval('#recentBox', (el) => el.hidden);
  check('清除最近浏览', recentCleared, '');
  const storageCleared = await page.evaluate(() => !localStorage.getItem('docshare-recent') || JSON.parse(localStorage.getItem('docshare-recent')).length === 0);
  check('清除后存储为空', storageCleared, '');

  // ---- 4. 编辑/审批功能已彻底移除 ----
  const editGone = await page.evaluate(() =>
    !document.querySelector('#editBtn') && !document.querySelector('#editMask') &&
    !document.querySelector('#adminMask') && !document.querySelector('#reqMask'));
  check('编辑与审批功能彻底移除', editGone, '');

  // ---- 5. 网页端无管理入口 ----
  const menuGone = await page.evaluate(() =>
    !document.querySelector('#menuBtn') && !document.querySelector('#menuPop'));
  check('网页端管理入口彻底移除', menuGone, '');

  // ---- 5.2 协议隔离: http 页面即使存在 window.go 也不显示管理入口 ----
  await page.evaluateOnNewDocument(() => {
    window.go = { main: { App: { ServerInfo: async () => ({ port: 18080, running: true }) } } };
  });
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' });
  const isolated = await page.evaluate(() => !document.querySelector('#menuBtn'));
  check('http 页面协议隔离(有 window.go 也不显示)', isolated, '');

  // ---- 5.3 桌面壳页识别: Wails Windows 壳页是 http://wails.localhost/ ----
  const isWebFn = (protocol, hostname) => /^https?:/.test(protocol) && hostname !== 'wails.localhost';
  check('壳页识别: wails.localhost 视为桌面', !isWebFn('http:', 'wails.localhost'), '');
  check('壳页识别: wails:// 视为桌面', !isWebFn('wails:', 'app'), '');
  check('壳页识别: 局域网 http 视为网页', isWebFn('http:', '192.168.1.5'), '');

  // ---- 5.5 侧边栏拖拽调宽 ----
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' }); // 干净状态
  const rBox = await (await page.$('#sidebarResizer')).boundingBox();
  await page.mouse.move(rBox.x + 3, rBox.y + 200);
  await page.mouse.down();
  await page.mouse.move(rBox.x + 3 + 110, rBox.y + 200, { steps: 6 });
  await page.mouse.up();
  const dragW = await page.$eval('.sidebar', (el) => el.offsetWidth);
  check('侧边栏拖拽调宽', dragW > 288, `width=${dragW}`);
  // 宽度记忆: 刷新后保持
  await page.reload({ waitUntil: 'networkidle0' });
  await new Promise((r) => setTimeout(r, 600));
  const keptW = await page.$eval('.sidebar', (el) => el.offsetWidth);
  check('侧边栏宽度记忆', Math.abs(keptW - dragW) < 2, `saved=${dragW} kept=${keptW}`);

  // ---- 6. 桌面模式 mock ----
  await page.evaluateOnNewDocument(() => {
    window.__DSH_TEST_DESKTOP = true; // 自动化测试标记: 允许 http 页面模拟桌面模式
    window.go = { main: { App: {
      ServerInfo: async () => ({ port: Number(location.port || 8080), docsDir: 'E:/code/DocShare/docs', docsDirs: ['E:/code/DocShare/docs'], lan: true, lanUrl: 'http://192.168.1.5:8080', running: true, dataDir: 'x', error: '', blacklist: [], password: '', version: '1.1.1' }),
      ListDir: async (p) => (p ? [{ name: '子目录', path: p + '/sub' }] : [{ name: 'C:\\', path: 'C:\\' }]),
      SaveConfig: async (dirs, p, l, bl, pw) => ({ port: p, docsDirs: dirs, docsDir: dirs[0] || '', lan: l, running: true }),
      OpenBrowser: async () => {},
      AutoStart: async () => false,
      SetAutoStart: async () => {},
      CheckUpdate: async () => ({ current: '1.0.0', latest: '1.1.0', url: 'https://github.com/zorroe/DocShare/releases', downloadUrl: 'https://example.com/Setup.exe', notes: '## 更新内容\n- 新增全文搜索\n- 修复若干问题', hasUpdate: true }),
      DownloadUpdate: async () => 'C:/Users/test/AppData/Local/Temp/DocShare-Setup-1.1.0.exe',
      ApplyUpdate: async () => { window.__dsApplyCalls = (window.__dsApplyCalls || 0) + 1; },
      ListAccessLogs: async () => [{ time: '2026-08-19T10:00:00+08:00', doc: 'README.md', ip: '192.168.1.5', ua: 'Mozilla/5.0 Chrome' }],
    } } };
  });
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' });
  const menuVisible = await page.$eval('#menuBtn', (el) => !el.hidden);
  check('桌面模式显示管理按钮', menuVisible, '');
  // 管理菜单中无审批项(编辑功能已删除)
  const adminItemGone = await page.evaluate(() => !document.querySelector('#menuPop [data-act="admin"]'));
  check('管理菜单无审批项', adminItemGone, '');
  // 启动静默更新检查: 不点击也会出现新版本徽标
  await page.waitForFunction(() => !document.querySelector('#updateBadge').hidden, { timeout: 8000 });
  const badgeText = await page.$eval('#updateBadge', (el) => el.textContent);
  check('启动静默更新检查(徽标)', badgeText.includes('新版本 v1.1.0'), badgeText);
  // popover 菜单打开设置
  await page.click('#menuBtn');
  await page.waitForSelector('#menuPop:not([hidden])');
  const popVisible = await page.$eval('#menuPop', (el) => !el.hidden);
  check('popover 菜单弹出', popVisible, '');
  // 主题菜单: 三项 + 跟随系统生效
  const themeItems = await page.$$eval('#menuPop .theme-item', (els) => els.length);
  check('主题菜单三项', themeItems === 3, `count=${themeItems}`);
  const reopenThemeMenu = async () => {
    await page.waitForFunction(() => !document.documentElement.classList.contains('theme-transitioning'));
    await page.click('#menuBtn');
    await page.waitForSelector('#menuPop:not([hidden])');
  };
  await page.click('#menuPop [data-act="theme-auto"]');
  const themeAuto = await page.evaluate(() => document.documentElement.dataset.theme);
  check('跟随系统主题生效', themeAuto === 'light' || themeAuto === 'dark', `theme=${themeAuto}`);
  await reopenThemeMenu();
  await page.click('#menuPop [data-act="theme-light"]');
  await page.waitForFunction(() => document.documentElement.dataset.theme === 'light');
  const themeLight = await page.evaluate(() => document.documentElement.dataset.theme);
  check('浅色主题切换', themeLight === 'light', `theme=${themeLight}`);
  await reopenThemeMenu();
  await page.click('#menuPop [data-act="theme-dark"]');
  await page.waitForFunction(() => document.documentElement.dataset.theme === 'dark');
  const themeDark = await page.evaluate(() => document.documentElement.dataset.theme);
  check('深色主题切换', themeDark === 'dark', `theme=${themeDark}`);

  // 主题双向动画: 浅转深向外扩散，深转浅向按钮收拢
  await page.waitForFunction(() => !document.documentElement.classList.contains('theme-transitioning'));
  await page.emulateMediaFeatures([
    { name: 'prefers-reduced-motion', value: 'no-preference' },
    { name: 'prefers-color-scheme', value: 'dark' },
  ]);
  const themeBox = await (await page.$('#themeBtn')).boundingBox();
  const themeViewport = await page.evaluate(() => ({ width: innerWidth, height: innerHeight }));
  const requiredThemeRadius = Math.hypot(
    Math.max(themeBox.x + themeBox.width / 2, themeViewport.width - themeBox.x - themeBox.width / 2),
    Math.max(themeBox.y + themeBox.height / 2, themeViewport.height - themeBox.y - themeBox.height / 2),
  );
  await page.evaluate(() => {
    window.__dsThemeTransitionCalls = 0;
    window.__dsThemeAnimation = null;
    window.__dsThemeUpdateReturnedPromise = null;
    document.startViewTransition = (update) => {
      window.__dsThemeTransitionCalls++;
      const updated = Promise.resolve().then(() => {
        const updateResult = update();
        window.__dsThemeUpdateReturnedPromise = !!updateResult && typeof updateResult.then === 'function';
        return updateResult;
      });
      return { ready: updated, finished: updated };
    };
    document.documentElement.animate = (frames, options) => {
      window.__dsThemeAnimation = { frames, options };
      return { finished: Promise.resolve(), cancel() {} };
    };
  });
  await page.click('#themeBtn');
  await page.waitForFunction(() => !!window.__dsThemeAnimation, { timeout: 3000 });
  const contract = await page.evaluate(() => ({
    calls: window.__dsThemeTransitionCalls,
    updateReturnedPromise: window.__dsThemeUpdateReturnedPromise,
    animation: window.__dsThemeAnimation,
    theme: document.documentElement.dataset.theme,
  }));
  const contractFrames = Array.isArray(contract.animation.frames)
    ? contract.animation.frames.map((frame) => frame.clipPath)
    : contract.animation.frames.clipPath;
  const contractStart = contractFrames[0];
  const contractEnd = contractFrames[contractFrames.length - 1];
  const contractRadius = Number((contractStart.match(/^circle\(([\d.]+)px/) || [])[1]);
  const originMatch = contractEnd.match(/at ([\d.]+)px ([\d.]+)px/);
  const originOK = originMatch &&
    Math.abs(Number(originMatch[1]) - (themeBox.x + themeBox.width / 2)) < 2 &&
    Math.abs(Number(originMatch[2]) - (themeBox.y + themeBox.height / 2)) < 2;
  check('深色向按钮收拢', contract.calls === 1 && contract.theme === 'light' && originOK &&
    contract.updateReturnedPromise === false &&
    contract.animation.options.pseudoElement === '::view-transition-old(root)' &&
    contract.animation.options.duration === 650 && contract.animation.options.fill === 'forwards' &&
    contractRadius > requiredThemeRadius &&
    contractEnd.startsWith('circle(0px'), JSON.stringify(contract.animation));

  await page.evaluate(() => { window.__dsThemeAnimation = null; });
  await page.click('#themeBtn');
  await page.waitForFunction(() => !!window.__dsThemeAnimation, { timeout: 3000 });
  const spread = await page.evaluate(() => ({
    calls: window.__dsThemeTransitionCalls,
    animation: window.__dsThemeAnimation,
    theme: document.documentElement.dataset.theme,
  }));
  const spreadFrames = Array.isArray(spread.animation.frames)
    ? spread.animation.frames.map((frame) => frame.clipPath)
    : spread.animation.frames.clipPath;
  const spreadRadius = Number((spreadFrames[spreadFrames.length - 1].match(/^circle\(([\d.]+)px/) || [])[1]);
  check('浅色从按钮向外扩散', spread.calls === 2 && spread.theme === 'dark' &&
    spread.animation.options.pseudoElement === '::view-transition-new(root)' &&
    spread.animation.options.duration === 650 && spread.animation.options.fill === 'forwards' &&
    spreadFrames[0].startsWith('circle(0px') && spreadRadius > requiredThemeRadius,
    JSON.stringify(spread.animation));

  // 系统要求减少动画时直接切换，不启动 View Transition
  await page.emulateMediaFeatures([
    { name: 'prefers-reduced-motion', value: 'reduce' },
    { name: 'prefers-color-scheme', value: 'dark' },
  ]);
  await reopenThemeMenu();
  await page.click('#menuPop [data-act="theme-light"]');
  const reduced = await page.evaluate(() => ({
    calls: window.__dsThemeTransitionCalls,
    theme: document.documentElement.dataset.theme,
  }));
  check('减少动画偏好降级', reduced.calls === 2 && reduced.theme === 'light', JSON.stringify(reduced));
  await page.emulateMediaFeatures([
    { name: 'prefers-reduced-motion', value: 'no-preference' },
    { name: 'prefers-color-scheme', value: 'dark' },
  ]);

  // 键盘触发没有指针坐标，以按钮中心作为扩散原点
  await page.evaluate(() => { window.__dsThemeAnimation = null; });
  await page.focus('#themeBtn');
  await page.keyboard.press('Enter');
  await page.waitForFunction(() => !!window.__dsThemeAnimation, { timeout: 3000 });
  const keyboardClip = await page.evaluate(() => {
    const frames = window.__dsThemeAnimation.frames;
    return Array.isArray(frames) ? frames[0].clipPath : frames.clipPath[0];
  });
  const keyboardOrigin = keyboardClip.match(/at ([\d.]+)px ([\d.]+)px/);
  const keyboardOriginOK = keyboardOrigin &&
    Math.abs(Number(keyboardOrigin[1]) - (themeBox.x + themeBox.width / 2)) < 2 &&
    Math.abs(Number(keyboardOrigin[2]) - (themeBox.y + themeBox.height / 2)) < 2;
  const keyboardPseudo = await page.evaluate(() => window.__dsThemeAnimation.options.pseudoElement);
  check('键盘切换使用按钮中心', keyboardOriginOK && keyboardPseudo === '::view-transition-new(root)', keyboardClip);

  await reopenThemeMenu();
  await page.click('#menuPop [data-act="settings"]');
  await page.waitForSelector('#settingsMask:not([hidden])');
  const docsDirVal = await page.$eval('#multiDirs', (el) => el.textContent);
  check('设置面板回填配置', docsDirVal.includes('DocShare/docs'), docsDirVal.slice(0, 40));
  // 访问地址展示 + 复制
  const lanUrl = await page.$eval('#lanUrlText', (el) => el.textContent);
  check('访问地址展示', lanUrl === 'http://192.168.1.5:8080', lanUrl);
  await page.click('#copyUrlBtn');
  await page.waitForFunction(() => document.getElementById('toast').textContent.includes('已复制'), { timeout: 6000 });
  const copyTip = await page.$eval('#toast', (el) => el.textContent);
  check('复制访问地址', copyTip.includes('已复制'), copyTip);
  // 密码输入框: 样式 + 显示/隐藏切换
  const pwdStyled = await page.$eval('#setPassword', (el) => {
    const cs = getComputedStyle(el);
    return cs.borderRadius !== '0px' && cs.padding !== '0px';
  });
  check('密码框样式统一', pwdStyled, '');
  await page.click('#pwdToggle');
  const pwdText = await page.$eval('#setPassword', (el) => el.type === 'text');
  check('密码显示切换', pwdText, '');
  await page.click('#pwdToggle');
  const pwdHidden = await page.$eval('#setPassword', (el) => el.type === 'password');
  check('密码隐藏切换', pwdHidden, '');
  // 多目录: 添加 + 删除
  await page.$eval('#setDocsDir', (el) => { el.value = 'D:/second-docs'; });
  await page.click('#addDirBtn');
  const twoDirs = await page.$$eval('#multiDirs .multi-dir-item', (els) => els.length);
  check('多目录添加', twoDirs === 2, `count=${twoDirs}`);
  await page.click('#multiDirs .multi-dir-del:last-child');
  const oneDir = await page.$$eval('#multiDirs .multi-dir-item', (els) => els.length);
  check('多目录删除', oneDir === 1, `count=${oneDir}`);
  // 检查更新(发现新版本 → 下载更新按钮)
  const curVer = await page.$eval('#updateStatus', (el) => el.textContent);
  check('版本号动态显示', curVer.includes('1.1.1'), curVer.slice(0, 30));
  await page.click('#checkUpdateBtn');
  await page.waitForFunction(() => document.querySelector('#updateStatus').textContent.includes('发现新版本'), { timeout: 6000 });
  check('检查更新(发现新版本)', true, '');
  // 一键下载更新 → 自定义弹窗展示版本变化与更新说明(不再使用原生 confirm)
  const applyBefore = await page.evaluate(() => window.__dsApplyCalls || 0);
  await page.click('#dlUpdateBtn');
  await page.waitForFunction(() => !document.querySelector('#updateMask').hidden, { timeout: 6000 });
  const verInfo = await page.$eval('#updateVersionInfo', (el) => el.textContent);
  check('更新弹窗版本信息', verInfo.includes('1.0.0') && verInfo.includes('1.1.0'), verInfo);
  const notesText = await page.$eval('#updateNotes', (el) => el.textContent);
  check('更新弹窗显示更新说明', notesText.includes('更新内容') && notesText.includes('全文搜索'), notesText.slice(0, 40));
  const dlPath = await page.$eval('#updateStatus', (el) => el.textContent);
  check('一键下载更新', dlPath.includes('DocShare-Setup-1.1.0.exe'), dlPath.slice(0, 40));
  check('无原生确认对话框', nativeDialogs === 0, `dialogs=${nativeDialogs}`);
  // 稍后再说: 关闭弹窗且不触发安装
  await page.click('#updateLaterBtn');
  const maskHidden = await page.$eval('#updateMask', (el) => el.hidden);
  check('稍后再说关闭弹窗', maskHidden, '');
  const applyAfterLater = await page.evaluate(() => window.__dsApplyCalls || 0);
  check('稍后不触发安装', applyAfterLater === applyBefore, '');
  // 再次检查并下载 → 退出并安装
  await page.click('#checkUpdateBtn');
  await page.waitForFunction(() => document.querySelector('#dlUpdateBtn'), { timeout: 6000 });
  await page.click('#dlUpdateBtn');
  await page.waitForFunction(() => !document.querySelector('#updateMask').hidden, { timeout: 6000 });
  await page.click('#updateInstallBtn');
  await page.waitForFunction((n) => (window.__dsApplyCalls || 0) > n, {}, applyBefore);
  check('退出并安装触发 ApplyUpdate', true, '');
  const maskHidden2 = await page.$eval('#updateMask', (el) => el.hidden);
  check('安装后弹窗关闭', maskHidden2, '');
  await page.click('#pickDirBtn');
  await page.waitForSelector('#dirMask:not([hidden])');
  await page.waitForSelector('.dir-item', { timeout: 4000 });
  await page.click('.dir-item'); // 进入 C:\
  await page.waitForFunction(() => document.querySelector('#dirPath').textContent.includes('C:'), { timeout: 4000 });
  await page.click('#dirChooseBtn');
  const picked = await page.$eval('#setDocsDir', (el) => el.value);
  check('目录选择器选择', picked.includes('C:'), picked);
  await page.click('#saveSettingsBtn');
  await page.waitForFunction(() => document.querySelector('#settingsStatus').textContent.includes('已保存'), { timeout: 6000 });
  check('保存配置', true, '');
  // 保存成功后会 location.reload(), 等待页面重载完成(状态文本被清空即新页面)
  await page.waitForFunction(() => document.querySelector('#settingsStatus').textContent === '', { timeout: 6000 });
  await new Promise((r) => setTimeout(r, 400));
  // 访问记录(通过 popover)
  await page.click('#menuBtn');
  await page.click('#menuPop [data-act="access"]');
  await page.waitForSelector('#accessMask:not([hidden])');
  await page.waitForFunction(() => document.querySelectorAll('.access-row').length > 0, { timeout: 6000 });
  const accessDoc = await page.$eval('.access-row .access-doc', (el) => el.textContent);
  const accessIp = await page.$eval('.access-row .access-ip', (el) => el.textContent);
  check('访问记录列表', accessDoc.includes('README.md') && accessIp.includes('192.168.1.5'), `${accessDoc} @ ${accessIp}`);
  await page.click('#accessMask [data-close]');

  // ---- 6.5 启动自动打开上次阅读的文档 ----
  await page.waitForFunction(() => [...document.querySelectorAll('.tree-row')].some((r) => r.textContent.includes('README.md')), { timeout: 8000 });
  await page.$$eval('.tree-row', (rows) => {
    rows.find((r) => r.textContent.includes('README.md')).click();
  });
  await page.waitForFunction(() => document.querySelector('#docView .md-body') && document.querySelector('#docView .md-body').textContent.includes('DocShare'), { timeout: 6000 });
  await page.reload({ waitUntil: 'networkidle0' });
  await page.waitForFunction(() => document.querySelector('#docView .md-body') && document.querySelector('#docView .md-body').textContent.includes('DocShare'), { timeout: 8000 });
  check('启动自动打开上次文档', true, '');

  // ---- 7. 文档批注: 选中创建 / 行内高亮 / 回复 / 删除 ----
  // 选中正文中的 "DocShare" 文字(触发选区浮动按钮)
  await page.evaluate(() => {
    const body = document.querySelector('#docView .md-body');
    const walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT);
    const nodes = [];
    while (walker.nextNode()) nodes.push(walker.currentNode);
    const target = nodes.find((n) => n.textContent.includes('DocShare') && !n.parentNode.closest('code, pre'));
    if (!target) throw new Error('未找到可选的正文文本');
    const idx = target.textContent.indexOf('DocShare');
    const range = document.createRange();
    range.setStart(target, idx);
    range.setEnd(target, idx + 'DocShare'.length);
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
    document.querySelector('#docView').dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
  });
  await page.waitForSelector('.anno-fab:not([hidden])', { timeout: 3000 });
  check('选中文字出现批注按钮', true, '');
  // 点击浮动按钮 → 创建弹窗(引文预览)
  await page.click('.anno-fab');
  await page.waitForSelector('#annoMask:not([hidden])');
  const quotePreview = await page.$eval('#annoQuotePreview', (el) => el.textContent);
  check('批注弹窗引文预览', quotePreview.includes('DocShare'), quotePreview);
  // 填写内容但先不填名字 → 提交被拒绝(名字必填)
  await page.type('#annoContent', '此处建议补充说明');
  await page.$eval('#annoSubmit', (el) => el.click());
  await page.waitForFunction(() => !document.querySelector('#annoMask').hidden, { timeout: 6000 });
  const nameToast = await page.$eval('#toast', (el) => el.textContent);
  check('无名字提交被拒', nameToast.includes('请输入你的名字'), nameToast);
  // 填写名字 → 提交成功
  await page.type('#annoAuthor', 'tester');
  await page.$eval('#annoSubmit', (el) => el.click());
  await page.waitForSelector('#docView mark.anno-mark', { timeout: 6000 });
  const markText = await page.$eval('#docView mark.anno-mark', (el) => el.textContent);
  check('批注提交后正文高亮', markText.includes('DocShare'), markText);
  // 顶栏批注按钮与计数
  await page.waitForFunction(() => !document.querySelector('#annoBtn').hidden && document.querySelector('#annoCount').textContent === '1', { timeout: 6000 });
  check('批注计数徽标', true, '');
  // 点击高亮 → 打开该批注的详情弹窗(先关闭提交后自动打开的弹窗)
  await page.click('#annoViewMask [data-close]');
  await page.waitForFunction(() => document.querySelector('#annoViewMask').hidden, { timeout: 6000 });
  await page.click('#docView mark.anno-mark');
  await page.waitForSelector('#annoViewMask:not([hidden])');
  const cardText = await page.$eval('#annoViewBody .anno-card', (el) => el.textContent);
  check('点击高亮打开批注弹窗', cardText.includes('此处建议补充说明'), '');
  // 已记忆名字: 回复无需重复输入名字
  const replyHasName = await page.evaluate(() => !!document.querySelector('#annoViewBody .anno-reply-name'));
  check('回复无需重复输入名字', !replyHasName, '');
  // 回复
  await page.type('#annoViewBody .anno-reply-input', '已补充完毕');
  await page.$eval('#annoViewBody .anno-reply-form', (el) => el.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  await page.waitForFunction(() => document.querySelectorAll('#annoViewBody .anno-reply').length === 1, { timeout: 6000 });
  const replyText = await page.$eval('#annoViewBody .anno-reply-text', (el) => el.textContent);
  check('批注回复', replyText.includes('已补充完毕'), replyText);
  // 解决批注: 徽标 + 计数清零 + 行内锚点弱化
  await page.click('#annoViewResolve');
  await page.waitForFunction(() => !!document.querySelector('#annoViewBody .anno-resolved-badge'), { timeout: 6000 });
  const badge = await page.$eval('#annoViewBody .anno-resolved-badge', (el) => el.textContent);
  check('批注标记解决', badge.includes('已解决'), badge);
  await page.waitForFunction(() => document.querySelector('#annoCount').textContent === '', { timeout: 6000 });
  check('解决后计数清零', true, '');
  await page.waitForFunction(() => !!document.querySelector('#docView mark.anno-mark.resolved'), { timeout: 6000 });
  check('解决后锚点弱化', true, '');
  // 重新打开
  await page.click('#annoViewResolve');
  await page.waitForFunction(() => !document.querySelector('#annoViewBody .anno-resolved-badge'), { timeout: 6000 });
  check('批注重新打开', true, '');
  // 顶栏按钮 → 批注列表弹窗(先关闭详情弹窗)
  await page.click('#annoViewMask [data-close]');
  await page.waitForFunction(() => document.querySelector('#annoViewMask').hidden, { timeout: 6000 });
  await page.click('#annoBtn');
  await page.waitForSelector('#annoListMask:not([hidden])');
  const listItem = await page.$eval('#annoList .anno-list-item', (el) => el.textContent);
  check('批注列表弹窗', listItem.includes('此处建议补充说明'), '');
  // 列表项点击 → 定位并打开详情弹窗
  await page.click('#annoList .anno-list-item');
  await page.waitForSelector('#annoViewMask:not([hidden])');
  check('列表点击打开详情', true, '');
  // 删除(mock confirm 放行)
  await page.evaluate(() => { window.confirm = () => true; });
  await page.click('#annoViewDelete');
  await page.waitForFunction(() => document.querySelector('#annoViewMask').hidden, { timeout: 6000 });
  await page.waitForFunction(() => document.querySelectorAll('#docView mark.anno-mark').length === 0, { timeout: 6000 });
  check('批注删除', true, '');

  // ---- 8. 访问密码登录流程(内嵌启动密码服务, 随机端口避免残留冲突) ----
  const authPort = 18100 + Math.floor(Math.random() * 400);
  const authServer = spawn(SERVER, [
    '-dir', path.join(__dirname, '..', 'docs'),
    '-addr', '127.0.0.1:' + authPort,
    '-data', path.join(os.tmpdir(), 'dshtest-auth-' + Date.now()),
    '-password', 'test-pass',
    '-lockout', '5', // 5 秒锁定(CI 上失败循环较慢, 需留足余量)
  ], { stdio: 'ignore', windowsHide: true });
  await new Promise((r) => setTimeout(r, 1500));
  const page2 = await browser.newPage();
  await page2.setViewport({ width: 1440, height: 900 });
  await page2.goto('http://127.0.0.1:' + authPort + '/', { waitUntil: 'networkidle0' });
  const maskVisible = await page2.$eval('#loginMask', (el) => !el.hidden);
  check('访问密码遮罩显示', maskVisible, '');
  // 错误密码
  await page2.type('#loginPassword', 'wrong-pass');
  await page2.$eval('#loginBtn', (el) => el.click());
  await page2.waitForFunction(() => !document.querySelector('#loginError').hidden, { timeout: 6000 });
  check('错误密码拒绝', true, '');
  // 连续失败触发锁定: 通过 API 直调快速累计失败次数(避免 UI 输入延迟
  // 导致 5 秒锁定期在循环期间过期, 造成偶发超时); 再用 UI 验证锁定效果
  const authBase = 'http://127.0.0.1:' + authPort;
  for (let i = 0; i < 4; i++) {
    await page2.evaluate(async (base) => {
      await fetch(base + '/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: 'wrong-pass' }),
      });
    }, authBase);
  }
  // UI 输入正确密码 → 锁定期间即使密码正确也被拒
  await page2.$eval('#loginPassword', (el) => { el.value = ''; });
  await page2.type('#loginPassword', 'test-pass');
  await page2.$eval('#loginBtn', (el) => el.click());
  await page2.waitForFunction(() => document.querySelector('#loginError').textContent.includes('再试'), { timeout: 6000 });
  check('连续失败锁定(正确密码也被拒)', true, '');
  // 锁定期过后恢复
  await new Promise((r) => setTimeout(r, 5600));
  await page2.$eval('#loginPassword', (el) => { el.value = ''; });
  await page2.type('#loginPassword', 'test-pass');
  await page2.$eval('#loginBtn', (el) => el.click());
  await page2.waitForFunction(() => document.querySelector('#tree').textContent.includes('README.md'), { timeout: 10000 });
  check('锁定期后正确密码进入', true, '');
  // 刷新免登录(token 记忆)
  await page2.reload({ waitUntil: 'networkidle0' });
  await page2.waitForFunction(() => document.querySelector('#tree').textContent.includes('README.md'), { timeout: 10000 });
  const noMask = await page2.$eval('#loginMask', (el) => el.hidden);
  check('刷新免登录', noMask, '');
  await page2.close();
  authServer.kill();

  await browser.close();
  const failed = results.filter((r) => !r.ok);
  console.log(`\n=== ${results.length - failed.length}/${results.length} passed ===`);
  process.exit(failed.length ? 1 : 0);
})().catch((e) => { console.error('TEST CRASH:', e); process.exit(1); });
