// FDT.Scout dashboard -- vanilla JS, no framework/CDN dependency (this is served by the device
// itself over a self-signed cert on a LAN that may have no other internet-facing dependency, so
// pulling in a CDN chart/terminal library would be one more thing that can silently fail to load).

// ---- Session expiry -------------------------------------------------------
// Sessions idle-timeout server-side after 5 minutes (SessionStore in session.go). Wrapping fetch
// once here, rather than adding a 401 check to every individual call site below, means an expired
// session bounces to the login page from ANY api call, not just whichever ones happened to
// remember to check -- a real risk on a page with this many independent fetch() calls scattered
// across tabs. Login/logout themselves are exempt so a failed login attempt doesn't loop.
const _nativeFetch = window.fetch.bind(window);
window.fetch = async (...args) => {
  const res = await _nativeFetch(...args);
  const url = String(args[0]);
  if (res.status === 401 && url !== '/api/login' && url !== '/api/logout') {
    window.location.href = '/login';
  }
  return res;
};

// ---- Tabs ----------------------------------------------------------------
document.querySelectorAll('.tab').forEach((btn) => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.tab').forEach((b) => b.classList.remove('active'));
    document.querySelectorAll('.panel').forEach((p) => p.classList.remove('active'));
    btn.classList.add('active');
    document.getElementById('panel-' + btn.dataset.tab).classList.add('active');
    if (btn.dataset.tab === 'health') { loadSysinfo(); loadMetrics(); loadUsbDrives(); startProcessAutoRefresh(); }
    else stopProcessAutoRefresh();
    if (btn.dataset.tab === 'monitoring') loadMonitoringTab();
    if (btn.dataset.tab === 'apps') loadApps();
    if (btn.dataset.tab === 'docker') loadDockerTab();
    if (btn.dataset.tab === 'users') loadUsers();
    if (btn.dataset.tab === 'certs') { loadCertInfo(); loadConfig(); }
    if (btn.dataset.tab === 'lcd') { loadLCDStatus(); loadDisplayConfig(); }
    if (btn.dataset.tab === 'settings') { loadLCDStatus(); loadNetworkStatus(); loadNTPStatus(); loadDNSStatus(); loadTimezoneStatus(); loadPushbulletSettings(); loadDdnsSettings(); loadTailscaleStatus(); }
    if (btn.dataset.tab === 'about') loadAbout();
    if (btn.dataset.tab === 'terminal') connectTerminal();
  });
});

document.getElementById('logoutBtn').addEventListener('click', async () => {
  await fetch('/api/logout', { method: 'POST' });
  window.location.href = '/login';
});

// ---- Terminal --------------------------------------------------------
// Raw-byte PTY stream rendered into a scrolling <pre>. Not a full VT100 emulator: cursor-motion
// and color escape sequences are stripped rather than interpreted, same deliberate scope limit as
// the Windows app's own "drop to shell" pane -- plain readable text over pixel-perfect terminal
// fidelity.
let termSocket = null;
const ANSI_RE = /\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][A-Za-z0-9]|\x1b[=>]/g;

function connectTerminal() {
  if (termSocket && termSocket.readyState === WebSocket.OPEN) return;
  const output = document.getElementById('termOutput');
  const input = document.getElementById('termInput');
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  termSocket = new WebSocket(proto + '//' + window.location.host + '/ws/terminal');
  termSocket.binaryType = 'arraybuffer';

  termSocket.onopen = () => {
    input.disabled = false;
    document.getElementById('termCtrlC').disabled = false;
    document.getElementById('termCtrlD').disabled = false;
    appendTerm('\n[connected]\n');
    sendResize();
  };
  termSocket.onmessage = (ev) => {
    const bytes = new Uint8Array(ev.data);
    appendTerm(new TextDecoder().decode(bytes));
  };
  termSocket.onclose = () => {
    input.disabled = true;
    document.getElementById('termCtrlC').disabled = true;
    document.getElementById('termCtrlD').disabled = true;
    appendTerm('\n[disconnected]\n');
  };
  termSocket.onerror = () => appendTerm('\n[connection error]\n');
}

function appendTerm(text) {
  const output = document.getElementById('termOutput');
  output.textContent += text.replace(ANSI_RE, '');
  output.scrollTop = output.scrollHeight;
}

function sendResize() {
  if (!termSocket || termSocket.readyState !== WebSocket.OPEN) return;
  // Fixed-ish estimate from the output pane's pixel size -- good enough for line-wrapping
  // programs (ls, top, etc.) without needing a full character-metrics measurement.
  const output = document.getElementById('termOutput');
  const cols = Math.max(20, Math.floor(output.clientWidth / 8));
  const rows = Math.max(10, Math.floor(output.clientHeight / 17));
  termSocket.send(JSON.stringify({ type: 'resize', cols, rows }));
}

document.getElementById('termInput').addEventListener('keydown', (e) => {
  if (e.key !== 'Enter' || !termSocket || termSocket.readyState !== WebSocket.OPEN) return;
  termSocket.send(e.target.value + '\n');
  e.target.value = '';
});

document.getElementById('termCtrlC').addEventListener('click', () => {
  if (termSocket && termSocket.readyState === WebSocket.OPEN) termSocket.send('\x03');
});
document.getElementById('termCtrlD').addEventListener('click', () => {
  if (termSocket && termSocket.readyState === WebSocket.OPEN) termSocket.send('\x04');
});

// ---- Health / metrics ------------------------------------------------
async function loadMetrics() {
  const res = await fetch('/api/metrics');
  if (!res.ok) return;
  const samples = await res.json();
  drawSparkline('chartCpu', samples.map((s) => s.cpu), { maxValue: 100, suffix: '%' });
  drawSparkline('chartMem', samples.map((s) => s.mem), { maxValue: 100, suffix: '%' });
  drawSparkline('chartDiskRoot', samples.map((s) => s.diskRoot), { maxValue: 100, suffix: '%' });
  drawSparkline('chartNetRx', samples.map((s) => s.netRxKBs), { maxValue: null, suffix: ' KB/s' });
  drawSparkline('chartNetTx', samples.map((s) => s.netTxKBs), { maxValue: null, suffix: ' KB/s' });

  const volumeSamples = samples.filter((s) => s.diskVolume >= 0);
  const volumeCard = document.getElementById('volumeCard');
  if (volumeSamples.length > 0) {
    volumeCard.style.display = '';
    drawSparkline('chartDiskVolume', volumeSamples.map((s) => s.diskVolume), { maxValue: 100, suffix: '%' });
  } else {
    volumeCard.style.display = 'none';
  }
}

document.getElementById('refreshMetrics').addEventListener('click', loadMetrics);

// maxValue: fixed axis ceiling (percentages use 100); pass null to auto-scale to the data's own
// peak instead (throughput has no natural ceiling).
function drawSparkline(canvasId, values, opts) {
  const { maxValue, suffix } = opts || { maxValue: 100, suffix: '%' };
  const canvas = document.getElementById(canvasId);
  const ctx = canvas.getContext('2d');
  const w = canvas.width, h = canvas.height;
  ctx.clearRect(0, 0, w, h);

  const dataMax = values.length > 0 ? Math.max(...values) : 0;
  const axisMax = maxValue != null ? maxValue : Math.max(dataMax, 1);

  ctx.strokeStyle = '#333844';
  ctx.lineWidth = 1;
  for (let step = 0; step <= 4; step++) {
    const y = h - (step / 4) * (h - 20) - 10;
    ctx.beginPath();
    ctx.moveTo(0, y);
    ctx.lineTo(w, y);
    ctx.stroke();
  }

  if (values.length === 0) {
    ctx.fillStyle = '#8a92a3';
    ctx.font = '12px sans-serif';
    ctx.fillText('no data yet', 10, h / 2);
    return;
  }

  ctx.strokeStyle = '#4a9eff';
  ctx.lineWidth = 2;
  ctx.beginPath();
  values.forEach((v, i) => {
    const x = (i / Math.max(1, values.length - 1)) * w;
    const y = h - (Math.max(0, Math.min(axisMax, v)) / axisMax) * (h - 20) - 10;
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  });
  ctx.stroke();

  const last = values[values.length - 1];
  ctx.fillStyle = '#e4e6eb';
  ctx.font = '12px sans-serif';
  ctx.fillText(last.toFixed(1) + suffix, w - 70, 14);
}

// ---- Processes (top-style) ---------------------------------------------
let procRefreshTimer = null;

function startProcessAutoRefresh() {
  loadProcesses();
  stopProcessAutoRefresh();
  if (document.getElementById('procAutoRefresh').checked) {
    procRefreshTimer = setInterval(loadProcesses, 3000);
  }
}

function stopProcessAutoRefresh() {
  if (procRefreshTimer) {
    clearInterval(procRefreshTimer);
    procRefreshTimer = null;
  }
}

document.getElementById('procAutoRefresh').addEventListener('change', () => {
  if (document.getElementById('procAutoRefresh').checked) startProcessAutoRefresh();
  else stopProcessAutoRefresh();
});
document.getElementById('refreshProcesses').addEventListener('click', loadProcesses);

