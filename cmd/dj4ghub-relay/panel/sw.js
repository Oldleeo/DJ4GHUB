const CACHE='oldlee-dj4ghub-panel-spatial-v8';
self.addEventListener('install',event=>event.waitUntil(caches.open(CACHE).then(cache=>cache.addAll(['/panel/','/panel/app.css','/panel/app.js','/panel/spatial.css?v=0.6.1','/panel/spatial.js?v=0.6.1','/panel/esim-qr.js','/panel/jsQR.js','/panel/oldlee-avatar.png']))));
self.addEventListener('activate',event=>event.waitUntil(Promise.all([caches.keys().then(keys=>Promise.all(keys.filter(key=>key!==CACHE).map(key=>caches.delete(key)))),self.clients.claim()])));
self.addEventListener('fetch',event=>{if(event.request.method==='GET'&&new URL(event.request.url).pathname.startsWith('/panel/'))event.respondWith(fetch(event.request).catch(()=>caches.match(event.request)));});
self.addEventListener('notificationclick',event=>{event.notification.close();event.waitUntil(self.clients.matchAll({type:'window',includeUncontrolled:true}).then(list=>list[0]?list[0].focus():self.clients.openWindow('/panel/')));});
