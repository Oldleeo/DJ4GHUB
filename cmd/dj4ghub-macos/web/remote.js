let remotePollTimer = null;
let remotePairingCode = '';

function configureRemoteClientUI(enabled) {
  document.body.classList.toggle('remote-client-mode', enabled);
  $('#platform-label').textContent = enabled ? 'Windows Remote Client' : 'Device Console';
  $('#sidebar-mode-label').textContent = enabled ? 'Remote-first · Secure relay' : 'Local-first · USB control';
  $('#remote-host-card').hidden = enabled;
  $('#remote-pairing-card').hidden = enabled;
  document.querySelector('.sidebar-device-stack').hidden = enabled;
  document.querySelector('.header-actions').hidden = enabled;
  document.querySelector('.nav-label').textContent = enabled ? '远程连接' : '模块管理';
  document.querySelectorAll('.task-nav [data-view]').forEach((button) => {
    if (enabled) {
      button.hidden = button.dataset.view !== 'remote';
    } else {
      button.hidden = button.hasAttribute('data-advanced')
        ? localStorage.getItem('dj4ghub-advanced') !== '1'
        : false;
    }
  });
  if (enabled) {
    const remoteTab = document.querySelector('[data-view="remote"]');
    if (!$('#remote').classList.contains('active')) remoteTab.click();
    $('#page-title').textContent = '远程设备控制台';
    $('#device-summary').textContent = '绑定设备主机后查看状态、短信和来电';
  }
}

function remoteTime(value) {
  if (!value || String(value).startsWith('0001-')) return '尚未同步';
  return new Date(value).toLocaleString();
}

async function loadRemoteSettings() {
  try {
    const [settings, health] = await Promise.all([api('/api/remote'), api('/api/health')]);
    const remoteClientMode = Boolean(health.remote_client);
    configureRemoteClientUI(remoteClientMode);
    $('#remote-relay-url').value = settings.relay_url || '';
    $('#remote-host-enabled').checked = Boolean(settings.host_enabled);
    const hostParts = [];
    hostParts.push(settings.owner_configured ? '主机密钥已配置' : '尚未配置主机密钥');
    hostParts.push(settings.host_registered ? '本机已注册' : '本机未注册');
    if (settings.last_sync && !String(settings.last_sync).startsWith('0001-')) hostParts.push(`最近同步 ${remoteTime(settings.last_sync)}`);
    if (settings.last_error) hostParts.push(`错误：${settings.last_error}`);
    $('#remote-host-state').textContent = hostParts.join(' · ');
    $('#remote-service-badge').textContent = settings.last_error ? '需要处理' : (settings.last_sync && !String(settings.last_sync).startsWith('0001-') ? '中继在线' : (remoteClientMode ? '等待绑定' : '等待启用'));
    $('#remote-client-state').textContent = settings.client_bound
      ? `已绑定 ${settings.client_device_id || '远程设备'} · ${settings.client_scope === 'control' ? '可控制' : '只读'}`
      : '尚未绑定远程设备';
    $('#remote-unlink').hidden = !settings.client_bound;
    $('#remote-device-panel').hidden = !settings.client_bound;
    if (settings.client_bound) await loadRemoteDevice();
  } catch (error) {
    $('#remote-service-badge').textContent = '中继不可用';
    $('#remote-host-state').textContent = error.message;
  }
}

function renderRemoteMessages(messages) {
  const list = $('#remote-sms-list');
  if (!Array.isArray(messages) || messages.length === 0) {
    list.className = 'list empty';
    list.textContent = '暂无远程短信';
    return;
  }
  list.className = 'list';
  list.replaceChildren(...messages.map((message) => {
    const row = document.createElement('article');
    row.className = 'item';
    const sender = document.createElement('strong'); sender.textContent = message.sender || '未知号码';
    const content = document.createElement('p'); content.textContent = message.content || '';
    const time = document.createElement('time'); time.textContent = new Date(message.timestamp).toLocaleString();
    row.append(sender, content, time);
    return row;
  }));
}

