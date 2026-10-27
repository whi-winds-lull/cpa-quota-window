'use strict';
const $ = (id) => document.getElementById(id);
const RES_BASE = CPAPluginSession.resourcePrefix(window) + '/v0/resource/plugins/window-anchor';
const MGMT_ROOT = CPAPluginSession.resourcePrefix(window) + '/v0/management';
const API = MGMT_ROOT + '/plugins/window-anchor';

let managementKey = '';
let connected = false;
let STATUS = null;
let ACCOUNTS = [];
let catalog = new Map();
let dirty = false;
let lang = 'zh-CN';
let provider = 'codex';
let saving = false;
let pendingDelete = null;
let historyView = new CPAHistory.View();
let historySelection = new Set();

const t = (key, values = {}) => {
  let text = translations[key]?.[lang === 'en' ? 1 : 0] || key;
  for (const [k, v] of Object.entries(values)) text = text.replaceAll('{' + k + '}', v);
  return text;
};
const backend = (text) => (lang === 'en' ? String(text) : String(text));
const node = (tag, text, cls) => {
  const el = document.createElement(tag);
  if (text !== undefined) el.textContent = text;
  if (cls) el.className = cls;
  return el;
};
function feedback(text, error = false) {
  $('feedback').textContent = text;
  $('feedback').classList.toggle('error', error);
  clearTimeout(feedback.timer);
  if (text) feedback.timer = setTimeout(() => { $('feedback').textContent = ''; }, 6000);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    cache: 'no-store',
    credentials: 'same-origin',
    headers: { Authorization: `Bearer ${managementKey}`, 'Content-Type': 'application/json', ...(options.headers || {}) },
  });
  let data;
  try { data = await response.json(); } catch { throw new Error('响应不是有效 JSON'); }
  if (!response.ok) {
    if (response.status === 401 || response.status === 403) throw new Error('登录态已失效');
    throw new Error(typeof data.error === 'string' ? data.error : data.message || `HTTP ${response.status}`);
  }
  return data;
}

// ---------- 数据 ----------

async function readStatus() {
  if (managementKey) {
    try { return await api(API + '/status'); } catch (e) { /* 降级到免鉴权资源 */ }
  }
  const res = await fetch(RES_BASE + '/status.json', { cache: 'no-store' });
  if (!res.ok) throw new Error('读不到 status.json，插件可能未正确加载');
  return res.json();
}

async function readAccounts() {
  if (managementKey) {
    try {
      const data = await api(API + '/accounts');
      if (Array.isArray(data.accounts) && data.accounts.length) return data.accounts;
    } catch (e) { /* 降级 */ }
  }
  const res = await fetch(RES_BASE + '/accounts.json', { cache: 'no-store' });
  if (!res.ok) return [];
  const data = await res.json().catch(() => ({}));
  return Array.isArray(data.accounts) ? data.accounts : [];
}

// 账号列表本身不带套餐信息，补一次 auth-files 才能分组显示 Plus / Pro
async function enrichAccounts() {
  if (!managementKey || !ACCOUNTS.length) return;
  try {
    const data = await api(`${MGMT_ROOT}/auth-files`);
    const files = Array.isArray(data.files) ? data.files : [];
    ACCOUNTS = ACCOUNTS.map((acc) => {
      const meta = files.find((f) => f.id === acc.id || (acc.name && f.name === acc.name));
      if (!meta) return acc;
      return {
        ...acc,
        plan_type: acc.plan_type || meta.plan_type || '',
      };
    });
  } catch (e) { /* 只读模式下拿不到，归入未知套餐即可 */ }
}

async function loadCatalog() {
  if (!managementKey) return;
  const targets = ACCOUNTS.slice(0, 12);
  await Promise.all(targets.map(async (acc) => {
    try {
      const name = acc.name || acc.id;
      const data = await api(`${MGMT_ROOT}/auth-files/models?name=${encodeURIComponent(name)}`);
      catalog.set(acc.id, Array.isArray(data.models) ? data.models.map((m) => m.id).filter(Boolean) : []);
    } catch {
      catalog.set(acc.id, null);
    }
  }));
}

// 把配置里的计划和 CPA 现有账号合并：没配置过的账号自动补一行，用户只填时间
function providerOf(acc) {
  const p = String(acc.provider || acc.type || '').toLowerCase();
  return (p === 'antigravity' || p === 'gemini') ? 'antigravity' : 'codex';
}

