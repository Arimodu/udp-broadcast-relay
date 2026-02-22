// UBR Setup Wizard
(function() {
    'use strict';

    const state = {
        mode: window.UBR_INITIAL_MODE || '',
        step: '0',
        // Server fields
        adminUser: '',
        adminPass: '',
        iface: '',
        selectedBroadcasts: [],
        installSystemd: true,
        // Client fields
        serverAddr: '',
        authMethod: 'apikey',   // 'apikey' or 'credentials'
        apiKey: '',
        clientUsername: '',
        clientPassword: '',
        clientIfaces: '',
        installSystemdClient: true,
    };

    let discoveryInterval = null;

    // All step element IDs used in the HTML
    const ALL_STEPS = ['0', '1', '1c', '2', '3', '4', '2c', '5'];

    const SERVER_STEPS = [
        { id: '1', label: 'Admin Account' },
        { id: '2', label: 'Interface' },
        { id: '3', label: 'Discovery' },
        { id: '4', label: 'Service' },
        { id: '5', label: 'Summary' },
    ];

    const CLIENT_STEPS = [
        { id: '1c', label: 'Connect' },
        { id: '2c', label: 'Service' },
        { id: '5', label: 'Summary' },
    ];

    function updateIndicator() {
        const steps = state.mode === 'server' ? SERVER_STEPS
                    : state.mode === 'client' ? CLIENT_STEPS
                    : [];
        const indicator = document.getElementById('stepIndicator');
        if (!indicator) return;
        indicator.innerHTML = '';
        const currentIdx = steps.findIndex(s => s.id === state.step);
        steps.forEach((s, i) => {
            const el = document.createElement('span');
            el.className = 'step' + (i < currentIdx ? ' done' : i === currentIdx ? ' active' : '');
            el.dataset.step = i + 1;
            el.textContent = s.label;
            indicator.appendChild(el);
        });
    }

    function showStep(id) {
        state.step = id;
        for (const sid of ALL_STEPS) {
            const el = document.getElementById('step' + sid);
            if (el) el.style.display = sid === id ? 'block' : 'none';
        }
        updateIndicator();
    }

    // Step 0: Mode selection
    document.getElementById('modeServer')?.addEventListener('click', () => {
        state.mode = 'server';
        showStep('1');
        loadInterfaces();
    });

    document.getElementById('modeClient')?.addEventListener('click', () => {
        state.mode = 'client';
        showStep('1c');
    });

    // Step 1 (Server): Admin account
    document.getElementById('adminForm')?.addEventListener('submit', (e) => {
        e.preventDefault();
        const user = document.getElementById('adminUser').value.trim();
        const pass = document.getElementById('adminPass').value;
        const confirm = document.getElementById('adminPassConfirm').value;
        const errEl = document.getElementById('adminError');

        if (!user || !pass) {
            errEl.textContent = 'Username and password are required';
            errEl.style.display = 'block';
            return;
        }
        if (pass !== confirm) {
            errEl.textContent = 'Passwords do not match';
            errEl.style.display = 'block';
            return;
        }
        if (pass.length < 6) {
            errEl.textContent = 'Password must be at least 6 characters';
            errEl.style.display = 'block';
            return;
        }

        state.adminUser = user;
        state.adminPass = pass;
        errEl.style.display = 'none';
        showStep('2');
        loadInterfaces();
    });

    // Step 1c (Client): auth method toggle
    document.querySelectorAll('input[name="clientAuthMethod"]').forEach(radio => {
        radio.addEventListener('change', () => {
            state.authMethod = radio.value;
            document.getElementById('authApiKeySection').style.display =
                radio.value === 'apikey' ? '' : 'none';
            document.getElementById('authCredSection').style.display =
                radio.value === 'credentials' ? '' : 'none';
        });
    });

    // Step 1c (Client): Server connection
    document.getElementById('clientConnForm')?.addEventListener('submit', (e) => {
        e.preventDefault();
        const addr = document.getElementById('clientServerAddr').value.trim();
        const ifaces = document.getElementById('clientIfaces').value.trim();
        const errEl = document.getElementById('clientConnError');

        if (!addr) {
            errEl.textContent = 'Server address is required';
            errEl.style.display = 'block';
            return;
        }

        if (state.authMethod === 'credentials') {
            const user = document.getElementById('clientUsername').value.trim();
            const pass = document.getElementById('clientPassword').value;
            if (!user || !pass) {
                errEl.textContent = 'Username and password are required';
                errEl.style.display = 'block';
                return;
            }
            state.clientUsername = user;
            state.clientPassword = pass;
            state.apiKey = '';
        } else {
            const key = document.getElementById('clientAPIKey').value.trim();
            if (!key) {
                errEl.textContent = 'API key is required';
                errEl.style.display = 'block';
                return;
            }
            state.apiKey = key;
            state.clientUsername = '';
            state.clientPassword = '';
        }

        state.serverAddr = addr;
        state.clientIfaces = ifaces;
        errEl.style.display = 'none';
        showStep('2c');
    });

    // Step 2 (Server): Interface
    async function loadInterfaces() {
        try {
            const resp = await fetch('/api/wizard/interfaces');
            const ifaces = await resp.json();
            const select = document.getElementById('ifaceSelect');
            if (!select) return;
            select.innerHTML = '';
            for (const iface of ifaces) {
                const opt = document.createElement('option');
                opt.value = iface.name;
                opt.textContent = `${iface.name} - ${iface.ip} (${iface.broadcast})`;
                select.appendChild(opt);
            }
            if (ifaces.length > 0) select.selectedIndex = 0;
        } catch (err) {
            console.error('Failed to load interfaces:', err);
        }
    }

    document.getElementById('step2Next')?.addEventListener('click', () => {
        const select = document.getElementById('ifaceSelect');
        state.iface = select ? select.value : '';
        if (!state.iface) {
            alert('Please select a network interface');
            return;
        }
        showStep('3');
        startDiscovery();
    });

    document.getElementById('step2Back')?.addEventListener('click', () => showStep('1'));

    // Step 3 (Server): Broadcast discovery
    function startDiscovery() {
        fetch('/api/wizard/start-monitor', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ interface: state.iface }),
        });

        discoveryInterval = setInterval(async () => {
            try {
                const resp = await fetch('/api/wizard/discoveries');
                const discoveries = await resp.json();
                renderDiscoveries(discoveries || []);
            } catch (err) {
                console.error('Discovery poll error:', err);
            }
        }, 2000);
    }

    function stopDiscovery() {
        if (discoveryInterval) {
            clearInterval(discoveryInterval);
            discoveryInterval = null;
        }
        fetch('/api/wizard/stop-monitor', { method: 'POST' });
    }

    function renderDiscoveries(discoveries) {
        const tbody = document.getElementById('discoveryBody');
        if (!tbody) return;
        tbody.innerHTML = '';

        for (const d of discoveries) {
            const checked = state.selectedBroadcasts.some(b => b.dst_port === d.dst_port) ? 'checked' : '';
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td><input type="checkbox" class="disc-check" data-port="${d.dst_port}" data-proto="${escapeHtml(d.protocol_type)}" ${checked}></td>
                <td><strong>${escapeHtml(d.protocol_type)}</strong></td>
                <td>${d.dst_port}</td>
                <td>${escapeHtml(d.src_ip)}</td>
                <td>${d.count}</td>`;
            tbody.appendChild(tr);
        }

        tbody.querySelectorAll('.disc-check').forEach(cb => {
            cb.addEventListener('change', () => {
                const port = parseInt(cb.dataset.port);
                const proto = cb.dataset.proto;
                if (cb.checked) {
                    if (!state.selectedBroadcasts.some(b => b.dst_port === port)) {
                        state.selectedBroadcasts.push({ dst_port: port, protocol_type: proto });
                    }
                } else {
                    state.selectedBroadcasts = state.selectedBroadcasts.filter(b => b.dst_port !== port);
                }
            });
        });
    }

    document.getElementById('selectAll')?.addEventListener('change', (e) => {
        document.querySelectorAll('.disc-check').forEach(cb => {
            cb.checked = e.target.checked;
            cb.dispatchEvent(new Event('change'));
        });
    });

    document.getElementById('step3Next')?.addEventListener('click', () => {
        stopDiscovery();
        showStep('4');
    });
    document.getElementById('step3Skip')?.addEventListener('click', () => {
        stopDiscovery();
        state.selectedBroadcasts = [];
        showStep('4');
    });
    document.getElementById('step3Back')?.addEventListener('click', () => {
        stopDiscovery();
        showStep('2');
    });

    // Step 4 (Server): Systemd
    document.getElementById('step4Next')?.addEventListener('click', () => {
        state.installSystemd = document.getElementById('installSystemd').checked;
        showStep('5');
        renderSummary();
    });
    document.getElementById('step4Back')?.addEventListener('click', () => showStep('3'));

    // Step 2c (Client): Systemd
    document.getElementById('step2cNext')?.addEventListener('click', () => {
        state.installSystemdClient = document.getElementById('installSystemdClient').checked;
        showStep('5');
        renderSummary();
    });
    document.getElementById('step2cBack')?.addEventListener('click', () => showStep('1c'));

    // Step 5: Summary
    function renderSummary() {
        const tbody = document.getElementById('summaryBody');
        if (!tbody) return;
        tbody.innerHTML = '';

        const row = (label, value) => {
            const tr = document.createElement('tr');
            tr.innerHTML = `<th>${escapeHtml(label)}</th><td>${escapeHtml(String(value))}</td>`;
            tbody.appendChild(tr);
        };

        if (state.mode === 'server') {
            row('Mode', 'Server');
            row('Admin User', state.adminUser);
            row('Interface', state.iface || '(all)');
            row('Systemd Service', state.installSystemd ? 'Yes (install + enable)' : 'No');
            if (state.selectedBroadcasts.length === 0) {
                row('Forwarding Rules', 'None (configure later)');
            } else {
                row('Forwarding Rules',
                    state.selectedBroadcasts.map(b => `${b.protocol_type} (port ${b.dst_port})`).join(', '));
            }
        } else {
            row('Mode', 'Client');
            row('Server Address', state.serverAddr);
            if (state.authMethod === 'credentials') {
                row('Auth', 'Username + password → API key generated now');
                row('Username', state.clientUsername);
                // password deliberately not shown in summary
            } else {
                row('Auth', 'API key');
                row('API Key', state.apiKey);
            }
            row('Rebroadcast Interfaces', state.clientIfaces || '(all)');
            row('Systemd Service', state.installSystemdClient ? 'Yes (install + enable)' : 'No');
        }
    }

    document.getElementById('step5Back')?.addEventListener('click', () => {
        showStep(state.mode === 'client' ? '2c' : '4');
    });

    document.getElementById('step5Save')?.addEventListener('click', async () => {
        const resultEl = document.getElementById('saveResult');
        resultEl.innerHTML = '<p><em>Saving configuration...</em></p>';
        resultEl.style.display = 'block';

        let payload;
        if (state.mode === 'client') {
            payload = {
                mode: 'client',
                server_addr: state.serverAddr,
                api_key: state.apiKey,
                username: state.clientUsername,
                password: state.clientPassword,
                client_ifaces: state.clientIfaces,
                install_systemd: state.installSystemdClient,
            };
        } else {
            payload = {
                mode: 'server',
                admin_user: state.adminUser,
                admin_pass: state.adminPass,
                interface: state.iface,
                install_systemd: state.installSystemd,
                rules: state.selectedBroadcasts.map(b => ({
                    name: b.protocol_type + ' (port ' + b.dst_port + ')',
                    listen_port: b.dst_port,
                    direction: 'server_to_client',
                })),
            };
        }

        try {
            const resp = await fetch('/api/wizard/save', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });

            const result = await resp.json();
            if (result.success) {
                let msg;
                if (state.mode === 'client') {
                    const credNote = state.authMethod === 'credentials'
                        ? ' Your credentials were used to generate an API key and were not saved to disk.'
                        : '';
                    msg = state.installSystemdClient
                        ? 'The client has been installed as a systemd service and will start automatically.' + credNote
                        : 'You can start the client manually with: <code>ubr client --config /etc/ubr/config.toml</code>' + credNote;
                } else {
                    msg = state.installSystemd
                        ? 'The server has been installed as a systemd service and will start automatically.'
                        : 'You can start the server manually with: <code>ubr server --config /etc/ubr/config.toml</code>';
                    msg += '<br>The WebUI will be available at port <strong>21480</strong>.';
                }
                resultEl.innerHTML = `
                    <div class="alert alert-success">
                        <strong>Setup complete!</strong><br>
                        ${msg}<br><br>
                        This wizard will now shut down.
                    </div>`;
                document.getElementById('step5Save').disabled = true;
                document.getElementById('step5Back').disabled = true;
            } else {
                resultEl.innerHTML = `<div class="alert" style="border-color: var(--color-danger);">Error: ${escapeHtml(result.error || 'Unknown error')}</div>`;
            }
        } catch (err) {
            resultEl.innerHTML = `<div class="alert" style="border-color: var(--color-danger);">Network error: ${escapeHtml(err.message)}</div>`;
        }
    });

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str || '';
        return div.innerHTML;
    }

    // Initialize: jump to appropriate starting step
    if (state.mode === 'server') {
        showStep('1');
        loadInterfaces();
    } else if (state.mode === 'client') {
        showStep('1c');
    } else {
        showStep('0');
    }
})();
