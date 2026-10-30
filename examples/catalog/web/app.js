// The demo page. The controls build a go-paginate query string, the table renders the answer, and
// the panel below it shows the request and the SQL behind it. The query string lives in the URL
// hash, so every state of the page is a link.

import { connect } from './engine.js';

const PRESETS = [
  ['Lighting or decor under 50', [['filter.category', '$in:lighting,decor'], ['filter.price', '$lt:50'], ['sortBy', 'price:ASC']]],
  ['Every word must match', [['search', 'ember lamp']]],
  ['% is literal', [['search', '%']]],
  ['OR across fields', [['filter', '(category=$in:lighting,decor OR price=$lt:20) AND NOT name=$sw:Ember']]],
  ['OR within a field', [['filter.price', '$lt:10'], ['filter.price', '$or:$gt:490']]],
  ['Tags contain both', [['filter.tags', '$contains:sale,eco']]],
  ['No rating', [['filter.rating', '$null']]],
  ['Rated, on sale', [['filter.rating', '$not:$null'], ['filter.discontinuedAt', '$null'], ['sortBy', 'rating:DESC']]],
  ['Released in 2024', [['filter.releasedAt', '$btw:2024-01-01,2024-12-31'], ['sortBy', 'releasedAt:ASC']]],
  ['Two sort keys, nulls last', [['sortBy', 'rating:DESC'], ['sortBy', 'price:ASC']]],
  ['Count only', [['limit', '0'], ['filter.category', '$in:seating']]],
  ['Every row', [['limit', '-1'], ['filter.brand', '$sw:lu']]],
  ['400: not a number', [['filter.price', '$gt:abc']]],
  ['400: operator not allowed', [['filter.sku', '$ilike:84']]],
];

const OPERATORS = {
  $eq: '= equals',
  $gt: '> greater',
  $gte: '≥ at least',
  $lt: '< less',
  $lte: '≤ at most',
  $in: 'in a list',
  $btw: 'between',
  $ilike: 'contains',
  $sw: 'starts with',
  $null: 'is null',
  $contains: 'array contains',
};

const PLACEHOLDERS = { $in: 'a,b,c', $btw: 'from,to', $contains: 'a,b' };
const DECIMALS = { price: 2, rating: 1 };
const LIMIT = '10';

const $ = (id) => document.getElementById(id);

let engine;
let fields = [];
let state = initialState();
let shown = null;
let sequence = 0;
let typing;

function initialState(format = 'nestjs') {
  return { page: 1, limit: LIMIT, format, search: '', searchBy: [], sortBy: [], filters: [], expression: '' };
}

// The query string.

function toQuery(s) {
  const params = [['page', String(s.page)], ['limit', s.limit]];
  for (const [field, order] of s.sortBy) params.push(['sortBy', `${field}:${order}`]);
  if (s.search) params.push(['search', s.search]);
  for (const field of s.searchBy) params.push(['searchBy', field]);
  for (const filter of s.filters) {
    const token = filterToken(filter);
    if (token !== null) params.push([`filter.${filter.field}`, token]);
  }
  if (s.expression.trim()) params.push(['filter', s.expression.trim()]);
  if (s.format === 'default') params.push(['format', 'default']);
  return encode(params);
}

function fromQuery(query, format) {
  const params = new URLSearchParams(query);
  const s = initialState(params.get('format') === 'default' ? 'default' : format ?? 'nestjs');
  s.page = Math.max(1, Number.parseInt(params.get('page'), 10) || 1);
  s.limit = params.get('limit') ?? LIMIT;
  s.search = params.get('search') ?? '';
  s.searchBy = params.getAll('searchBy');
  s.sortBy = params.getAll('sortBy').map((value) => {
    const at = value.lastIndexOf(':');
    return at < 0 ? [value, 'ASC'] : [value.slice(0, at), value.slice(at + 1).toUpperCase()];
  });
  for (const [key, value] of params) {
    if (key.startsWith('filter.')) s.filters.push(parseToken(key.slice('filter.'.length), value));
  }
  s.expression = params.getAll('filter').join(' AND ');
  return s;
}

// filterToken writes a filter row as a filter.<field> value: [$or:][$not:]$operator[:value].
function filterToken({ or, not, operator, value }) {
  if (operator !== '$null' && value === '') return null;
  const parts = [];
  if (or) parts.push('$or');
  if (not) parts.push('$not');
  parts.push(operator);
  if (operator !== '$null') parts.push(value);
  return parts.join(':');
}