function buildPlans() {
  const all = (STATUS && STATUS.plans) || [];
  const configured = all.filter((p) => (p.provider || 'codex') === provider);
  const out = configured.map((p) => ({
    auth_id: p.auth_id,
    label: p.label || p.auth_id,
    model: p.model || '',
    times: [...(p.times || [])],
    enabled: p.enabled !== false,
    next_trigger: p.next_trigger || null,
    missing: !!p.missing,
    disabled: !!p.disabled,
    pending: false,
  }));
  const seen = new Set(out.map((p) => p.auth_id));
  const fallbackModel = (out.find((p) => p.model) || {}).model || '';
  for (const acc of ACCOUNTS) {
    if (providerOf(acc) !== provider) continue;
    if (seen.has(acc.id)) continue;
    out.push({
      auth_id: acc.id,
      label: acc.label || acc.email || acc.name || acc.id,
      model: fallbackModel,
      times: [],
      enabled: false,
      next_trigger: null,
      missing: false,
      disabled: !!(acc.disabled || acc.status === 'disabled'),
      pending: true,
    });
  }
  return out;
}

// ---------- 渲染 ----------

function addTimeEntry(container, value = '') {
  const wrap = node('div', undefined, 'timeEntry');
  const input = node('input');
  input.type = 'time';
  input.step = '60';
  input.value = value || '';
  input.addEventListener('input', markDirty);
  const remove = node('button', '×', 'remove');
  remove.type = 'button';
  remove.title = '删除这个时间';
  remove.addEventListener('click', () => { wrap.remove(); markDirty(); });
  wrap.append(input, remove);
  container.append(wrap);
}

function markDirty() {
  dirty = true;
  if ($('dirtyHint')) $('dirtyHint').textContent = t('dirty') === 'dirty' ? '未保存' : t('dirty');
}

function renderAccounts() {
  const list = $('accountList');
  list.replaceChildren();
  const plans = buildPlans();
  if (!plans.length) {
    list.append(node('p', 'CPA 里还没有 Codex 账号。', 'empty'));
    return;
  }

  // 按套餐分组（取自 CPA 元数据，缺失则归入「未知」）
  const groups = new Map();
  for (const plan of plans) {
    const acc = ACCOUNTS.find((a) => a.id === plan.auth_id);
    const group = acc ? CPAWindowUI.planOf(acc) : 'unknown';
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push(plan);
  }

  for (const [group, items] of groups) {
    const heading = node('div', undefined, 'accountGroup');
    const title = group === 'unknown' ? '未知套餐' : group;
    heading.append(node('strong', `${title} (${items.length})`));
    list.append(heading);

    for (const plan of items) {
      const card = node('div', undefined, 'accountCard');
      card.dataset.auth = plan.auth_id;

      const on = node('input');
      on.type = 'checkbox';
      on.className = 'acctOn';
      on.checked = plan.enabled;
      on.addEventListener('change', () => {
        // 勾选但没填时间时，自动补一个默认时间，省一次点击
        const timesBox = card.querySelector('.timeList');
        if (on.checked && !timesBox.querySelector('input')) addTimeEntry(timesBox, '05:30');
        markDirty();
      });

      const info = node('span');
      info.append(node('strong', plan.label), node('small', plan.auth_id));
      const badge = node('span', plan.pending ? '待配置' : (plan.disabled ? '已禁用' : '就绪'), 'badge');

      const planBox = node('div', undefined, 'accountPlan');
      const timeList = node('div', undefined, 'timeList');
      for (const at of plan.times) addTimeEntry(timeList, at);
      const addBtn = node('button', '+ 时间', 'secondary addTime');
      addBtn.type = 'button';
      addBtn.addEventListener('click', () => {
        if (timeList.children.length >= 24) return feedback('每个账号最多 24 个时间点', true);
        addTimeEntry(timeList, '');
        markDirty();
      });

      const modelWrap = node('span');
      modelWrap.className = 'selectControl';
      const select = node('select', undefined, 'acctModel');
      const listId = 'models-' + plan.auth_id.replace(/[^a-zA-Z0-9]/g, '');
      const models = catalog.get(plan.auth_id);
      if (Array.isArray(models) && models.length) {
        const dl = node('datalist');
        dl.id = listId;
        for (const m of models) {
          const opt = node('option');
          opt.value = m;
          dl.append(opt);
        }
        modelWrap.append(dl);
        select.setAttribute('list', listId);
      }
      // 用 select 的兄弟 input 更稳妥：select 只列已知模型，允许手输时用 input
      const modelInput = node('input');
      modelInput.type = 'text';
      modelInput.className = 'acctModel';
      modelInput.value = plan.model || '';
      modelInput.placeholder = 'gpt-6-luna';
      modelInput.addEventListener('input', markDirty);
      if (Array.isArray(models) && models.length) modelInput.setAttribute('list', listId);
      modelInput.style.minWidth = '150px';
      modelWrap.append(modelInput);
      select.remove();

      const next = node('span', plan.pending ? '待配置：填时间并保存'
        : (plan.next_trigger ? '下次 ' + formatDate(plan.next_trigger) : '—'), 'acctNext');
      if (plan.pending) next.classList.add('pending');

      planBox.append(timeList, addBtn, modelWrap, next);
      card.append(on, info, badge, planBox);
      list.append(card);
    }
  }
  updateSummary(plans);
}

