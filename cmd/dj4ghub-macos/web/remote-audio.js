const remoteAudioHost={enabled:false,session:'',after:0,pc:null,input:null,polling:false};

function remoteAudioPreferred(devices,kind,patterns){
  const matches=devices.filter(device=>device.kind===kind);
  return matches.find(device=>patterns.some(pattern=>pattern.test(device.label)))||matches[0]||null;
}

function fillRemoteAudioDevices(select,devices,selected){
  select.replaceChildren(...devices.map(device=>{const option=document.createElement('option');option.value=device.deviceId;option.textContent=device.label||`${device.kind} ${device.deviceId.slice(0,8)}`;option.selected=device.deviceId===selected;return option;}));
  if(!devices.length){const option=document.createElement('option');option.value='';option.textContent='未发现设备';select.append(option)}
}

async function scanRemoteAudioDevices(){
  const permission=await navigator.mediaDevices.getUserMedia({audio:true});permission.getTracks().forEach(track=>track.stop());
  const devices=await navigator.mediaDevices.enumerateDevices();const inputs=devices.filter(device=>device.kind==='audioinput'),outputs=devices.filter(device=>device.kind==='audiooutput');
  const savedIn=localStorage.getItem('dj4ghub.remoteAudioInput')||'',savedOut=localStorage.getItem('dj4ghub.remoteAudioOutput')||'';
  const input=inputs.find(device=>device.deviceId===savedIn)||remoteAudioPreferred(inputs,'audioinput',[/AC Interface/i,/BAIWANG/i,/Quectel/i,/DJI/i]);
  const output=outputs.find(device=>device.deviceId===savedOut)||remoteAudioPreferred(outputs,'audiooutput',[/AS Interface/i,/BAIWANG/i,/Quectel/i,/DJI/i]);
  fillRemoteAudioDevices($('#remote-audio-input'),inputs,input?.deviceId||'');fillRemoteAudioDevices($('#remote-audio-output'),outputs,output?.deviceId||'');
  if(!input||!output)throw new Error('没有发现可用的模块音频输入/输出');
  return {input:input.deviceId,output:output.deviceId};
}

async function enableRemoteAudioHost(){
  try{
    const selected=await scanRemoteAudioDevices();localStorage.setItem('dj4ghub.remoteAudioInput',$('#remote-audio-input').value||selected.input);localStorage.setItem('dj4ghub.remoteAudioOutput',$('#remote-audio-output').value||selected.output);
    remoteAudioHost.enabled=true;remoteAudioHost.polling=true;$('#remote-audio-host-state').textContent='等待云端';$('#remote-audio-host-detail').textContent='音频桥已准备，等待云端控制台发起 WebRTC 会话。';void pollRemoteAudioSessions();
  }catch(error){$('#remote-audio-host-state').textContent='不可用';$('#remote-audio-host-detail').textContent=error.message}
}

async function pollRemoteAudioSessions(){
  while(remoteAudioHost.polling){
    try{const result=await api('/api/remote/audio/sessions?wait=15');const session=(result.sessions||[])[0];if(session&&session.id!==remoteAudioHost.session)await acceptRemoteAudioSession(session)}
    catch(error){if(remoteAudioHost.polling){$('#remote-audio-host-detail').textContent=error.message;await new Promise(resolve=>setTimeout(resolve,1500))}}
  }
}

async function acceptRemoteAudioSession(session){
  stopRemoteAudioPeer(false);remoteAudioHost.session=session.id;remoteAudioHost.after=0;
  const inputId=$('#remote-audio-input').value,outputId=$('#remote-audio-output').value;
  const input=await navigator.mediaDevices.getUserMedia({audio:{deviceId:{exact:inputId},echoCancellation:false,noiseSuppression:false,autoGainControl:false}});remoteAudioHost.input=input;
  const pc=new RTCPeerConnection({iceServers:session.ice_servers||[]});remoteAudioHost.pc=pc;input.getTracks().forEach(track=>pc.addTrack(track,input));
  pc.ontrack=async event=>{const player=$('#remote-audio-host-playback');player.srcObject=event.streams[0];if(typeof player.setSinkId==='function'&&outputId)await player.setSinkId(outputId);await player.play()};
  pc.onicecandidate=event=>{if(event.candidate)sendRemoteAudioHostSignal('candidate',event.candidate.toJSON()).catch(()=>{})};
  pc.onconnectionstatechange=()=>{$('#remote-audio-host-state').textContent=pc.connectionState;if(pc.connectionState==='connected')$('#remote-audio-host-detail').textContent='远程浏览器与模块 USB 音频已连接。';if(['failed','closed','disconnected'].includes(pc.connectionState))stopRemoteAudioPeer(false)};
  $('#remote-audio-host-state').textContent='协商中';void pollRemoteAudioHostSignals();
}

async function sendRemoteAudioHostSignal(type,payload){return api(`/api/remote/audio/sessions/${remoteAudioHost.session}/signals`,{method:'POST',body:JSON.stringify({type,payload})})}
async function pollRemoteAudioHostSignals(){
  const session=remoteAudioHost.session;
  while(remoteAudioHost.polling&&session===remoteAudioHost.session){
    try{const result=await api(`/api/remote/audio/sessions/${session}/signals?after=${remoteAudioHost.after}&wait=15`);for(const signal of result.signals||[]){remoteAudioHost.after=Math.max(remoteAudioHost.after,signal.id);if(signal.type==='offer'){await remoteAudioHost.pc.setRemoteDescription(signal.payload);const answer=await remoteAudioHost.pc.createAnswer();await remoteAudioHost.pc.setLocalDescription(answer);await sendRemoteAudioHostSignal('answer',{type:answer.type,sdp:answer.sdp})}else if(signal.type==='candidate')await remoteAudioHost.pc.addIceCandidate(signal.payload);else if(signal.type==='hangup'){stopRemoteAudioPeer(false);return}}}
    catch(error){if(remoteAudioHost.polling&&session===remoteAudioHost.session){$('#remote-audio-host-detail').textContent=error.message;await new Promise(resolve=>setTimeout(resolve,1200))}}
  }
}

function stopRemoteAudioPeer(signal=true){if(signal&&remoteAudioHost.session)sendRemoteAudioHostSignal('hangup',{}).catch(()=>{});remoteAudioHost.pc?.close();remoteAudioHost.input?.getTracks().forEach(track=>track.stop());$('#remote-audio-host-playback').srcObject=null;remoteAudioHost.pc=null;remoteAudioHost.input=null;remoteAudioHost.session='';remoteAudioHost.after=0;if(remoteAudioHost.enabled)$('#remote-audio-host-state').textContent='等待云端'}
function disableRemoteAudioHost(){remoteAudioHost.polling=false;remoteAudioHost.enabled=false;stopRemoteAudioPeer(true);$('#remote-audio-host-state').textContent='未启用';$('#remote-audio-host-detail').textContent='音频桥已停止。'}

$('#remote-audio-enable').addEventListener('click',enableRemoteAudioHost);$('#remote-audio-disable').addEventListener('click',disableRemoteAudioHost);
$('#remote-audio-input').addEventListener('change',()=>localStorage.setItem('dj4ghub.remoteAudioInput',$('#remote-audio-input').value));$('#remote-audio-output').addEventListener('change',()=>localStorage.setItem('dj4ghub.remoteAudioOutput',$('#remote-audio-output').value));
window.addEventListener('beforeunload',()=>disableRemoteAudioHost());
