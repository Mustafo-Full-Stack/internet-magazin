const TARGET = process.env.API_TARGET || 'https://webhost2026.pythonanywhere.com';

module.exports = async (req, res) => {
  const path = Array.isArray(req.query.path) ? req.query.path.join('/') : (req.query.path || '');
  const targetUrl = TARGET + '/api/' + path + (req.url.includes('?') ? req.url.slice(req.url.indexOf('?')) : '');

  const headers = {};
  ['content-type', 'authorization', 'x-order-token', 'accept'].forEach((h) => {
    const v = req.headers[h];
    if (v) headers[h] = v;
  });

  let body;
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    body = await new Promise((resolve) => {
      const chunks = [];
      req.on('data', (c) => chunks.push(c));
      req.on('end', () => resolve(Buffer.concat(chunks)));
    });
  }

  try {
    const upstream = await fetch(targetUrl, { method: req.method, headers, body: body && body.length ? body : undefined });
    const text = await upstream.text();
    res.status(upstream.status);
    res.setHeader('content-type', upstream.headers.get('content-type') || 'application/json');
    res.send(text);
  } catch (e) {
    res.status(502).setHeader('content-type', 'application/json');
    res.send(JSON.stringify({ error: 'backend unavailable', detail: String(e) }));
  }
};