async function loadProcesses() {
  const res = await fetch('/api/processes');
  if (!res.ok) return;
  const snap = await res.json();

  const hours = Math.floor(snap.uptimeSecs / 3600);
  const mins = Math.floor((snap.uptimeSecs % 3600) / 60);
  document.getElementById('procSummary').textContent =
    `up ${hours}h ${mins}m -- load average: ${snap.loadAvg1.toFixed(2)}, ${snap.loadAvg5.toFixed(2)}, ${snap.loadAvg15.toFixed(2)}\n` +
    `${snap.processCount} processes -- mem: ${snap.memUsedMb.toFixed(0)} / ${snap.memTotalMb.toFixed(0)} MB used`;

  const tbody = document.querySelector('#procTable tbody');
  tbody.innerHTML = '';
  (snap.processes || []).forEach((p) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${p.pid}</td><td>${escapeHtml(p.user)}</td><td>${p.cpuPct.toFixed(1)}</td><td>${p.memPct.toFixed(1)}</td><td>${humanSize(p.rssKb * 1024)}</td><td class="muted">${escapeHtml(p.elapsed)}</td><td style="font-family:monospace">${escapeHtml(p.command)}</td><td></td>`;
    const killCell = tr.lastElementChild;
    const killBtn = document.createElement('button');
    killBtn.className = 'link-btn';
    killBtn.textContent = 'Kill';
    killBtn.addEventListener('click', () => killProcess(p.pid, p.command));
    killCell.appendChild(killBtn);
    tbody.appendChild(tr);
  });
}

async function killProcess(pid, command) {
  if (!confirm(`Send SIGTERM to PID ${pid} (${command})?`)) return;
  const res = await fetch(`/api/processes/${pid}/kill`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ signal: 'TERM' }),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) alert(body.error || 'Failed to kill process.');
  loadProcesses();
}

async function loadSysinfo() {
  const res = await fetch('/api/sysinfo');
  if (!res.ok) return;
  const s = await res.json();
  document.getElementById('sysSpecs').innerHTML = `
    <div>CPU: <strong>${escapeHtml(s.cpuModel || 'unknown')}</strong> (${s.cpuCores} core${s.cpuCores === 1 ? '' : 's'})</div>
    <div>RAM: <strong>${s.memTotalMb.toFixed(0)} MB</strong></div>
    <div>Disk (/): <strong>${s.rootTotalGb.toFixed(1)} GB</strong>${s.volumeTotalGb ? ` &nbsp; Disk (/volume): <strong>${s.volumeTotalGb.toFixed(1)} GB</strong>` : ''}</div>
    <div>Kernel: <strong>${escapeHtml(s.kernel || 'unknown')}</strong> (${escapeHtml(s.arch || 'unknown')})</div>
    <div>Hostname: <strong>${escapeHtml(s.hostname || 'unknown')}</strong></div>
  `;
}

// ---- Apps ------------------------------------------------------------
// Merges two things per row: live status (from /api/apps, systemctl/docker-backed) and, for
// anything not installed yet, a real install form (from /api/install -- the same bundled scripts
// CloudKey Wizard itself runs, executed locally here instead of over SSH).
let installCatalogCache = null;

async function loadApps() {
  const [statusRes, catalogRes] = await Promise.all([fetch('/api/apps'), fetch('/api/install')]);
  if (!statusRes.ok) return;
  const apps = await statusRes.json();
  if (catalogRes.ok) installCatalogCache = await catalogRes.json();
  const catalogById = Object.fromEntries((installCatalogCache || []).map((d) => [d.id, d]));

  const tbody = document.querySelector('#appsTable tbody');
  tbody.innerHTML = '';
  apps.forEach((app) => {
    const tr = document.createElement('tr');
    const statusText = !app.installed ? 'Not installed'
      : app.active ? 'Running' + (app.enabled ? '' : ' (not enabled at boot)')
      : 'Stopped';
    const statusColor = !app.installed ? '#8a92a3' : app.active ? '#4caf7d' : '#e0b050';
    tr.innerHTML = `
      <td>${escapeHtml(app.name)}</td>
      <td style="color:${statusColor}">${escapeHtml(statusText)}${app.count ? ` (${app.count})` : ''}</td>
      <td class="muted">${escapeHtml(app.detail || '')}</td>
      <td></td>
    `;
    const actionCell = tr.lastElementChild;
    if (app.toggleable) {
      const actions = app.active ? ['stop', 'disable'] : ['start', 'enable'];
      actions.forEach((action) => {
        const btn = document.createElement('button');
        btn.className = 'secondary';
        btn.style.marginRight = '6px';
        btn.textContent = action[0].toUpperCase() + action.slice(1);
        btn.addEventListener('click', () => doAppAction(app.id, action));
        actionCell.appendChild(btn);
      });
    }

    const def = catalogById[app.id];
    let formRow = null;
    if (!app.installed && def) {
      const installBtn = document.createElement('button');
      installBtn.textContent = 'Install';
      installBtn.addEventListener('click', () => {
        formRow.style.display = formRow.style.display === 'none' ? '' : 'none';
      });
      actionCell.appendChild(installBtn);

      formRow = buildInstallFormRow(def);
      formRow.style.display = 'none';
    }

    tbody.appendChild(tr);
    if (formRow) tbody.appendChild(formRow);
  });
}

function buildInstallFormRow(def) {
  const row = document.createElement('tr');
  const cell = document.createElement('td');
  cell.colSpan = 4;
  cell.style.background = '#1a1d22';
  cell.style.padding = '14px';

  const desc = document.createElement('p');
  desc.className = 'muted';
  desc.style.marginTop = '0';
  desc.textContent = def.description;
  cell.appendChild(desc);

  const form = document.createElement('form');
  form.className = 'inline-form';
  form.style.flexDirection = 'column';
  form.style.alignItems = 'stretch';

  (def.params || []).forEach((p) => {
    const label = document.createElement('label');
    label.style.maxWidth = '480px';
    label.innerHTML = `${escapeHtml(p.label)}${p.required ? ' *' : ''}`;
    const input = document.createElement(p.envVar === 'WG_CONFIG' ? 'textarea' : 'input');
    if (p.envVar === 'WG_CONFIG') {
      input.rows = 6;
      input.style.fontFamily = 'monospace';
    } else {
      input.type = 'text';
    }
    input.name = p.envVar;
    input.value = p.default || '';
    if (p.helpText) {
      const help = document.createElement('div');
      help.className = 'muted';
      help.style.fontSize = '11px';
      help.textContent = p.helpText;
      label.appendChild(input);
      label.appendChild(help);
    } else {
      label.appendChild(input);
    }
    form.appendChild(label);
  });

  const submitBtn = document.createElement('button');
  submitBtn.type = 'submit';
  submitBtn.textContent = `Install ${def.title}`;
  form.appendChild(submitBtn);

  const output = document.createElement('pre');
  output.className = 'term-output';
  output.style.display = 'none';
  output.style.marginTop = '10px';
  output.style.maxHeight = '260px';

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const params = {};
    (def.params || []).forEach((p) => { params[p.envVar] = form.elements[p.envVar].value; });

    submitBtn.disabled = true;
    submitBtn.textContent = 'Installing... (this can take a few minutes)';
    output.style.display = '';
    output.textContent = 'Running -- please wait, this page will update when it finishes.\n';

    try {
      const res = await fetch(`/api/install/${encodeURIComponent(def.id)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ params }),
      });
      const body = await res.json().catch(() => ({}));
      output.textContent = body.output || body.error || '(no output)';
      if (res.ok && body.exitCode === 0) {
        output.textContent += '\n\n--- Done ---';
        loadApps();
      }
    } catch (err) {
      output.textContent = 'Request failed: ' + err;
    } finally {
      submitBtn.disabled = false;
      submitBtn.textContent = `Install ${def.title}`;
    }
  });

  cell.appendChild(form);
  cell.appendChild(output);
  row.appendChild(cell);
  return row;
}

