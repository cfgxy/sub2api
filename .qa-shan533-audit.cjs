const { chromium } = require('/home/guxy/qa-ruyi268/node_modules/playwright');
const fs = require('fs');
const path = require('path');

const ORIGIN = 'https://qiye.v2.huayuebridge.com';
const CANDIDATE_SHA = '3cf9d8c2e76d7df2825c692547475c86ebfbf4c1';
const OUT = path.join(process.cwd(), 'qa-attachments');
fs.mkdirSync(OUT, { recursive: true });
const EMAIL = 'qa411.guide@example.com';
const PASSWORD = fs.readFileSync('/home/guxy/qa411/.pw', 'utf8').trim();
const report = {
  candidate_sha: CANDIDATE_SHA,
  origin: ORIGIN,
  route: '/enterprise/admin/audit',
  steps: [],
  screenshots: [],
  states: {},
  network: [],
  console: [],
  uncovered: [],
};

function redact(value) {
  return String(value)
    .replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, '[REDACTED_EMAIL]')
    .replace(/Bearer\s+[A-Za-z0-9._-]+/gi, 'Bearer [REDACTED]')
    .replace(/sk-[A-Za-z0-9_-]+/g, 'sk-[REDACTED]')
    .replace(/([?&](?:token|code|password|authorization|cookie|email|phone)=)[^&]*/gi, '$1[REDACTED]')
    .replace(/\b\d{6}\b/g, '[REDACTED_CODE]');
}
function safeUrl(url) {
  try {
    const u = new URL(url);
    for (const key of ['token', 'code', 'password', 'authorization', 'cookie', 'email', 'phone']) {
      if (u.searchParams.has(key)) u.searchParams.set(key, '[REDACTED]');
    }
    return u.toString();
  } catch {
    return redact(url);
  }
}
function step(name, pass, detail = '') {
  report.steps.push({ name, pass: Boolean(pass), detail: redact(detail).slice(0, 600) });
  console.log(`${pass ? 'PASS' : 'FAIL'} ${name}${detail ? ` :: ${redact(detail).slice(0, 240)}` : ''}`);
}

