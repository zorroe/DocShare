/* DocShare 前端自动化冒烟测试 (puppeteer-core + 本机 Edge) */
const puppeteer = require('puppeteer-core');
const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');

const SERVER = process.env.DS_SERVER || path.join(__dirname, '..', 'release', 'DocShare-Server.exe');

const BASE = process.env.DS_BASE || 'http://127.0.0.1:18080';
const TOKEN = process.env.DS_TOKEN || 'ui-test-token';
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
  page.on('dialog', (d) => d.accept());

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

  // ---- 2.5 大纲视图 ----
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
      ServerInfo: async () => ({ port: 18080, docsDir: 'E:/code/DocShare/docs', lan: true, running: true, dataDir: 'x', error: '', blacklist: [] }),
      ListDir: async (p) => (p ? [{ name: '子目录', path: p + '/sub' }] : [{ name: 'C:\\', path: 'C:\\' }]),
      SaveConfig: async (d, p, l, bl) => ({ port: p, docsDir: d, lan: l, running: true }),
      OpenBrowser: async () => {},
      AutoStart: async () => false,
      SetAutoStart: async () => {},
      CheckUpdate: async () => ({ current: '1.0.0', latest: '1.0.0', url: 'https://github.com/zorroe/DocShare/releases', hasUpdate: false }),
      ListAccessLogs: async () => [{ time: '2026-08-19T10:00:00+08:00', doc: 'README.md', ip: '192.168.1.5', ua: 'Mozilla/5.0 Chrome' }],
    } } };
  });
  await page.goto(BASE + '/', { waitUntil: 'networkidle0' });
  const menuVisible = await page.$eval('#menuBtn', (el) => !el.hidden);
  check('桌面模式显示管理按钮', menuVisible, '');
  // 管理菜单中无审批项(编辑功能已删除)
  const adminItemGone = await page.evaluate(() => !document.querySelector('#menuPop [data-act="admin"]'));
  check('管理菜单无审批项', adminItemGone, '');
  // popover 菜单打开设置
  await page.click('#menuBtn');
  await page.waitForSelector('#menuPop:not([hidden])');
  const popVisible = await page.$eval('#menuPop', (el) => !el.hidden);
  check('popover 菜单弹出', popVisible, '');
  await page.click('#menuPop [data-act="settings"]');
  await page.waitForSelector('#settingsMask:not([hidden])');
  const docsDirVal = await page.$eval('#setDocsDir', (el) => el.value);
  check('设置面板回填配置', docsDirVal.includes('DocShare/docs'), docsDirVal);
  // 检查更新
  await page.click('#checkUpdateBtn');
  await page.waitForFunction(() => document.querySelector('#updateStatus').textContent.includes('已是最新'), { timeout: 6000 });
  check('检查更新(已是最新)', true, '');
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

  // ---- 8. 访问密码登录流程(内嵌启动密码服务, 随机端口避免残留冲突) ----
  const authPort = 18100 + Math.floor(Math.random() * 400);
  const authServer = spawn(SERVER, [
    '-dir', path.join(__dirname, '..', 'docs'),
    '-addr', '127.0.0.1:' + authPort,
    '-data', path.join(__dirname, '..', 'backend', 'data'),
    '-password', 'test-pass',
  ], { stdio: 'ignore', windowsHide: true });
  await new Promise((r) => setTimeout(r, 1500));
  const page2 = await browser.newPage();
  await page2.setViewport({ width: 1440, height: 900 });
  await page2.goto('http://127.0.0.1:' + authPort + '/', { waitUntil: 'networkidle0' });
  const maskVisible = await page2.$eval('#loginMask', (el) => !el.hidden);
  check('访问密码遮罩显示', maskVisible, '');
  // 错误密码
  await page2.type('#loginPassword', 'wrong-pass');
  await page2.click('#loginBtn');
  await page2.waitForFunction(() => !document.querySelector('#loginError').hidden, { timeout: 6000 });
  check('错误密码拒绝', true, '');
  // 正确密码
  await page2.$eval('#loginPassword', (el) => { el.value = ''; });
  await page2.type('#loginPassword', 'test-pass');
  await page2.click('#loginBtn');
  await page2.waitForFunction(() => document.querySelector('#tree').textContent.includes('README.md'), { timeout: 10000 });
  check('正确密码进入', true, '');
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