async function doAppAction(id, action) {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}/action`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
  });
  const body = await res.json().catch(() => ({}));
  setMsg('appsMsg', res.ok, res.ok ? `${action} succeeded.` : (body.error || 'failed'));
  loadApps();
}

document.getElementById('refreshApps').addEventListener('click', loadApps);

// ---- Docker ------------------------------------------------------------
// General-purpose container manager -- deliberately separate from the Apps tab's own
// kindDocker handling (which only ever knows about one specific, pre-named container, e.g.
// Home Assistant's). Trimmed scope: install/verify (one-way, not a real toggle -- something may
// already depend on Docker once installed), lifecycle control, run-from-image, log viewing, an
// explicit storage-location choice. No compose, no exec shell, no image search, no bulk ops.
async function loadDockerTab() {
  const res = await fetch('/api/docker');
  if (!res.ok) return;
  const status = await res.json();

  document.getElementById('dockerNotInstalled').style.display = status.installed ? 'none' : '';
  document.getElementById('dockerInstalled').style.display = status.installed ? '' : 'none';
  if (!status.installed) return;

  document.getElementById('dockerStatusText').textContent =
    `Docker ${status.version || '(version unknown)'} -- ${status.running ? 'running' : 'installed but not running'}, ${status.containerCount} container(s).`;

  const storageEl = document.getElementById('dockerStorageText');
  const moveBtn = document.getElementById('moveDockerStorageBtn');
  storageEl.textContent = `Data root: ${status.storageRoot}` +
    (status.volumeMounted ? (status.storageOnVolume ? ' (already on /volume).' : ' -- /volume is mounted and not yet used for this.') : ' -- no /volume mount detected on this device.');
  moveBtn.style.display = (status.volumeMounted && !status.storageOnVolume) ? '' : 'none';

  await loadDockerContainers();
}

async function loadDockerContainers() {
  const res = await fetch('/api/docker/containers');
  const tbody = document.querySelector('#dockerContainersTable tbody');
  tbody.innerHTML = '';
  if (!res.ok) return;
  const containers = await res.json();
  (containers || []).forEach((c) => {
    const tr = document.createElement('tr');
    const running = c.state === 'running';
    const statusColor = running ? '#4caf7d' : c.state === 'paused' ? '#e0b050' : '#8a92a3';
    tr.innerHTML = `
      <td>${escapeHtml(c.name)}</td>
      <td class="muted">${escapeHtml(c.image)}</td>
      <td style="color:${statusColor}">${escapeHtml(c.status)}</td>
      <td class="muted">${escapeHtml(c.ports || '')}</td>
      <td class="muted">${c.sizeMb ? c.sizeMb.toFixed(1) + ' MB' : ''}</td>
      <td></td>
    `;
    const actionCell = tr.lastElementChild;
    const actions = running ? ['stop', 'pause', 'restart', 'logs', 'remove'] : ['start', 'logs', 'remove'];
    actions.forEach((action) => {
      const btn = document.createElement('button');
      btn.className = 'secondary';
      btn.style.marginRight = '6px';
      btn.textContent = action[0].toUpperCase() + action.slice(1);
      btn.addEventListener('click', () => action === 'logs' ? showDockerLogs(c.id, c.name) : doDockerAction(c.id, action));
      actionCell.appendChild(btn);
    });
    tbody.appendChild(tr);
  });
}

async function doDockerAction(id, action) {
  if (action === 'remove' && !confirm('Remove this container? This does not delete its image, but any data not in a mounted volume is gone.')) return;
  const res = await fetch(`/api/docker/containers/${encodeURIComponent(id)}/${action}`, { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  setMsg('dockerMsg', res.ok, res.ok ? `${action} succeeded.` : (body.error || 'failed'));
  loadDockerContainers();
}

async function showDockerLogs(id, name) {
  const out = document.getElementById('dockerLogOutput');
  out.style.display = '';
  out.textContent = `Loading logs for ${name}...`;
  const res = await fetch(`/api/docker/containers/${encodeURIComponent(id)}/logs?lines=200`);
  const body = await res.json().catch(() => ({}));
  out.textContent = res.ok ? (body.log || '(no output)') : (body.error || 'failed to load logs');
}

document.getElementById('installDockerBtn').addEventListener('click', async () => {
  const btn = document.getElementById('installDockerBtn');
  btn.disabled = true;
  btn.textContent = 'Installing...';
  const res = await fetch('/api/docker/install', { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  btn.disabled = false;
  btn.textContent = 'Install Docker';
  setMsg('dockerInstallMsg', res.ok, res.ok ? 'Docker installed.' : (body.error || 'install failed'));
  if (res.ok) loadDockerTab();
});

document.getElementById('moveDockerStorageBtn').addEventListener('click', async () => {
  if (!confirm('Move Docker\'s image and container storage to /volume? Docker will be stopped briefly while the existing data is copied.')) return;
  const btn = document.getElementById('moveDockerStorageBtn');
  btn.disabled = true;
  btn.textContent = 'Moving...';
  const res = await fetch('/api/docker/storage', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path: '/volume/docker' }),
  });
  const body = await res.json().catch(() => ({}));
  btn.disabled = false;
  btn.textContent = 'Move to /volume';
  setMsg('dockerStorageMsg', res.ok, res.ok ? 'Storage moved.' : (body.error || 'move failed'));
  loadDockerTab();
});

document.getElementById('refreshDocker').addEventListener('click', loadDockerTab);

document.getElementById('dockerRunForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const splitList = (s) => s.split(',').map((x) => x.trim()).filter(Boolean);
  const req = {
    image: document.getElementById('dockerImage').value.trim(),
    name: document.getElementById('dockerName').value.trim(),
    ports: splitList(document.getElementById('dockerPorts').value),
    volumes: splitList(document.getElementById('dockerVolumes').value),
    env: splitList(document.getElementById('dockerEnv').value),
    restartPolicy: document.getElementById('dockerRestartPolicy').value,
    memoryLimit: document.getElementById('dockerMemLimit').value.trim(),
  };
  const submitBtn = e.target.querySelector('button[type="submit"]');
  const out = document.getElementById('dockerRunOutput');
  submitBtn.disabled = true;
  submitBtn.textContent = 'Pulling & running... (this can take a while on a first pull)';
  out.style.display = '';
  out.textContent = 'Running -- please wait, this page will update when it finishes.\n';

  try {
    const res = await fetch('/api/docker/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
    });
    const body = await res.json().catch(() => ({}));
    out.textContent = body.log || body.error || '(no output)';
    if (res.ok) {
      out.textContent += '\n\n--- Done ---';
      loadDockerContainers();
    }
  } catch (err) {
    out.textContent = String(err);
  } finally {
    submitBtn.disabled = false;
    submitBtn.textContent = 'Pull & run';
  }
});

// ---- About -----------------------------------------------------------
async function loadAbout() {
  const res = await fetch('/api/about');
  if (!res.ok) return;
  const about = await res.json();
  document.getElementById('aboutVersion').innerHTML = `<strong>FDT.Scout ${escapeHtml(about.version)}</strong> &mdash; built ${escapeHtml(about.buildDate)}`;
  document.getElementById('aboutText').textContent = about.aboutText;
  const log = document.getElementById('aboutChangelog');
  log.innerHTML = '';
  (about.changelog || []).forEach((entry) => {
    const div = document.createElement('div');
    div.className = 'info-block';
    div.style.marginBottom = '10px';
    div.innerHTML = `<strong>${escapeHtml(entry.version)}</strong> <span class="muted">${escapeHtml(entry.date)}</span><ul style="margin:6px 0 0 18px">${entry.notes.map((n) => `<li>${escapeHtml(n)}</li>`).join('')}</ul>`;
    log.appendChild(div);
  });
}

// ---- Users -------------------------------------------------------------
async function loadUsers() {
  await Promise.all([loadUsersTable(), loadAuthLog()]);
}

async function loadUsersTable() {
  const res = await fetch('/api/users');
  if (!res.ok) return;
  const users = await res.json();
  const tbody = document.querySelector('#usersTable tbody');
  tbody.innerHTML = '';
  users.forEach((u) => {
    const tr = document.createElement('tr');
    const created = new Date(u.createdAt).toLocaleString();
    const lastLogin = u.lastLogin ? new Date(u.lastLogin).toLocaleString() : 'never';
    tr.innerHTML = `<td>${escapeHtml(u.username)}</td><td>${escapeHtml(created)}</td><td>${escapeHtml(lastLogin)}</td><td></td>`;
    const delCell = tr.lastElementChild;
    const delBtn = document.createElement('button');
    delBtn.className = 'link-btn';
    delBtn.textContent = 'Remove';
    delBtn.addEventListener('click', () => removeUser(u.username));
    delCell.appendChild(delBtn);
    tbody.appendChild(tr);
  });
}

async function loadAuthLog() {
  const res = await fetch('/api/auth-log');
  if (!res.ok) return;
  const attempts = await res.json();
  const tbody = document.querySelector('#authLogTable tbody');
  tbody.innerHTML = '';
  attempts.forEach((a) => {
    const tr = document.createElement('tr');
    const when = new Date(a.time).toLocaleString();
    const resultColor = a.success ? '#4caf7d' : '#ff6b6b';
    tr.innerHTML = `<td>${escapeHtml(when)}</td><td>${escapeHtml(a.username)}</td><td style="color:${resultColor}">${a.success ? 'Success' : 'Failed'}</td><td class="muted">${escapeHtml(a.remoteIp)}</td>`;
    tbody.appendChild(tr);
  });
}

async function removeUser(username) {
  if (!confirm(`Remove account "${username}"?`)) return;
  const res = await fetch('/api/users/' + encodeURIComponent(username), { method: 'DELETE' });
  const body = await res.json().catch(() => ({}));
  setMsg('userMsg', res.ok, res.ok ? 'Removed.' : (body.error || 'failed'));
  if (res.ok) loadUsers();
}

document.getElementById('userForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const username = document.getElementById('newUsername').value;
  const password = document.getElementById('newPassword').value;
  const res = await fetch('/api/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const body = await res.json().catch(() => ({}));
  setMsg('userMsg', res.ok, res.ok ? 'Saved.' : (body.error || 'failed'));
  if (res.ok) {
    document.getElementById('userForm').reset();
    loadUsers();
  }
});

// ---- Certificates --------------------------------------------------------
async function loadCertInfo() {
  const res = await fetch('/api/cert/status');
  if (!res.ok) return;
  const info = await res.json();
  renderCertInfo(info);
}

function renderCertInfo(info) {
  const el = document.getElementById('certInfo');
  if (!info.loaded) {
    el.textContent = 'No certificate loaded.';
    return;
  }
  el.innerHTML = `
    <div>Subject: ${escapeHtml(info.subject || '(none)')}</div>
    <div>Type: ${info.selfSigned ? 'Self-signed' : 'CA-issued (' + escapeHtml(info.issuer || '') + ')'}</div>
    <div>Valid: ${new Date(info.notBefore).toLocaleDateString()} &ndash; ${new Date(info.notAfter).toLocaleDateString()}</div>
    <div>Names: ${(info.dnsNames || []).map(escapeHtml).join(', ') || '(none)'}</div>
  `;
}

document.getElementById('genCertBtn').addEventListener('click', async () => {
  if (!confirm('Generate a new self-signed certificate? Anyone with an open connection may need to reload.')) return;
  const res = await fetch('/api/cert/generate', { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    renderCertInfo(body);
    setMsg('certMsg', true, 'New self-signed certificate generated.');
  } else {
    setMsg('certMsg', false, body.error || 'failed');
  }
});

document.getElementById('certUploadForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const form = new FormData();
  form.append('cert', document.getElementById('certFile').files[0]);
  form.append('key', document.getElementById('keyFile').files[0]);
  const res = await fetch('/api/cert/upload', { method: 'POST', body: form });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    renderCertInfo(body);
    setMsg('certMsg', true, 'Certificate installed.');
    e.target.reset();
  } else {
    setMsg('certMsg', false, body.error || 'failed');
  }
});

// ---- Listening port / redirect -----------------------------------------
async function loadConfig() {
  const res = await fetch('/api/config');
  if (!res.ok) return;
  const cfg = await res.json();
  document.getElementById('httpsPort').value = cfg.port;
  document.getElementById('redirectHttp').checked = cfg.redirectHttp;
  document.getElementById('configInfo').innerHTML =
    `Currently listening on port <strong>${cfg.port}</strong>` +
    (cfg.redirectHttp ? ', with port 80 redirecting to it.' : '.');
}

document.getElementById('configForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const port = parseInt(document.getElementById('httpsPort').value, 10);
  const redirectHttp = document.getElementById('redirectHttp').checked;
  if (!port || port < 1 || port > 65535) {
    setMsg('configMsg', false, 'Enter a valid port (1-65535).');
    return;
  }
  const changingPort = String(port) !== String(window.location.port || (window.location.protocol === 'https:' ? 443 : 80));
  if (!confirm(`This restarts FDT.Scout${changingPort ? ` and moves it to port ${port}` : ''}. Continue?`)) return;

  const res = await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ port, redirectHttp }),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    if (changingPort) {
      setMsg('configMsg', true, `Restarting -- reconnect at https://${window.location.hostname}:${port}/ in a few seconds.`);
    } else {
      setMsg('configMsg', true, 'Restarting to apply changes -- this page will reconnect automatically.');
      setTimeout(() => window.location.reload(), 4000);
    }
  } else {
    setMsg('configMsg', false, body.error || 'failed');
  }
});