// parseToken reads a filter.<field> value the way the library does: up to three prefixes in any
// order, then the value.
function parseToken(field, raw) {
  const filter = { field, or: false, not: false, operator: '$eq', value: '' };
  const seen = new Set();
  let rest = raw;
  while (rest !== '') {
    const at = rest.indexOf(':');
    const head = at < 0 ? rest : rest.slice(0, at);
    const kind = head === '$or' || head === '$and' ? 'comparator' : head === '$not' ? 'not' : head in OPERATORS ? 'operator' : null;
    if (kind === null || seen.has(kind)) break;
    seen.add(kind);
    if (kind === 'comparator') filter.or = head === '$or';
    if (kind === 'not') filter.not = true;
    if (kind === 'operator') filter.operator = head;
    rest = at < 0 ? '' : rest.slice(at + 1);
  }
  filter.value = filter.operator === '$null' ? '' : rest;
  return filter;
}

// encode writes a query string the way the library's links do: $ : , ( ) stay readable.
function encode(params) {
  const escape = (value) =>
    encodeURIComponent(value)
      .replaceAll('%20', '+')
      .replace(/%(24|3A|2C)/g, (_, hex) => String.fromCharCode(Number.parseInt(hex, 16)));
  return params.map(([key, value]) => `${escape(key)}=${escape(value)}`).join('&');
}

// Navigation.

function commit({ push = false, keepPage = false } = {}) {
  if (!keepPage) state.page = 1;
  const query = toQuery(state);
  if (query !== location.hash.slice(1)) {
    history[push ? 'pushState' : 'replaceState'](null, '', `#${query}`);
  }
  load(query);
}

function commitSoon() {
  clearTimeout(typing);
  typing = setTimeout(commit, 350);
}

function apply(next, push = true) {
  clearTimeout(typing);
  state = next;
  renderControls();
  commit({ push, keepPage: true });
}

function followHash() {
  const query = location.hash.slice(1);
  if (query !== shown) apply(fromQuery(query), false);
}

// Loading.

async function load(query) {
  const current = ++sequence;
  shown = query;
  $('request').textContent = `GET /api/products?${query}`;
  const started = performance.now();
  let answer;
  try {
    answer = await engine.query(query);
  } catch (error) {
    answer = { status: 0, body: { message: error.message }, sql: {} };
  }
  if (current !== sequence) return;
  render(answer, Math.round(performance.now() - started));
  // The query on screen, for tests and for anyone reading the DOM.
  document.body.dataset.rendered = query;
}

function render({ status, body, sql }, ms) {
  const failed = status !== 200;
  const statusBox = $('status');
  statusBox.classList.toggle('error', failed);
  if (failed) {
    statusBox.textContent = `${status || 'Error'}: ${body.message}`;
    renderTable(null);
    $('pager').replaceChildren();
    $('sql').textContent = sql.count ? sqlText(sql) : '-- no SQL: the request did not pass the checks';
    $('meta').textContent = JSON.stringify(body, null, 2);
    return;
  }
  const { meta, links } = body;
  statusBox.textContent = `${meta.totalItems} matching · ${engine.label} · ${ms} ms`;
  renderTable(body);
  renderPager(meta, links);
  $('sql').textContent = sqlText(sql);
  $('meta').textContent = JSON.stringify(links ? { meta, links } : { meta }, null, 2);
}

function sqlText({ count, select }) {
  const page = select ?? '-- limit=0 asks for the count only: no page is selected';
  return `-- the count\n${count}\n\n-- the page\n${page}`;
}

// The table.

function renderTable(body) {
  const table = $('table');
  const sort = effectiveSort(body?.meta);
  const head = el('tr', {}, ...fields.map((field) => headerCell(field, sort)));
  table.tHead.replaceChildren(head);

  const rows = body?.data ?? [];
  if (rows.length === 0) {
    const text = !body ? 'No answer.' : body.meta.totalItems && state.limit === '0' ? 'limit=0: the count only, no rows.' : 'No rows match.';
    table.tBodies[0].replaceChildren(el('tr', {}, el('td', { class: 'empty', colspan: fields.length }, text)));
    return;
  }
  table.tBodies[0].replaceChildren(...rows.map((row) => el('tr', {}, ...fields.map((field) => dataCell(field, row[field.name])))));
}

// effectiveSort is what the rows are sorted by: the client's keys, or the default the server
// reports in the nestjs meta.
function effectiveSort(meta) {
  if (state.sortBy.length) return { keys: state.sortBy, isDefault: false };
  if (Array.isArray(meta?.sortBy)) return { keys: meta.sortBy, isDefault: true };
  return { keys: [], isDefault: true };
}

