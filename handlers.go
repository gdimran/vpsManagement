package main

const dashboardHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>VPS Dashboard</title>
<style>
body { font-family: sans-serif; background:#111; color:#eee; padding:20px; }
table { border-collapse: collapse; width: 100%; margin-bottom:20px; }
th, td { padding: 8px 10px; border-bottom: 1px solid #333; text-align: left; font-size: 13px; }
.up { color: #4caf50; font-weight: bold; }
.down { color: #f44336; font-weight: bold; }
.warn { color: #ff9800; font-weight: bold; }
.muted { color: #888; font-size: 12px; }
button { padding:8px 14px; margin:3px; background:#2196f3; color:#fff; border:none; border-radius:4px; cursor:pointer; font-size: 13px; }
button:hover { background:#1976d2; }
#vps-table td:last-child, #sites-table td:last-child { white-space: nowrap; }
#vps-table button, #sites-table button { padding: 5px 9px; margin: 1px; font-size: 12px; }
input, textarea, select { padding:8px; margin:4px; background:#222; color:#eee; border:1px solid #444; border-radius:4px; }
label { display:inline-block; width:220px; vertical-align: top; }
textarea { width: 400px; font-family: monospace; }
h2 { margin-top: 30px; }
</style>
</head>
<body>
<h1>VPS Dashboard</h1>

<h2>Global Actions</h2>
<button onclick="post('/backup')">Run Backup (All)</button>
<button onclick="post('/ssl/check')">Check SSL (All)</button>
<button onclick="post('/ssl/renew')">Renew SSL (All)</button>
<button onclick="autoConfigureSites()">Auto-configure unconfigured sites</button>

<h2>Sites</h2>
<table id="sites-table">
<thead><tr>
  <th>Domain</th>
  <th>Server</th>
  <th>Status</th>
  <th>Code</th>
  <th>Response</th>
  <th>SSL</th>
  <th>SSL Expiry</th>
  <th>Days</th>
  <th>Doc Root</th>
  <th>DB</th>
  <th>SSL Method</th>
  <th>Actions</th>
</tr></thead>
<tbody id="sites-list"></tbody>
</table>

<h2>Backup Settings</h2>
<form id="settings-form" onsubmit="saveSettings(event)">
  <label>Auto backup enabled:</label>
  <input type="checkbox" name="auto_backup_enabled" id="s_enabled"><br>

  <label>Mode:</label>
  <select name="auto_backup_mode" id="s_mode" onchange="toggleModeFields()">
    <option value="daily">Daily</option>
    <option value="weekly">Weekly</option>
    <option value="monthly">Monthly</option>
    <option value="semimonthly">Semi-monthly (1st &amp; 15th)</option>
    <option value="custom">Custom one-off date/time</option>
  </select><br>

  <div id="field-day" style="display:none;">
    <label>Day (weekly 0-6, monthly 1-31):</label>
    <input type="number" name="auto_backup_day" id="s_day" value="1" min="0" max="31"><br>
  </div>

  <div id="field-time">
    <label>Hour (0-23):</label>
    <input type="number" name="auto_backup_hour" id="s_hour" value="3" min="0" max="23"><br>
    <label>Minute (0-59):</label>
    <input type="number" name="auto_backup_minute" id="s_minute" value="0" min="0" max="59"><br>
  </div>

  <div id="field-custom" style="display:none;">
    <label>One-off date &amp; time (local):</label>
    <input type="datetime-local" name="custom_backup_time" id="s_custom"><br>
    <span class="muted">Fires once, then switches off automatically.</span><br>
  </div>

  <label>Keep last N backups in Drive:</label>
  <input type="number" name="keep_backups" id="s_keep" value="7" min="1" max="100"><br>

  <button type="submit">Save Settings</button>
  <button type="button" onclick="pruneNow()">Prune Now</button>
</form>

<h2>Backups in Google Drive</h2>
<button onclick="loadBackups()">Refresh</button>
<button onclick="pruneNow()">Prune old backups</button>
<table id="backups-table">
<thead><tr><th>Folder</th><th>Actions</th></tr></thead>
<tbody id="backups-list"></tbody>
</table>

<h2>Configured VPS</h2>
<table id="vps-table">
<thead><tr>
  <th>Name</th>
  <th>Host</th>
  <th>Last Backup</th>
  <th>Last SSL</th>
  <th>Actions</th>
</tr></thead>
<tbody id="vps-list"></tbody>
</table>

<h2>Add VPS</h2>
<form method="POST" action="/api/servers/add">
  <label>Name:</label><input name="name" required><br>
  <label>Host IP:</label><input name="host" required><br>
  <label>SSH Port:</label><input name="ssh_port" value="22"><br>
  <label>SSH User:</label><input name="user" value="root"><br>
  <label>SSH Password:</label><input type="password" name="ssh_password"><br>
  <label>DB User:</label><input name="db_user" value="root"><br>
  <label>DB Password:</label><input type="password" name="db_pass"><br>
  <label>Status URLs:</label><input name="status_urls" placeholder="https://a.com,https://b.com" size="60"><br>
  <button type="submit">Add Server</button>
</form>

<h2>Add Site to Existing Server</h2>
<form method="POST" action="/api/sites/add">
  <label>Server:</label>
  <select name="server" id="server-select" required></select><br>
  <label>Domain:</label><input name="domain" required><br>
  <label>Doc Root:</label><input name="doc_root" placeholder="/var/www/example" required><br>
  <label>DB Name:</label><input name="db_name"><br>
  <label>SSL Method:</label>
  <select name="ssl_method">
    <option value="certbot">certbot</option>
    <option value="cloudpanel">cloudpanel</option>
    <option value="none">none</option>
  </select><br>
  <button type="submit">Add Site</button>
</form>

<script>
function post(path) {
  fetch(path, { method: 'POST' }).then(r => r.text()).then(t => alert(t));
}

function fmtTime(iso) {
  if (!iso || iso === '0001-01-01T00:00:00Z') return '<span class="muted">never</span>';
  return new Date(iso).toLocaleString();
}

async function loadVPS() {
  const res = await fetch('/api/servers');
  const servers = await res.json();
  const tbody = document.getElementById('vps-list');
  tbody.innerHTML = '';
  servers.forEach(s => {
    const nameEnc = encodeURIComponent(s.name);
    const row = document.createElement('tr');
    row.innerHTML =
      '<td>' + s.name + '</td>' +
      '<td>' + s.host + '</td>' +
      '<td>' + fmtTime(s.last_backup) + '</td>' +
      '<td>' + fmtTime(s.last_ssl) + '</td>' +
      '<td>' +
        '<button onclick="post(\'/backup?name=' + nameEnc + '\')">Full Backup</button>' +
        '<button onclick="post(\'/backup/sites?name=' + nameEnc + '\')">All Sites</button>' +
        '<button onclick="post(\'/ssl/check?name=' + nameEnc + '\')">Check SSL</button>' +
        '<button onclick="post(\'/ssl/renew?name=' + nameEnc + '\')">Renew SSL</button>' +
        '<button onclick="editServer(\'' + s.name + '\')">Edit</button>' +
        '<button onclick="removeVPS(\'' + s.name + '\')">Remove</button>' +
      '</td>';
    tbody.appendChild(row);
  });
}

function removeVPS(name) {
  if (!confirm('Remove ' + name + '?')) return;
  fetch('/api/servers/remove?name=' + encodeURIComponent(name), { method: 'POST' }).then(() => loadVPS());
}

async function loadSites() {
  const [serversRes, statusRes] = await Promise.all([
    fetch('/api/servers'),
    fetch('/status')
  ]);
  const servers = await serversRes.json();
  const statuses = await statusRes.json();

  const statusByDomain = {};
  for (const url in statuses) {
    try {
      const host = new URL(url).hostname.replace(/^www\./, '');
      statusByDomain[host] = statuses[url];
    } catch (e) {}
  }

  const tbody = document.getElementById('sites-list');
  tbody.innerHTML = '';

  servers.forEach(srv => {
    const configByDomain = {};
    (srv.sites || []).forEach(site => { configByDomain[site.domain] = site; });

    (srv.status_urls || []).forEach(url => {
      let host = '';
      try { host = new URL(url).hostname.replace(/^www\./, ''); } catch (e) {}
      const live = statusByDomain[host] || {};
      const cfg = configByDomain[host] || {};

      const srvEnc = encodeURIComponent(srv.name);
      const domEnc = encodeURIComponent(host);

      const upCell = live.up
        ? '<span class="up">UP</span>'
        : (live.checked_at ? '<span class="down">DOWN</span>' : '<span class="muted">-</span>');
      const codeCell = live.status_code || '-';
      const respCell = live.response_time ? (live.response_time / 1e6).toFixed(0) + ' ms' : '-';

      let sslCell = '-', sslExpiry = '-', sslDays = '-';
      if (live.ssl_expiry && live.ssl_expiry !== '0001-01-01T00:00:00Z') {
        sslCell = live.ssl_valid ? '<span class="up">VALID</span>' : '<span class="down">EXPIRED</span>';
        sslExpiry = new Date(live.ssl_expiry).toLocaleDateString();
        let cls = 'up';
        if (live.ssl_days_left < 30) cls = 'down';
        else if (live.ssl_days_left < 60) cls = 'warn';
        sslDays = '<span class="' + cls + '">' + live.ssl_days_left + '</span>';
      }

      const configured = cfg.doc_root ? true : false;
      const statusIcon = configured ? '✅' : '⚠️';
      const docRoot = cfg.doc_root || '<span class="muted">-</span>';
      const dbName = cfg.db_name || '-';
      const sslMethod = cfg.ssl_method || '-';

      let actions = '';
      if (configured) {
        actions =
          '<button onclick="post(\'/backup/site?server=' + srvEnc + '&domain=' + domEnc + '\')">Backup</button>' +
          '<button onclick="post(\'/ssl/renew/site?server=' + srvEnc + '&domain=' + domEnc + '\')">Renew</button>' +
          '<button onclick="editSite(\'' + srv.name + '\',\'' + host + '\')">Edit</button>' +
          '<button onclick="removeSite(\'' + srv.name + '\',\'' + host + '\')">✕</button>';
      } else {
        actions = '<button onclick="configureSite(\'' + srv.name + '\',\'' + host + '\')">Configure</button>';
      }

      const row = document.createElement('tr');
      row.innerHTML =
        '<td>' + statusIcon + ' ' + host + '</td>' +
        '<td>' + srv.name + '</td>' +
        '<td>' + upCell + '</td>' +
        '<td>' + codeCell + '</td>' +
        '<td>' + respCell + '</td>' +
        '<td>' + sslCell + '</td>' +
        '<td>' + sslExpiry + '</td>' +
        '<td>' + sslDays + '</td>' +
        '<td>' + docRoot + '</td>' +
        '<td>' + dbName + '</td>' +
        '<td>' + sslMethod + '</td>' +
        '<td>' + actions + '</td>';
      tbody.appendChild(row);
    });
  });
}

function configureSite(server, domain) {
  const docRoot = prompt('Doc root for ' + domain + ':', '/var/www');
  if (docRoot === null) return;
  const dbName = prompt('Database name for ' + domain + ':', '');
  if (dbName === null) return;
  const sslMethod = prompt('SSL method (certbot/cloudpanel/none):', 'certbot');
  if (sslMethod === null) return;

  const params = new URLSearchParams({
    server: server,
    domain: domain,
    doc_root: docRoot,
    db_name: dbName,
    ssl_method: sslMethod
  });

  fetch('/api/sites/add', { method: 'POST', body: params }).then(() => loadSites());
}

function removeSite(server, domain) {
  if (!confirm('Remove ' + domain + ' from ' + server + '?')) return;
  fetch('/api/sites/remove?server=' + encodeURIComponent(server) + '&domain=' + encodeURIComponent(domain),
        { method: 'POST' }).then(() => loadSites());
}

async function editServer(name) {
  const res = await fetch('/api/servers');
  const servers = await res.json();
  const s = servers.find(x => x.name === name);
  if (!s) return;

  const host = prompt('Host IP:', s.host); if (host === null) return;
  const sshPort = prompt('SSH Port:', s.ssh_port || '22'); if (sshPort === null) return;
  const user = prompt('SSH User:', s.user || 'root'); if (user === null) return;
  const sshPass = prompt('SSH Password (leave blank to keep):', '');
  const dbUser = prompt('DB User:', s.db_user || 'root'); if (dbUser === null) return;
  const dbPass = prompt('DB Password (leave blank to keep):', '');
  const statusUrls = prompt('Status URLs (comma-separated):', (s.status_urls || []).join(','));
  if (statusUrls === null) return;

  const params = new URLSearchParams({
    name: name,
    host: host,
    ssh_port: sshPort,
    user: user,
    ssh_password: sshPass || s.ssh_password || '',
    db_user: dbUser,
    db_pass: dbPass || s.db_pass || '',
    status_urls: statusUrls
  });

  await fetch('/api/servers/update', { method: 'POST', body: params });
  loadVPS();
}

async function editSite(server, domain) {
  const res = await fetch('/api/servers');
  const servers = await res.json();
  const srv = servers.find(x => x.name === server);
  if (!srv) return;
  const site = (srv.sites || []).find(x => x.domain === domain) || {};

  const newDomain = prompt('Domain:', site.domain || domain); if (newDomain === null) return;
  const docRoot = prompt('Doc root:', site.doc_root || '/var/www'); if (docRoot === null) return;
  const dbName = prompt('DB name:', site.db_name || ''); if (dbName === null) return;
  const sslMethod = prompt('SSL method (certbot/cloudpanel/none):', site.ssl_method || 'certbot');
  if (sslMethod === null) return;

  const params = new URLSearchParams({
    server: server,
    old_domain: domain,
    domain: newDomain,
    doc_root: docRoot,
    db_name: dbName,
    ssl_method: sslMethod
  });

  await fetch('/api/sites/update', { method: 'POST', body: params });
  loadSites();
}

async function autoConfigureSites() {
  const res = await fetch('/api/servers');
  const servers = await res.json();
  let count = 0;

  for (const srv of servers) {
    const configured = new Set((srv.sites || []).map(x => x.domain));
    for (const url of (srv.status_urls || [])) {
      let host = '';
      try { host = new URL(url).hostname.replace(/^www\./, ''); } catch (e) { continue; }
      if (configured.has(host)) continue;

      const params = new URLSearchParams({
        server: srv.name,
        domain: host,
        doc_root: '/var/www',
        db_name: '',
        ssl_method: 'certbot'
      });
      await fetch('/api/sites/add', { method: 'POST', body: params });
      count++;
    }
  }
  alert('Auto-configured ' + count + ' sites. Edit each to fix doc roots and DB names.');
  loadSites();
}

async function loadServerOptions() {
  const res = await fetch('/api/servers');
  const servers = await res.json();
  const sel = document.getElementById('server-select');
  sel.innerHTML = '';
  servers.forEach(s => {
    const opt = document.createElement('option');
    opt.value = s.name;
    opt.textContent = s.name + ' (' + s.host + ')';
    sel.appendChild(opt);
  });
}

function toggleModeFields() {
  const mode = document.getElementById('s_mode').value;
  document.getElementById('field-day').style.display =
    (mode === 'weekly' || mode === 'monthly') ? 'block' : 'none';
  document.getElementById('field-time').style.display =
    (mode === 'custom') ? 'none' : 'block';
  document.getElementById('field-custom').style.display =
    (mode === 'custom') ? 'block' : 'none';
}

async function loadSettings() {
  const res = await fetch('/api/settings');
  const s = await res.json();
  document.getElementById('s_enabled').checked = s.auto_backup_enabled;
  document.getElementById('s_mode').value = s.auto_backup_mode || 'daily';
  document.getElementById('s_day').value = s.auto_backup_day || 1;
  document.getElementById('s_hour').value = s.auto_backup_hour ?? 3;
  document.getElementById('s_minute').value = s.auto_backup_minute ?? 0;
  document.getElementById('s_keep').value = s.keep_backups ?? 7;

  if (s.custom_backup_time) {
    const dt = new Date(s.custom_backup_time);
    const pad = n => String(n).padStart(2, '0');
    const local = dt.getFullYear() + '-' + pad(dt.getMonth() + 1) + '-' +
                  pad(dt.getDate()) + 'T' + pad(dt.getHours()) + ':' + pad(dt.getMinutes());
    document.getElementById('s_custom').value = local;
  }

  toggleModeFields();
}

async function saveSettings(e) {
  e.preventDefault();
  const mode = document.getElementById('s_mode').value;
  let customRFC = '';

  if (mode === 'custom') {
    const dtLocal = document.getElementById('s_custom').value;
    if (!dtLocal) {
      alert('Please pick a date and time');
      return;
    }
    const d = new Date(dtLocal);
    customRFC = d.toISOString();
  }

  const params = new URLSearchParams({
    auto_backup_enabled: document.getElementById('s_enabled').checked ? 'true' : 'false',
    auto_backup_mode: mode,
    auto_backup_day: document.getElementById('s_day').value,
    auto_backup_hour: document.getElementById('s_hour').value,
    auto_backup_minute: document.getElementById('s_minute').value,
    keep_backups: document.getElementById('s_keep').value,
    custom_backup_time: customRFC
  });

  const res = await fetch('/api/settings', { method: 'POST', body: params });
  alert(res.ok ? 'Settings saved' : 'Save failed: ' + await res.text());
}

async function loadBackups() {
  const res = await fetch('/api/backups/list');
  const folders = await res.json();
  const tbody = document.getElementById('backups-list');
  tbody.innerHTML = '';
  (folders || []).forEach(f => {
    const row = document.createElement('tr');
    row.innerHTML =
      '<td>' + f.name + '</td>' +
      '<td><button onclick="deleteBackup(\'' + f.name + '\')">Delete</button></td>';
    tbody.appendChild(row);
  });
}

async function deleteBackup(name) {
  if (!confirm('Permanently delete ' + name + ' from Google Drive?')) return;
  const res = await fetch('/api/backups/delete?name=' + encodeURIComponent(name), { method: 'POST' });
  alert(await res.text());
  loadBackups();
}

async function pruneNow() {
  if (!confirm('Delete all backups except the most recent N (per settings)?')) return;
  const res = await fetch('/api/backups/prune', { method: 'POST' });
  alert(await res.text());
  loadBackups();
}

// Refresh intervals
setInterval(loadSites, 15000);
setInterval(loadVPS, 30000);
setInterval(loadBackups, 60000);

// Initial load
loadVPS();
loadSites();
loadServerOptions();
loadSettings();
loadBackups();
</script>
</body>
</html>`
