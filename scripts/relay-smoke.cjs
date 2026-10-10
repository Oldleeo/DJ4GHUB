/* Isolated HTTP-behind-proxy-mode smoke. No real devices or private credentials.
 * node scripts/relay-smoke.cjs <relay-binary>
 * The HTTP listener is loopback, never public. Does not verify public TLS/Caddy.
 */
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),net=require('node:net'),crypto=require('node:crypto');
const {spawn}=require('node:child_process');
const repo=path.resolve(__dirname,'..');
function otp(secret){let bits='';for(const ch of secret.trim())bits+='ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(ch).toString(2).padStart(5,'0');const bytes=[];for(let i=0;i+8<=bits.length;i+=8)bytes.push(parseInt(bits.slice(i,i+8),2));const counter=Buffer.alloc(8);counter.writeBigUInt64BE(BigInt(Math.floor(Date.now()/30000)));const hash=crypto.createHmac('sha1',Buffer.from(bytes)).update(counter).digest();const offset=hash.at(-1)&15;return String((hash.readUInt32BE(offset)&0x7fffffff)%1000000).padStart(6,'0');}
async function main(){
 const listener=net.createServer();await new Promise(resolve=>listener.listen(0,'127.0.0.1',resolve));const port=listener.address().port;await new Promise(resolve=>listener.close(resolve));
 const root=path.join(repo,'artifacts','relay-smoke');fs.mkdirSync(root,{recursive:true});const data=fs.mkdtempSync(path.join(root,'isolated-'));
 const child=spawn(path.resolve(process.argv[2]),['-http-only','-listen',`127.0.0.1:${port}`,'-data-dir',data],{stdio:'ignore',windowsHide:true});
 const base=`http://127.0.0.1:${port}`;
 try {
  let ready=false;for(let i=0;i<80;i++){try{if((await fetch(base+'/healthz')).ok){ready=true;break;}}catch{}await new Promise(resolve=>setTimeout(resolve,100));}
  assert.ok(ready,'loopback relay started');
  for(const file of ['master.key','owner.token','panel.password','panel.totp','state.enc']){if(file!=='state.enc')assert.ok(fs.existsSync(path.join(data,file)),`generated ${file}`);}
  const panel=await fetch(base+'/panel/');assert.equal(panel.status,200);assert.ok(panel.headers.get('permissions-policy').includes('camera=(self)'));
  assert.equal((await fetch(base+'/panel/api/devices')).status,401);
  const login=await fetch(base+'/panel/api/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:fs.readFileSync(path.join(data,'panel.password'),'utf8').trim(),otp:otp(fs.readFileSync(path.join(data,'panel.totp'),'utf8'))})});
  assert.equal(login.status,200,'generated password and TOTP work');const cookie=login.headers.get('set-cookie');assert.ok(cookie.includes('Secure')&&cookie.includes('HttpOnly'));
  const session=await login.json();assert.ok(session.csrf);
  const headers={Cookie:cookie.split(';')[0]};assert.equal((await fetch(base+'/panel/api/devices',{headers})).status,200);
  const unauthorized=await fetch(base+'/panel/api/logout',{method:'POST',headers});assert.equal(unauthorized.status,403,'mutations require CSRF');
  const logout=await fetch(base+'/panel/api/logout',{method:'POST',headers:{...headers,'X-CSRF-Token':session.csrf}});assert.equal(logout.status,200);
  assert.equal((await fetch(base+'/panel/api/devices',{headers})).status,401,'logout revokes session');
  console.log('PASS loopback HTTP-proxy mode, generated credentials, TOTP login, secure cookies, authenticated reads, CSRF and logout. No secrets printed.');
 } finally {child.kill();}
}
main().catch(error=>{console.error(error.message);process.exitCode=1;});
