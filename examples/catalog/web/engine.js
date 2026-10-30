// An engine answers a query string the way GET /api/products does. The server engine asks the Go
// server; the browser engine runs the same Go code compiled to WebAssembly and the same SQL in
// PGlite, Postgres compiled to WebAssembly, so the demo also works as a static page. The page
// names its engine in <meta name="demo-engine">: "make demo" sets "browser" for the static build.
//
// Every engine answers { status, body, sql }: the HTTP status, the JSON body (an answer, or an
// error with code, field and message) and the statements that ran, each with a label.

const PGLITE = 'https://cdn.jsdelivr.net/npm/@electric-sql/pglite@0.5.10/dist/index.js';

export async function connect(progress) {
  const engine = document.querySelector('meta[name="demo-engine"]')?.content;
  return engine === 'browser' ? browserEngine(progress) : serverEngine();
}

async function serverEngine() {
  const response = await fetch('api/schema');
  if (!response.ok) throw new Error(`GET api/schema answered ${response.status}`);
  const schema = await response.json();
  return {
    name: 'Go server · chi + GORM + Postgres',
    schema,
    async query(query) {
      const [answer, sql] = await Promise.all([
        fetch(`api/products?${query}`),
        fetch(`api/products/sql?${query}`),
      ]);
      const statements = sql.ok ? await sql.json() : [];
      return { status: answer.status, body: await answer.json(), sql: statements };
    },
  };
}

async function browserEngine(progress) {
  progress('Loading the Go WebAssembly build…');
  await loadScript('wasm_exec.js');
  const go = new globalThis.Go();
  const ready = new Promise((resolve) => globalThis.addEventListener('go-paginate-ready', resolve, { once: true }));
  const module = await instantiate('demo.wasm', go.importObject);
  go.run(module.instance);
  await ready;
  const api = globalThis.goPaginate;

  progress('Starting Postgres in the browser (PGlite)…');
  const { PGlite } = await import(PGLITE);
  const db = await PGlite.create();
  await db.exec(api.seed());

  return {
    name: 'In the browser · Go WebAssembly + PGlite',
    schema: JSON.parse(api.schema()),
    async query(query) {
      const prepared = JSON.parse(api.prepare(query));
      if (prepared.error) {
        return { status: prepared.status, body: prepared.error, sql: [] };
      }
      const sql = [];
      try {
        let total = 0;
        let count = 'none';
        if (prepared.estimate) {
          sql.push({ label: 'the estimate', sql: prepared.estimate.text });
          const plan = await db.query(prepared.estimate.sql, prepared.estimate.args);
          const estimate = api.estimate(JSON.stringify(plan.rows[0]['QUERY PLAN']));
          if (estimate >= prepared.exactCountBelow) {
            total = estimate;
            count = 'estimate';
          }
        }
        if (prepared.count && count !== 'estimate') {
          sql.push({ label: 'the count', sql: prepared.count.text });
          total = Number((await db.query(prepared.count.sql, prepared.count.args)).rows[0].total);
          count = 'exact';
        }
        let rows = [];
        if (prepared.page) {
          sql.push({ label: 'the page', sql: prepared.page.text });
          rows = (await db.query(prepared.page.sql, prepared.page.args)).rows;
        }
        const body = JSON.parse(api.respond(query, JSON.stringify(rows), total, count));
        if (body.error) {
          return { status: body.status, body: body.error, sql };
        }
        return { status: 200, body, sql };
      } catch (error) {
        // A value of the wrong type is a data exception (SQLSTATE 22): the client's mistake, as
        // Query.WrapError reports it on the server.
        if (String(error.code ?? '').startsWith('22')) {
          const message = `invalid filter value: ${error.message} (SQLSTATE ${error.code})`;
          return { status: 400, body: { code: 'invalid_value', message }, sql };
        }
        return { status: 500, body: { code: 'internal', message: error.message }, sql };
      }
    },
  };
}

function loadScript(src) {
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = src;
    script.onload = resolve;
    script.onerror = () => reject(new Error(`cannot load ${src}`));
    document.head.append(script);
  });
}

async function instantiate(url, imports) {
  try {
    return await WebAssembly.instantiateStreaming(fetch(url), imports);
  } catch {
    const bytes = await (await fetch(url)).arrayBuffer();
    return WebAssembly.instantiate(bytes, imports);
  }
}
