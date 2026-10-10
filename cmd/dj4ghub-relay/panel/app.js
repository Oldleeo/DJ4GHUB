const state={csrf:'',devices:[],deviceId:'',timer:null,lastCalls:'',callActive:false,view:'overview',smsMessages:[],smsStatus:{},retention:{cards:[],counts:{}},esimData:null,audio:{pc:null,stream:null,session:'',after:0,polling:false},qr:{stream:null,frame:0,scanning:false}};
const $=id=>document.getElementById(id);
const viewMeta={overview:['DEVICE OVERVIEW','设备概览'],sms:['MESSAGES','短信'],calls:['PHONE','电话'],retention:['NUMBER RETENTION','保号助手'],esim:['SIM & ESIM','eSIM / 卡片'],network:['CONNECTIVITY','网络'],notifications:['MESSAGE FORWARDING','短信转发'],history:['HISTORY','通信记录'],audit:['SECURITY LOG','操作审计']};

async function api(path,options={}){
  const headers={Accept:'application/json',...(options.headers||{})};
  if(options.body)headers['Content-Type']='application/json';
  if(options.method&&options.method!=='GET')headers['X-CSRF-Token']=state.csrf;
  const response=await fetch(path,{credentials:'same-origin',...options,headers});
  const data=await response.json().catch(()=>({}));
  if(!response.ok)throw new Error(data.error||`HTTP ${response.status}`);
  return data;
}

function toast(message){const el=$('toast');el.textContent=message;el.hidden=false;clearTimeout(el._timer);el._timer=setTimeout(()=>el.hidden=true,4200)}
function showApp(on){$('login').hidden=on;$('app').hidden=!on;document.body.classList.toggle('authenticated',on);if(!on)$('password').focus()}
function time(value){if(!value)return '尚未同步';const date=new Date(value);return Number.isNaN(date.getTime())?'—':date.toLocaleString('zh-CN',{hour12:false})}
function operatorName(value){if(!value)return '—';if(typeof value==='string')return value;return value.name||value.long_name||value.numeric||'—'}
function pretty(value){return JSON.stringify(value??{},null,2)}
function commandData(result){return result&&Object.prototype.hasOwnProperty.call(result,'data')?result.data:result}

async function bootstrap(){
  try{const session=await api('/panel/api/session');state.csrf=session.csrf;state.deviceId=new URLSearchParams(location.search).get('device')||'';showApp(true);await loadDevices();showView(new URLSearchParams(location.search).get('view')||'overview');registerWorker()}
  catch{showApp(false)}
}

$('login-form').addEventListener('submit',async event=>{
  event.preventDefault();$('login-error').textContent='';const button=event.submitter;button.disabled=true;button.textContent='正在验证…';
  try{const result=await api('/panel/api/login',{method:'POST',body:JSON.stringify({password:$('password').value,otp:$('otp').value.replace(/\s/g,'')})});state.csrf=result.csrf;$('password').value='';$('otp').value='';showApp(true);await loadDevices();showView('overview');registerWorker()}
  catch(error){$('login-error').textContent=error.message}
  finally{button.disabled=false;button.textContent='安全登录'}
});

$('logout').addEventListener('click',async()=>{try{await api('/panel/api/logout',{method:'POST',body:'{}'})}catch{}stopAudio(false);clearInterval(state.timer);state.csrf='';state.deviceId='';resetSelectedDeviceUI();showApp(false)});

function showView(name){
  if(!viewMeta[name])name='overview';state.view=name;
  document.querySelectorAll('.view').forEach(view=>view.classList.remove('active'));
  const target=name==='network'?$('network-view'):$(name);target?.classList.add('active');
  document.querySelectorAll('[data-view]').forEach(button=>{const active=button.dataset.view===name;button.classList.toggle('active',active);if(active)button.setAttribute('aria-current','page');else button.removeAttribute('aria-current')});
  $('page-kicker').textContent=viewMeta[name][0];$('page-title').textContent=viewMeta[name][1];
  const url=new URL(location.href);url.searchParams.set('view',name);if(state.deviceId)url.searchParams.set('device',state.deviceId);history.replaceState(null,'',url);document.title=`老李 Oldlee · ${viewMeta[name][1]} · DJ4GHUB CLOUD`;
  window.DJSpatial?.enter(target,name);
  if(name==='history')void loadHistory('');if(name==='audit')void loadAudit();if(name==='esim'&&$('esim-profiles').children.length===0)void loadESIM(false);if(name==='notifications')void loadNotificationSettings(false);if(name==='retention')void loadRetention(false);
}
document.querySelectorAll('[data-view]').forEach(button=>button.onclick=()=>showView(button.dataset.view));
document.querySelectorAll('[data-go]').forEach(button=>button.onclick=()=>showView(button.dataset.go));