function headerCell(field, sort) {
  const index = sort.keys.findIndex(([name]) => name === field.name);
  const th = el('th', { class: field.type === 'number' ? 'num' : '' }, field.label);
  if (index >= 0) {
    th.append(el('span', { class: 'arrow', title: sort.isDefault ? 'the default sort' : '' }, sort.keys[index][1] === 'ASC' ? '↑' : '↓'));
    if (sort.keys.length > 1) th.append(el('span', { class: 'priority' }, String(index + 1)));
  }
  if (field.sortable) {
    th.classList.add('sortable');
    th.title = 'Sort; Shift+click adds a key';
    th.addEventListener('click', (event) => toggleSort(field.name, event.shiftKey));
  }
  return th;
}

function toggleSort(name, add) {
  const keys = state.sortBy.map((pair) => [...pair]);
  const index = keys.findIndex(([field]) => field === name);
  const next = index < 0 ? 'ASC' : keys[index][1] === 'ASC' ? 'DESC' : null;
  if (!add) {
    state.sortBy = next ? [[name, next]] : [];
  } else if (index < 0) {
    state.sortBy = [...keys, [name, next]];
  } else if (next) {
    keys[index][1] = next;
    state.sortBy = keys;
  } else {
    keys.splice(index, 1);
    state.sortBy = keys;
  }
  commit({ push: true });
}

function dataCell(field, value) {
  const numeric = field.type === 'number';
  if (value === null || value === undefined) return el('td', { class: `muted${numeric ? ' num' : ''}` }, '—');
  switch (field.type) {
    case 'number':
      return el('td', { class: 'num' }, field.name in DECIMALS ? Number(value).toFixed(DECIMALS[field.name]) : String(value));
    case 'uuid':
      return el('td', { class: 'muted', title: value }, `${value.slice(0, 8)}…`);
    case 'tags':
      return el('td', {}, ...value.map((tag) => el('span', { class: 'tag' }, tag)));
    case 'date':
      return el('td', {}, String(value).slice(0, 10));
    default:
      return el('td', {}, String(value));
  }
}

// The pager follows the links of the nestjs answer; the default answer has none, so it counts
// pages from the meta.

function renderPager(meta, links) {
  const pager = $('pager');
  if (state.limit === '0' || state.limit === '-1') {
    pager.replaceChildren(el('span', { class: 'info' }, state.limit === '0' ? 'The count only.' : `Every row: ${meta.totalItems}.`));
    return;
  }
  const page = meta.currentPage;
  const pages = meta.totalPages;
  const target = (name, fallback) => {
    if (links) return links[name] ? Number(new URLSearchParams(links[name].split('?')[1]).get('page')) : null;
    return fallback >= 1 && fallback <= pages && fallback !== page ? fallback : null;
  };
  const button = (label, name, fallback) => {
    const to = target(name, fallback);
    const node = el('button', { type: 'button', class: 'ghost', title: links?.[name] ?? '' }, label);
    node.disabled = to === null;
    node.addEventListener('click', () => {
      state.page = to;
      commit({ push: true, keepPage: true });
    });
    return node;
  };
  pager.replaceChildren(
    el('span', { class: 'info' }, `Page ${page} of ${Math.max(pages, 1)} · ${meta.itemsPerPage} per page`),
    el(
      'span',
      { class: 'buttons' },
      button('« First', 'first', 1),
      button('‹ Previous', 'previous', page - 1),
      button('Next ›', 'next', page + 1),
      button('Last »', 'last', pages),
    ),
  );
}

// The controls.

function renderControls() {
  $('search').value = state.search;
  $('format').value = state.format;
  const limit = $('limit');
  if (![...limit.options].some((option) => option.value === state.limit)) {
    limit.append(el('option', { value: state.limit }, state.limit));
  }
  limit.value = state.limit;
  $('expression').value = state.expression;
  for (const box of $('searchBy').querySelectorAll('input')) box.checked = state.searchBy.includes(box.value);
  renderFilters();
}

function renderFilters() {
  $('filters').replaceChildren(...state.filters.map(filterRow));
}

