// UBR Single Page Application
(function() {
    'use strict';

    const app = document.getElementById('app');
    let currentPage = '';
    let refreshInterval = null;

    // Router
    function navigate(path) {
        if (refreshInterval) {
            clearInterval(refreshInterval);
            refreshInterval = null;
        }
        window.history.pushState({}, '', path);
        route();
    }

    function route() {
        const path = window.location.pathname;
        switch (path) {
            case '/':
                currentPage = 'dashboard';
                renderDashboard();
                break;
            case '/rules':
                currentPage = 'rules';
                renderRules();
                break;
            case '/packets':
                currentPage = 'packets';
                renderPackets();
                break;
            case '/monitor':
                currentPage = 'monitor';
                renderMonitor();
                break;
            case '/keys':
                currentPage = 'keys';
                renderKeys();
                break;
            case '/settings':
                currentPage = 'settings';
                renderSettings();
                break;
            default:
                currentPage = 'dashboard';
                renderDashboard();
        }
        updateNav();
    }

    function updateNav() {
        document.querySelectorAll('nav a[data-page]').forEach(a => {
            a.classList.toggle('active', a.dataset.page === currentPage);
        });
    }

    // API helper
    async function api(path, method = 'GET', body = null) {
        const opts = { method, headers: {} };
        if (body) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        const resp = await fetch('/api' + path, opts);
        if (resp.status === 401) {
            window.location.href = '/login';
            return null;
        }
        return resp.json();
    }

    // Formatters
    function timeAgo(dateStr) {
        if (!dateStr) return 'never';
        const d = new Date(dateStr);
        const secs = Math.floor((Date.now() - d.getTime()) / 1000);
        if (secs < 60) return secs + 's ago';
        if (secs < 3600) return Math.floor(secs / 60) + 'm ago';
        if (secs < 86400) return Math.floor(secs / 3600) + 'h ago';
        return Math.floor(secs / 86400) + 'd ago';
    }

    function formatBytes(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    // Dashboard
    async function renderDashboard() {
        app.innerHTML = '<h2>Dashboard</h2><p>Loading...</p>';
        const clients = await api('/clients');
        if (!clients) return;

        let html = '<h2>Connected Clients</h2>';
        if (!clients || clients.length === 0) {
            html += '<p class="text-muted">No clients connected.</p>';
        } else {
            html += `<table>
                <thead><tr><th>Name</th><th>Address</th><th>Connected</th><th>Last Seen</th><th>Sent</th><th>Received</th><th>Status</th></tr></thead>
                <tbody>`;
            for (const c of clients) {
                const statusClass = c.online ? 'status-online' : 'status-offline';
                const statusText = c.online ? 'Online' : 'Offline';
                html += `<tr>
                    <td>${escapeHtml(c.key_name)}</td>
                    <td>${escapeHtml(c.addr)}</td>
                    <td>${timeAgo(c.connect_at)}</td>
                    <td>${timeAgo(c.last_seen)}</td>
                    <td>${formatBytes(c.bytes_sent)}</td>
                    <td>${formatBytes(c.bytes_recv)}</td>
                    <td class="${statusClass}">${statusText}</td>
                </tr>`;
            }
            html += '</tbody></table>';
        }
        app.innerHTML = html;

        refreshInterval = setInterval(() => {
            if (currentPage === 'dashboard') renderDashboard();
        }, 5000);
    }

    // Rules
    async function renderRules() {
        app.innerHTML = '<h2>Forwarding Rules</h2><p>Loading...</p>';
        const rules = await api('/rules');
        if (!rules) return;

        let html = '<h2>Forwarding Rules</h2>';
        html += `<button id="addRuleBtn" class="mb-1">Add Rule</button>`;

        if (!rules || rules.length === 0) {
            html += '<p class="text-muted">No forwarding rules configured.</p>';
        } else {
            html += `<table>
                <thead><tr><th>Name</th><th>Port</th><th>Listen IP</th><th>Broadcast</th><th>Direction</th><th>Status</th><th>Actions</th></tr></thead>
                <tbody>`;
            for (const r of rules) {
                const badge = r.IsEnabled ? '<span class="badge badge-enabled">Enabled</span>' : '<span class="badge badge-disabled">Disabled</span>';
                html += `<tr>
                    <td>${escapeHtml(r.Name)}</td>
                    <td>${r.ListenPort}</td>
                    <td>${escapeHtml(r.ListenIP)}</td>
                    <td>${escapeHtml(r.DestBroadcast)}</td>
                    <td>${escapeHtml(r.Direction)}</td>
                    <td>${badge}</td>
                    <td>
                        <button class="btn-small" onclick="ubrToggleRule(${r.ID})">Toggle</button>
                        <button class="btn-small btn-danger" onclick="ubrDeleteRule(${r.ID})">Delete</button>
                    </td>
                </tr>`;
            }
            html += '</tbody></table>';
        }
        app.innerHTML = html;

        document.getElementById('addRuleBtn')?.addEventListener('click', showAddRuleDialog);
    }

    function showAddRuleDialog() {
        const dialog = document.createElement('article');
        dialog.innerHTML = `
            <h3>Add Forwarding Rule</h3>
            <form id="addRuleForm">
                <label>Name <input type="text" id="ruleName" required></label>
                <label>Listen Port <input type="number" id="rulePort" required min="1" max="65535"></label>
                <label>Listen IP <input type="text" id="ruleIP" value="0.0.0.0"></label>
                <label>Dest Broadcast <input type="text" id="ruleBroadcast" value="255.255.255.255"></label>
                <label>Direction
                    <select id="ruleDirection">
                        <option value="server_to_client">Server to Client</option>
                        <option value="client_to_server">Client to Server</option>
                        <option value="bidirectional">Bidirectional</option>
                    </select>
                </label>
                <button type="submit">Create</button>
                <button type="button" id="cancelRule">Cancel</button>
            </form>`;
        app.prepend(dialog);

        document.getElementById('cancelRule').addEventListener('click', () => dialog.remove());
        document.getElementById('addRuleForm').addEventListener('submit', async (e) => {
            e.preventDefault();
            await api('/rules', 'POST', {
                name: document.getElementById('ruleName').value,
                listen_port: parseInt(document.getElementById('rulePort').value),
                listen_ip: document.getElementById('ruleIP').value,
                dest_broadcast: document.getElementById('ruleBroadcast').value,
                direction: document.getElementById('ruleDirection').value,
            });
            renderRules();
        });
    }

    window.ubrToggleRule = async function(id) {
        await api(`/rules/${id}/toggle`, 'PUT');
        renderRules();
    };

    window.ubrDeleteRule = async function(id) {
        if (!confirm('Delete this rule?')) return;
        await api(`/rules/${id}`, 'DELETE');
        renderRules();
    };

    // Packets
    let packetOffset = 0;
    async function renderPackets() {
        app.innerHTML = '<h2>Packet Log</h2><p>Loading...</p>';
        const data = await api(`/packets?limit=50&offset=${packetOffset}`);
        if (!data) return;

        let html = '<h2>Packet Log</h2>';
        const entries = data.entries || [];

        if (entries.length === 0) {
            html += '<p class="text-muted">No packets logged yet.</p>';
        } else {
            html += `<p class="text-muted">Showing ${entries.length} of ${data.total} entries</p>`;
            html += `<table>
                <thead><tr><th>Time</th><th>Source</th><th>Destination</th><th>Size</th><th>Direction</th><th>Client</th></tr></thead>
                <tbody>`;
            for (const e of entries) {
                html += `<tr>
                    <td>${timeAgo(e.Timestamp)}</td>
                    <td>${escapeHtml(e.SrcIP)}:${e.SrcPort}</td>
                    <td>${escapeHtml(e.DstIP)}:${e.DstPort}</td>
                    <td>${e.Size}</td>
                    <td>${escapeHtml(e.Direction)}</td>
                    <td>${escapeHtml(e.ClientName || '-')}</td>
                </tr>`;
            }
            html += '</tbody></table>';

            html += '<div>';
            if (packetOffset > 0) html += `<button id="prevPackets">Previous</button> `;
            if (packetOffset + 50 < data.total) html += `<button id="nextPackets">Next</button>`;
            html += '</div>';
        }
        app.innerHTML = html;

        document.getElementById('prevPackets')?.addEventListener('click', () => { packetOffset = Math.max(0, packetOffset - 50); renderPackets(); });
        document.getElementById('nextPackets')?.addEventListener('click', () => { packetOffset += 50; renderPackets(); });

        refreshInterval = setInterval(() => {
            if (currentPage === 'packets') renderPackets();
        }, 3000);
    }

    // Monitor
    async function renderMonitor() {
        app.innerHTML = '<h2>Broadcast Monitor</h2><p>Loading...</p>';
        const observations = await api('/monitor');
        if (!observations) return;

        let html = '<h2>Broadcast Monitor</h2>';
        html += '<p class="text-muted">Detected broadcast traffic on the network. Create forwarding rules for packets you want to relay.</p>';

        if (!observations || observations.length === 0) {
            html += '<p class="text-muted">No broadcast traffic detected yet. Make sure the server is running with a network interface configured.</p>';
        } else {
            html += `<table>
                <thead><tr><th>Protocol</th><th>Source</th><th>Dest Port</th><th>Count</th><th>Last Seen</th><th>Has Rule</th><th>Action</th></tr></thead>
                <tbody>`;
            for (const o of observations) {
                const hasRule = o.has_rule ? '<span class="badge badge-enabled">Yes</span>' : '<span class="badge badge-disabled">No</span>';
                const action = o.has_rule ? '' : `<button class="btn-small" onclick="ubrCreateRuleFromMonitor('${escapeHtml(o.protocol_type)}', ${o.dst_port})">Create Rule</button>`;
                html += `<tr>
                    <td><strong>${escapeHtml(o.protocol_type)}</strong></td>
                    <td>${escapeHtml(o.src_ip)}</td>
                    <td>${o.dst_port}</td>
                    <td>${o.count}</td>
                    <td>${timeAgo(o.last_seen)}</td>
                    <td>${hasRule}</td>
                    <td>${action}</td>
                </tr>`;
            }
            html += '</tbody></table>';
        }
        app.innerHTML = html;

        refreshInterval = setInterval(() => {
            if (currentPage === 'monitor') renderMonitor();
        }, 3000);
    }

    window.ubrCreateRuleFromMonitor = async function(protocolName, port) {
        await api('/rules', 'POST', {
            name: protocolName + ' (port ' + port + ')',
            listen_port: port,
            direction: 'server_to_client',
        });
        renderMonitor();
    };

    // API Keys
    async function renderKeys() {
        app.innerHTML = '<h2>API Keys</h2><p>Loading...</p>';
        const [keys, rules] = await Promise.all([api('/keys'), api('/rules')]);
        if (!keys || !rules) return;

        let html = '<h2>API Keys</h2>';
        html += '<p class="text-muted">API keys are used by clients to authenticate. Assign forwarding rules to each key to control what traffic gets relayed.</p>';
        html += '<button id="addKeyBtn" class="mb-1">Create API Key</button>';
        html += '<div id="newKeyDisplay" style="display:none;" class="alert alert-success mb-1"></div>';

        if (keys.length === 0) {
            html += '<p class="text-muted">No API keys created yet.</p>';
        } else {
            for (const k of keys) {
                const statusBadge = k.is_revoked
                    ? '<span class="badge badge-revoked">Revoked</span>'
                    : '<span class="badge badge-enabled">Active</span>';
                const revokeBtn = k.is_revoked ? '' : `<button class="btn-small btn-danger" onclick="ubrRevokeKey(${k.id})">Revoke</button>`;

                html += `<article>
                    <div style="display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:0.5rem;">
                        <div>
                            <strong>${escapeHtml(k.name)}</strong> ${statusBadge}<br>
                            <span class="text-muted"><code>${escapeHtml(k.key_preview)}</code>
                            &nbsp;·&nbsp; Created ${timeAgo(k.created_at)}
                            &nbsp;·&nbsp; Last used ${timeAgo(k.last_used_at)}</span>
                        </div>
                        <div>
                            ${revokeBtn}
                            <button class="btn-small btn-danger" onclick="ubrDeleteKey(${k.id})">Delete</button>
                        </div>
                    </div>`;

                // Rule assignment
                html += `<div style="margin-top:0.75rem;">
                    <strong>Forwarding Rules</strong><br>`;
                if (rules.length === 0) {
                    html += '<span class="text-muted">No rules defined yet. Create one in the Rules page.</span>';
                } else {
                    html += '<div style="display:flex; flex-wrap:wrap; gap:0.5rem; margin-top:0.25rem;">';
                    for (const r of rules) {
                        html += `<label style="display:flex;align-items:center;gap:0.25rem;font-weight:normal;">
                            <input type="checkbox" class="rule-assign"
                                data-key-id="${k.id}" data-rule-id="${r.ID}"
                                ${!k.is_revoked ? '' : 'disabled'}>
                            ${escapeHtml(r.Name)} <span class="text-muted">(port ${r.ListenPort})</span>
                        </label>`;
                    }
                    html += '</div>';
                }
                html += '</div></article>';
            }
        }
        app.innerHTML = html;

        // Load current rule assignments for each key and check the boxes
        for (const k of keys) {
            const assigned = await api(`/keys/${k.id}/rules`);
            if (!assigned) continue;
            const assignedIds = new Set(assigned.map(r => r.ID));
            document.querySelectorAll(`.rule-assign[data-key-id="${k.id}"]`).forEach(cb => {
                cb.checked = assignedIds.has(parseInt(cb.dataset.ruleId));
            });
        }

        // Bind rule assignment checkboxes
        document.querySelectorAll('.rule-assign').forEach(cb => {
            cb.addEventListener('change', async () => {
                const keyID = cb.dataset.keyId;
                const ruleID = cb.dataset.ruleId;
                if (cb.checked) {
                    await api(`/keys/${keyID}/rules/${ruleID}`, 'POST');
                } else {
                    await api(`/keys/${keyID}/rules/${ruleID}`, 'DELETE');
                }
            });
        });

        document.getElementById('addKeyBtn')?.addEventListener('click', async () => {
            const name = prompt('Key name (e.g., "office-client"):');
            if (!name) return;
            const result = await api('/keys', 'POST', { name });
            if (result && result.key) {
                const keyHtml = `<strong>New API Key Created!</strong> Copy this key now — it won't be shown again:<br><br>
                    <code style="font-size:1.05rem; user-select:all; word-break:break-all;">${escapeHtml(result.key)}</code>`;
                await renderKeys();
                const display = document.getElementById('newKeyDisplay');
                if (display) {
                    display.innerHTML = keyHtml;
                    display.style.display = 'block';
                    display.scrollIntoView({ behavior: 'smooth' });
                }
            }
        });
    }

    window.ubrRevokeKey = async function(id) {
        if (!confirm('Revoke this API key? Clients using it will be disconnected.')) return;
        await api(`/keys/${id}/revoke`, 'PUT');
        renderKeys();
    };

    window.ubrDeleteKey = async function(id) {
        if (!confirm('Permanently delete this API key?')) return;
        await api(`/keys/${id}`, 'DELETE');
        renderKeys();
    };

    // Settings
    async function renderSettings() {
        let html = '<h2>Settings</h2>';
        html += `<article>
            <h3>Change Password</h3>
            <form id="passwordForm">
                <label>Current Password <input type="password" id="oldPass" required></label>
                <label>New Password <input type="password" id="newPass" required></label>
                <label>Confirm New Password <input type="password" id="confirmPass" required></label>
                <button type="submit">Change Password</button>
                <p id="passResult" style="display:none;"></p>
            </form>
        </article>`;
        html += '<article id="updateCard"><h3>Updates</h3><p class="text-muted">Loading update status…</p></article>';
        app.innerHTML = html;

        document.getElementById('passwordForm').addEventListener('submit', async (e) => {
            e.preventDefault();
            const resultEl = document.getElementById('passResult');
            const newPass = document.getElementById('newPass').value;
            const confirm = document.getElementById('confirmPass').value;

            if (newPass !== confirm) {
                resultEl.textContent = 'Passwords do not match';
                resultEl.style.color = 'var(--color-danger)';
                resultEl.style.display = 'block';
                return;
            }

            const result = await api('/settings/password', 'POST', {
                old_password: document.getElementById('oldPass').value,
                new_password: newPass,
            });

            if (result && result.success) {
                resultEl.textContent = 'Password changed successfully';
                resultEl.style.color = 'var(--color-success)';
            } else {
                resultEl.textContent = result?.error || 'Failed to change password';
                resultEl.style.color = 'var(--color-danger)';
            }
            resultEl.style.display = 'block';
        });

        loadUpdateCard();
    }

    async function loadUpdateCard() {
        const card = document.getElementById('updateCard');
        if (!card) return;
        const status = await api('/update/status');
        if (!status) return;
        renderUpdateCard(card, status);
    }

    function renderUpdateCard(card, s) {
        let html = '<h3>Updates</h3>';

        if (!s.enabled) {
            html += '<p class="text-muted">Update checking is disabled. Set <code>check_updates = true</code> in the server config to enable it.</p>';
            card.innerHTML = html;
            return;
        }

        html += `<p><strong>Current version:</strong> ${escapeHtml(s.current_version)}</p>`;

        if (s.last_checked) {
            html += `<p class="text-muted">Last checked: ${timeAgo(s.last_checked)}</p>`;
        } else {
            html += `<p class="text-muted">Not yet checked this session.</p>`;
        }

        if (s.update_available) {
            html += `<div class="alert alert-warning">
                <strong>Update available:</strong> ${escapeHtml(s.latest_tag)}`;
            if (!s.asset_available) {
                html += `<br><span class="text-muted">No pre-built binary found for this platform — build from source.</span>`;
            }
            html += `</div>`;
        } else if (s.latest_version) {
            html += `<div class="alert alert-info">You are running the latest version (${escapeHtml(s.latest_version)}).</div>`;
        }

        html += `<div style="display:flex;gap:0.5rem;flex-wrap:wrap;margin-top:0.5rem;">`;
        html += `<button id="checkUpdateBtn">Check Now</button>`;
        if (s.update_available && s.asset_available) {
            html += `<button id="applyUpdateBtn">Update Now</button>`;
        }
        html += `</div>`;
        html += `<p id="updateResult" style="display:none;margin-top:0.5rem;"></p>`;

        card.innerHTML = html;

        document.getElementById('checkUpdateBtn').addEventListener('click', async () => {
            const btn = document.getElementById('checkUpdateBtn');
            const resultEl = document.getElementById('updateResult');
            btn.disabled = true;
            btn.textContent = 'Checking…';
            resultEl.style.display = 'none';
            const fresh = await api('/update/check', 'POST');
            if (fresh) {
                renderUpdateCard(card, fresh);
            } else {
                btn.disabled = false;
                btn.textContent = 'Check Now';
                resultEl.textContent = 'Check failed.';
                resultEl.style.color = 'var(--color-danger)';
                resultEl.style.display = 'block';
            }
        });

        document.getElementById('applyUpdateBtn')?.addEventListener('click', async () => {
            if (!confirm(`Apply update ${s.latest_tag} and restart the server?`)) return;
            const btn = document.getElementById('applyUpdateBtn');
            const resultEl = document.getElementById('updateResult');
            btn.disabled = true;
            btn.textContent = 'Updating…';
            const result = await api('/update/apply', 'POST');
            if (result && result.message) {
                resultEl.textContent = result.message;
                resultEl.style.color = 'var(--color-success)';
            } else {
                resultEl.textContent = result?.error || 'Update failed.';
                resultEl.style.color = 'var(--color-danger)';
                btn.disabled = false;
                btn.textContent = 'Update Now';
            }
            resultEl.style.display = 'block';
        });
    }

    // Navigation
    document.addEventListener('click', (e) => {
        const link = e.target.closest('a[data-page]');
        if (link) {
            e.preventDefault();
            navigate(link.getAttribute('href'));
        }
    });

    document.getElementById('logoutBtn')?.addEventListener('click', async (e) => {
        e.preventDefault();
        await fetch('/api/auth/logout', { method: 'POST' });
        window.location.href = '/login';
    });

    window.addEventListener('popstate', route);

    // Initial route
    route();
})();