async function loadDevices({refreshSettings=true}={}){
  const previous=state.deviceId;
  const result=await api('/panel/api/devices');state.devices=result.devices||[];$('device-count').textContent=String(state.devices.length);
  if(state.deviceId&&!state.devices.some(device=>device.id===state.deviceId))state.deviceId='';
  if(!state.deviceId&&state.devices[0])state.deviceId=state.devices[0].id;
  const changed=Boolean(previous)&&previous!==state.deviceId;if(changed)resetSelectedDeviceUI();
  window.DJSpatial?.moduleCards($('devices'),state.devices.map(device=>({key:device.id,label:device.name||'DJI 4G 模块',meta:device.host_name||'设备主机',online:Boolean(device.online)})),state.deviceId,id=>void selectDevice(id).catch(error=>toast(error.message)));
  if(state.deviceId){$('empty').hidden=true;$('device').hidden=false;await Promise.all([loadDevice(),loadAudit()]);if(refreshSettings||changed)void loadNotificationSettings(false);if(changed)showView(state.view)}else{$('empty').hidden=false;$('device').hidden=true}
  clearInterval(state.timer);state.timer=setInterval(()=>void refreshAll(),5000);
}
async function refreshAll(){if(!state.csrf)return;try{await loadDevices({refreshSettings:false})}catch(error){if(error.message==='需要登录'){clearInterval(state.timer);showApp(false)}else toast(error.message)}}
function resetSelectedDeviceUI(){
  state.esimData=null;state.smsMessages=[];state.retention={cards:[],counts:{}};state.esimRequest=(state.esimRequest||0)+1;
  $('load-esim').disabled=false;$('esim-profiles').replaceChildren();$('esim-summary').textContent='正在读取所选模块的卡片…';
  for(const id of ['esim-imei','esim-aid','esim-smdp','esim-matching-id','esim-confirmation','esim-activation-code','sms-number','sms-message','sms-filter','dial-number','apn','bark-url','bark-group','telegram-token','telegram-chat-id'])$(id).value='';
  $('esim-output').hidden=true;$('network-output').textContent='尚未读取所选模块的网络状态';$('bark-enabled').checked=false;$('telegram-enabled').checked=false;$('bark-state').textContent='等待读取所选模块';$('telegram-state').textContent='等待读取所选模块';
  $('sms-message').dispatchEvent(new Event('input'));closeRetentionForm();window.DJSpatial?.resetMessages();renderSMS([]);renderRetention(state.retention);window.DJSpatial?.summary({ready:false,sim:false,cloudConnected:false,bark:false,telegram:false});window.DJSpatial?.card('正在读取所选模块','等待主机同步');
}
async function selectDevice(id){if(state.deviceId===id||window.DJSpatial?.locked())return;stopAudio(true);state.deviceId=id;resetSelectedDeviceUI();await loadDevices();showView(state.view)}

async function loadDevice(){
  const requestedDevice=state.deviceId;
  const device=await api(`/panel/api/devices/${requestedDevice}`),snapshot=device.snapshot||{},status=snapshot.status||{},calls=Array.isArray(snapshot.calls)?snapshot.calls:[];
  if(requestedDevice!==state.deviceId)return;
  $('online').textContent=device.online?'在线':'离线';$('online').classList.toggle('online',Boolean(device.online));$('device-name').textContent=device.name||'DJI 4G Module';$('device-meta').textContent=`${device.host_name||'设备主机'} · 更新于 ${time(device.updated_at)}`;
  renderDeviceStatus(status);renderSMS(snapshot.sms||[],snapshot.sms_status||{});renderCalls(calls);renderRetention(snapshot.retention||state.retention);
  window.DJSpatial?.summary({ready:Boolean(device.online)&&Boolean(status.sim_inserted),sim:Boolean(status.sim_inserted),network:status.reg_status===1||status.reg_status===5,cloudConnected:Boolean(device.online)});
  window.DJSpatial?.card(status.sim_inserted ? operatorName(status.operator) : '等待卡片就绪',[status.network_mode||status.reg_status_text,status.iccid?`ICCID ···${String(status.iccid).slice(-6)}`:'未读取卡片身份'].filter(Boolean).join(' · '));
}

function renderDeviceStatus(status={}){$('operator').textContent=operatorName(status.operator);$('registration').textContent=status.reg_status_text||status.registration||'等待注册';$('network').textContent=status.network_mode||'—';$('band').textContent=[status.network_duplex,status.radio_band].filter(Boolean).join(' · ')||'频段未知';$('signal').textContent=Number.isFinite(status.signal_dbm)?`${status.signal_dbm} dBm`:'—';$('signal-extra').textContent=[Number.isFinite(status.signal_rsrp)?`RSRP ${status.signal_rsrp}`:'',Number.isFinite(status.signal_rsrq)?`RSRQ ${status.signal_rsrq}`:''].filter(Boolean).join(' · ')||'RSRP / RSRQ';$('sim').textContent=status.sim_inserted?'已插入':'未就绪';$('iccid').textContent=status.iccid?`ICCID ···${String(status.iccid).slice(-6)}`:'未读取 ICCID';if(status.imei&&!$('esim-imei').value)$('esim-imei').value=status.imei;renderDetails(status)}