async function loadRemoteDevice() {
  try {
    const device = await api('/api/remote/device');
    const snapshot = device.snapshot || {};
    const status = snapshot.status || {};
    const calls = Array.isArray(snapshot.calls) ? snapshot.calls : [];
    $('#remote-device-online').textContent = device.online ? '在线' : '离线';
    $('#remote-device-online').classList.toggle('is-offline', !device.online);
    $('#remote-device-meta').textContent = `${device.host_name || '设备主机'} · 更新于 ${remoteTime(device.updated_at)}`;
    $('#remote-operator').textContent = displayOperatorName(status.operator) || '--';
    $('#remote-signal').textContent = status.signal_dbm ? `${status.signal_dbm} dBm` : '--';
    $('#remote-network').textContent = status.network_mode || status.reg_status_text || '--';
    $('#remote-sim').textContent = status.sim_inserted ? '已插入' : '未就绪';
    renderRemoteMessages(snapshot.sms || []);
    const states = ['通话中', '保持', '拨号中', '响铃中', '来电', '呼叫等待'];
    $('#remote-call-state').textContent = calls.map((call) => `${call.number || '未知号码'} · ${states[call.state] || '未知状态'}`).join(' / ') || '无语音通话';
    $('#remote-answer').disabled = !calls.some((call) => call.state === 4 || call.state === 5);
    $('#remote-hangup').disabled = calls.length === 0;
  } catch (error) {
    $('#remote-device-online').textContent = '连接失败';
    $('#remote-device-meta').textContent = error.message;
  }
}

$('#remote-host-save').addEventListener('click', async () => {
  const button = $('#remote-host-save'); button.disabled = true;
  try {
    await api('/api/remote/host', {method:'PUT', body:JSON.stringify({enabled:$('#remote-host-enabled').checked, relay_url:$('#remote-relay-url').value.trim()})});
    notice('远程主机设置已保存');
    await loadRemoteSettings();
  } catch (error) { notice(error.message); }
  finally { button.disabled = false; }
});

$('#remote-host-claim').addEventListener('click', async () => {
  const code = $('#remote-host-claim-code').value.trim();
  if (!code) return;
  try {
    await api('/api/remote/host-claim', {method:'POST', body:JSON.stringify({code})});
    $('#remote-host-claim-code').value = '';
    notice('本机已接管设备主机权限');
    await loadRemoteSettings();
  } catch (error) { notice(error.message); }
});

document.querySelectorAll('[data-remote-pair]').forEach((button) => {
  button.addEventListener('click', async () => {
    button.disabled = true;
    try {
      const result = await api('/api/remote/pairing', {method:'POST', body:JSON.stringify({scope:button.dataset.remotePair})});
      remotePairingCode = result.code || '';
      $('#remote-pairing-code').textContent = remotePairingCode;
      $('#remote-pairing-expiry').textContent = `有效至 ${remoteTime(result.expires_at)}`;
      $('#remote-pairing-result').hidden = false;
    } catch (error) { notice(error.message); }
    finally { button.disabled = false; }
  });
});

$('#remote-pairing-copy').addEventListener('click', async () => {
  if (!remotePairingCode) return;
  await navigator.clipboard.writeText(remotePairingCode);
  notice('授权码已复制');
});

$('#remote-client-bind').addEventListener('click', async () => {
  const code = $('#remote-client-code').value.trim();
  if (!code) return;
  try {
    await api('/api/remote/redeem', {method:'POST', body:JSON.stringify({code})});
    $('#remote-client-code').value = '';
    notice('远程设备已绑定');
    await loadRemoteSettings();
  } catch (error) { notice(error.message); }
});

$('#remote-unlink').addEventListener('click', async () => {
  if (!await showModal({title:'解除远程绑定', message:'只删除本机保存的远程授权，不会影响设备主机。', confirmLabel:'解除绑定', danger:true})) return;
  try { await api('/api/remote/client', {method:'DELETE'}); await loadRemoteSettings(); notice('已解除远程绑定'); }
  catch (error) { notice(error.message); }
});

document.querySelectorAll('[data-remote-command]').forEach((button) => {
  button.addEventListener('click', async () => {
    button.disabled = true;
    try { await api('/api/remote/commands', {method:'POST', body:JSON.stringify({name:button.dataset.remoteCommand})}); notice('远程操作已提交'); setTimeout(loadRemoteDevice, 1200); }
    catch (error) { notice(error.message); }
    finally { button.disabled = false; }
  });
});

document.querySelector('[data-view="remote"]').addEventListener('click', () => {
  void loadRemoteSettings();
  clearInterval(remotePollTimer);
  remotePollTimer = setInterval(() => { if ($('#remote').classList.contains('active')) void loadRemoteSettings(); }, 5000);
});

void loadRemoteSettings();