// ---- Front panel / LCD -----------------------------------------------
async function loadLCDStatus() {
  const res = await fetch('/api/lcd');
  if (!res.ok) return;
  const status = await res.json();
  renderLCDStatus(status);
  document.getElementById('newHostname').value = status.hostname;
}

function renderLCDStatus(status) {
  const el = document.getElementById('lcdStatus');
  let html = `
    <div>Current hostname: <strong>${escapeHtml(status.hostname)}</strong></div>
    <div>LCD app (cloudkey.service): ${status.cloudkeyInstalled ? (status.cloudkeyActive ? 'installed, running' : 'installed, not running') : 'not installed'}</div>
  `;
  if (status.ckUiInstalled && status.ckUiActive) {
    html += `<div style="color:#ff6b6b">Warning: UniFi's original LCD app (ck-ui.service) is still active. It very likely still owns the display, which explains the panel not updating regardless of hostname or cloudkey.service's state.</div>`;
  }
  if (status.apparmorBlocking) {
    html += `<div style="color:#ff6b6b;margin-top:8px">
      <div>Confirmed in the kernel log: AppArmor is blocking cloudkey from opening the display (profile: <strong>${escapeHtml(status.apparmorProfile || 'unknown')}</strong>). This is why the panel only ever shows the UniFi logo, regardless of hostname or service state.</div>
      <details style="margin-top:6px"><summary style="cursor:pointer">Exact denial line</summary><pre style="white-space:pre-wrap;font-size:12px;margin-top:6px">${escapeHtml(status.apparmorDenialLine || '')}</pre></details>
      <button id="apparmorFixBtn" class="secondary" style="margin-top:8px">Attempt fix (set this profile to complain mode)</button>
      <p class="muted" style="font-size:11px;margin-top:4px">This only changes the ONE profile identified above to log-but-allow instead of block -- it does not disable AppArmor system-wide. Requires apparmor-utils (aa-complain) to be installed; reports plainly if it isn't.</p>
    </div>`;
  }
  if (status.warnings && status.warnings.length > 0) {
    html += `<div style="color:#e0b050;margin-top:8px">${status.warnings.map(escapeHtml).join('<br>')}</div>`;
  }
  if (status.recentLog) {
    html += `<details style="margin-top:8px"><summary style="cursor:pointer;color:#8a92a3">Recent cloudkey.service log</summary><pre style="white-space:pre-wrap;font-size:12px;margin-top:6px">${escapeHtml(status.recentLog)}</pre></details>`;
  }
  el.innerHTML = html;

  const fixBtn = document.getElementById('apparmorFixBtn');
  if (fixBtn) {
    fixBtn.addEventListener('click', async () => {
      fixBtn.disabled = true;
      fixBtn.textContent = 'Attempting...';
      const res = await fetch('/api/lcd/apparmor-fix', { method: 'POST' });
      const body = await res.json().catch(() => ({}));
      if (res.ok) {
        setMsg('lcdMsg', true, 'Profile set to complain mode and cloudkey.service restarted -- check the physical panel now.');
        renderLCDStatus(body);
      } else {
        setMsg('lcdMsg', false, body.error || 'failed');
        fixBtn.disabled = false;
        fixBtn.textContent = 'Attempt fix (set this profile to complain mode)';
      }
    });
  }
}

document.getElementById('hostnameForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const hostname = document.getElementById('newHostname').value;
  const res = await fetch('/api/lcd', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ hostname }),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    renderLCDStatus(body);
    setMsg('hostnameMsg', true, 'Hostname updated.');
  } else {
    setMsg('hostnameMsg', false, body.error || 'failed');
  }
});

// ---- Front panel screens/widgets editor -------------------------------
// The physical panel is small (a handful of short lines per screen), so each screen is capped at 4
// widgets here to match RenderWidget/displayconfig.go server-side -- the save call would reject an
// oversized screen anyway, but catching it in the UI avoids a round-trip just to find out.
const WIDGET_TYPES = [
  { value: 'hostname', label: 'Hostname' },
  { value: 'ip', label: 'IP address' },
  { value: 'time', label: 'Time' },
  { value: 'cpu', label: 'CPU %' },
  { value: 'mem', label: 'RAM %' },
  { value: 'diskRoot', label: 'Disk (/) %' },
  { value: 'diskVolume', label: 'Disk (/volume) %' },
  { value: 'uptime', label: 'Uptime' },
  { value: 'customText', label: 'Custom text' },
];

let displayConfigCache = null;
let lcdPreviewData = { hostname: '', ip: '', cpu: 0, mem: 0, diskRoot: 0, diskVolume: null };

async function loadDisplayConfig() {
  const [cfgRes, sysRes, metricsRes, netRes] = await Promise.all([
    fetch('/api/display'), fetch('/api/sysinfo'), fetch('/api/metrics'), fetch('/api/network'),
  ]);
  if (!cfgRes.ok) return;
  displayConfigCache = await cfgRes.json();
  if (sysRes.ok) lcdPreviewData.hostname = (await sysRes.json()).hostname || '';
  if (metricsRes.ok) {
    const samples = await metricsRes.json();
    const last = samples[samples.length - 1];
    if (last) {
      lcdPreviewData.cpu = last.cpu; lcdPreviewData.mem = last.mem;
      lcdPreviewData.diskRoot = last.diskRoot; lcdPreviewData.diskVolume = last.diskVolume;
    }
  }
  if (netRes.ok) lcdPreviewData.ip = ((await netRes.json()).addresses || [])[0] || '';

  document.getElementById('displayEnabled').checked = displayConfigCache.enabled;
  document.getElementById('displayCycleSeconds').value = displayConfigCache.cycleSeconds || 10;
  renderScreensEditor();
}

function renderScreensEditor() {
  const container = document.getElementById('screensEditor');
  container.innerHTML = '';
  (displayConfigCache.screens || []).forEach((screen, screenIdx) => {
    const card = document.createElement('div');
    card.className = 'info-block';
    card.style.marginBottom = '10px';

    const header = document.createElement('div');
    header.style.cssText = 'display:flex;align-items:center;gap:8px;margin-bottom:8px';

    const nameInput = document.createElement('input');
    nameInput.type = 'text';
    nameInput.value = screen.name || '';
    nameInput.placeholder = 'Screen name';
    nameInput.style.maxWidth = '220px';
    nameInput.addEventListener('input', () => { screen.name = nameInput.value; renderPreviews(); });
    header.appendChild(nameInput);

    const removeScreenBtn = document.createElement('button');
    removeScreenBtn.className = 'link-btn';
    removeScreenBtn.textContent = 'Remove screen';
    removeScreenBtn.addEventListener('click', () => {
      if (displayConfigCache.screens.length <= 1) { alert('At least one screen is required.'); return; }
      displayConfigCache.screens.splice(screenIdx, 1);
      renderScreensEditor();
    });
    header.appendChild(removeScreenBtn);
    card.appendChild(header);

    const widgetsList = document.createElement('div');
    (screen.widgets || []).forEach((widget, widgetIdx) => widgetsList.appendChild(buildWidgetRow(screen, widget, widgetIdx)));
    card.appendChild(widgetsList);

    const addWidgetBtn = document.createElement('button');
    addWidgetBtn.className = 'secondary';
    addWidgetBtn.textContent = 'Add line';
    addWidgetBtn.disabled = (screen.widgets || []).length >= 4;
    addWidgetBtn.addEventListener('click', () => {
      if (!screen.widgets) screen.widgets = [];
      if (screen.widgets.length >= 4) return;
      screen.widgets.push({ type: 'customText', label: '', customText: '' });
      renderScreensEditor();
    });
    card.appendChild(addWidgetBtn);

    container.appendChild(card);
  });
  renderPreviews();
}