function renderDetails(status){const fields=[['IMEI',status.imei],['固件',status.firmware],['本机号码',status.phone_number],['IMSI',status.imsi],['USB 网卡模式',status.usb_net_mode],['硬件状态',status.hardware_status]];$('module-details').replaceChildren(...fields.map(([label,value])=>{const el=document.createElement('div');el.className='detail';const span=document.createElement('span');span.textContent=label;const strong=document.createElement('strong');strong.textContent=value===0?'0':value||'—';el.append(span,strong);return el}))}
function renderSMS(messages,status={}){
  state.smsMessages=Array.isArray(messages)?messages:[];state.smsStatus=status||{};
  const list=$('sms-list'),query=$('sms-filter').value.trim().toLocaleLowerCase(),visible=state.smsMessages.filter(message=>!query||[message.sender,message.content,message.code].filter(Boolean).join(' ').toLocaleLowerCase().includes(query));
  $('sms-status').textContent=state.smsStatus.last_poll_error?`读取异常：${state.smsStatus.last_poll_error}`:`${state.smsMessages.length} 条 · ${time(state.smsStatus.last_poll)}`;$('sms-visible').textContent=query?`${visible.length} / ${state.smsMessages.length} 条`:`${state.smsMessages.length} 条`;
  if(window.DJSpatial?.messages){window.DJSpatial.messages(state.smsMessages,query,{copy:copyText,copyCode:code=>copyText(code,'验证码已复制'),remove:deleteSMSMessage});return}
  if(!visible.length){list.textContent=state.smsMessages.length?'没有匹配的短信':'暂无短信';list.classList.add('empty-messages');return}list.classList.remove('empty-messages');
  list.replaceChildren(...visible.map(message=>{const row=document.createElement('article');row.className='message';const head=document.createElement('div');head.className='message-head';const sender=document.createElement('strong');sender.textContent=message.sender||'未知号码';const stamp=document.createElement('time');stamp.textContent=time(message.timestamp);head.append(sender,stamp);const content=document.createElement('p');content.textContent=message.content||'';const actions=document.createElement('div');actions.className='message-actions';if(message.code){const code=document.createElement('button');code.className='code-chip';code.type='button';code.textContent=`验证码 ${message.code}`;code.title='复制验证码';code.onclick=()=>copyText(message.code,'验证码已复制');actions.append(code)}const reply=document.createElement('button');reply.className='secondary compact';reply.type='button';reply.textContent='回复';reply.onclick=()=>{$('sms-number').value=message.sender||'';$('sms-message').focus();toast('已填入收件号码')};const copy=document.createElement('button');copy.className='secondary compact';copy.type='button';copy.textContent='复制正文';copy.onclick=()=>copyText(message.content||'','短信内容已复制');const remove=document.createElement('button');remove.className='danger compact';remove.type='button';remove.textContent='删除';remove.disabled=!message.id;remove.onclick=()=>deleteSMSMessage(message,remove);actions.append(reply,copy,remove);row.append(head,content,actions);return row}))
}
async function copyText(value,success){try{await navigator.clipboard.writeText(value);toast(success)}catch{toast('复制失败，请手动复制')}}
async function deleteSMSMessage(message,button){if(!await confirmAction('只删除模块收件箱中的这条短信，通信记录仍会保留。此操作无法撤销。','删除短信'))return;button.disabled=true;try{await command('delete_sms',{id:message.id},{success:'短信已删除'});state.smsMessages=state.smsMessages.filter(item=>item.id!==message.id);renderSMS(state.smsMessages,state.smsStatus)}catch(error){toast(error.message);button.disabled=false}}
$('sms-filter').addEventListener('input',()=>renderSMS(state.smsMessages,state.smsStatus));
function renderCalls(calls){const names=['通话中','保持','拨号中','响铃中','来电','呼叫等待'];state.callActive=calls.length>0;$('call-state').textContent=calls.map(call=>`${call.number||'未知号码'} · ${names[call.state]||'未知'}`).join(' / ')||'无通话';$('answer').disabled=!calls.some(call=>call.state===4||call.state===5);$('hangup').disabled=!calls.length;const signature=JSON.stringify(calls.map(call=>[call.number,call.state]));if(signature!==state.lastCalls&&calls.some(call=>call.state===4||call.state===5))void notifyIncoming(calls.find(call=>call.state===4||call.state===5));state.lastCalls=signature}

async function command(name,payload,options={}){
  if(!state.deviceId)throw new Error('请先选择设备');
  const commandDevice=state.deviceId;
  const reading=/^(read_|refresh_|check_|probe_)/.test(name);const silent=options.silent??reading;
  if(!reading)window.DJSpatial?.lockOperation(true);
  try {
  const created=await api(`/panel/api/devices/${commandDevice}/commands`,{method:'POST',body:JSON.stringify({name,payload})});if(!silent)toast('已提交，正在等待设备…');
  const attempts=['download_esim','switch_esim'].includes(name)?360:60;
  for(let i=0;i<attempts;i++){await new Promise(resolve=>setTimeout(resolve,1000));const status=await api(`/panel/api/devices/${commandDevice}/commands/${created.id}`);if(commandDevice!==state.deviceId)throw new Error('已切换模块；原命令仍属于原模块，请返回原模块核对结果');if(status.done){if(status.result&&status.result.ok===false)throw new Error(status.result.error||'设备执行失败');if(!silent)toast(options.success||'设备已执行');setTimeout(()=>{if(commandDevice===state.deviceId)void loadDevice()},10500);return status.result||{ok:true}}}
  throw new Error('设备处理超时，可在操作审计中查看结果');
  } finally {if(!reading)window.DJSpatial?.lockOperation(false)}
}
async function runButton(button,name,payload){button.disabled=true;try{return await command(name,payload)}catch(error){toast(error.message);throw error}finally{button.disabled=false}}
document.querySelectorAll('[data-command]').forEach(button=>button.addEventListener('click',()=>runButton(button,button.dataset.command).then(result=>{if(button.dataset.command==='refresh_status')renderDeviceStatus(commandData(result)||{})}).catch(()=>{})));

function confirmAction(message,confirmLabel='继续执行'){return new Promise(resolve=>{const modal=$('confirm-modal');$('confirm-message').textContent=message;$('confirm-ok').textContent=confirmLabel;modal.hidden=false;const finish=value=>{modal.hidden=true;$('confirm-ok').onclick=null;$('confirm-cancel').onclick=null;resolve(value)};$('confirm-ok').onclick=()=>finish(true);$('confirm-cancel').onclick=()=>finish(false)})}