function updateSummary(plans) {
  const active = plans.filter((p) => p.enabled && p.times.length);
  $('selectedCount').textContent = active.length;
  const nexts = plans.map((p) => p.next_trigger).filter(Boolean).sort();
  $('nextTrigger').textContent = nexts.length ? formatDate(nexts[0]) : '—';
  $('timezoneLabel').textContent = (STATUS && STATUS.timezone) || 'UTC';
  $('scheduleState').textContent = (STATUS && STATUS.last_error)
    ? '需要检查'
    : (STATUS && !STATUS.enabled ? '已暂停' : (active.length ? '等待触发' : '待配置'));
  $('runningHint').textContent = (STATUS && STATUS.last_error) || '';
}

function formatDate(value) {
  if (!value || String(value).startsWith('0001-')) return '—';
  const date = new Date(value);
  if (Number.isNaN(+date)) return '—';
  return new Intl.DateTimeFormat(lang === 'en' ? 'en' : 'zh-CN', {
    timeZone: (STATUS && STATUS.timezone) || 'UTC',
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).format(date);
}

function renderHistory() {
  if (!STATUS) return;
  historyView.update(STATUS);
  const view = historyView.snapshot();
  $('historyDate').value = view.date;
  $('historyDate').max = view.today;
  $('historyNextDay').disabled = view.date >= view.today;
  $('historyTimezone').textContent = view.timezone;
  $('historyCount').textContent = `共 ${view.total} 条 · 第 ${view.start}-${view.end} 条`;
  $('historyPageNumber').textContent = `${view.page} / ${view.pages}`;
  $('historyPrevPage').disabled = view.page <= 1;
  $('historyNextPage').disabled = view.page >= view.pages;

  const body = $('history');
  body.replaceChildren();
  if (!view.rows.length) {
    const tr = node('tr');
    const td = node('td', '这一天还没有记录', 'empty');
    td.colSpan = 6;
    tr.append(td);
    body.append(tr);
    return;
  }
  for (const row of view.rows) {
    const tr = node('tr');
    const tdCheck = node('td', undefined, 'checkCell');
    const check = node('input');
    check.type = 'checkbox';
    check.checked = historySelection.has(row.id);
    check.addEventListener('change', () => {
      if (check.checked) historySelection.add(row.id); else historySelection.delete(row.id);
      updateDeleteButton();
    });
    tdCheck.append(check);

    const tdAt = node('td', formatDate(row.scheduled_for));
    tdAt.append(node('small', row.mode === 'manual' ? '手动' : '定时'));

    const tdResult = node('td');
    tdResult.append(node('span', row.status, `tag ${row.status}`));
    tdResult.append(node('small', backend(row.message || '')));

    const tdReset = node('td', row.reset_at ? formatDate(row.reset_at) : '未确认');
    if (row.reset_at) tdReset.append(node('small', '上游窗口'));

    tr.append(tdCheck, node('td', row.label), tdAt, tdResult, tdReset);
    body.append(tr);
  }
  updateDeleteButton();
}

function updateDeleteButton() {
  const btn = $('historyDeleteSelected');
  btn.hidden = historySelection.size === 0;
  btn.textContent = `删除选中 (${historySelection.size})`;
  btn.disabled = !connected || historySelection.size === 0;
}

function render() {
  $('ver').textContent = STATUS ? 'v' + (STATUS.version || '?') : '';
  $('timezone').value = (STATUS && STATUS.timezone) || 'Asia/Shanghai';
  $('scheduleEnabled').checked = !!(STATUS && STATUS.enabled);
  $('timezone').disabled = !connected;
  $('scheduleEnabled').disabled = !connected;
  $('saveButton').disabled = !connected || saving;
  $('runButton').disabled = !connected;
  $('codexTab').classList.toggle('secondary', provider !== 'codex');
  $('antigravityTab').classList.toggle('secondary', provider !== 'antigravity');
  $('zoneOffset').textContent = CPAWindowUI.offset($('timezone').value);
  renderAccounts();
  renderHistory();
}

// ---------- 交互 ----------

function collectPlans() {
  return [...document.querySelectorAll('#accountList .accountCard')].map((card) => ({
    auth_id: card.dataset.auth,
    enabled: card.querySelector('.acctOn').checked,
    model: card.querySelector('.acctModel').value.trim(),
    times: [...card.querySelectorAll('.timeList input')].map((i) => i.value).filter(Boolean),
  })).filter((p) => p.times.length > 0 || p.enabled);
}

async function save() {
  if (!connected || saving) return;
  const current = collectPlans().map((p) => ({ ...p, provider }));
  const others = ((STATUS && STATUS.plans) || [])
    .filter((p) => (p.provider || 'codex') !== provider)
    .map((p) => ({
      provider: p.provider || 'codex',
      auth_id: p.auth_id,
      enabled: p.enabled !== false,
      model: p.model || '',
      times: p.times || [],
    }));
  const plans = [...others, ...current];
  for (const plan of plans) {
    if (plan.times.length && !plan.model) {
      return feedback(`账号 ${plan.auth_id} 填了时间但没填模型`, true);
    }
  }
  saving = true;
  $('saveButton').disabled = true;
  try {
    await api(API + '/config', {
      method: 'PATCH',
      body: JSON.stringify({
        timezone: $('timezone').value.trim() || 'Asia/Shanghai',
        enabled: $('scheduleEnabled').checked,
        plans,
      }),
    });
    await new Promise((r) => setTimeout(r, 600));
    await load();
    dirty = false;
    $('dirtyHint').textContent = '';
    feedback('已保存');
  } catch (e) {
    feedback(e.message, true);
  } finally {
    saving = false;
    $('saveButton').disabled = !connected;
  }
}

async function runNow() {
  if (!connected) return;
  if (dirty) return feedback('先保存计划再触发', true);
  $('runButton').disabled = true;
  try {
    const result = await api(API + '/run', { method: 'POST', body: JSON.stringify({ auth_id: '' }) });
    feedback(`已触发 ${result.triggered || 0} 个账号`);
    await new Promise((r) => setTimeout(r, 2500));
    await load();
  } catch (e) {
    feedback(e.message, true);
  } finally {
    $('runButton').disabled = !connected;
  }
}

async function load() {
  STATUS = await readStatus();
  ACCOUNTS = await readAccounts();
  await enrichAccounts();
  await loadCatalog();
  dirty = false;
  render();
}

// ---------- 启动 ----------

async function connect() {
  const session = CPAPluginSession.savedConnection(window);
  $('nativeSettings').href = CPAPluginSession.nativeSettingsURL(window);
  if (!session) {
    $('connectionHint').textContent = '没有读到 CPA 的登录态。可以先用只读模式查看计划，改配置请到 CPA 原生插件设置或直接编辑 config.yaml。';
    $('connectButton').textContent = '重新尝试';
    // 只读模式：仍然渲染页面，让用户看到计划和历史
    try {
      await load();
      $('connectionPanel').hidden = false;
      $('workspace').hidden = false;
      $('headerActions').hidden = false;
      connected = false;
      $('saveButton').disabled = true;
      $('runButton').disabled = true;
    } catch (e) {
      feedback(e.message, true);
    }
    return;
  }
  managementKey = session.managementKey;
  connected = true;
  try {
    await load();
    $('connectionPanel').hidden = true;
    $('workspace').hidden = false;
    $('headerActions').hidden = false;
  } catch (e) {
    connected = false;
    $('connectionHint').textContent = '连接失败：' + e.message;
    $('connectionPanel').hidden = false;
  }
}

function setLanguage() {
  lang = CPAWindowUI.language(window);
  document.documentElement.lang = lang;
  for (const el of document.querySelectorAll('[data-i18n]')) el.textContent = t(el.dataset.i18n);
  for (const el of document.querySelectorAll('[data-i18n-label]')) {
    el.setAttribute('aria-label', t(el.dataset.i18nLabel));
    el.title = t(el.dataset.i18nLabel);
  }
  document.title = t('title');
}

function setTheme() {
  document.documentElement.dataset.theme = CPAWindowUI.theme(window);
}

function switchProvider(next) {
  if (next === provider) return;
  if (dirty) { feedback('先保存当前改动再切换', true); return; }
  provider = next;
  $('codexTab').classList.toggle('secondary', provider !== 'codex');
  $('antigravityTab').classList.toggle('secondary', provider !== 'antigravity');
  renderAccounts();
}

$('codexTab').addEventListener('click', () => switchProvider('codex'));
$('antigravityTab').addEventListener('click', () => switchProvider('antigravity'));
$('saveButton').addEventListener('click', (e) => { e.preventDefault(); save(); });
$('runButton').addEventListener('click', runNow);
$('connectButton').addEventListener('click', connect);
$('refreshAccounts').addEventListener('click', () => load().catch((e) => feedback(e.message, true)));
$('refreshStatus').addEventListener('click', () => load().catch((e) => feedback(e.message, true)));
$('settingsForm').addEventListener('submit', (e) => { e.preventDefault(); save(); });
$('timezone').addEventListener('input', () => {
  markDirty();
  $('zoneOffset').textContent = CPAWindowUI.offset($('timezone').value);
});
$('scheduleEnabled').addEventListener('change', markDirty);
$('historyPrevDay').addEventListener('click', () => { historyView.moveDay(-1); historySelection.clear(); renderHistory(); });
$('historyNextDay').addEventListener('click', () => { historyView.moveDay(1); historySelection.clear(); renderHistory(); });
$('historyToday').addEventListener('click', () => { historyView.goToday(); historySelection.clear(); renderHistory(); });
$('historyDate').addEventListener('change', () => { historyView.setDate($('historyDate').value); historySelection.clear(); renderHistory(); });
$('historyPrevPage').addEventListener('click', () => { historyView.setPage(historyView.snapshot().page - 1); renderHistory(); });
$('historyNextPage').addEventListener('click', () => { historyView.setPage(historyView.snapshot().page + 1); renderHistory(); });
$('historyClearDate').addEventListener('click', () => {
  const view = historyView.snapshot();
  historySelection = new Set(view.rows.map((r) => r.id));
  updateDeleteButton();
});
$('historyDeleteCancel').addEventListener('click', () => {
  pendingDelete = null;
  $('historyDeleteDialog').close();
  historySelection.clear();
  renderHistory();
});
$('historyDeleteSelected').addEventListener('click', () => {
  if (!historySelection.size) return;
  $('historyDeleteMessage').textContent = `确定删除选中的 ${historySelection.size} 条记录？删除后不可恢复。`;
  $('historyDeleteDialog').showModal();
});
$('historyDeleteConfirm').addEventListener('click', async () => {
  const ids = [...historySelection];
  if (!ids.length) return;
  $('historyDeleteConfirm').disabled = true;
  try {
    const result = await api(API + '/history/delete', { method: 'POST', body: JSON.stringify({ ids }) });
    historySelection.clear();
    $('historyDeleteDialog').close();
    await load();
    feedback(`已删除 ${result.deleted || 0} 条记录`);
  } catch (e) {
    feedback(e.message, true);
  } finally {
    $('historyDeleteConfirm').disabled = false;
  }
});

CPATimezonePicker.mount({ input: $('timezone'), zones: CPAWindowUI.zones(), language: () => lang });
setTheme();
setLanguage();
connect();

window.addEventListener('storage', (e) => {
  if (e.key === null || e.key === 'cli-proxy-theme') setTheme();
  if (e.key === null || e.key === 'cli-proxy-language') { setLanguage(); render(); }
});
setInterval(() => {
  if (dirty || document.hidden) return;
  if (document.activeElement && ['INPUT', 'SELECT', 'TEXTAREA'].includes(document.activeElement.tagName)) return;
  load().catch(() => {});
}, 10000);