function buildWidgetRow(screen, widget, widgetIdx) {
  const row = document.createElement('div');
  row.style.cssText = 'display:flex;align-items:center;gap:8px;margin-bottom:6px';

  const select = document.createElement('select');
  select.style.cssText = 'padding:6px 8px;background:#262a31;border:1px solid #333844;border-radius:4px;color:#e4e6eb';
  WIDGET_TYPES.forEach((t) => {
    const opt = document.createElement('option');
    opt.value = t.value;
    opt.textContent = t.label;
    if (widget.type === t.value) opt.selected = true;
    select.appendChild(opt);
  });
  select.addEventListener('change', () => { widget.type = select.value; renderScreensEditor(); });
  row.appendChild(select);

  const labelInput = document.createElement('input');
  labelInput.type = 'text';
  labelInput.placeholder = 'label (optional)';
  labelInput.value = widget.label || '';
  labelInput.style.maxWidth = '140px';
  labelInput.addEventListener('input', () => { widget.label = labelInput.value; renderPreviews(); });
  row.appendChild(labelInput);

  if (widget.type === 'customText') {
    const textInput = document.createElement('input');
    textInput.type = 'text';
    textInput.placeholder = 'text';
    textInput.value = widget.customText || '';
    textInput.style.maxWidth = '160px';
    textInput.addEventListener('input', () => { widget.customText = textInput.value; renderPreviews(); });
    row.appendChild(textInput);
  }

  const removeBtn = document.createElement('button');
  removeBtn.className = 'link-btn';
  removeBtn.textContent = 'Remove';
  removeBtn.addEventListener('click', () => {
    screen.widgets.splice(widgetIdx, 1);
    renderScreensEditor();
  });
  row.appendChild(removeBtn);

  return row;
}

document.getElementById('addScreenBtn').addEventListener('click', () => {
  if (!displayConfigCache) return;
  displayConfigCache.screens = displayConfigCache.screens || [];
  displayConfigCache.screens.push({ name: `Screen ${displayConfigCache.screens.length + 1}`, widgets: [{ type: 'hostname', label: '' }] });
  renderScreensEditor();
});

document.getElementById('saveDisplayBtn').addEventListener('click', async () => {
  if (!displayConfigCache) return;
  displayConfigCache.enabled = document.getElementById('displayEnabled').checked;
  displayConfigCache.cycleSeconds = parseInt(document.getElementById('displayCycleSeconds').value, 10) || 10;
  const res = await fetch('/api/display', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(displayConfigCache),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    displayConfigCache = body;
    renderScreensEditor();
    setMsg('displayMsg', true, 'Saved -- the panel updates immediately.');
  } else {
    setMsg('displayMsg', false, body.error || 'failed');
  }
});

// Approximate preview using values already fetched for this tab (sysinfo/metrics/network) -- not
// meant to be pixel-exact against the real monochrome panel, just "does this look roughly right."
function renderPreviews() {
  const container = document.getElementById('screenPreviews');
  container.innerHTML = '';
  if (!displayConfigCache) return;
  (displayConfigCache.screens || []).forEach((screen) => {
    const box = document.createElement('div');
    box.style.cssText = 'background:#0d1f0d;border:2px solid #333844;border-radius:4px;padding:8px;width:180px;font-family:monospace;font-size:12px;color:#7CFC7C;white-space:pre-line;line-height:1.5';
    box.textContent = (screen.widgets || []).map(previewLineFor).join('\n') || '(empty)';
    container.appendChild(box);
  });
}

function previewLineFor(widget) {
  const prefix = widget.label ? widget.label + ': ' : '';
  switch (widget.type) {
    case 'hostname': return prefix + (lcdPreviewData.hostname || 'hostname');
    case 'ip': return prefix + (lcdPreviewData.ip || '(no ip)');
    case 'time': return prefix + new Date().toLocaleTimeString();
    case 'cpu': return prefix + 'CPU ' + lcdPreviewData.cpu.toFixed(0) + '%';
    case 'mem': return prefix + 'RAM ' + lcdPreviewData.mem.toFixed(0) + '%';
    case 'diskRoot': return prefix + 'Disk ' + lcdPreviewData.diskRoot.toFixed(0) + '%';
    case 'diskVolume': return prefix + (lcdPreviewData.diskVolume != null && lcdPreviewData.diskVolume >= 0 ? 'Vol ' + lcdPreviewData.diskVolume.toFixed(0) + '%' : '(no volume)');
    case 'uptime': return prefix + 'uptime';
    case 'customText': return prefix + (widget.customText || '');
    default: return prefix + '?';
  }
}

// ---- Settings: NTP, DNS -------------------------------------------------
async function loadNTPStatus() {
  const res = await fetch('/api/ntp');
  if (!res.ok) return;
  const status = await res.json();
  const el = document.getElementById('ntpStatus');
  const form = document.getElementById('ntpForm');
  if (!status.available) {
    el.innerHTML = `<div class="muted">${escapeHtml(status.detail || 'NTP not available on this device.')}</div>`;
    form.style.display = 'none';
    return;
  }
  form.style.display = '';
  el.innerHTML = `
    <div>Status: <strong style="color:${status.synchronized ? '#4caf7d' : '#e0b050'}">${status.synchronized ? 'Synchronized' : 'Not synchronized'}</strong></div>
    <div>Servers: ${escapeHtml((status.servers || []).join(', ') || 'system default')}</div>
  `;
  document.getElementById('ntpEnabled').checked = status.enabled;
  document.getElementById('ntpServers').value = (status.servers || []).join(' ');
}

document.getElementById('ntpForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const enabled = document.getElementById('ntpEnabled').checked;
  const servers = document.getElementById('ntpServers').value.trim().split(/\s+/).filter(Boolean);
  const res = await fetch('/api/ntp', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled, servers }),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    setMsg('ntpMsg', true, 'Saved.');
    loadNTPStatus();
  } else {
    setMsg('ntpMsg', false, body.error || 'failed');
  }
});

async function loadDNSStatus() {
  const res = await fetch('/api/dns');
  if (!res.ok) return;
  const status = await res.json();
  document.getElementById('dnsStatus').innerHTML = `<div>Managed by: <strong>${escapeHtml(status.managedBy)}</strong></div>
    <div>Servers: ${escapeHtml((status.servers || []).join(', ') || 'none')}</div>`;
  document.getElementById('dnsServers').value = (status.servers || []).join(' ');
}

document.getElementById('dnsForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const servers = document.getElementById('dnsServers').value.trim().split(/\s+/).filter(Boolean);
  if (servers.length === 0) {
    setMsg('dnsMsg', false, 'Enter at least one DNS server.');
    return;
  }
  const res = await fetch('/api/dns', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ servers }),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    setMsg('dnsMsg', true, 'Applied.');
    loadDNSStatus();
  } else {
    setMsg('dnsMsg', false, body.error || 'failed');
  }
});

// Timezone list comes straight from this device's own tzdata (`timedatectl list-timezones`) --
// real live data, not a curated guess (unlike CloudKeyWizard.exe's own timezone dropdown, which has
// no natural moment to SSH in and query this before the user has even connected).
async function loadTimezoneStatus() {
  const res = await fetch('/api/timezone');
  if (!res.ok) return;
  const status = await res.json();
  const select = document.getElementById('timezoneSelect');
  select.innerHTML = '';
  (status.zones && status.zones.length > 0 ? status.zones : [status.current || 'UTC']).forEach((z) => {
    const opt = document.createElement('option');
    opt.value = z;
    opt.textContent = z;
    if (z === status.current) opt.selected = true;
    select.appendChild(opt);
  });
}

document.getElementById('timezoneForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const timezone = document.getElementById('timezoneSelect').value;
  const res = await fetch('/api/timezone', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ timezone }),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    setMsg('timezoneMsg', true, 'Applied.');
  } else {
    setMsg('timezoneMsg', false, body.error || 'failed');
  }
});

// ---- Shared helpers --------------------------------------------------
function setMsg(elId, ok, text) {
  const el = document.getElementById(elId);
  el.textContent = text;
  el.className = 'msg ' + (ok ? 'ok' : 'error');
}

function escapeHtml(s) {
  const div = document.createElement('div');
  div.textContent = String(s);
  return div.innerHTML;
}

// ---- Network / static IP -------------------------------------------------
document.getElementById('netMode').addEventListener('change', updateNetFormVisibility);
function updateNetFormVisibility() {
  const isStatic = document.getElementById('netMode').value === 'static';
  document.getElementById('netAddressLabel').style.display = isStatic ? '' : 'none';
  document.getElementById('netGatewayLabel').style.display = isStatic ? '' : 'none';
}
updateNetFormVisibility();

async function loadNetworkStatus() {
  const res = await fetch('/api/network');
  if (!res.ok) return;
  const status = await res.json();
  let html = `<div>Interface: <strong>${escapeHtml(status.interface || 'unknown')}</strong></div>
    <div>Address: <strong>${escapeHtml((status.addresses || []).join(', ') || 'none')}</strong></div>
    <div>Gateway: <strong>${escapeHtml(status.gateway || 'none')}</strong></div>`;
  if (status.pending) {
    const mins = Math.max(0, Math.ceil(status.pending.secondsRemaining / 60));
    html += `<div style="color:#e0b050;margin-top:6px">A change to ${escapeHtml(status.pending.newAddress || status.pending.newMode)} is pending confirmation -- reverts automatically in about ${mins} minute(s) unless accepted.</div>`;
  }
  document.getElementById('netStatus').innerHTML = html;
}

document.getElementById('netForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const mode = document.getElementById('netMode').value;
  const address = document.getElementById('netAddress').value.trim();
  const gateway = document.getElementById('netGateway').value.trim();
  if (mode === 'static' && !address) {
    setMsg('netMsg', false, 'Enter an address (CIDR, e.g. 192.168.1.50/24).');
    return;
  }
  if (!confirm(`Apply this now? You have 5 minutes to reconnect and click "Accept IP changes" or it reverts automatically. This device may become briefly unreachable.`)) return;

  const res = await fetch('/api/network/apply', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode, address, gateway }),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    setMsg('netMsg', true, 'Applied -- reconnect at the new address within 5 minutes to keep it.');
    loadNetworkStatus();
  } else {
    setMsg('netMsg', false, body.error || 'failed');
  }
});