$('sms-message').oninput=()=>$('sms-count').textContent=`${[...$('sms-message').value].length} / 2000`;
$('sms-form').addEventListener('submit',async event=>{event.preventDefault();const phone=$('sms-number').value.trim(),message=$('sms-message').value;if(!await confirmAction(`确认向 ${phone} 发送这条短信？`,'发送短信'))return;try{await runButton(event.submitter,'send_sms',{phone,message});$('sms-message').value='';$('sms-message').dispatchEvent(new Event('input'))}catch{}});
$('clear-sms').onclick=async event=>{if(await confirmAction('这会清空模块 ME 存储中的短信，通信历史不受影响。','确认清空'))await runButton(event.currentTarget,'clear_sms').catch(()=>{})};
$('dial-form').addEventListener('submit',async event=>{event.preventDefault();const number=$('dial-number').value.trim();if(!await confirmAction(`确认拨打 ${number}？`,'确认拨号'))return;await runButton(event.submitter,'dial',{number}).catch(()=>{})});
$('answer').onclick=event=>runButton(event.currentTarget,'answer').catch(()=>{});$('hangup').onclick=event=>runButton(event.currentTarget,'hangup').catch(()=>{});
'123456789*0#'.split('').forEach(digit=>{const button=document.createElement('button');button.textContent=digit;button.onclick=async()=>{if(!state.callActive){$('dial-number').value+=digit;$('dial-number').focus();return}button.disabled=true;try{await command('dtmf',{digit},{silent:true})}catch(error){toast(error.message)}finally{button.disabled=false}};$('dtmf').append(button)});

$('host-pairing').onclick=async()=>{try{const result=await api(`/panel/api/devices/${state.deviceId}/host-pairing`,{method:'POST',body:'{}'});await DJSpatial.prompt({title:'一次性设备主机码',message:'10 分钟内有效，请只提供给你自己的设备主机。',fields:[{name:'code',label:'主机授权码',value:result.code,readOnly:true}],confirmLabel:'完成'})}catch(error){toast(error.message)}};
const auditLabels={refresh_status:'刷新状态',refresh_sms:'读取短信',read_sms_status:'读取短信状态',clear_sms:'清空模块短信',delete_sms:'删除单条短信',send_sms:'发送短信',dial:'拨号',answer:'接听',hangup:'挂断',dtmf:'DTMF 按键',read_history:'读取通信记录',read_network:'网络诊断',check_4g:'4G 公网检测',check_proxy:'代理检测',save_apn:'保存 APN',set_usbnet:'切换 USB 网卡模式',reboot_module:'重启模块',read_esim:'读取 eSIM',switch_esim:'切换 Profile',rename_esim:'改名 Profile',delete_esim:'删除 Profile',download_esim:'下载 Profile',read_notifications:'读取短信转发配置',save_notifications:'保存短信转发配置',test_notification:'发送转发测试',host_pairing:'生成主机授权码'};
Object.assign(auditLabels,{read_retention:'读取保号档案',save_retention:'保存保号档案',delete_retention:'删除保号档案',complete_retention:'确认已保号',verify_retention:'验证当前卡'});
async function loadAudit(){if(!state.csrf)return;const result=await api('/panel/api/audit');$('audit-list').replaceChildren(...(result.audit||[]).slice().reverse().map(item=>{const row=document.createElement('article');row.className='record';const head=document.createElement('div');head.className='record-head';const action=document.createElement('strong');action.className='audit-action';action.textContent=auditLabels[item.action]||item.action;const stamp=document.createElement('time');stamp.textContent=time(item.created_at);head.append(action,stamp);const source=document.createElement('p');source.textContent=`来源：${item.source||'panel'}`;row.append(head,source);return row}))}

$('browser-notifications').onclick=async()=>{if(!('Notification'in window)){toast('当前浏览器不支持通知');return}const permission=await Notification.requestPermission();toast(permission==='granted'?'来电提醒已开启':'通知权限未开启')};
async function notifyIncoming(call){if(Notification.permission!=='granted')return;const registration=await navigator.serviceWorker?.ready;const options={body:`${call.number||'未知号码'} 正在呼入`,tag:'dj4ghub-incoming-call',renotify:true};if(registration)await registration.showNotification('老李 Oldlee · 来电',options);else new Notification('老李 Oldlee · 来电',options)}
function registerWorker(){if('serviceWorker'in navigator)navigator.serviceWorker.register('/panel/sw.js',{scope:'/panel/'}).then(reg=>reg.update()).catch(()=>{})}

function renderNotificationSettings(data){
  const telegram=data.telegram||{},bark=data.bark||{};
  $('telegram-enabled').checked=Boolean(telegram.enabled);$('telegram-chat-id').value=telegram.chat_id||'';$('telegram-token').value='';$('telegram-token').placeholder=telegram.configured?'已保存，留空保留 Bot Token':'输入 Bot Token';$('telegram-state').textContent=telegram.last_error?`上次失败：${telegram.last_error}`:(telegram.configured?'已配置':'未配置');
  $('bark-enabled').checked=Boolean(bark.enabled);$('bark-group').value=bark.group||'';$('bark-url').value='';$('bark-url').placeholder=bark.configured?'已保存，留空保留地址':'https://api.day.app/你的Key';$('bark-state').textContent=bark.last_error?`上次失败：${bark.last_error}`:(bark.configured?'已配置':'未配置');
  $('telegram-clear').checked=false;$('bark-clear').checked=false;
  window.DJSpatial?.summary({bark:Boolean(bark.enabled&&bark.configured),telegram:Boolean(telegram.enabled&&telegram.configured)});
}
async function loadNotificationSettings(showMessage=true){
  const button=$('load-notifications'),device=state.deviceId;button.disabled=true;
  try{const result=await command('read_notifications',undefined,{silent:true});if(device!==state.deviceId)return;renderNotificationSettings(commandData(result)||{});if(showMessage)toast('转发配置已更新')}
  catch(error){if(device!==state.deviceId)return;$('notification-status').textContent=`读取失败：${error.message}`;if(showMessage)toast(error.message)}finally{button.disabled=false}
}
$('load-notifications').onclick=()=>loadNotificationSettings(true);
$('notification-form').onsubmit=async event=>{
  event.preventDefault();const button=event.submitter;button.disabled=true;
  const payload={telegram_enabled:$('telegram-enabled').checked,telegram_bot_token:$('telegram-token').value.trim(),telegram_chat_id:$('telegram-chat-id').value.trim(),clear_telegram_token:$('telegram-clear').checked,bark_enabled:$('bark-enabled').checked,bark_url:$('bark-url').value.trim(),bark_group:$('bark-group').value.trim(),clear_bark_url:$('bark-clear').checked};
  try{const result=await command('save_notifications',payload,{success:'短信转发配置已保存'});renderNotificationSettings(commandData(result)||{});$('notification-status').textContent='已保存到设备主机。只转发之后收到的新短信。'}
  catch(error){$('notification-status').textContent=`保存失败：${error.message}`}finally{button.disabled=false}
};
async function testNotification(channel,button){if(!await confirmAction(`确认发送一条 ${channel==='bark'?'Bark':'Telegram'} 测试通知？`,'发送测试'))return;button.disabled=true;try{await command('test_notification',{channel},{success:'测试通知已发送'})}catch(error){toast(error.message)}finally{button.disabled=false}}
$('test-bark').onclick=event=>testNotification('bark',event.currentTarget);$('test-telegram').onclick=event=>testNotification('telegram',event.currentTarget);