(async () => {
  const profileDir = path.join(OUT, '.browser-profile');
  fs.rmSync(profileDir, { recursive: true, force: true });
  const context = await chromium.launchPersistentContext(profileDir, {
    headless: true,
    viewport: { width: 1440, height: 1000 },
    locale: 'zh-CN',
    serviceWorkers: 'block',
  });
  const page = context.pages()[0] || await context.newPage();
  page.setDefaultTimeout(18000);
  page.on('console', msg => report.console.push({ type: msg.type(), text: redact(msg.text()).slice(0, 500) }));
  page.on('pageerror', err => report.console.push({ type: 'pageerror', text: redact(err.message).slice(0, 500) }));
  page.on('response', response => {
    const url = response.url();
    if (url.includes('/api/')) {
      report.network.push({ method: response.request().method(), status: response.status(), url: safeUrl(url) });
    }
  });

  async function state(label) {
    const value = await page.evaluate(() => {
      const visible = el => {
        const s = getComputedStyle(el);
        const r = el.getBoundingClientRect();
        return s.display !== 'none' && s.visibility !== 'hidden' && r.width > 0 && r.height > 0;
      };
      const inputs = [...document.querySelectorAll('input, select, textarea')].filter(visible).map(el => ({
        tag: el.tagName.toLowerCase(), type: el.getAttribute('type'), aria: el.getAttribute('aria-label'),
        placeholder: el.getAttribute('placeholder'), name: el.getAttribute('name'), value: el.value || '',
      }));
      const buttons = [...document.querySelectorAll('button')].filter(visible).map(el => ({
        text: (el.innerText || '').trim().slice(0, 120), aria: el.getAttribute('aria-label'), title: el.getAttribute('title'),
      }));
      const rows = [...document.querySelectorAll('table tbody tr')].filter(visible).map(row => ({
        text: (row.innerText || '').trim().slice(0, 800),
        event_title: row.querySelector('[title]')?.getAttribute('title') || '',
      }));
      const body = (document.body?.innerText || '').slice(0, 6000);
      return {
        url: location.href,
        title: document.title,
        body,
        inputs,
        buttons,
        table_count: document.querySelectorAll('table').length,
        rows,
        empty_text: [...document.querySelectorAll('[role="status"]')].map(el => (el.innerText || '').trim()).filter(Boolean),
        html_lang: document.documentElement.lang,
      };
    });
    value.body = redact(value.body);
    value.inputs = value.inputs.map(item => ({ ...item, value: redact(item.value) }));
    value.rows = value.rows.map(item => ({ ...item, text: redact(item.text) }));
    report.states[label] = value;
    return value;
  }
  async function shot(label) {
    const file = path.join(OUT, `shan-533-${label}.png`);
    await page.screenshot({ path: file, fullPage: true });
    report.screenshots.push(file);
    return file;
  }
  async function waitForAudit() {
    await page.waitForTimeout(2500);
    if (!/\/enterprise\/admin\/audit/.test(page.url())) {
      const diagnostic = await page.evaluate(() => ({ url: location.href, title: document.title, body: (document.body?.innerText || '').slice(0, 3000) }));
      throw new Error(`管理审计路由未到达：${JSON.stringify(diagnostic)}`);
    }
    await page.getByText(/管理审计|Audit/i).first().waitFor({ state: 'visible' });
    await page.waitForTimeout(1200);
  }
  async function clickQuery() {
    await page.getByRole('button', { name: /查询/ }).click();
    await page.waitForTimeout(1200);
  }
  async function clearTextFilters() {
    for (const label of ['搜索审计记录', '操作者', '动作类型', '对象类型', '原因']) {
      const loc = page.locator(`input[aria-label="${label}"]`);
      if (await loc.count()) await loc.fill('');
    }
    for (const label of ['开始时间', '结束时间']) {
      const loc = page.locator(`input[aria-label="${label}"]`);
      if (await loc.count()) await loc.fill('');
    }
    const select = page.locator('select[aria-label="结果"]');
    if (await select.count()) await select.selectOption('');
  }
  async function filterInput(label, value, screenshotLabel) {
    const loc = page.locator(`input[aria-label="${label}"]`);
    const exists = await loc.count() > 0;
    step(`页面筛选控件：${label}`, exists, exists ? '' : '未找到控件');
    if (!exists) return null;
    await loc.fill(value);
    await clickQuery();
    const current = await state(screenshotLabel);
    await shot(screenshotLabel);
    return current;
  }

  try {
    await page.goto(`${ORIGIN}/enterprise/login`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await page.locator('#enterprise-email').fill(EMAIL);
    await page.locator('#enterprise-password').fill(PASSWORD);
    await page.getByRole('button', { name: /登录|Sign in|Login/i }).click();
    await page.waitForURL(/\/enterprise\/(home|keys|admin)/, { timeout: 25000 });
    step('企业管理员正常登录', true, page.url());
    await page.goto(`${ORIGIN}/enterprise/admin/audit`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await waitForAudit();
    const defaultState = await state('default_zh');
    await shot('audit-default-zh');
    step('登录后进入管理审计页面', /\/enterprise\/admin\/audit/.test(defaultState.url) && defaultState.table_count > 0, defaultState.url);
    step('默认列表真实渲染', defaultState.rows.length > 0, `rows=${defaultState.rows.length}`);
    const defaultEvents = defaultState.rows.map(row => row.event_title).filter(Boolean);
    step('默认列表隐藏安全自助事件（当前页行）', !defaultEvents.some(e => /employee\.password_changed|password\.reset_requested|session\./.test(e)), defaultEvents.join(','));
    step('默认列表保留企业管理成功变更（当前页行）', defaultEvents.some(e => /employee\.|department\.|enterprise\.|subscription\.|key\.|allocation\.|brand\./.test(e)), defaultEvents.join(','));

    await filterInput('动作类型', 'employee.password_changed', 'event_password_changed');
    const changedState = report.states.event_password_changed;
    step('显式事件筛选读回安全自助记录', (changedState?.rows.length || 0) > 0 || /暂无符合|total|password/i.test(changedState?.body || ''), `rows=${changedState?.rows.length || 0}`);

    await clearTextFilters();
    await filterInput('动作类型', 'employee.password_reset_by_admin', 'event_admin_password_reset');
    const adminResetState = report.states.event_admin_password_reset;
    step('管理员重置密码事件可显式查询', (adminResetState?.rows.length || 0) > 0 || /暂无符合/i.test(adminResetState?.body || ''), `rows=${adminResetState?.rows.length || 0}`);

    await clearTextFilters();
    await filterInput('对象类型', 'employee', 'entity_employee');
    await clearTextFilters();
    await filterInput('操作者', 'admin', 'actor_admin');
    await clearTextFilters();
    await filterInput('原因', 'password', 'reason_password');
    await clearTextFilters();
    await filterInput('搜索审计记录', 'password', 'search_password');

    await clearTextFilters();
    const resultSelect = page.locator('select[aria-label="结果"]');
    const resultControl = await resultSelect.count() > 0;
    step('页面存在结果筛选控件', resultControl, resultControl ? 'native select' : 'custom select or missing');
    if (resultControl) {
      await resultSelect.selectOption('failure');
      await clickQuery();
      await state('result_failure');
      await shot('result-failure');
      await clearTextFilters();
      await resultSelect.selectOption('rejected');
      await clickQuery();
      await state('result_rejected');
      await shot('result-rejected');
    } else {
      const resultTrigger = page.locator('[aria-label="结果"]');
      if (await resultTrigger.count()) {
        await resultTrigger.click();
        const failureOption = page.getByText('失败', { exact: true });
        if (await failureOption.count()) {
          await failureOption.click();
          await clickQuery();
          await state('result_failure');
          await shot('result-failure');
        }
        await clearTextFilters();
      }
    }

    await clearTextFilters();
    const start = page.locator('input[aria-label="开始时间"]');
    const end = page.locator('input[aria-label="结束时间"]');
    const timeControls = await start.count() > 0 && await end.count() > 0;
    step('页面存在时间范围筛选控件', timeControls, timeControls ? '' : '开始时间/结束时间控件缺失');
    if (timeControls) {
      const now = new Date();
      const prior = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000);
      const local = d => new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
      await start.fill(local(prior));
      await end.fill(local(now));
      await clickQuery();
      await state('time_30d');
      await shot('time-30d');
    }

    const controls = report.states.default_zh.inputs.map(i => `${i.aria || i.name || i.placeholder || i.tag}:${i.type || ''}`);
    report.states.default_zh.control_summary = controls;
    const missingStructured = ['员工', '部门', 'API Key'].filter(token => !controls.some(item => item.includes(token)));
    report.states.default_zh.missing_structured_filters = missingStructured;
    for (const token of ['员工', '部门', 'API Key']) {
      step(`页面存在${token}筛选控件`, !missingStructured.includes(token), missingStructured.includes(token) ? '未发现 aria-label、name 或 placeholder' : 'found');
    }

    await clearTextFilters();
    await filterInput('动作类型', 'session.revoke', 'event_session_revoke');
    await clearTextFilters();
    await filterInput('搜索审计记录', 'concurrent_conflict', 'search_concurrent_conflict');

    const originalLocale = await page.evaluate(() => localStorage.getItem('sub2api_locale'));
    await page.evaluate(() => localStorage.setItem('sub2api_locale', 'en'));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await waitForAudit();
    const enState = await state('default_en');
    await shot('audit-default-en');
    step('英文管理审计文案实际渲染', /Audit|Management audit|Action|Object|Result|Reason/i.test(enState.body) && !/未知事件|未知对象/.test(enState.body), enState.body.slice(0, 800));
    if (originalLocale === null) await page.evaluate(() => localStorage.removeItem('sub2api_locale'));
    else await page.evaluate(locale => localStorage.setItem('sub2api_locale', locale), originalLocale);

    report.network = report.network.slice(-300).map(item => ({ ...item, url: safeUrl(item.url) }));
    report.console = report.console.slice(-50);
  } catch (error) {
    step('浏览器 E2E 执行完成', false, error.stack || error.message);
    report.failure = redact(error.stack || error.message);
  } finally {
    fs.writeFileSync(path.join(OUT, 'shan-533-audit-e2e.json'), JSON.stringify(report, null, 2));
    await context.close();
  }
})().catch(error => {
  console.error(redact(error.stack || error.message));
  process.exitCode = 1;
});