// ---- USB storage + file browser -------------------------------------------
async function loadUsbDrives() {
  const res = await fetch('/api/storage/usb');
  if (!res.ok) return;
  const drives = await res.json();
  const tbody = document.querySelector('#usbTable tbody');
  tbody.innerHTML = '';
  if (drives.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" class="muted">No USB drives detected.</td></tr>';
    return;
  }
  drives.forEach((d) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${escapeHtml(d.device)}</td><td>${escapeHtml(d.size)}</td><td>${escapeHtml(d.fstype || 'unknown')}</td><td>${d.mounted ? 'Mounted at ' + escapeHtml(d.mountPoint) : 'Not mounted'}</td><td></td>`;
    const actionCell = tr.lastElementChild;
    if (d.mounted) {
      const browseBtn = document.createElement('button');
      browseBtn.className = 'secondary';
      browseBtn.style.marginRight = '6px';
      browseBtn.textContent = 'Browse';
      browseBtn.addEventListener('click', () => openFileBrowser(d.device));
      actionCell.appendChild(browseBtn);

      const unmountBtn = document.createElement('button');
      unmountBtn.className = 'secondary';
      unmountBtn.textContent = 'Unmount';
      unmountBtn.addEventListener('click', () => usbAction(d.device, 'unmount'));
      actionCell.appendChild(unmountBtn);
    } else {
      const mountBtn = document.createElement('button');
      mountBtn.className = 'secondary';
      mountBtn.textContent = 'Mount';
      mountBtn.addEventListener('click', () => usbAction(d.device, 'mount'));
      actionCell.appendChild(mountBtn);
    }
    tbody.appendChild(tr);
  });
}

document.getElementById('refreshUsb').addEventListener('click', loadUsbDrives);

async function usbAction(device, action) {
  const res = await fetch(`/api/storage/usb/${encodeURIComponent(device)}/${action}`, { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  setMsg('usbMsg', res.ok, res.ok ? `${action === 'mount' ? 'Mounted' : 'Unmounted'}.` : (body.error || 'failed'));
  loadUsbDrives();
}

let fileBrowserDevice = null;
let fileBrowserPath = '/';

function openFileBrowser(device) {
  fileBrowserDevice = device;
  fileBrowserPath = '/';
  document.getElementById('fileBrowser').style.display = '';
  loadFileList();
}

document.getElementById('fileBrowserClose').addEventListener('click', () => {
  document.getElementById('fileBrowser').style.display = 'none';
  fileBrowserDevice = null;
});

async function loadFileList() {
  document.getElementById('fileBrowserPath').textContent = `${fileBrowserDevice}:${fileBrowserPath}`;
  const res = await fetch(`/api/files?device=${encodeURIComponent(fileBrowserDevice)}&path=${encodeURIComponent(fileBrowserPath)}`);
  const tbody = document.querySelector('#fileBrowserTable tbody');
  tbody.innerHTML = '';
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    tbody.innerHTML = `<tr><td colspan="3" class="msg error">${escapeHtml(body.error || 'failed to list directory')}</td></tr>`;
    return;
  }
  const entries = await res.json();

  if (fileBrowserPath !== '/') {
    const upRow = document.createElement('tr');
    upRow.innerHTML = '<td>&hellip; (up)</td><td></td><td></td>';
    upRow.style.cursor = 'pointer';
    upRow.addEventListener('click', () => {
      fileBrowserPath = fileBrowserPath.replace(/\/[^/]+\/?$/, '') || '/';
      loadFileList();
    });
    tbody.appendChild(upRow);
  }

  (entries || []).sort((a, b) => (b.isDir - a.isDir) || a.name.localeCompare(b.name)).forEach((entry) => {
    const tr = document.createElement('tr');
    const modified = new Date(entry.modTime).toLocaleString();
    const sizeText = entry.isDir ? '' : humanSize(entry.size);
    tr.innerHTML = `<td>${entry.isDir ? '&#128193; ' : '&#128196; '}${escapeHtml(entry.name)}</td><td>${sizeText}</td><td class="muted">${escapeHtml(modified)}</td>`;
    if (entry.isDir) {
      tr.style.cursor = 'pointer';
      tr.addEventListener('click', () => {
        fileBrowserPath = (fileBrowserPath === '/' ? '' : fileBrowserPath) + '/' + entry.name;
        loadFileList();
      });
    } else {
      tr.style.cursor = 'pointer';
      tr.title = 'Click to download';
      tr.addEventListener('click', () => {
        const filePath = (fileBrowserPath === '/' ? '' : fileBrowserPath) + '/' + entry.name;
        window.open(`/api/files/download?device=${encodeURIComponent(fileBrowserDevice)}&path=${encodeURIComponent(filePath)}`, '_blank');
      });
    }
    tbody.appendChild(tr);
  });
}

function humanSize(bytes) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = bytes, unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return size.toFixed(1) + ' ' + units[unit];
}

// ---- "Accept IP changes" splash -------------------------------------------
// Checked on every authenticated page load, not gated behind a specific tab -- covers "presented
// at login" naturally, since this is the first thing that runs after landing on the dashboard.
async function checkPendingIPChange() {
  const res = await fetch('/api/network');
  if (!res.ok) return;
  const status = await res.json();
  if (!status.pending) return;

  const splash = document.getElementById('ipSplash');
  splash.style.display = 'flex';
  document.getElementById('ipSplashDetail').textContent =
    `Changed to ${status.pending.newAddress || status.pending.newMode} at ${new Date(status.pending.appliedAt).toLocaleTimeString()}.`;

  const tick = () => {
    const secs = Math.max(0, Math.round((new Date(status.pending.revertAt) - Date.now()) / 1000));
    document.getElementById('ipSplashCountdown').textContent =
      secs > 0 ? `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}` : 'reverting...';
    if (secs <= 0) clearInterval(interval);
  };
  tick();
  const interval = setInterval(tick, 1000);

  document.getElementById('ipSplashAccept').addEventListener('click', async () => {
    const r = await fetch('/api/network/confirm', { method: 'POST' });
    if (r.ok) {
      clearInterval(interval);
      splash.style.display = 'none';
    }
  }, { once: true });
}

// ---- Monitoring tab -----------------------------------------------------
// What the device watches on the user's behalf -- separate from Health (how the device itself is
// doing). Loaded all at once when the tab is opened; each section degrades gracefully if its own
// fetch fails (a bad WAN speed test, say, shouldn't blank the whole tab).
function loadMonitoringTab() {
  loadMonitors();
  loadWanSpeed();
  loadPublicIp();
  loadLanList();
  loadLogsList();
  loadTasks();
}

// -- Watched hosts (ping/TCP/HTTP/DNS monitors) --
let monitorsCache = [];

async function loadMonitors() {
  const res = await fetch('/api/monitors');
  if (!res.ok) return;
  monitorsCache = await res.json();
  const tbody = document.querySelector('#monitorsTable tbody');
  tbody.innerHTML = '';
  monitorsCache.forEach((m) => {
    const tr = document.createElement('tr');
    const statusText = !m.lastResult ? 'no data yet' : (m.lastResult.up ? 'up' : 'DOWN');
    const statusColor = !m.lastResult ? '#8a92a3' : (m.lastResult.up ? '#4caf7d' : '#ff6b6b');
    const uptimeText = m.lastResult ? `${m.uptimePct.toFixed(1)}%` : '';
    const notifyBadge = m.notifyPushbullet ? ' <span title="Notifies via Pushbullet" style="opacity:0.8">🔔</span>' : '';
    tr.innerHTML = `<td>${escapeHtml(m.label)}${notifyBadge}</td><td>${escapeHtml(m.type)}</td><td class="muted">${escapeHtml(m.target)}${m.port ? ':' + m.port : ''}</td>
      <td style="color:${statusColor}">${statusText}</td><td>${uptimeText}</td><td></td>`;
    const actionCell = tr.lastElementChild;
    const runBtn = document.createElement('button');
    runBtn.className = 'link-btn';
    runBtn.style.color = '#4a9eff';
    runBtn.textContent = 'Check now';
    runBtn.addEventListener('click', () => runMonitorNow(m.id));
    actionCell.appendChild(runBtn);
    const delBtn = document.createElement('button');
    delBtn.className = 'link-btn';
    delBtn.style.marginLeft = '10px';
    delBtn.textContent = 'Remove';
    delBtn.addEventListener('click', () => removeMonitor(m.id));
    actionCell.appendChild(delBtn);
    tbody.appendChild(tr);
  });
}

async function runMonitorNow(id) {
  await fetch(`/api/monitors/${encodeURIComponent(id)}/run`, { method: 'POST' });
  loadMonitors();
}

function removeMonitor(id) {
  const updated = monitorsCache.filter((m) => m.id !== id);
  saveMonitors(updated);
}

async function saveMonitors(list) {
  const res = await fetch('/api/monitors', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(list),
  });
  const body = await res.json().catch(() => ([]));
  if (res.ok) {
    monitorsCache = body;
    loadMonitors();
  } else {
    setMsg('monitorsMsg', false, body.error || 'failed');
  }
}

document.getElementById('addMonitorBtn').addEventListener('click', () => {
  document.getElementById('monitorForm').style.display = '';
});
document.getElementById('cancelMonitorBtn').addEventListener('click', () => {
  document.getElementById('monitorForm').style.display = 'none';
});
document.getElementById('monType').addEventListener('change', updateMonitorFormFields);
function updateMonitorFormFields() {
  const type = document.getElementById('monType').value;
  document.getElementById('monPortLabel').style.display = type === 'tcp' ? '' : 'none';
  document.getElementById('monExpectLabel').style.display = type === 'http' ? '' : 'none';
}
updateMonitorFormFields();

document.getElementById('saveMonitorBtn').addEventListener('click', () => {
  const label = document.getElementById('monLabel').value.trim();
  const type = document.getElementById('monType').value;
  const target = document.getElementById('monTarget').value.trim();
  const port = parseInt(document.getElementById('monPort').value, 10) || 0;
  const expectedText = document.getElementById('monExpect').value.trim();
  const intervalSecs = parseInt(document.getElementById('monInterval').value, 10) || 60;
  const notifyPushbullet = document.getElementById('monNotify').checked;
  if (!label || !target) {
    setMsg('monitorsMsg', false, 'Label and target are required.');
    return;
  }
  const updated = monitorsCache.concat([{ label, type, target, port, expectedText, intervalSecs, enabled: true, notifyPushbullet }]);
  saveMonitors(updated).then(() => {
    document.getElementById('monitorForm').style.display = 'none';
    document.getElementById('monLabel').value = '';
    document.getElementById('monTarget').value = '';
    document.getElementById('monNotify').checked = false;
    setMsg('monitorsMsg', true, 'Saved.');
  });
});

// -- WAN speed test --
async function loadWanSpeed() {
  const res = await fetch('/api/wanspeed');
  if (!res.ok) return;
  const samples = await res.json();
  drawSparkline('chartWanDown', samples.map((s) => s.downloadMbps), { maxValue: null, suffix: ' Mbps' });
  drawSparkline('chartWanUp', samples.map((s) => s.uploadMbps), { maxValue: null, suffix: ' Mbps' });
  drawSparkline('chartWanLatency', samples.map((s) => s.latencyMs), { maxValue: null, suffix: ' ms' });
}

document.getElementById('runSpeedTestBtn').addEventListener('click', async () => {
  const btn = document.getElementById('runSpeedTestBtn');
  btn.disabled = true;
  btn.textContent = 'Testing...';
  const res = await fetch('/api/wanspeed/run', { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  btn.disabled = false;
  btn.textContent = 'Test now (takes ~20-30s)';
  if (res.ok && !body.error) {
    setMsg('wanSpeedMsg', true, `Down ${body.downloadMbps.toFixed(1)} Mbps, up ${body.uploadMbps.toFixed(1)} Mbps, latency ${body.latencyMs.toFixed(0)}ms.`);
    loadWanSpeed();
  } else {
    setMsg('wanSpeedMsg', false, body.error || 'Speed test failed.');
  }
});

// -- Public IP --
async function loadPublicIp() {
  const res = await fetch('/api/publicip');
  if (!res.ok) return;
  const history = await res.json();
  const el = document.getElementById('publicIpStatus');
  if (history.length === 0) {
    el.innerHTML = '<div class="muted">No data yet -- checked every 10 minutes.</div>';
    return;
  }
  const current = history[history.length - 1];
  el.innerHTML = `<div>Current: <strong>${escapeHtml(current.ip)}</strong></div>
    <div class="muted">Last changed: ${new Date(current.t).toLocaleString()}</div>
    <div class="muted">${history.length} recorded change${history.length === 1 ? '' : 's'}</div>`;
}

// -- Active scouting: IP range scan + port scan --
document.getElementById('scanSubnetBtn').addEventListener('click', async () => {
  const btn = document.getElementById('scanSubnetBtn');
  const start = document.getElementById('scanStartIp').value.trim();
  const end = document.getElementById('scanEndIp').value.trim();
  setMsg('scanSubnetMsg', true, '');
  if ((start && !end) || (!start && end)) {
    setMsg('scanSubnetMsg', false, 'Enter both a start and end address, or leave both blank to scan the local subnet.');
    return;
  }
  const qs = start && end ? `?start=${encodeURIComponent(start)}&end=${encodeURIComponent(end)}` : '';
  btn.disabled = true;
  btn.textContent = 'Scanning...';
  const res = await fetch('/api/scan/subnet' + qs);
  const body = await res.json().catch(() => ([]));
  btn.disabled = false;
  btn.textContent = 'Scan now';
  const tbody = document.querySelector('#scanSubnetTable tbody');
  tbody.innerHTML = '';
  if (!res.ok) {
    setMsg('scanSubnetMsg', false, body.error || 'scan failed');
    return;
  }
  if (body.length === 0) {
    tbody.innerHTML = '<tr><td colspan="3" class="muted">No hosts responded.</td></tr>';
    return;
  }
  body.forEach((h) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${escapeHtml(h.ip)}</td><td class="muted">${escapeHtml(h.mac || '')}</td><td>${escapeHtml(h.hostname || '')}</td>`;
    tbody.appendChild(tr);
  });
});