function retentionField(id){return $(`retention-${id}`)}
function openRetentionForm(card={}){
  $('retention-form').hidden=false;retentionField('form-title').textContent=card.id?'编辑保号卡片':'添加保号卡片';
  for(const key of ['id','label','country','operator','phone','iccid','plan'])retentionField(key).value=card[key]||'';
  retentionField('next-due').value=card.next_due||'';retentionField('grace').value=Number(card.grace_period_days||0);retentionField('action-note').value=card.action_note||'';retentionField('source-url').value=card.source_url||'';retentionField('enabled').checked=card.id?Boolean(card.enabled):true;
  $('retention-form').scrollIntoView({behavior:'smooth',block:'start'});retentionField('label').focus();
}
function closeRetentionForm(){$('retention-form').hidden=true;$('retention-form').reset();retentionField('id').value='';retentionField('enabled').checked=true}
function retentionMeta(label,value){const box=document.createElement('div'),span=document.createElement('span'),strong=document.createElement('strong');span.textContent=label;strong.textContent=value||'—';box.append(span,strong);return box}
function renderRetention(data={}){
  state.retention={cards:Array.isArray(data.cards)?data.cards:[],counts:data.counts||{}};const counts=state.retention.counts;
  $('retention-total').textContent=counts.total||0;$('retention-due-count').textContent=counts.due||0;$('retention-urgent-count').textContent=(counts.urgent||0)+(counts.grace||0);$('retention-risk-count').textContent=counts.risk||0;
  const list=$('retention-list');if(!state.retention.cards.length){const empty=document.createElement('div');empty.className='retention-empty';empty.textContent='还没有保号卡片。添加卡片后，设备主机会按日期发送提醒。';list.replaceChildren(empty);return}
  list.replaceChildren(...state.retention.cards.map(card=>{const article=document.createElement('article');article.className='retention-card';const head=document.createElement('div');head.className='retention-card-head';const heading=document.createElement('div');const kicker=document.createElement('span');kicker.className='section-kicker';kicker.textContent=[card.country,card.operator].filter(Boolean).join(' · ')||'SIM / ESIM';const title=document.createElement('h3');title.textContent=card.label;heading.append(kicker,title);const status=document.createElement('span');status.className=`retention-status ${card.status||'unknown'}`;status.textContent=card.status_text||'待设置';head.append(heading,status);const meta=document.createElement('div');meta.className='retention-meta';let remaining='未设置';if(card.next_due)remaining=card.days_remaining>=0?`剩余 ${card.days_remaining} 天`:`已过 ${Math.abs(card.days_remaining)} 天`;meta.append(retentionMeta('下次保号',card.next_due||'未设置'),retentionMeta('距离日期',remaining),retentionMeta('号码',card.phone||'未填写'),retentionMeta('ICCID',card.iccid?`···${card.iccid.slice(-6)}`:'未绑定'));const note=document.createElement('p');note.className='retention-note';note.textContent=card.action_note||'未填写保号动作说明。';const actions=document.createElement('div');actions.className='retention-actions';const complete=document.createElement('button');complete.textContent='已完成保号';complete.onclick=()=>completeRetention(card,complete);const verify=document.createElement('button');verify.className='secondary';verify.textContent='验证当前卡';verify.onclick=()=>runRetentionCommand(verify,'verify_retention',{id:card.id},'当前卡已验证');const edit=document.createElement('button');edit.className='secondary';edit.textContent='编辑';edit.onclick=()=>openRetentionForm(card);const remove=document.createElement('button');remove.className='danger';remove.textContent='删除';remove.onclick=()=>deleteRetention(card,remove);actions.append(complete,verify,edit,remove);article.append(head,meta,note,actions);return article}));
}
async function loadRetention(showMessage=true){try{const result=await command('read_retention',undefined,{silent:true});renderRetention(commandData(result)||{});if(showMessage)toast('保号资料已刷新')}catch(error){if(showMessage)toast(error.message)}}
async function runRetentionCommand(button,name,payload,success){button.disabled=true;try{const result=await command(name,payload,{success});renderRetention(commandData(result)||{});return true}catch(error){toast(error.message);return false}finally{button.disabled=false}}
async function completeRetention(card,button){const values=await DJSpatial.prompt({title:'更新下次保号日期',message:card.label,fields:[{name:'date',label:'下次保号日期',type:'date',value:card.next_due||'',required:true}]});if(!values?.date)return;await runRetentionCommand(button,'complete_retention',{id:card.id,next_due:values.date},'下次保号日期已更新')}
async function deleteRetention(card,button){if(!await confirmAction(`删除“${card.label}”的保号档案？`,'删除档案'))return;await runRetentionCommand(button,'delete_retention',{id:card.id},'保号档案已删除')}
$('retention-new').onclick=()=>openRetentionForm();$('retention-cancel').onclick=closeRetentionForm;$('retention-refresh').onclick=()=>loadRetention(true);
$('retention-form').onsubmit=async event=>{event.preventDefault();const button=event.submitter,payload={id:retentionField('id').value,label:retentionField('label').value.trim(),country:retentionField('country').value.trim(),operator:retentionField('operator').value.trim(),phone:retentionField('phone').value.trim(),iccid:retentionField('iccid').value.trim(),plan:retentionField('plan').value.trim(),next_due:retentionField('next-due').value,grace_period_days:Number(retentionField('grace').value||0),action_note:retentionField('action-note').value.trim(),source_url:retentionField('source-url').value.trim(),enabled:retentionField('enabled').checked};if(await runRetentionCommand(button,'save_retention',payload,'保号卡片已保存'))closeRetentionForm()};

