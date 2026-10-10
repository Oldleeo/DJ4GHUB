/* Shared, presentation-only interactions. No HTTP, AT, device or notification commands. */
(() => {
  'use strict';
  const body = document.body;
  const cloud = body.dataset.console === 'cloud';
  body.classList.toggle('is-desktop-host', !cloud && Boolean(window.chrome?.webview));
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const finePointer = matchMedia('(hover: hover) and (pointer: fine)');
  const spring = 'cubic-bezier(.22,.72,.16,1)';
  const animations = new WeakMap();
  let lastView = null;
  function motion(element, frames, duration = 420) {
    if (!element) return null;
    animations.get(element)?.cancel();
    if (reduced.matches || !element.animate) return null;
    const animation = element.animate(frames, {duration, easing: spring});
    animations.set(element, animation);
    return animation;
  }
  function enter(target, name) {
    if (!target) return;
    body.dataset.workspaceView = name;
    if (lastView === name) return;
    const order = ['overview','sms','calls','retention','remote','esim','network','notifications','history','audit','settings','at'];
    const direction = order.indexOf(name) >= order.indexOf(lastView) ? 1 : -1;
    lastView = name;
    motion(target, [
      {opacity:.65, transform:`translateX(${direction * 8}px) translateY(3px)`},
      {opacity:1, transform:'translateX(0) translateY(0)'}
    ], 280);
    target.querySelectorAll(':scope > section, :scope > .surface-panel, :scope > .content-grid, :scope > .profile-grid').forEach((item, index) => {
      if (index > 0 && index < 5) motion(item, [{opacity:.7,transform:'translateY(6px)'},{opacity:1,transform:'translateY(0)'}], 260 + index * 20);
    });
    const active = document.querySelector('.task-nav .tab.active, #nav .nav-item.active');
    const indicator = document.querySelector('.workspace-nav-indicator');
    if (active && indicator) {
      indicator.style.height = `${active.offsetHeight}px`;
      indicator.style.transform = `translateY(${active.offsetTop}px)`;
    }
  }
  function text(node, value) {
    const normalized = String(value || '');
    if (node && node.textContent !== normalized) node.textContent = normalized;
  }
  function card(title, detail) {
    text(document.getElementById('workspace-card-title'), title || '待读取卡片');
    text(document.getElementById('workspace-card-detail'), detail || '卡片资料尚未就绪');
  }
  function moduleCards(container, devices, active, onSelect) {
    if (!container) return;
    const keys = new Set(devices.map(device => String(device.key)));
    for (const node of [...container.children]) if (!keys.has(node.dataset.deviceKey)) node.remove();
    for (const device of devices) {
      const key = String(device.key);
      let button = [...container.children].find(node => node.dataset.deviceKey === key);
      if (!button) {
        button = document.createElement('button');
        button.type = 'button'; button.className = 'workspace-module'; button.dataset.deviceKey = key;
        const silhouette = document.createElement('span'); silhouette.className = 'workspace-device-shape'; silhouette.setAttribute('aria-hidden','true');
        const content = document.createElement('span'); content.className = 'workspace-module-copy';
        const label = document.createElement('strong'); label.className = 'workspace-module-name';
        const meta = document.createElement('small'); meta.className = 'workspace-module-meta';
        const status = document.createElement('span'); status.className = 'workspace-module-state';
        content.append(label, meta, status);
        const mark = document.createElement('span'); mark.className = 'workspace-module-check'; mark.textContent = '✓'; mark.setAttribute('aria-hidden','true');
        button.append(silhouette, content, mark); container.append(button);
        button.onclick = () => { if (!locked() && button.getAttribute('aria-pressed') !== 'true') button._select?.(button.dataset.deviceKey); };
      }
      button._select = onSelect;
      button.disabled = locked();
      const selected = key === String(active);
      button.setAttribute('aria-pressed', String(selected));
      button.classList.toggle('is-offline', device.online === false);
      button.setAttribute('aria-label', `${device.label || 'DJI 4G 模块'}，${selected ? '当前控制' : device.online === false ? '离线' : '切换至此模块'}`);
      text(button.querySelector('.workspace-module-name'), device.label || 'DJI 4G 模块');
      text(button.querySelector('.workspace-module-meta'), device.meta || '设备主机');
      text(button.querySelector('.workspace-module-state'), device.online === false ? '主机离线' : selected ? '当前控制' : cloud ? '主机在线' : '已识别');
    }
    if (!devices.length) {
      const empty = document.createElement('div'); empty.className = 'workspace-device-placeholder'; empty.textContent = cloud ? '等待 Windows 设备主机上线' : '等待识别 DJI 4G 模块'; container.replaceChildren(empty);
    }
  }
  const navigation = document.querySelector('.task-nav, #nav');
  if (navigation) {
    for (const button of navigation.querySelectorAll('[data-view]')) {
      button.setAttribute('aria-label', button.textContent.trim().replace(/\s+/g,' '));
      const label = button.querySelector('strong') || button.querySelector('span');
      const compact = {esim:'eSIM',settings:'设置',retention:'保号',remote:'云同步',notifications:'转发',history:'记录',audit:'审计'};
      if (label && compact[button.dataset.view]) label.textContent = compact[button.dataset.view];
    }
  }
  const header = document.querySelector('.app-header .page-heading, .topbar > div');
  if (header) {
    const brand = document.createElement('div'); brand.className = 'workspace-brand'; brand.textContent = `老李 Oldlee · DJ4GHUB ${cloud ? 'CLOUD' : ''}`.trim(); header.prepend(brand);
  }
  // Keep the native select and original status IDs available in a compact disclosure.
  if (!cloud) {
    const stack = document.querySelector('.sidebar-device-stack');
    if (stack) {
      const tools = document.createElement('details'); tools.className = 'workspace-device-tools';
      const summary = document.createElement('summary'); summary.textContent = '设备与连接详情'; tools.append(summary, stack);
      document.querySelector('.workspace-modules')?.append(tools);
    }
    const theme = document.querySelector('.theme-switch');
    if (theme) {
      const section = document.createElement('section'); section.className = 'workspace-theme-settings';
      const label = document.createElement('strong'); label.textContent = '界面外观'; section.append(label, theme);
      document.querySelector('#settings')?.append(section);
    }
    document.getElementById('workspace-rescan')?.addEventListener('click', () => document.getElementById('module-rescan')?.click());
    document.querySelectorAll('[data-workspace-go]').forEach(button => button.addEventListener('click', () => document.querySelector(`.tab[data-view="${button.dataset.workspaceGo}"]`)?.click()));
    const presence = document.getElementById('header-device-state');
    const refresh = document.getElementById('refresh');
    const footer = document.createElement('footer'); footer.className = 'workspace-status-footer';
    if (presence) footer.append(presence);
    const copyright = document.createElement('span'); copyright.textContent = '© 2026 老李 Oldlee'; footer.append(copyright);
    if (refresh) footer.append(refresh);
    document.querySelector('.app-main')?.append(footer);
  } else {
    const footer=document.createElement('footer');footer.className='workspace-status-footer';
    const link=document.createElement('span');link.id='workspace-footer-connection';link.textContent='等待主机同步';
    const copyright=document.createElement('span');copyright.textContent='© 2026 老李 Oldlee';footer.append(link,copyright);
    const refresh=document.querySelector('.header-actions [data-command="refresh_status"]');if(refresh){refresh.textContent='刷新状态';footer.append(refresh);}
    document.querySelector('.main')?.append(footer);
  }
  // Keyboard navigation retains native buttons and never sends a command.
  navigation?.addEventListener('keydown', event => {
    if (!['ArrowDown','ArrowUp','ArrowLeft','ArrowRight','Home','End'].includes(event.key)) return;
    const items = [...navigation.querySelectorAll('button[data-view]')].filter(button => !button.hidden);
    const index = items.indexOf(document.activeElement);
    if (index < 0) return;
    event.preventDefault();
    const offset = ['ArrowDown','ArrowRight'].includes(event.key) ? 1 : -1;
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (index + offset + items.length) % items.length;
    items[next].focus(); items[next].click();
  });
  // Focus containment also covers the QR and destructive-confirmation dialogs.
  let modalFocus = null, visibleModal = null;
  let operationLocks = 0;
  function locked() { return operationLocks > 0 || Boolean(visibleModal); }
  function lockOperation(on) {
    operationLocks = Math.max(0, operationLocks + (on ? 1 : -1));
    refreshLocks();
  }
  function refreshLocks() {
    document.querySelectorAll('.workspace-module, #module-select, #workspace-rescan').forEach(node => node.disabled = locked());
  }
  const inputModal = document.createElement('div'); inputModal.className = 'modal workspace-input-modal'; inputModal.hidden = true;
  inputModal.innerHTML = '<form class="modal-card" role="dialog" aria-modal="true" aria-labelledby="workspace-input-title"><h2 id="workspace-input-title"></h2><p class="workspace-input-message"></p><div class="modal-fields"></div><div class="actions end"><button type="button" class="secondary" data-input-cancel>取消</button><button type="submit" data-input-confirm>保存</button></div></form>';
  body.append(inputModal);
  let inputResolve = null;
  function finishInput(value) {inputModal.hidden=true;const resolve=inputResolve;inputResolve=null;resolve?.(value);}
  function prompt({title, message='', fields=[], confirmLabel='保存'}) {
    if (inputResolve) finishInput(null);
    text(inputModal.querySelector('h2'),title);text(inputModal.querySelector('p'),message);inputModal.querySelector('p').hidden=!message;
    text(inputModal.querySelector('[data-input-confirm]'),confirmLabel);
    const holder=inputModal.querySelector('.modal-fields');holder.replaceChildren(...fields.map(field=>{
      const label=document.createElement('label');label.className='modal-field';const caption=document.createElement('span');caption.textContent=field.label;
      const input=document.createElement('input');input.name=field.name;input.type=field.type||'text';input.value=field.value||'';input.required=Boolean(field.required);input.readOnly=Boolean(field.readOnly);input.autocomplete='off';input.maxLength=field.maxLength||200;label.append(caption,input);return label;
    }));
    inputModal.hidden=false;
    return new Promise(resolve=>{inputResolve=resolve;});
  }
  inputModal.querySelector('form').addEventListener('submit',event=>{event.preventDefault();finishInput(Object.fromEntries([...inputModal.querySelectorAll('input')].map(input=>[input.name,input.value.trim()])));});
  inputModal.querySelector('[data-input-cancel]').addEventListener('click',()=>finishInput(null));
  inputModal.addEventListener('click',event=>{if(event.target===inputModal)finishInput(null);});
  const modals = [...document.querySelectorAll('.modal-backdrop, .modal')];
  const modalObserver = new MutationObserver(() => {
    const opened = modals.find(modal => !modal.hidden);
    if (opened && opened !== visibleModal) {
      modalFocus = document.activeElement; visibleModal = opened;
      document.querySelectorAll('.app-shell > aside, .app-shell > main').forEach(node => node.inert = true);
      document.body.classList.add('workspace-modal-open'); refreshLocks();
      motion(opened.querySelector('[role="dialog"]'), [{opacity:.5,transform:'translateY(18px) scale(.985)'},{opacity:1,transform:'translateY(-1px) scale(1.003)'},{opacity:1,transform:'translateY(0) scale(1)'}], 320);
      opened.querySelector('input:not([type="hidden"]),button')?.focus({preventScroll:true});
    } else if (!opened && visibleModal) {
      visibleModal = null;
      document.querySelectorAll('.app-shell > aside, .app-shell > main').forEach(node => node.inert = false);
      document.body.classList.remove('workspace-modal-open'); refreshLocks();
      if (modalFocus?.isConnected) modalFocus.focus({preventScroll:true});
    }
  });
  modals.forEach(modal => modalObserver.observe(modal, {attributes:true,attributeFilter:['hidden']}));
  document.addEventListener('keydown', event => {
    if (!visibleModal) return;
    if (event.key === 'Escape') {
      const cancel = visibleModal.querySelector('#confirm-cancel, #modal-cancel, #esim-qr-cancel, [data-input-cancel]');
      if (cancel && !cancel.disabled) { event.preventDefault(); cancel.click(); }
    }
    if (event.key !== 'Tab') return;
    const controls = [...visibleModal.querySelectorAll('button,input,select,textarea,a[href]')].filter(node => !node.disabled && !node.hidden && node.getClientRects().length);
    const first = controls[0], last = controls.at(-1);
    if (!first) return;
    if (event.shiftKey && (document.activeElement === first || !visibleModal.contains(document.activeElement))) {event.preventDefault();last.focus();}
    else if (!event.shiftKey && (document.activeElement === last || !visibleModal.contains(document.activeElement))) {event.preventDefault();first.focus();}
  });
  const toast = document.querySelector('#notice, #toast');
  if (toast) {
    toast.setAttribute('role','status'); toast.setAttribute('aria-live','polite');
    const observer = new MutationObserver(() => {
      if (!toast.hidden && (cloud || toast.classList.contains('show'))) motion(toast, [{opacity:.2,transform:'translateX(-50%) translateY(16px) scale(.96)'},{opacity:1,transform:'translateX(-50%) translateY(0) scale(1)'}], 360);
    });
    observer.observe(toast,{childList:true,attributes:true,attributeFilter:['hidden','class']});
  }
  let pointerFrame = 0, hovered = null;
  document.addEventListener('pointermove', event => {
    if (!finePointer.matches || reduced.matches) return;
    const element = event.target.closest('.workspace-module, .workspace-focus');
    if (!element) return;
    hovered = element;
    cancelAnimationFrame(pointerFrame);
    pointerFrame = requestAnimationFrame(() => {
      const bounds = element.getBoundingClientRect();
      element.style.setProperty('--spot-x', `${event.clientX - bounds.left}px`);
      element.style.setProperty('--spot-y', `${event.clientY - bounds.top}px`);
    });
  },{passive:true});
  document.addEventListener('pointerout', event => { if (hovered && !hovered.contains(event.relatedTarget)) {cancelAnimationFrame(pointerFrame);hovered.style.removeProperty('--spot-x');hovered.style.removeProperty('--spot-y');hovered=null;} },{passive:true});
  reduced.addEventListener('change', () => { if (reduced.matches) document.getAnimations().forEach(animation => animation.cancel()); });
  window.addEventListener('resize', () => {
    const active = document.querySelector('.task-nav .tab.active, #nav .nav-item.active');
    const indicator = document.querySelector('.workspace-nav-indicator');
    if (active && indicator) {indicator.style.height=`${active.offsetHeight}px`;indicator.style.transform=`translateY(${active.offsetTop}px)`;}
  });
  // Reuse operational controls and IDs; only the composition changes.
  const create = (tag, className, value) => { const el=document.createElement(tag); el.className=className; if(value!==undefined)el.textContent=value; return el; };
  // Tabler Icons paths already distributed by the project under the MIT license.
  function symbol(name) {
    const paths={refresh:['M20 11a8.1 8.1 0 0 0 -15.5 -2m-.5 -4v4h4','M4 13a8.1 8.1 0 0 0 15.5 2m.5 4v-4h-4'],message:['M8 9h8','M8 13h6','M18 4a3 3 0 0 1 3 3v8a3 3 0 0 1 -3 3h-5l-5 3v-3h-2a3 3 0 0 1 -3 -3v-8a3 3 0 0 1 3 -3h12'],card:['M8 3h5l5 5v11a2 2 0 0 1 -2 2h-8a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2','M9 13h6v5h-6z'],download:['M12 3v12m-4 -4l4 4l4 -4','M5 16v4h14v-4'],activity:['M3 12h4l3 -8l4 16l3 -8h4'],contacts:['M5 4h14v16h-14z','M3 8h4m-4 8h4','M10 9a2 2 0 1 0 4 0a2 2 0 1 0 -4 0','M9 16a3 3 0 0 1 6 0']};
    const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('aria-hidden','true');svg.setAttribute('class','workspace-symbol');
    for(const d of paths[name]||paths.message){const path=document.createElementNS(svg.namespaceURI,'path');path.setAttribute('d',d);svg.append(path);}return svg;
  }
  const go = (name) => document.querySelector(`${cloud?'#nav .nav-item':'.task-nav .tab'}[data-view="${name}"]`)?.click();
  for (const icon of document.querySelectorAll('#esim .fold-panel>summary .panel-icon')) {
    const id=icon.closest('details')?.id;
    icon.replaceChildren(symbol(id==='esim-download-section'?'download':id==='esim-runtime-section'?'activity':id==='esim-phonebook-section'?'contacts':'card'));
  }
  const overview = document.getElementById('overview');
  const focus = overview?.querySelector('.workspace-focus');
  if (focus) {
    const top=create('div','workspace-focus-top'),copy=create('div','workspace-focus-copy');
    const pill=focus.querySelector('.workspace-pill'),title=focus.querySelector('h2'),detail=focus.querySelector('p');
    copy.append(pill,title,detail); const badge=create('span','workspace-card-state','等待读取');badge.id='workspace-card-state';top.append(copy,badge);focus.prepend(top);
    const stats=create('div','workspace-focus-stats');
    for (const id of cloud?['signal','network']:['signal','network-mode']) {const node=document.getElementById(id)?.parentElement;if(node){node.classList.add('workspace-stat');stats.append(node);}}
    const count=create('div','workspace-stat');count.append(create('span','','收件箱'));const value=create('strong','','0 条');value.id='workspace-sms-total';count.append(value);stats.append(count);focus.append(stats);
    const original=overview.querySelector('.status-grid, .stats');
    const diagnostics=create('details','workspace-diagnostics fold-panel');diagnostics.append(create('summary','','网络、流量与模块资料'));
    if(original)diagnostics.append(original);
    for(const node of overview.querySelectorAll(':scope > .overview-heading, :scope > #hardware-details, :scope > .activity-section, :scope > .content-grid'))diagnostics.append(node);
    const detailGrid=create('div','workspace-detail-grid');
    const recent=create('section','workspace-panel workspace-recent');const recentHeading=create('div','workspace-panel-heading');recentHeading.append(create('h3','','最近消息'));const all=create('button','workspace-link','全部 ↗');all.type='button';all.onclick=()=>go('sms');recentHeading.append(all);recent.append(recentHeading);const recentList=create('div','workspace-recent-list');recentList.id='workspace-recent-list';recent.append(recentList);
    const sync=create('section','workspace-panel workspace-sync');sync.append(create('h3','','同步与提醒'));
    for(const [label,id,destination] of [['短信转发','workspace-forwarding',cloud?'notifications':'sms'],['云端控制台','workspace-cloud',cloud?'overview':'remote'],['模块通信','workspace-module-health',cloud?'overview':'settings']]){const row=create('button','workspace-setting-row');row.type='button';row.onclick=()=>go(destination);row.append(create('span','',label));const state=create('span','workspace-setting-value','待读取');state.id=id;row.append(state);sync.append(row);}
    detailGrid.append(recent,sync);focus.after(detailGrid);overview.append(diagnostics);
  }
  const moduleSection=document.querySelector('.workspace-modules');
  const calls=document.getElementById('calls');
  if(calls){
    const dial=cloud?calls.querySelector('.content-grid > .card'):calls.querySelector('.settings-section');
    dial?.classList.add('workspace-dial-panel');
    if(!cloud && dial){const audio=calls.querySelector('.audio-settings');if(audio){const container=create('div','workspace-call-layout');dial.before(container);container.append(dial,audio);}}
    const number=document.getElementById(cloud?'dial-number':'phone-number');
    if(number){const erase=create('button','secondary workspace-backspace','⌫');erase.type='button';erase.title='删除末位数字';erase.setAttribute('aria-label','删除末位数字');erase.onclick=()=>{number.value=number.value.slice(0,-1);number.dispatchEvent(new Event('input',{bubbles:true}));};number.after(erase);}
    if(!cloud && dial){const actions=create('div','workspace-call-actions');for(const id of ['phone-dial','phone-answer','phone-hangup']){const button=document.getElementById(id);if(button)actions.append(button);}dial.querySelector('#phone-keypad')?.after(actions);}
  }
  const tools=document.querySelector('.workspace-device-tools');
  if(tools)document.getElementById('settings')?.append(tools);
  const rescan=document.getElementById('workspace-rescan');
  if(rescan){rescan.replaceChildren(symbol('refresh'));rescan.classList.add('workspace-rescan');rescan.setAttribute('aria-label','重新扫描模块');rescan.title='重新扫描模块';document.querySelector('.header-actions')?.append(rescan);}
  moduleSection?.querySelector('.workspace-module-heading')?.setAttribute('hidden','');
  const sms=document.getElementById('sms'),list=document.getElementById('sms-list');
  const threadState={selected:'',messages:[],handlers:{},query:'',signature:'',threadSignature:'',force:false,composing:false,drafts:new Map()};
  function chooseThread(sender) {
    const phone=document.getElementById(cloud?'sms-number':'phone'),message=document.getElementById(cloud?'sms-message':'message');
    threadState.drafts.set(threadState.selected||'__new__',{phone:phone?.value||'',message:message?.value||''});
    threadState.selected=sender;threadState.composing=!sender;threadState.force=true;
    const draft=threadState.drafts.get(sender||'__new__');
    if(phone)phone.value=draft?.phone??sender;
    if(message){message.value=draft?.message||'';message.dispatchEvent(new Event('input',{bubbles:true}));}
    renderMessages();
  }
  if(sms && list) {
    const layout=create('section','workspace-sms-layout'),threads=create('aside','workspace-threads');threads.setAttribute('aria-label','短信会话');
    const toolbar=sms.querySelector('.sms-inbox-toolbar,.sms-toolbar');if(toolbar)threads.append(toolbar);
    const threadList=create('div','workspace-thread-list');threadList.id='workspace-thread-list';threads.append(threadList);
    const conversation=create('div','workspace-conversation'),heading=create('div','workspace-conversation-heading');
    const headingCopy=create('div','');const name=create('strong','','选择一段会话');name.id='workspace-thread-name';const sub=document.getElementById('sms-status')||create('small','','当前模块的收件箱');sub.classList.add('workspace-conversation-status');headingCopy.append(name,sub);heading.append(headingCopy);
    const compose=create('button','secondary workspace-new-message','新消息');compose.type='button';compose.onclick=()=>{chooseThread('');document.getElementById(cloud?'sms-number':'phone')?.focus();};heading.append(compose);
    conversation.append(heading,list);list.classList.add('workspace-chat-list');
    const form=document.getElementById(cloud?'sms-form':'send-form');const oldComposer=form?.parentElement;
    if(form){form.classList.add('workspace-composer');conversation.append(form);}
    const oldLayout=cloud?sms.querySelector('.content-grid.sms-layout'):null;
    if(oldLayout)oldLayout.remove();else if(oldComposer?.classList.contains('compose-panel'))oldComposer.remove();
    if(cloud){const actions=sms.querySelector('.view-intro .actions');if(actions){actions.classList.add('workspace-inbox-actions');sms.prepend(actions);}}
    const oldInboxHeading=sms.querySelector(':scope > .section-heading');
    if(oldInboxHeading){oldInboxHeading.classList.add('workspace-inbox-actions');sms.prepend(oldInboxHeading);}
    layout.append(threads,conversation);const intro=sms.querySelector('.view-intro');if(intro)intro.after(layout);else sms.prepend(layout);
  }
  function messages(items, query='', handlers={}) {
    threadState.messages=Array.isArray(items)?items:[];threadState.query=String(query).toLowerCase();threadState.handlers=handlers;
    text(document.getElementById('workspace-sms-total'),`${threadState.messages.length} 条`);
    renderRecent();renderMessages();
  }
  function renderRecent() {
    const holder=document.getElementById('workspace-recent-list');if(!holder)return;
    const items=threadState.messages.slice().sort((a,b)=>new Date(b.timestamp)-new Date(a.timestamp)).slice(0,2);
    const key=JSON.stringify(items.map(m=>[m.id,m.sender,m.content,m.timestamp]));if(holder.dataset.signature===key)return;holder.dataset.signature=key;
    holder.replaceChildren(...items.map(m=>{const row=create('button','workspace-recent-row');row.type='button';const icon=create('span','workspace-message-symbol');icon.append(symbol('message'));const copy=create('span','');copy.append(create('strong','',m.sender||'未知号码'),create('small','',m.content||'空短信'));row.append(icon,copy);row.onclick=()=>{chooseThread(String(m.sender||'未知号码'));go('sms');};return row;}));
    if(!items.length)holder.append(create('p','workspace-empty','暂无短信，收到后会显示在这里。'));
  }
  function renderMessages() {
    const holder=document.getElementById('workspace-thread-list');if(!holder||!list)return;
    const all=threadState.messages,query=threadState.query;
    const visible=all.filter(m=>!query||[m.sender,m.content,m.code].filter(Boolean).join(' ').toLowerCase().includes(query));
    const groups=new Map();for(const m of visible.slice().sort((a,b)=>new Date(b.timestamp)-new Date(a.timestamp))){const sender=String(m.sender||'未知号码');if(!groups.has(sender))groups.set(sender,[]);groups.get(sender).push(m);}
    if(threadState.selected && !groups.has(threadState.selected))threadState.selected='';
    if(!threadState.selected && groups.size && !threadState.composing){
      threadState.selected=groups.keys().next().value;
      const phone=document.getElementById(cloud?'sms-number':'phone'),message=document.getElementById(cloud?'sms-message':'message');
      if(phone&&!phone.value&&!message?.value)phone.value=threadState.selected;
    }
    const threadKey=JSON.stringify([...groups].map(([sender,rows])=>[sender,rows[0].content,rows.length]));
    if(threadState.threadSignature!==threadKey){
      threadState.threadSignature=threadKey;holder.replaceChildren(...[...groups].map(([sender,rows])=>{const button=create('button','workspace-thread');button.type='button';button.dataset.sender=sender;const initial=create('span','workspace-thread-initial',sender.replace(/^\+/,'').slice(0,2));const copy=create('span','workspace-thread-copy');copy.append(create('strong','',sender),create('small','',rows[0].content||'空短信'));button.append(initial,copy);button.onclick=()=>{if(threadState.selected!==sender)chooseThread(sender);};return button;}));
      if(!groups.size)holder.append(create('p','workspace-empty',query?'没有匹配的会话':'暂无会话'));
    }
    for(const button of holder.querySelectorAll('.workspace-thread'))button.setAttribute('aria-pressed',String(button.dataset.sender===threadState.selected));
    text(document.getElementById('workspace-thread-name'),threadState.selected||'新消息');
    const rows=(groups.get(threadState.selected)||[]).slice().sort((a,b)=>new Date(a.timestamp)-new Date(b.timestamp));
    const signature=JSON.stringify([threadState.selected,rows.map(m=>[m.id,m.timestamp,m.content,m.code])]);
    if(threadState.signature===signature && !threadState.force)return;
    threadState.signature=signature;threadState.force=false;
    const scroll=list.scrollTop;const nearBottom=list.scrollHeight-list.clientHeight-scroll<48;
    list.className='workspace-chat-list';
    list.replaceChildren(...rows.map(m=>{const row=create('article','workspace-message');row.dataset.messageId=String(m.id||'');const stamp=create('time','',new Date(m.timestamp).toLocaleString('zh-CN',{hour12:false}));const bubble=create('div','workspace-bubble');bubble.append(create('p','',m.content||''));const actions=create('div','workspace-message-actions');
      const action=(label,fn,cls='secondary')=>{const button=create('button',cls,label);button.type='button';button.onclick=()=>fn?.(m,button);return button;};
      if(m.code)bubble.append(action(`验证码 ${m.code}`,()=>threadState.handlers.copyCode?.(m.code),'workspace-code'));
      actions.append(action('回复',()=>{const input=document.getElementById(cloud?'sms-number':'phone');if(input){input.value=m.sender||'';input.dispatchEvent(new Event('input',{bubbles:true}));}document.getElementById(cloud?'sms-message':'message')?.focus();}),action('复制',()=>threadState.handlers.copy?.(m.content||'')));
      const remove=action('删除',(message,button)=>threadState.handlers.remove?.(message,button),'danger');remove.disabled=!m.id;actions.append(remove);row.append(stamp,bubble,actions);return row;}));
    if(!rows.length)list.append(create('p','workspace-empty',query?'没有匹配的短信':'输入号码和内容，开始一条新消息。'));
    list.scrollTop=nearBottom?list.scrollHeight:scroll;
    if(!reduced.matches)motion(list,[{opacity:.45,transform:'translateY(8px)'},{opacity:1,transform:'translateY(0)'}],240);
  }
  function resetMessages() {threadState.selected='';threadState.signature='';threadState.threadSignature='';threadState.force=false;threadState.composing=false;threadState.drafts.clear();messages([]);}
  function summary({ready,sim,network,cloudConnected,bark,telegram}={}) {
    if(ready!==undefined){text(document.getElementById('workspace-module-health'),cloud?(ready?'主机在线':'主机离线'):(ready?'运行正常':'正在恢复'));document.getElementById('workspace-module-health')?.classList.toggle('is-good',ready);}
    if(sim!==undefined){text(document.getElementById('workspace-card-state'),sim?(network?'已注册网络':'等待注册'):'等待卡片');}
    if(cloudConnected!==undefined){text(document.getElementById('workspace-cloud'),cloudConnected?'已连接':'未连接');document.getElementById('workspace-cloud')?.classList.toggle('is-good',cloudConnected);}
    if(cloudConnected!==undefined)text(document.getElementById('workspace-footer-connection'),cloudConnected?'设备主机在线 · 云同步已连接':'设备主机离线');
    if(bark!==undefined||telegram!==undefined)text(document.getElementById('workspace-forwarding'),[bark?'Bark':'',telegram?'Telegram':''].filter(Boolean).join(' · ')||'未开启');
  }
  window.DJSpatial = {enter, moduleCards, card, prompt, messages, resetMessages, summary, lockOperation, locked};
})();