document.getElementById('portScanForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const host = document.getElementById('portScanHost').value.trim();
  if (!host) return;
  const tbody = document.querySelector('#portScanTable tbody');
  tbody.innerHTML = '<tr><td colspan="3" class="muted">Scanning...</td></tr>';
  const res = await fetch('/api/scan/ports', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ host }),
  });
  const body = await res.json().catch(() => ([]));
  tbody.innerHTML = '';
  if (!res.ok) {
    tbody.innerHTML = `<tr><td colspan="3" class="msg error">${escapeHtml(body.error || 'scan failed')}</td></tr>`;
    return;
  }
  const open = body.filter((p) => p.open);
  if (open.length === 0) {
    tbody.innerHTML = '<tr><td colspan="3" class="muted">No open ports found (checked a curated common-port list).</td></tr>';
    return;
  }
  open.forEach((p) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${p.port}</td><td class="muted">${escapeHtml(p.name || '')}</td><td style="color:#4caf7d">Open</td>`;
    tbody.appendChild(tr);
  });
});

// -- LAN devices (passive) + Wake-on-LAN --
async function loadLanList() {
  const res = await fetch('/api/lan');
  if (!res.ok) return;
  const hosts = await res.json();
  const tbody = document.querySelector('#lanTable tbody');
  tbody.innerHTML = '';
  if (!hosts || hosts.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" class="muted">No entries in the ARP table yet -- try the IP range scan above, or refresh after some network activity.</td></tr>';
    return;
  }
  hosts.forEach((h) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${escapeHtml(h.ip)}</td><td class="muted">${escapeHtml(h.mac || '')}</td><td>${escapeHtml(h.hostname || '')}</td><td></td>`;
    if (h.mac) {
      const wakeBtn = document.createElement('button');
      wakeBtn.className = 'link-btn';
      wakeBtn.style.color = '#4a9eff';
      wakeBtn.textContent = 'Wake';
      wakeBtn.addEventListener('click', () => sendWakeOnLan(h.mac));
      tr.lastElementChild.appendChild(wakeBtn);
    }
    tbody.appendChild(tr);
  });
}
document.getElementById('refreshLanBtn').addEventListener('click', loadLanList);

async function sendWakeOnLan(mac) {
  const res = await fetch('/api/wol', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mac }),
  });
  const body = await res.json().catch(() => ({}));
  setMsg('wolMsg', res.ok, res.ok ? `Sent to ${mac}.` : (body.error || 'failed'));
}

document.getElementById('wolForm').addEventListener('submit', (e) => {
  e.preventDefault();
  const mac = document.getElementById('wolMac').value.trim();
  if (!mac) return;
  sendWakeOnLan(mac);
});