function applyESIMActivationCode(raw){
  const parsed=window.DJ4HubESIMQR.parseActivationCode(raw);$('esim-activation-code').value=parsed.raw;$('esim-smdp').value=parsed.smdp;$('esim-matching-id').value=parsed.matchingID;$('esim-confirmation').value=parsed.confirmationCode;$('esim-qr-status').textContent='已识别并填入激活信息，请核对后再点击“确认下载”。';toast('eSIM 激活信息已填入');return parsed;
}
function decodeESIMQRCanvas(canvas){if(typeof window.jsQR!=='function')throw new Error('二维码解码器未加载');const context=canvas.getContext('2d',{willReadFrequently:true}),image=context.getImageData(0,0,canvas.width,canvas.height);return window.jsQR(image.data,image.width,image.height,{inversionAttempts:'attemptBoth'})}
function closeESIMQRScanner(){state.qr.scanning=false;if(state.qr.frame)cancelAnimationFrame(state.qr.frame);state.qr.frame=0;if(state.qr.stream)state.qr.stream.getTracks().forEach(track=>track.stop());state.qr.stream=null;$('esim-qr-video').srcObject=null;$('esim-qr-scanner').hidden=true}
function scanESIMQRFrame(){
  if(!state.qr.scanning)return;const video=$('esim-qr-video');
  if(video.readyState>=HTMLMediaElement.HAVE_CURRENT_DATA&&video.videoWidth&&video.videoHeight){const canvas=$('esim-qr-canvas'),scale=Math.min(1,960/video.videoWidth);canvas.width=Math.max(1,Math.round(video.videoWidth*scale));canvas.height=Math.max(1,Math.round(video.videoHeight*scale));const context=canvas.getContext('2d',{willReadFrequently:true});context.drawImage(video,0,0,canvas.width,canvas.height);const result=decodeESIMQRCanvas(canvas);if(result?.data){try{applyESIMActivationCode(result.data);closeESIMQRScanner();return}catch(error){$('esim-camera-status').textContent=`识别到二维码，但${error.message}。`}}}
  state.qr.frame=requestAnimationFrame(scanESIMQRFrame);
}
async function openESIMQRScanner(){
  $('esim-qr-scanner').hidden=false;$('esim-camera-status').textContent='正在请求摄像头权限…';
  try{if(!navigator.mediaDevices?.getUserMedia)throw new Error('当前浏览器不支持摄像头读取');state.qr.stream=await navigator.mediaDevices.getUserMedia({video:{facingMode:{ideal:'environment'}},audio:false});const video=$('esim-qr-video');video.srcObject=state.qr.stream;await video.play();$('esim-camera-status').textContent='将 eSIM 二维码放入取景框。画面只在本机解析。';state.qr.scanning=true;scanESIMQRFrame()}
  catch(error){closeESIMQRScanner();$('esim-qr-status').textContent=`无法打开摄像头：${error.message}。可改用“选择二维码图片”。`;toast('无法打开摄像头')}
}
async function decodeESIMQRFile(file){if(!file||!file.type.startsWith('image/'))throw new Error('请选择二维码图片');const bitmap=await createImageBitmap(file);try{const scale=Math.min(1,1600/Math.max(bitmap.width,bitmap.height)),canvas=$('esim-qr-canvas');canvas.width=Math.max(1,Math.round(bitmap.width*scale));canvas.height=Math.max(1,Math.round(bitmap.height*scale));canvas.getContext('2d',{willReadFrequently:true}).drawImage(bitmap,0,0,canvas.width,canvas.height);const result=decodeESIMQRCanvas(canvas);if(!result?.data)throw new Error('图片中没有识别到二维码');applyESIMActivationCode(result.data)}finally{bitmap.close()}}
$('esim-scan-qr').onclick=openESIMQRScanner;$('esim-qr-close').onclick=closeESIMQRScanner;$('esim-qr-cancel').onclick=closeESIMQRScanner;$('esim-qr-scanner').onclick=event=>{if(event.target===$('esim-qr-scanner'))closeESIMQRScanner()};$('esim-choose-qr').onclick=()=>$('esim-qr-file').click();$('esim-qr-file').onchange=async event=>{const file=event.target.files?.[0];event.target.value='';if(!file)return;$('esim-qr-status').textContent='正在本机识别二维码…';try{await decodeESIMQRFile(file)}catch(error){$('esim-qr-status').textContent=`识别失败：${error.message}`;toast(error.message)}};window.addEventListener('keydown',event=>{if(event.key==='Escape'&&!$('esim-qr-scanner').hidden)closeESIMQRScanner()});

