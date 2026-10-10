/* node scripts/preview-readme.mjs
 * npm install --no-save marked (optional documentation tooling only).
 * MARKED_MODULE may point to a separately installed ESM package.
 * Generates root README-preview.html for file-based viewing; no remote scripts.
 */
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
const repo=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const {marked}=await import(process.env.MARKED_MODULE?pathToFileURL(process.env.MARKED_MODULE).href:'marked');
const html=marked.parse(fs.readFileSync(path.join(repo,'README.md'),'utf8'));
const page=`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>DJ4GHUB · 老李 Oldlee — GitHub 图文介绍预览</title><style>
*{box-sizing:border-box}body{margin:0;background:#eef2f8;color:#253047;font:16px/1.8 "Microsoft YaHei",system-ui,sans-serif}article{max-width:1110px;margin:30px auto;padding:44px 54px;background:white;border:1px solid #dce5f0;border-radius:28px;box-shadow:0 15px 60px #1e365b0d}img{max-width:100%;height:auto;border-radius:18px}h1,h2,h3{line-height:1.35;color:#1e2e46}h1{font-size:35px}h2{font-size:25px;margin:45px 0 18px;border-bottom:1px solid #e4eaf3;padding-bottom:12px}p{margin:16px 0}a{color:#246da7;text-decoration:none}a:hover{text-decoration:underline}blockquote{margin:22px 0;padding:14px 22px;border-left:4px solid #a4b9d8;border-radius:0 12px 12px 0;background:#f4f7fc;color:#62738c}table{border-collapse:collapse;width:100%;font-size:14px;display:block;overflow:auto}td,th{border:1px solid #dce4f0;padding:12px 15px;min-width:110px}th{background:#f4f7fc;text-align:left}td img{max-width:480px;width:100%;border-radius:10px}pre{padding:20px 22px;background:#1e2b40;color:#deebff;border-radius:14px;overflow:auto;font-size:14px}code{font:14px/1.7 Consolas,monospace}p code,li code,td code{background:#f0f4fa;padding:3px 5px;border-radius:5px}details{background:#f5f8fc;padding:15px 20px;border-radius:14px}li{margin:9px 0}summary{cursor:pointer;font-weight:600}.preview-note{font-size:12px;color:#8190a5;margin-top:30px;text-align:center}@media(max-width:800px){article{padding:22px;margin:12px;border-radius:18px}td img{max-width:100%}}
</style><article>${html}<p class="preview-note">本地 README 排版预览 · GitHub 实际渲染可能略有不同 · 配图均为演示数据</p></article></html>`;
fs.writeFileSync(path.join(repo,'README-preview.html'),page);
console.log('README-preview.html generated.');
