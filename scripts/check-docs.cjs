/* Links/assets check without network or GitHub credentials. */
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const repo=path.resolve(__dirname,'..');
const docs=['README.md','CONTRIBUTING.md','SECURITY.md','CHANGELOG.md','THIRD_PARTY_NOTICES.md',...fs.readdirSync(path.join(repo,'docs')).filter(file=>file.endsWith('.md')).map(file=>'docs/'+file)];
let links=0;
for(const file of docs){
 const content=fs.readFileSync(path.join(repo,file),'utf8');
 const references=[...content.matchAll(/\]\(([^\s)]+)(?:\s+"[^"]*")?\)/g)].map(match=>match[1]);
 references.push(...[...content.matchAll(/<img[^>]+src="([^"]+)"/g)].map(match=>match[1]));
 for(const target of references){
  if(/^(https?:|#|mailto:)/.test(target))continue;
  const resolved=path.resolve(repo,path.dirname(file),decodeURIComponent(target.split('#')[0]));
  assert.ok(resolved.startsWith(repo+path.sep),`${file}: path escaped repository`);
  assert.ok(fs.existsSync(resolved),`${file}: missing ${target}`);links++;
 }
}
for(const name of ['hero','benefits','architecture','overview','messages','settings-dark','cloud']){
 const bytes=fs.readFileSync(path.join(repo,'docs','images',name+'.png'));
 assert.equal(bytes.subarray(1,4).toString(),'PNG');
 assert.ok(bytes.length>20000,`${name}: unexpectedly small screenshot/illustration`);
}
console.log(`Documentation check passed: ${docs.length} documents, ${links} local links, 7 PNG assets.`);
