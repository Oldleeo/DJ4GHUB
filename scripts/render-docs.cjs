/* Public screenshots: isolated demo backend + synthetic cloud fixtures only.
 * Prerequisites: node, npm install --no-save playwright; npx playwright install chromium
 * Usage: node scripts/render-docs.cjs <path-to-demo-capable-backend>
 * Optional environment: PLAYWRIGHT_MODULE, CHROMIUM_PATH (never committed).
 */
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const net = require('node:net');
const {spawn} = require('node:child_process');
const assert = require('node:assert/strict');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const repo = path.resolve(__dirname, '..');
const images = path.join(repo, 'docs', 'images');
const work = path.join(repo, 'artifacts', 'docs-demo');
fs.mkdirSync(work, {recursive:true});
const fixtureTime = '2026-10-10T04:00:00Z';
const hosts = ['module-a', 'module-b'].map((id,i)=>({id,name:`模块 ${i?'B':'A'} · 演示`,host_name:'演示设备主机',online:true,updated_at:fixtureTime}));
const messages = [
 {id:'demo-1',sender:'+15550100001',content:'【演示消息】这是一条虚构短信，用于展示双模块收件箱。没有真实账号或验证码。',timestamp:fixtureTime},
 {id:'demo-2',sender:'+15550100002',content:'【演示通知】已收到你的测试消息。请确认当前选中的模块。',timestamp:'2026-10-10T03:30:00Z'},
 {id:'demo-3',sender:'+15550100001',content:'【演示消息】Bark / Telegram 需要填写自己的配置。本图不包含任何推送密钥。',timestamp:'2026-10-10T03:10:00Z'},
];
const status = {imei:'000000000001111',iccid:'89000000000000000001',imsi:'000000000000001',phone_number:'+15550100001',operator:'演示运营商 A',sim_inserted:true,signal_dbm:-73,reg_status:1,reg_status_text:'已注册',network_mode:'LTE',firmware:'DEMO-FIRMWARE',usbnet_mode:0};
let authenticated = true;
const commands = new Map();
function result(name) {
 if (name==='read_notifications') return {bark:{enabled:false,configured:false},telegram:{enabled:false,configured:false}};
 if (name==='read_retention') return {cards:[],counts:{}};
 if (name==='read_history') return {records:[]};
 if (name==='read_esim') return {profiles:[]};
 return {};
}
const cloud = http.createServer((request,response)=>{
 const url = new URL(request.url, 'http://127.0.0.1');
 const json = value=>{response.setHeader('Content-Type','application/json');response.end(JSON.stringify(value));};
 if (url.pathname==='/panel/api/session') { response.statusCode=authenticated?200:401; return json(authenticated?{csrf:'demo-only-not-a-secret'}:{error:'请登录'}); }
 if (url.pathname==='/panel/api/login') { authenticated=true; return json({csrf:'demo-only-not-a-secret'}); }
 if (url.pathname==='/panel/api/logout') { authenticated=false; return json({ok:true}); }
 if (url.pathname==='/panel/api/devices') return json({devices:hosts});
 if (url.pathname==='/panel/api/audit') return json({items:[]});
 if (/^\/panel\/api\/devices\/[^/]+$/.test(url.pathname)) {
  const id=url.pathname.split('/').at(-1);
  return json({...hosts.find(h=>h.id===id),snapshot:{status:{...status,operator:id==='module-a'?'演示运营商 A':'演示运营商 B'},sms:messages,calls:[],retention:{cards:[],counts:{}}}});
 }
 const route=url.pathname.match(/^\/panel\/api\/devices\/([^/]+)\/commands(?:\/([^/]+))?$/);
 if (route) {
  if(request.method==='POST') {let body='';request.on('data',part=>body+=part);request.on('end',()=>{const command=JSON.parse(body);assert.ok(command.name.startsWith('read_')||command.name==='refresh_status','No hardware mutations in documentation screenshots');const id=String(commands.size+1);commands.set(id,command);json({id});});return;}
  return json({done:true,result:{ok:true,data:result(commands.get(route[2])?.name)}});
 }
 const name=decodeURIComponent(url.pathname.replace(/^\/panel\//,''))||'index.html';
 if(name.includes('..')||path.isAbsolute(name)) {response.statusCode=404;return response.end();}
 const file=['spatial.js','spatial.css'].includes(name)?path.join(repo,'internal','uiassets',name):path.join(repo,'cmd','dj4ghub-relay','panel',name);
 if(!fs.existsSync(file)){response.statusCode=404;return response.end();}
 response.setHeader('Content-Type',({'.js':'application/javascript','.css':'text/css','.html':'text/html','.png':'image/png','.svg':'image/svg+xml'})[path.extname(file)]||'application/octet-stream');
 response.end(fs.readFileSync(file));
});
async function freePort() { const server=net.createServer();await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const port=server.address().port;await new Promise(resolve=>server.close(resolve));return port; }
async function main() {
 const port=await freePort();
 const backend=spawn(path.resolve(process.argv[2]),['-demo','-listen',`127.0.0.1:${port}`,'-data-dir',work],{stdio:'ignore',windowsHide:true});
 let browser;
 const errors=[];
 try {
  browser=await chromium.launch({headless:true,...(process.env.CHROMIUM_PATH?{executablePath:process.env.CHROMIUM_PATH}:{})});
  const localURL=`http://127.0.0.1:${port}`;
  for(let i=0;i<60;i++){try{if((await fetch(`${localURL}/api/health`)).ok)break;}catch{} await new Promise(resolve=>setTimeout(resolve,100));}
  await new Promise(resolve=>cloud.listen(0,'127.0.0.1',resolve));
  for(const kind of ['local','cloud']) {
   const context=await browser.newContext({viewport:{width:1440,height:1000},serviceWorkers:'block',reducedMotion:'reduce'});
   await context.addInitScript(()=>{localStorage.setItem('dj4ghub-theme','light');localStorage.setItem('dj4ghub-ui-scale','1');});
   await context.route('**/*',async route=>{
    const url=new URL(route.request().url());
    if(url.hostname!=='127.0.0.1') return route.abort();
    if(kind==='local') {
     if(route.request().method()!=='GET') return route.fulfill({status:409,json:{error:'Read-only documentation demo'}});
     if(url.pathname==='/api/status') return route.fulfill({json:status});
     if(url.pathname==='/api/sms') return route.fulfill({json:messages});
     if(url.pathname==='/api/sms/status') return route.fulfill({json:{count:messages.length,polling:true,poll_interval_s:8,auto_cleanup_me:false,last_poll:fixtureTime}});
    }
    return route.continue();
   });
   const page=await context.newPage();page.on('pageerror',error=>errors.push(`${kind}: ${error.message}`));
   await page.goto(kind==='local'?localURL:`http://127.0.0.1:${cloud.address().port}/panel/`,{waitUntil:'networkidle'});
   await page.locator('.workspace-module').first().waitFor();
   if(kind==='local') await page.evaluate(()=>renderHostDevices({active:'module-a',devices:[{key:'module-a',label:'模块 A · 演示',port:'COM-DEMO-A'},{key:'module-b',label:'模块 B · 演示',port:'COM-DEMO-B'}]}));
   await page.evaluate(()=>{const badge=document.createElement('aside');badge.textContent='演示数据 · 非实机截图';badge.style.cssText='position:fixed;right:22px;bottom:14px;z-index:99999;padding:7px 12px;border-radius:12px;background:#33435ee6;color:white;font:12px Microsoft YaHei,sans-serif;pointer-events:none';document.body.append(badge);});
   const nav=kind==='local'?'.task-nav .tab':'#nav .nav-item';
   const views=kind==='local'?[['overview','overview'],['sms','messages'],['settings','settings-dark']]:[['overview','cloud']];
   for(const [view,file] of views) {
    await page.locator(`${nav}[data-view="${view}"]`).click();
    if(file==='settings-dark') await page.evaluate(()=>setThemePreference('dark'));
    await page.waitForTimeout(600);
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),`${kind}/${view}: no horizontal overflow`);
    await page.screenshot({path:path.join(images,`${file}.png`)});
   }
   await context.close();
  }
  const page=await browser.newPage();
  for(const file of ['hero','benefits','architecture']) {
   const dimensions=file==='hero'?[1600,680]:file==='benefits'?[1600,590]:[1600,650];
   await page.setViewportSize({width:dimensions[0],height:dimensions[1]});
   await page.goto(new URL(`file://${path.join(images,`${file}.svg`).replaceAll('\\','/')}`).href);
   await page.evaluate(()=>document.fonts.ready);await page.waitForTimeout(150);
   await page.screenshot({path:path.join(images,`${file}.png`)});
  }
  assert.deepEqual(errors,[],'No application JavaScript errors');
  console.log('Rendered 4 UI demo screenshots + 3 illustrations; no private server or physical module accessed.');
 } finally { if(browser)await browser.close();if(cloud.listening)await new Promise(resolve=>cloud.close(resolve));backend.kill(); }
}
main().catch(error=>{console.error(error);process.exitCode=1;});