function filterRow(filter) {
  const filterable = fields.filter((field) => field.filterable);
  const known = fields.find((field) => field.name === filter.field);
  const operators = known?.operators ?? Object.keys(OPERATORS);

  const join = select(
    [
      ['and', 'and'],
      ['or', '$or'],
    ],
    filter.or ? 'or' : 'and',
  );
  join.title = 'How this value joins the other values of the field';
  join.addEventListener('change', () => {
    filter.or = join.value === 'or';
    commit();
  });

  const fieldOptions = filterable.map((field) => [field.name, field.label]);
  if (!known) fieldOptions.push([filter.field, `${filter.field} (unknown)`]);
  const fieldSelect = select(fieldOptions, filter.field);
  fieldSelect.addEventListener('change', () => {
    filter.field = fieldSelect.value;
    const allowed = fields.find((field) => field.name === filter.field)?.operators ?? [];
    if (!allowed.includes(filter.operator)) filter.operator = allowed[0] ?? '$eq';
    renderFilters();
    commit();
  });

  const not = el('input', { type: 'checkbox' });
  not.checked = filter.not;
  not.addEventListener('change', () => {
    filter.not = not.checked;
    commit();
  });

  const operatorOptions = operators.map((operator) => [operator, `${operator} ${OPERATORS[operator] ?? ''}`]);
  if (!operators.includes(filter.operator)) operatorOptions.push([filter.operator, `${filter.operator} (not allowed)`]);
  const operatorSelect = select(operatorOptions, filter.operator);
  operatorSelect.addEventListener('change', () => {
    filter.operator = operatorSelect.value;
    renderFilters();
    commit();
  });

  const value = el('input', { class: 'value', autocomplete: 'off', spellcheck: 'false' });
  value.value = filter.value;
  value.placeholder = PLACEHOLDERS[filter.operator] ?? known?.example ?? '';
  value.disabled = filter.operator === '$null';
  if (value.disabled) value.placeholder = 'no value';
  value.addEventListener('input', () => {
    filter.value = value.value;
    commitSoon();
  });

  const remove = el('button', { type: 'button', class: 'remove', title: 'Remove the filter' }, '×');
  remove.addEventListener('click', () => {
    state.filters.splice(state.filters.indexOf(filter), 1);
    renderFilters();
    commit({ push: true });
  });

  return el('div', { class: 'filter-row' }, join, fieldSelect, el('label', { class: 'toggle' }, not, '$not'), operatorSelect, value, remove);
}

function setupControls() {
  const searchBy = $('searchBy');
  for (const field of fields.filter((field) => field.searchable)) {
    const box = el('input', { type: 'checkbox', value: field.name });
    box.addEventListener('change', () => {
      state.searchBy = [...searchBy.querySelectorAll('input:checked')].map((input) => input.value);
      commit();
    });
    searchBy.append(el('label', { class: 'toggle', title: 'None checked: every searchable field' }, box, field.name));
  }

  $('search').addEventListener('input', (event) => {
    state.search = event.target.value;
    commitSoon();
  });
  $('expression').addEventListener('input', (event) => {
    state.expression = event.target.value;
    commitSoon();
  });
  $('limit').addEventListener('change', (event) => {
    state.limit = event.target.value;
    commit({ push: true });
  });
  $('format').addEventListener('change', (event) => {
    state.format = event.target.value;
    commit({ push: true, keepPage: true });
  });
  $('addFilter').addEventListener('click', () => {
    const field = fields.find((candidate) => candidate.filterable);
    state.filters.push({ field: field.name, or: false, not: false, operator: field.operators[0], value: '' });
    renderFilters();
    $('filters').lastElementChild.querySelector('.value').focus();
  });
  $('reset').addEventListener('click', () => apply(initialState(state.format)));

  $('presets').replaceChildren(
    ...PRESETS.map(([label, params]) => {
      const chip = el('button', { type: 'button', class: 'chip', title: params.map((pair) => pair.join('=')).join('&') }, label);
      chip.addEventListener('click', () => apply(fromQuery(encode(params), state.format)));
      return chip;
    }),
  );

  addEventListener('popstate', followHash);
  addEventListener('hashchange', followHash);
}

// DOM helpers.

function el(tag, attributes, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (value !== '' && value !== undefined) node.setAttribute(name, value);
  }
  node.append(...children);
  return node;
}

function select(options, selected) {
  const node = el('select', {}, ...options.map(([value, label]) => el('option', { value }, label)));
  node.value = selected;
  return node;
}

// Start.

async function start() {
  const status = $('status');
  const progress = (message) => {
    status.textContent = message;
  };
  try {
    engine = await connect(progress);
  } catch (error) {
    status.classList.add('error');
    status.textContent = `The demo could not start: ${error.message}`;
    $('engine').textContent = 'Not started';
    return;
  }
  engine.label = engine.name.split(' · ')[0];
  $('engine').textContent = engine.name;
  fields = engine.schema;
  setupControls();
  state = fromQuery(location.hash.slice(1));
  renderControls();
  commit({ keepPage: true });
}

start();