async function loadESIM(showMessage=true){const requestedDevice=state.deviceId,request=state.esimRequest=(state.esimRequest||0)+1;const button=$('load-esim');button.disabled=true;try{const result=await command('read_esim',undefined,{silent:true}),data=commandData(result)||{};if(requestedDevice!==state.deviceId||request!==state.esimRequest)return;renderESIM(data);if(showMessage)toast('卡片信息已更新')}catch(error){if(requestedDevice!==state.deviceId||request!==state.esimRequest)return;$('esim-summary').textContent=error.message;if(showMessage)toast(error.message)}finally{if(request===state.esimRequest)button.disabled=false}}
function renderESIM(data){
  state.esimData=data;
  const summary=$('esim-summary'),download=$('esim-download-panel');if(data.card_type==='physical_sim'){summary.textContent=data.message||'当前为实体 SIM 卡，无 eSIM Profile 可管理';$('esim-profiles').replaceChildren();download.hidden=true;return}
  download.hidden=false;
  const chip=data.chip_info||{};const eid=Array.isArray(chip.eids)&&chip.eids[0]?.eid;summary.textContent=[chip.sku_name||'已识别 eUICC',chip.firmware?`固件 ${chip.firmware}`:'',eid?`EID ···${String(eid).slice(-8)}`:''].filter(Boolean).join(' · ');
  const profiles=(data.profiles||[]).flatMap(group=>(group.profiles||[]).map(profile=>({...profile,aid:profile.aid||profile.aid_hex||group.aid_hex||group.aid||''})));
  $('esim-profiles').replaceChildren(...profiles.map(profile=>{const card=document.createElement('article');card.className='profile'+(profile.state===1?' enabled':'');const stateEl=document.createElement('span');stateEl.className='badge'+(profile.state===1?' online':'');stateEl.textContent=profile.state===1?'已启用':'未启用';const title=document.createElement('h3');title.textContent=profile.name||profile.service_provider_name||'未命名 Profile';const meta=document.createElement('p');meta.textContent=`${profile.service_provider_name||'未知运营商'} · ICCID ···${String(profile.iccid||'').slice(-6)}`;const actions=document.createElement('div');actions.className='profile-actions';
    if(profile.state!==1){const enable=document.createElement('button');enable.textContent='启用';enable.onclick=async()=>{if(await confirmAction(`切换到 ${title.textContent}？模块会短暂重新注册网络。`,'启用 Profile')){await runButton(enable,'switch_esim',{iccid:profile.iccid,aid:profile.aid}).then(result=>{const data=commandData(result)||{};markESIMActive(profile.iccid);if(data.status)renderDeviceStatus(data.status);toast(data.host_restart_scheduled?`已切换到 ${title.textContent}，设备主机正在自动重连`:(data.module_reboot_warning||'Profile 已切换，正在等待新卡注册'));setTimeout(()=>void loadDevice(),(data.reconnect_wait_seconds||15)*1000)}).catch(()=>{})}};actions.append(enable)}
    const rename=document.createElement('button');rename.className='secondary';rename.textContent='改名';rename.onclick=async()=>{const values=await DJSpatial.prompt({title:'修改 Profile 名称',fields:[{name:'name',label:'新名称',value:title.textContent,required:true,maxLength:120}]});if(values?.name)await runButton(rename,'rename_esim',{iccid:profile.iccid,aid:profile.aid,name:values.name}).then(()=>loadESIM(false)).catch(()=>{})};
    const editNote=async(button,commandName,label)=>{const values=await DJSpatial.prompt({title:label,fields:[{name:'label',label:'备注名称',value:title.textContent},{name:'phone',label:'手机号（可空）',type:'tel'},{name:'tags',label:'标签（可空）'}]});if(values)await runButton(button,commandName,{iccid:profile.iccid,...values}).catch(()=>{})};
    const notes=document.createElement('button');notes.className='secondary';notes.textContent='电脑备注';notes.onclick=()=>editNote(notes,'save_esim_note','电脑备注');
    const moduleNotes=document.createElement('button');moduleNotes.className='secondary';moduleNotes.textContent='存到模块';moduleNotes.onclick=()=>editNote(moduleNotes,'save_module_esim_note','模块资料');
    const remove=document.createElement('button');remove.className='danger';remove.textContent='删除';remove.disabled=profile.state===1;remove.title=profile.state===1?'已启用的 Profile 不可删除':'';remove.onclick=async()=>{if(await confirmAction(`永久删除 ${title.textContent}？该操作不可撤销。`,'永久删除'))await runButton(remove,'delete_esim',{iccid:profile.iccid,aid:profile.aid}).then(()=>loadESIM(false)).catch(()=>{})};actions.append(rename,notes,moduleNotes,remove);card.append(stateEl,title,meta,actions);return card}))
}
function markESIMActive(iccid){if(!state.esimData)return;const data=JSON.parse(JSON.stringify(state.esimData));for(const group of data.profiles||[]){for(const profile of group.profiles||[]){profile.state=profile.iccid===iccid?1:0;profile.state_text=profile.state===1?'已启用':'已禁用'}}renderESIM(data)}
$('load-esim').onclick=()=>loadESIM(true);
async function showESIMResult(name,button){button.disabled=true;try{const result=await command(name);$('esim-output').hidden=false;$('esim-output').textContent=pretty(commandData(result))}catch(error){toast(error.message)}finally{button.disabled=false}}
$('esim-health').onclick=event=>showESIMResult('read_esim_health',event.currentTarget);$('phonebook-probe').onclick=event=>showESIMResult('probe_esim_phonebook',event.currentTarget);
$('read-esim-notes').onclick=event=>showESIMResult('read_esim_notes',event.currentTarget);$('read-module-notes').onclick=event=>showESIMResult('read_module_esim_notes',event.currentTarget);
$('esim-download-form').onsubmit=async event=>{event.preventDefault();if(!await confirmAction('下载 eSIM Profile 会与 SM-DP+ 服务器通信并写入卡片。','开始下载'))return;const payload={smdp:$('esim-smdp').value.trim(),matching_id:$('esim-matching-id').value.trim(),confirmation_code:$('esim-confirmation').value.trim(),imei:$('esim-imei').value.trim(),aid:$('esim-aid').value.trim()};try{const result=await runButton(event.submitter,'download_esim',payload);$('esim-output').hidden=false;$('esim-output').textContent=pretty(commandData(result));await loadESIM(false)}catch{}};