// -- Logs --
async function loadLogsList() {
  const res = await fetch('/api/logs');
  if (!res.ok) return;
  const files = await res.json();
  const tbody = document.querySelector('#logsTable tbody');
  tbody.innerHTML = '';
  if (!files || files.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" class="muted">No logs collected yet -- aggregated hourly.</td></tr>';
    return;
  }
  files.forEach((f) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${escapeHtml(f.unit)}</td><td class="muted">${escapeHtml(f.date)}</td><td>${f.sizeKb.toFixed(1)} KB</td><td></td>`;
    const viewBtn = document.createElement('button');
    viewBtn.className = 'link-btn';
    viewBtn.style.color = '#4a9eff';
    viewBtn.textContent = 'View';
    viewBtn.addEventListener('click', () => viewLogFile(f.name));
    tr.lastElementChild.appendChild(viewBtn);
    tbody.appendChild(tr);
  });
}
document.getElementById('refreshLogsBtn').addEventListener('click', loadLogsList);

async function viewLogFile(name) {
  const res = await fetch(`/api/logs/read?name=${encodeURIComponent(name)}`);
  const text = await res.text();
  const viewer = document.getElementById('logViewer');
  viewer.style.display = '';
  viewer.textContent = text || '(empty)';
  viewer.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

// -- Scheduled tasks --
let tasksCache = [];

async function loadTasks() {
  const res = await fetch('/api/tasks');
  if (!res.ok) return;
  tasksCache = await res.json();
  const tbody = document.querySelector('#tasksTable tbody');
  tbody.innerHTML = '';
  if (tasksCache.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" class="muted">No scheduled tasks.</td></tr>';
    return;
  }
  tasksCache.forEach((t) => {
    const tr = document.createElement('tr');
    tr.innerHTML = `<td>${escapeHtml(t.name)}</td><td class="muted" style="font-family:monospace">${escapeHtml(t.schedule)}</td><td style="font-family:monospace">${escapeHtml(t.command)}</td><td></td>`;
    const delBtn = document.createElement('button');
    delBtn.className = 'link-btn';
    delBtn.textContent = 'Remove';
    delBtn.addEventListener('click', () => removeTask(t.id));
    tr.lastElementChild.appendChild(delBtn);
    tbody.appendChild(tr);
  });
}

function removeTask(id) {
  saveTasks(tasksCache.filter((t) => t.id !== id));
}

async function saveTasks(list) {
  const res = await fetch('/api/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(list),
  });
  const body = await res.json().catch(() => ([]));
  if (res.ok) {
    tasksCache = body;
    loadTasks();
  } else {
    setMsg('tasksMsg', false, body.error || 'failed');
  }
}

document.getElementById('addTaskBtn').addEventListener('click', () => {
  document.getElementById('taskForm').style.display = '';
});
document.getElementById('cancelTaskBtn').addEventListener('click', () => {
  document.getElementById('taskForm').style.display = 'none';
});
document.getElementById('saveTaskBtn').addEventListener('click', () => {
  const name = document.getElementById('taskName').value.trim();
  const schedule = document.getElementById('taskSchedule').value.trim();
  const command = document.getElementById('taskCommand').value.trim();
  if (!name || !schedule || !command) {
    setMsg('tasksMsg', false, 'Name, schedule, and command are all required.');
    return;
  }
  const updated = tasksCache.concat([{ name, schedule, command }]);
  saveTasks(updated).then(() => {
    document.getElementById('taskForm').style.display = 'none';
    document.getElementById('taskName').value = '';
    document.getElementById('taskSchedule').value = '';
    document.getElementById('taskCommand').value = '';
    setMsg('tasksMsg', true, 'Saved.');
  });
});

// ---- Settings: Pushbullet + Dynamic DNS ----------------------------------
async function loadPushbulletSettings() {
  const res = await fetch('/api/pushbullet');
  if (!res.ok) return;
  const cfg = await res.json();
  document.getElementById('pbEnabled').checked = !!cfg.enabled;
  document.getElementById('pbToken').value = cfg.accessToken || '';
  document.getElementById('pbCallsign').value = cfg.callsign || '';
  document.getElementById('pbAlertDisk').checked = !!cfg.alertDiskFull;
  document.getElementById('pbAlertService').checked = !!cfg.alertServiceDown;
  document.getElementById('pbAlertLockout').checked = !!cfg.alertLockout;
  document.getElementById('pbAlertIp').checked = !!cfg.alertIpChange;
  document.getElementById('pbAlertDigest').checked = !!cfg.alertDigest;
}

document.getElementById('savePushbulletBtn').addEventListener('click', async () => {
  const cfg = {
    enabled: document.getElementById('pbEnabled').checked,
    accessToken: document.getElementById('pbToken').value,
    callsign: document.getElementById('pbCallsign').value.trim(),
    alertDiskFull: document.getElementById('pbAlertDisk').checked,
    alertServiceDown: document.getElementById('pbAlertService').checked,
    alertLockout: document.getElementById('pbAlertLockout').checked,
    alertIpChange: document.getElementById('pbAlertIp').checked,
    alertDigest: document.getElementById('pbAlertDigest').checked,
  };
  const res = await fetch('/api/pushbullet', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  const body = await res.json().catch(() => ({}));
  if (res.ok) {
    document.getElementById('pbToken').value = body.accessToken || '';
    setMsg('pushbulletMsg', true, 'Saved.');
  } else {
    setMsg('pushbulletMsg', false, body.error || 'failed');
  }
});

async function loadDdnsSettings() {
  const res = await fetch('/api/ddns');
  if (!res.ok) return;
  const cfg = await res.json();
  document.getElementById('ddnsEnabled').checked = !!cfg.enabled;
  document.getElementById('ddnsToken').value = cfg.apiToken || '';
  document.getElementById('ddnsZone').value = cfg.zoneId || '';
  document.getElementById('ddnsRecord').value = cfg.recordId || '';
  document.getElementById('ddnsHostname').value = cfg.hostname || '';
}

document.getElementById('saveDdnsBtn').addEventListener('click', async () => {
  const cfg = {
    enabled: document.getElementById('ddnsEnabled').checked,
    apiToken: document.getElementById('ddnsToken').value,
    zoneId: document.getElementById('ddnsZone').value.trim(),
    recordId: document.getElementById('ddnsRecord').value.trim(),
    hostname: document.getElementById('ddnsHostname').value.trim(),
  };
  const res = await fetch('/api/ddns', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  const body = await res.json().catch(() => ({}));
  setMsg('ddnsMsg', res.ok, res.ok ? 'Saved.' : (body.error || 'failed'));
});

// ---- Tailscale ---------------------------------------------------------
// Joining/leaving a tailnet from the GUI -- install itself is already covered by the Apps/Install
// tabs, this is specifically the "now actually connect it" step that used to require the terminal.
let tailscalePollTimer = null;

async function loadTailscaleStatus() {
  const res = await fetch('/api/tailscale');
  if (!res.ok) return;
  const status = await res.json();
  renderTailscaleStatus(status);
}

function renderTailscaleStatus(status) {
  const el = document.getElementById('tailscaleStatus');
  const joinForm = document.getElementById('tailscaleJoinForm');
  const loggedInBlock = document.getElementById('tailscaleLoggedInBlock');

  if (!status.installed) {
    el.innerHTML = `<div class="muted">Not installed.</div>`;
    joinForm.style.display = 'none';
    loggedInBlock.style.display = 'none';
    return;
  }
  if (status.loggedIn) {
    el.innerHTML = `
      <div>Status: <strong style="color:#4caf7d">Connected</strong></div>
      <div>Tailnet: ${escapeHtml(status.tailnetName || '(unknown)')}</div>
      <div>This device: ${escapeHtml(status.dnsName || '(unknown)')} -- ${escapeHtml(status.tailscaleIp || '')}</div>
    `;
    joinForm.style.display = 'none';
    loggedInBlock.style.display = '';
    stopTailscalePoll();
  } else {
    el.innerHTML = `<div>Status: <strong style="color:#e0b050">Not connected</strong> (${escapeHtml(status.backendState || 'unknown')})</div>`;
    joinForm.style.display = '';
    loggedInBlock.style.display = 'none';
  }
}

function stopTailscalePoll() {
  if (tailscalePollTimer) {
    clearInterval(tailscalePollTimer);
    tailscalePollTimer = null;
  }
}

// After starting a browser-based join, poll for up to ~3 minutes to notice when the user finishes
// approving it in their own browser -- there's no push notification for this, only polling.
function startTailscalePoll() {
  stopTailscalePoll();
  let attempts = 0;
  tailscalePollTimer = setInterval(async () => {
    attempts++;
    const res = await fetch('/api/tailscale');
    if (res.ok) {
      const status = await res.json();
      if (status.loggedIn) {
        document.getElementById('tailscaleAuthUrlBlock').style.display = 'none';
        setMsg('tailscaleMsg', true, 'Connected.');
        renderTailscaleStatus(status);
        return; // renderTailscaleStatus already calls stopTailscalePoll() once loggedIn is true
      }
    }
    if (attempts >= 36) stopTailscalePoll(); // ~3 minutes at 5s intervals -- give up quietly, not an error
  }, 5000);
}

document.getElementById('tailscaleAuthKeyForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const authKey = document.getElementById('tsAuthKey').value.trim();
  const loginServer = document.getElementById('tsLoginServer').value.trim();
  const hostname = document.getElementById('tsHostname').value.trim();
  if (!authKey) {
    setMsg('tailscaleMsg', false, 'Auth key is required for this option -- or use "Join via browser" instead.');
    return;
  }
  const submitBtn = e.target.querySelector('button[type="submit"]');
  submitBtn.disabled = true;
  submitBtn.textContent = 'Joining...';
  try {
    const res = await fetch('/api/tailscale/join', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ authKey, loginServer, hostname }),
    });
    const body = await res.json().catch(() => ({}));
    if (res.ok) {
      document.getElementById('tsAuthKey').value = '';
      setMsg('tailscaleMsg', true, 'Joined.');
      loadTailscaleStatus();
    } else {
      setMsg('tailscaleMsg', false, body.error || 'failed');
    }
  } finally {
    submitBtn.disabled = false;
    submitBtn.textContent = 'Join with auth key';
  }
});

document.getElementById('tsJoinBrowserBtn').addEventListener('click', async () => {
  const btn = document.getElementById('tsJoinBrowserBtn');
  const loginServer = document.getElementById('tsLoginServer').value.trim();
  const hostname = document.getElementById('tsHostname').value.trim();
  btn.disabled = true;
  btn.textContent = 'Starting...';
  try {
    const res = await fetch('/api/tailscale/join', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ authKey: '', loginServer, hostname }),
    });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) {
      setMsg('tailscaleMsg', false, body.error || 'failed');
      return;
    }
    if (!body.authUrl) {
      // No URL came back -- already connected (tailscaleUpInteractive's own "already Running" case).
      setMsg('tailscaleMsg', true, 'Already connected.');
      loadTailscaleStatus();
      return;
    }
    const urlBlock = document.getElementById('tailscaleAuthUrlBlock');
    const link = document.getElementById('tailscaleAuthUrlLink');
    const qr = document.getElementById('tailscaleQr');
    link.href = body.authUrl;
    link.textContent = body.authUrl;
    if (body.qr) {
      qr.src = body.qr;
      qr.style.display = '';
    } else {
      qr.style.display = 'none';
    }
    urlBlock.style.display = '';
    setMsg('tailscaleMsg', true, 'Waiting for you to approve this in a browser...');
    startTailscalePoll();
  } finally {
    btn.disabled = false;
    btn.textContent = 'Join via browser (no key)';
  }
});

document.getElementById('tsLogoutBtn').addEventListener('click', async () => {
  if (!confirm('Leave this tailnet? You\'ll need to join again (auth key or browser) to reconnect.')) return;
  const btn = document.getElementById('tsLogoutBtn');
  btn.disabled = true;
  const res = await fetch('/api/tailscale/logout', { method: 'POST' });
  const body = await res.json().catch(() => ({}));
  btn.disabled = false;
  setMsg('tailscaleLogoutMsg', res.ok, res.ok ? 'Left the tailnet.' : (body.error || 'failed'));
  if (res.ok) loadTailscaleStatus();
});

// ---- Initial load ------------------------------------------------------
fetch('/api/whoami').then((r) => r.ok ? r.json() : null).then((me) => {
  if (me) document.getElementById('whoami').textContent = me.username;
});
checkPendingIPChange();
connectTerminal();