async function showNetworkResult(name,button,payload){button.disabled=true;try{const result=await command(name,payload);$('network-output').textContent=pretty(commandData(result));return result}catch(error){toast(error.message);$('network-output').textContent=error.message}finally{button.disabled=false}}
$('read-network').onclick=event=>showNetworkResult('read_network',event.currentTarget);$('network-local').onclick=event=>showNetworkResult('read_network_local',event.currentTarget);$('network-traffic').onclick=event=>showNetworkResult('read_network_traffic',event.currentTarget);$('network-activity').onclick=event=>showNetworkResult('read_network_activity',event.currentTarget);$('check-4g').onclick=event=>showNetworkResult('check_4g',event.currentTarget);$('check-proxy').onclick=event=>showNetworkResult('check_proxy',event.currentTarget);$('enable-network').onclick=event=>showNetworkResult('enable_network',event.currentTarget);
$('apn-form').onsubmit=async event=>{event.preventDefault();if(!await confirmAction('修改 APN 可能使数据网络短暂中断。','保存 APN'))return;await showNetworkResult('save_apn',event.submitter,{apn:$('apn').value.trim(),pdn:$('pdn').value})};
document.querySelectorAll('[data-usbnet]').forEach(button=>button.onclick=async()=>{if(await confirmAction(`切换 USB 网卡模式为 ${button.dataset.usbnet}？切换后需重启模块。`,'确认切换'))await showNetworkResult('set_usbnet',button,{mode:Number(button.dataset.usbnet)})});
$('reboot-module').onclick=async event=>{if(await confirmAction('重启模块会造成约 10–30 秒离线，当前通话将中断。','重启模块'))await showNetworkResult('reboot_module',event.currentTarget)};

async function loadHistory(kind){const list=$('history-list');list.textContent='正在读取…';try{const result=await command('read_history',{kind,offset:0}),data=commandData(result)||{},records=data.records||[];if(!records.length){list.textContent='暂无通信记录';return}list.replaceChildren(...records.map(record=>{const row=document.createElement('article');row.className='record';const head=document.createElement('div');head.className='record-head';const title=document.createElement('strong');title.textContent=`${record.kind==='call'?'电话':'短信'} · ${record.direction==='outgoing'?'呼出/发送':'呼入/接收'} · ${record.number||'未知号码'}`;const stamp=document.createElement('time');stamp.textContent=time(record.started_at||record.started);head.append(title,stamp);const detail=document.createElement('p');detail.textContent=record.content||record.state||'';row.append(head,detail);return row}))}catch(error){list.textContent=error.message}}
document.querySelectorAll('[data-history-kind]').forEach(button=>button.onclick=()=>{document.querySelectorAll('[data-history-kind]').forEach(item=>item.classList.toggle('active',item===button));void loadHistory(button.dataset.historyKind)});

async function startAudio(){
  if(!state.deviceId||state.audio.pc)return;
  try{const stream=await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true,noiseSuppression:true,autoGainControl:true}}),session=await api(`/panel/api/devices/${state.deviceId}/audio/sessions`,{method:'POST',body:'{}'}),pc=new RTCPeerConnection({iceServers:session.ice_servers||[]});state.audio={pc,stream,session:session.id,after:0,polling:true};stream.getTracks().forEach(track=>pc.addTrack(track,stream));pc.ontrack=event=>{$('remote-audio').srcObject=event.streams[0]};pc.onicecandidate=event=>{if(event.candidate)sendAudioSignal('candidate',event.candidate.toJSON()).catch(error=>toast(error.message))};pc.onconnectionstatechange=()=>{$('audio-state').textContent=pc.connectionState;$('audio-state').classList.toggle('online',pc.connectionState==='connected')};const offer=await pc.createOffer();await pc.setLocalDescription(offer);await sendAudioSignal('offer',{type:offer.type,sdp:offer.sdp});$('audio-state').textContent='等待设备主机';void pollAudioSignals()}catch(error){toast(error.message);stopAudio(false)}
}
async function sendAudioSignal(type,payload){return api(`/panel/api/devices/${state.deviceId}/audio/sessions/${state.audio.session}/signals`,{method:'POST',body:JSON.stringify({type,payload})})}
async function pollAudioSignals(){while(state.audio.polling&&state.audio.session){try{const result=await api(`/panel/api/devices/${state.deviceId}/audio/sessions/${state.audio.session}/signals?after=${state.audio.after}&wait=20`);for(const message of result.signals||[]){state.audio.after=Math.max(state.audio.after,message.id);if(message.type==='answer')await state.audio.pc.setRemoteDescription(message.payload);else if(message.type==='candidate')await state.audio.pc.addIceCandidate(message.payload);else if(message.type==='hangup'){stopAudio(false);toast('设备主机已断开音频');return}}}catch(error){if(state.audio.polling){toast(error.message);await new Promise(resolve=>setTimeout(resolve,1500))}}}}
function stopAudio(signal=true){const audio=state.audio;audio.polling=false;if(signal&&audio.session)sendAudioSignal('hangup',{}).catch(()=>{});audio.pc?.close();audio.stream?.getTracks().forEach(track=>track.stop());$('remote-audio').srcObject=null;state.audio={pc:null,stream:null,session:'',after:0,polling:false};$('audio-state').textContent='未连接';$('audio-state').classList.remove('online')}
$('audio-start').onclick=()=>startAudio();$('audio-stop').onclick=()=>stopAudio(true);window.addEventListener('beforeunload',()=>stopAudio(true));

void bootstrap();
