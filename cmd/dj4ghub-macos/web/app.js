const $ = (selector) => document.querySelector(selector);
let lastSMSCount = null;
let currentSMSMessages = [];
let esimHealthPollTimer = null;
let esimHealthInFlight = false;
let networkTrafficTimer = null;
let networkTrafficPrevious = null;
let networkTrafficInFlight = false;
let networkActivityTimer = null;
let networkActivityInFlight = false;
let networkActivityCountdown = 5;
let currentSIMPhoneNumber = "";
let simPhoneNumberRevealed = false;
let esimQRStream = null;
let esimQRFrame = 0;
let esimQRScanning = false;
let retentionOverviewState = { cards: [], counts: {} };
let activePageView = "overview";
let overviewDeviceSummary = "正在连接 4G 模块";
let spatialHostDevices = [];

const pageHeaders = {
  overview: { kicker: "DEVICE WORKSPACE", title: "概览", description: "" },
  sms: { kicker: "MESSAGES", title: "短信", description: "收发短信、搜索验证码并管理 Bark / Telegram 转发。" },
  calls: { kicker: "PHONE", title: "电话", description: "拨号、接听、挂断，并管理本机通话音频。" },
  retention: { kicker: "NUMBER RETENTION", title: "保号助手", description: "管理海外 SIM / eSIM 的保号日期、宽限期和提醒。" },
  remote: { kicker: "REMOTE RELAY", title: "远程同步", description: "管理设备主机、授权连接和云端控制。" },
  esim: { kicker: "EUICC", title: "eSIM / 卡片", description: "查看卡片信息，下载、启用、改名或删除 Profile。" },
  network: { kicker: "CONNECTIVITY", title: "网络", description: "检查蜂窝注册、USB 网卡、系统出口与公网连通性。" },
  settings: { kicker: "PREFERENCES & MAINTENANCE", title: "设置与维护", description: "调整阅读体验，检查运行链路并导出隐私诊断。" },
  at: { kicker: "ADVANCED", title: "AT 调试", description: "直接向模块发送 AT 指令，仅供高级诊断使用。" },
};

function renderPageHeader(viewName) {
  const view = pageHeaders[viewName] || pageHeaders.overview;
  activePageView = viewName in pageHeaders ? viewName : "overview";
  $("#page-kicker").textContent = view.kicker;
  $("#page-title").textContent = view.title;
  $("#device-summary").textContent = activePageView === "overview" ? overviewDeviceSummary : view.description;
  document.title = `老李 Oldlee · ${view.title} · DJ4GHUB`;
  const url = new URL(location.href); url.searchParams.set("view", activePageView); history.replaceState(null, "", url);
  window.DJSpatial?.enter($(`#${activePageView}`), activePageView);
}

function setOverviewDeviceSummary(text) {
  overviewDeviceSummary = text || "等待连接 4G 模块";
  if (activePageView === "overview") $("#device-summary").textContent = overviewDeviceSummary;
}

function setThemePreference(theme) {
  if (theme === "light" || theme === "dark") {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("dj4ghub-theme", theme);
    localStorage.removeItem("djonehub-theme");
    localStorage.removeItem("vohive-theme");
  } else {
    delete document.documentElement.dataset.theme;
    localStorage.removeItem("dj4ghub-theme");
    localStorage.removeItem("djonehub-theme");
    localStorage.removeItem("vohive-theme");
  }
  document.querySelectorAll("[data-theme-option]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.themeOption === theme));
  });
  const effectiveTheme = theme === "dark" || (theme !== "light" && matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
  document.documentElement.dataset.theme = effectiveTheme;
  postHostMessage({ type: "window-theme", theme: effectiveTheme });
}

const savedTheme = localStorage.getItem("dj4ghub-theme") || localStorage.getItem("djonehub-theme") || localStorage.getItem("vohive-theme");
setThemePreference(savedTheme === "light" || savedTheme === "dark" ? savedTheme : "light");
document.querySelectorAll("[data-theme-option]").forEach((button) => {
  button.addEventListener("click", () => setThemePreference(button.dataset.themeOption));
});

function postHostMessage(message) {
  try {
    window.chrome?.webview?.postMessage(message);
    return Boolean(window.chrome?.webview);
  } catch (_) {
    return false;
  }
}

// Window controls live in the same themed document as the workspace. These
// messages contain only whitelisted window actions, never modem commands.
if (window.chrome?.webview) {
  const caption = document.createElement("header");
  caption.className = "desktop-window-chrome";
  caption.setAttribute("aria-label", "窗口控制");
  const drag = document.createElement("div");
  drag.className = "desktop-window-drag";
  drag.setAttribute("aria-hidden", "true");
  caption.append(drag);
  const paths = { minimize: "M2 8h10", maximize: "M2 2h10v10H2z", close: "M3 3l8 8M11 3l-8 8" };
  for (const [action, label] of [["minimize", "最小化"], ["maximize", "最大化"], ["close", "关闭到托盘，云端同步继续运行"]]) {
    const button = document.createElement("button");
    button.type = "button"; button.dataset.windowAction = action;
    button.setAttribute("aria-label", label); button.title = label;
    button.innerHTML = `<svg viewBox="0 0 14 14" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="${paths[action]}"/></svg>`;
    button.addEventListener("click", () => postHostMessage({ type: "window-action", action }));
    caption.append(button);
  }
  document.body.prepend(caption);
}
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (!localStorage.getItem("dj4ghub-theme")) setThemePreference("auto");
});

function setUIScale(value) {
  const scale = [1, 1.15, 1.3].includes(Number(value)) ? Number(value) : 1.15;
  localStorage.setItem("dj4ghub-ui-scale", String(scale));
  document.querySelectorAll("[data-ui-scale]").forEach((button) => {
    button.setAttribute("aria-pressed", String(Number(button.dataset.uiScale) === scale));
  });
  if (!postHostMessage({ type: "ui-scale", value: scale })) {
    document.body.style.zoom = String(scale);
  } else {
    document.body.style.zoom = "";
  }
}

function setDensity(value) {
  const density = value === "compact" ? "compact" : "comfortable";
  document.documentElement.dataset.density = density;
  localStorage.setItem("dj4ghub-density", density);
  document.querySelectorAll("button[data-density]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.density === density));
  });
}

function setAdvancedTools(enabled) {
  const visible = Boolean(enabled);
  localStorage.setItem("dj4ghub-advanced", visible ? "1" : "0");
  const toggle = $("#advanced-tools-enabled");
  if (toggle) toggle.checked = visible;
  document.querySelectorAll("[data-advanced]").forEach((element) => { element.hidden = !visible; });
  if (!visible && $("#at")?.classList.contains("active")) {
    document.querySelector('[data-view="settings"]')?.click();
  }
}

function renderHostDevices(message) {
  const devices = Array.isArray(message?.devices) ? message.devices : [];
  spatialHostDevices = devices;
  window.DJSpatial?.moduleCards($("#workspace-modules"), devices.map(device => ({
    key: device.key, label: device.label, meta: device.port || "自动识别",
  })), message.active, key => postHostMessage({ type: "select-device", key, view: activePageView }));
  const switcher = $("#module-switcher");
  const select = $("#module-select");
  if (!switcher || !select) return;
  switcher.hidden = devices.length < 2;
  const options = devices.map((device) => {
    const option = document.createElement("option");
    option.value = device.key;
    option.textContent = `${device.label || "DJI 4G 模块"} · ${device.port || ""}`.replace(/ · $/, "");
    option.selected = device.key === message.active;
    return option;
  });
  select.replaceChildren(...options);
}

window.chrome?.webview?.addEventListener("message", (event) => {
  if (event.data?.type === "device-list") renderHostDevices(event.data);
  if (event.data?.type === "window-state") {
    const button = document.querySelector('[data-window-action="maximize"]');
    if (button) {
      const label = event.data.maximized ? "还原窗口" : "最大化";
      button.title = label; button.setAttribute("aria-label", label);
      button.classList.toggle("is-maximized", Boolean(event.data.maximized));
    }
  }
  if (event.data?.type === "host-state") {
    const status = $("#header-device-state");
    const label = status?.querySelector("span");
    if (label) { label.textContent = String(event.data.text || "设备状态未知"); status.title = label.textContent; }
  }
});
$("#module-select")?.addEventListener("change", (event) => {
  postHostMessage({ type: "select-device", key: event.currentTarget.value, view: activePageView });
});
$("#module-rescan")?.addEventListener("click", () => {
  postHostMessage({ type: "rescan-devices" });
});

document.querySelectorAll("[data-ui-scale]").forEach((button) => {
  button.addEventListener("click", () => setUIScale(button.dataset.uiScale));
});
document.querySelectorAll("button[data-density]").forEach((button) => {
  button.addEventListener("click", () => setDensity(button.dataset.density));
});
$("#advanced-tools-enabled")?.addEventListener("change", (event) => setAdvancedTools(event.currentTarget.checked));
setUIScale(Number(localStorage.getItem("dj4ghub-ui-scale") || 1.15));
setDensity(localStorage.getItem("dj4ghub-density") || "comfortable");
setAdvancedTools(localStorage.getItem("dj4ghub-advanced") === "1");

const operatorNames = new Map([
  ["CHN-UNICOM", "中国联通"],
  ["CHINA UNICOM", "中国联通"],
  ["UNICOM", "中国联通"],
  ["46001", "中国联通"],
  ["46006", "中国联通"],
  ["46009", "中国联通"],
  ["CHINA MOBILE", "中国移动"],
  ["CMCC", "中国移动"],
  ["CHN-CMCC", "中国移动"],
  ["46000", "中国移动"],
  ["46002", "中国移动"],
  ["46004", "中国移动"],
  ["46007", "中国移动"],
  ["46008", "中国移动"],
  ["CHINA TELECOM", "中国电信"],
  ["CHN-CT", "中国电信"],
  ["CTCC", "中国电信"],
  ["46003", "中国电信"],
  ["46005", "中国电信"],
  ["46011", "中国电信"],
  ["CBN", "中国广电"],
  ["CHN-CBN", "中国广电"],
  ["CHINA BROADNET", "中国广电"],
  ["46015", "中国广电"],
]);

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

async function loadNotificationSettings() {
  try {
    const status = await api("/api/notifications");
    $("#telegram-enabled").checked = Boolean(status.telegram?.enabled);
    $("#telegram-chat-id").value = status.telegram?.chat_id || "";
    $("#telegram-token").value = "";
    $("#telegram-token").placeholder = status.telegram?.configured
      ? "已保存，留空保留 Token"
      : "BotFather 提供的 Token";
    $("#telegram-state").textContent = status.telegram?.last_error
      ? `上次失败：${status.telegram.last_error}`
      : (status.telegram?.configured ? "已配置" : "未配置");
    $("#bark-enabled").checked = Boolean(status.bark?.enabled);
    $("#bark-group").value = status.bark?.group || "";
    $("#bark-url").value = "";
    $("#bark-url").placeholder = status.bark?.configured
      ? "已保存，留空保留 URL"
      : "https://api.day.app/你的Key";
    $("#bark-state").textContent = status.bark?.last_error
      ? `上次失败：${status.bark.last_error}`
      : (status.bark?.configured ? "已配置" : "未配置");
    window.DJSpatial?.summary({bark:Boolean(status.bark?.enabled && status.bark?.configured),telegram:Boolean(status.telegram?.enabled && status.telegram?.configured)});
  } catch (error) {
    $("#notification-status").textContent = `读取转发设置失败：${error.message}`;
  }
}

function retentionField(id) { return $(`#retention-${id}`); }
function openRetentionForm(card = {}) {
  $("#retention-form").hidden = false;
  retentionField("form-title").textContent = card.id ? "编辑保号卡片" : "添加保号卡片";
  for (const key of ["id", "label", "country", "operator", "phone", "iccid", "plan"]) retentionField(key).value = card[key] || "";
  retentionField("next-due").value = card.next_due || "";
  retentionField("grace").value = Number(card.grace_period_days || 0);
  retentionField("action-note").value = card.action_note || "";
  retentionField("source-url").value = card.source_url || "";
  retentionField("enabled").checked = card.id ? Boolean(card.enabled) : true;
  $("#retention-form").scrollIntoView({ behavior: "smooth", block: "start" });
  retentionField("label").focus();
}
function closeRetentionForm() {
  $("#retention-form").hidden = true;
  $("#retention-form").reset();
  retentionField("id").value = "";
  retentionField("enabled").checked = true;
}
function retentionMeta(label, value) {
  const box = document.createElement("div");
  const caption = document.createElement("span");
  const content = document.createElement("strong");
  caption.textContent = label;
  content.textContent = value || "—";
  box.append(caption, content);
  return box;
}
function renderRetention(data = {}) {
  retentionOverviewState = { cards: Array.isArray(data.cards) ? data.cards : [], counts: data.counts || {} };
  const counts = retentionOverviewState.counts;
  $("#retention-total").textContent = counts.total || 0;
  $("#retention-due-count").textContent = counts.due || 0;
  $("#retention-urgent-count").textContent = (counts.urgent || 0) + (counts.grace || 0);
  $("#retention-risk-count").textContent = counts.risk || 0;
  const list = $("#retention-list");
  if (!retentionOverviewState.cards.length) {
    const empty = document.createElement("div");
    empty.className = "retention-empty";
    empty.textContent = "还没有保号卡片。添加后会按日期通过已启用的 Bark / Telegram 渠道提醒。";
    list.replaceChildren(empty);
    return;
  }
  list.replaceChildren(...retentionOverviewState.cards.map((card) => {
    const article = document.createElement("article"); article.className = "retention-card";
    const head = document.createElement("div"); head.className = "retention-card-head";
    const heading = document.createElement("div"); const kicker = document.createElement("span"); kicker.className = "section-kicker"; kicker.textContent = [card.country, card.operator].filter(Boolean).join(" · ") || "SIM / ESIM"; const title = document.createElement("h3"); title.textContent = card.label; heading.append(kicker, title);
    const status = document.createElement("span"); status.className = `retention-status ${card.status || "unknown"}`; status.textContent = card.status_text || "待设置"; head.append(heading, status);
    const meta = document.createElement("div"); meta.className = "retention-meta"; let remaining = "未设置"; if (card.next_due) remaining = card.days_remaining >= 0 ? `剩余 ${card.days_remaining} 天` : `已过 ${Math.abs(card.days_remaining)} 天`; meta.append(retentionMeta("下次保号", card.next_due || "未设置"), retentionMeta("距离日期", remaining), retentionMeta("号码", card.phone || "未填写"), retentionMeta("ICCID", card.iccid ? `···${card.iccid.slice(-6)}` : "未绑定"));
    const note = document.createElement("p"); note.className = "retention-note"; note.textContent = card.action_note || "未填写保号动作说明。";
    const actions = document.createElement("div"); actions.className = "retention-actions";
    const complete = document.createElement("button"); complete.type = "button"; complete.textContent = "已完成保号"; complete.addEventListener("click", () => completeRetention(card, complete));
    const verify = document.createElement("button"); verify.type = "button"; verify.className = "secondary"; verify.textContent = "验证当前卡"; verify.addEventListener("click", () => runRetentionAction(verify, "/api/retention/verify", { id: card.id }, "当前卡已验证"));
    const edit = document.createElement("button"); edit.type = "button"; edit.className = "secondary"; edit.textContent = "编辑"; edit.addEventListener("click", () => openRetentionForm(card));
    const remove = document.createElement("button"); remove.type = "button"; remove.className = "danger"; remove.textContent = "删除"; remove.addEventListener("click", () => deleteRetention(card, remove));
    actions.append(complete, verify, edit, remove); article.append(head, meta, note, actions); return article;
  }));
}
async function loadRetention(showMessage = false) {
  try { renderRetention(await api("/api/retention")); if (showMessage) notice("保号资料已刷新"); }
  catch (error) { if (showMessage) notice(error.message); }
}
async function runRetentionAction(button, path, payload, success, method = "POST") {
  button.disabled = true;
  try { renderRetention(await api(path, { method, body: JSON.stringify(payload) })); notice(success); return true; }
  catch (error) { notice(error.message); return false; }
  finally { button.disabled = false; }
}
async function completeRetention(card, button) {
  const nextDue = window.prompt(`${card.label} 已完成本次保号。请输入下次保号日期（YYYY-MM-DD）：`, card.next_due || "");
  if (nextDue) await runRetentionAction(button, "/api/retention/complete", { id: card.id, next_due: nextDue.trim() }, "下次保号日期已更新");
}
async function deleteRetention(card, button) {
  const confirmed = await showModal({ title: "删除保号档案", message: `确定删除“${card.label}”？该卡的提醒也会停止。`, confirmLabel: "删除", danger: true });
  if (confirmed) await runRetentionAction(button, "/api/retention", { id: card.id }, "保号档案已删除", "DELETE");
}

function applyESIMActivationCode(raw) {
  const parsed = window.DJ4HubESIMQR.parseActivationCode(raw);
  $("#esim-activation-code").value = parsed.raw;
  $("#esim-smdp").value = parsed.smdp;
  $("#esim-matching-id").value = parsed.matchingID;
  $("#esim-confirmation-code").value = parsed.confirmationCode;
  $("#esim-qr-status").textContent = "二维码已识别并填入表单；请核对后再手动开始下载。";
  notice("eSIM 激活信息已填入");
  return parsed;
}

function decodeESIMQRFromCanvas(canvas) {
  if (typeof window.jsQR !== "function") throw new Error("本地二维码解码器未加载");
  const context = canvas.getContext("2d", { willReadFrequently: true });
  const image = context.getImageData(0, 0, canvas.width, canvas.height);
  return window.jsQR(image.data, image.width, image.height, { inversionAttempts: "attemptBoth" });
}

function closeESIMQRScanner() {
  esimQRScanning = false;
  if (esimQRFrame) cancelAnimationFrame(esimQRFrame);
  esimQRFrame = 0;
  if (esimQRStream) esimQRStream.getTracks().forEach((track) => track.stop());
  esimQRStream = null;
  $("#esim-qr-video").srcObject = null;
  $("#esim-qr-scanner").hidden = true;
  if ($("#app-modal").hidden) document.body.classList.remove("modal-open");
}

function scanESIMQRFrame() {
  if (!esimQRScanning) return;
  const video = $("#esim-qr-video");
  if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && video.videoWidth && video.videoHeight) {
    const canvas = $("#esim-qr-canvas");
    const maxWidth = 960;
    const scale = Math.min(1, maxWidth / video.videoWidth);
    canvas.width = Math.max(1, Math.round(video.videoWidth * scale));
    canvas.height = Math.max(1, Math.round(video.videoHeight * scale));
    const context = canvas.getContext("2d", { willReadFrequently: true });
    context.drawImage(video, 0, 0, canvas.width, canvas.height);
    const result = decodeESIMQRFromCanvas(canvas);
    if (result?.data) {
      try {
        applyESIMActivationCode(result.data);
        closeESIMQRScanner();
        return;
      } catch (error) {
        $("#esim-camera-status").textContent = `识别到二维码，但${error.message}。`;
      }
    }
  }
  esimQRFrame = requestAnimationFrame(scanESIMQRFrame);
}

async function openESIMQRScanner() {
  const scanner = $("#esim-qr-scanner");
  const status = $("#esim-camera-status");
  scanner.hidden = false;
  document.body.classList.add("modal-open");
  status.textContent = "正在请求摄像头权限…";
  try {
    if (!navigator.mediaDevices?.getUserMedia) throw new Error("当前浏览器不支持摄像头读取");
    esimQRStream = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: { ideal: "environment" } },
      audio: false,
    });
    const video = $("#esim-qr-video");
    video.srcObject = esimQRStream;
    await video.play();
    status.textContent = "将 eSIM 二维码放入取景框。画面只在本机解析。";
    esimQRScanning = true;
    scanESIMQRFrame();
  } catch (error) {
    closeESIMQRScanner();
    $("#esim-qr-status").textContent = `无法打开摄像头：${error.message}。可改用“选择二维码图片”。`;
    notice("无法打开摄像头");
  }
}

async function decodeESIMQRFile(file) {
  if (!file || !file.type.startsWith("image/")) throw new Error("请选择二维码图片");
  const bitmap = await createImageBitmap(file);
  try {
    const maxSize = 1600;
    const scale = Math.min(1, maxSize / Math.max(bitmap.width, bitmap.height));
    const canvas = $("#esim-qr-canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const context = canvas.getContext("2d", { willReadFrequently: true });
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const result = decodeESIMQRFromCanvas(canvas);
    if (!result?.data) throw new Error("图片中没有识别到二维码");
    applyESIMActivationCode(result.data);
  } finally {
    bitmap.close();
  }
}

function notice(message) {
  const el = $("#notice");
  el.textContent = message;
  el.classList.add("show");
  clearTimeout(notice.timer);
  notice.timer = setTimeout(() => el.classList.remove("show"), 2600);
}

let modalResolve = null;

function closeModal(result = null) {
  const modal = $("#app-modal");
  modal.hidden = true;
  document.body.classList.remove("modal-open");
  if (modalResolve) {
    const resolve = modalResolve;
    modalResolve = null;
    resolve(result);
  }
}

function showModal({ title, message = "", fields = [], confirmLabel = "确定", danger = false }) {
  if (modalResolve) closeModal(null);
  const modal = $("#app-modal");
  const messageElement = $("#modal-message");
  const fieldsElement = $("#modal-fields");
  const confirmButton = $("#modal-confirm");
  $("#modal-title").textContent = title;
  messageElement.textContent = message;
  messageElement.hidden = !message;
  fieldsElement.replaceChildren(...fields.map((field) => {
    const label = document.createElement("label");
    label.className = "modal-field";
    const caption = document.createElement("span");
    caption.textContent = field.label;
    const input = document.createElement("input");
    input.name = field.name;
    input.value = field.value || "";
    input.placeholder = field.placeholder || "";
    input.autocomplete = "off";
    if (field.required) input.required = true;
    label.append(caption, input);
    return label;
  }));
  confirmButton.textContent = confirmLabel;
  confirmButton.className = danger ? "danger modal-danger" : "";
  modal.hidden = false;
  document.body.classList.add("modal-open");
  const firstInput = fieldsElement.querySelector("input");
  setTimeout(() => (firstInput || (danger ? $("#modal-cancel") : confirmButton)).focus(), 0);
  return new Promise((resolve) => { modalResolve = resolve; });
}

$("#modal-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const values = {};
  event.currentTarget.querySelectorAll(".modal-fields input").forEach((input) => {
    values[input.name] = input.value.trim();
  });
  closeModal(values);
});
$("#modal-cancel").addEventListener("click", () => closeModal(null));
$("#modal-close").addEventListener("click", () => closeModal(null));
$("#app-modal").addEventListener("click", (event) => {
  if (event.target === event.currentTarget) closeModal(null);
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && !$("#app-modal").hidden) closeModal(null);
  if (event.key === "Escape" && !$("#esim-qr-scanner").hidden) closeESIMQRScanner();
});

async function copySMSCode(code) {
  try {
    await navigator.clipboard.writeText(code);
    notice(`验证码 ${code} 已复制`);
  } catch (error) {
    notice("复制失败，请手动复制验证码");
  }
}

async function copySMSText(text) {
  try {
    await navigator.clipboard.writeText(text);
    notice("短信内容已复制");
  } catch (error) {
    notice("复制失败，请手动复制短信内容");
  }
}

async function deleteInboxSMS(message) {
  const confirmed = await showModal({
    title: "删除这条短信？",
    message: "只删除模块收件箱中的这条短信，通信记录仍会保留。此操作无法撤销。",
    confirmLabel: "删除短信",
    danger: true,
  });
  if (!confirmed) return;
  await api("/api/sms/delete", {
    method: "POST",
    body: JSON.stringify({ id: message.id }),
  });
  notice("短信已删除");
  await loadSMS();
}

function renderSMSInbox() {
  const list = $("#sms-list");
  const query = $("#sms-filter").value.trim().toLocaleLowerCase();
  const visible = currentSMSMessages.filter((message) => {
    if (!query) return true;
    return [message.sender, message.content, message.code].filter(Boolean).join(" ").toLocaleLowerCase().includes(query);
  });
  $("#sms-visible-count").textContent = query ? `${visible.length} / ${currentSMSMessages.length} 条` : `${currentSMSMessages.length} 条`;
  if (window.DJSpatial?.messages) {
    window.DJSpatial.messages(currentSMSMessages, query, {
      copy: copySMSText, copyCode: copySMSCode,
      remove: async (message, button) => {
        button.disabled = true;
        try { await deleteInboxSMS(message); }
        catch (error) { notice(`删除失败：${error.message}`); }
        finally { if (button.isConnected) button.disabled = !message.id; }
      },
    });
    return;
  }
  if (!visible.length) {
    list.className = "list empty";
    list.textContent = currentSMSMessages.length ? "没有匹配的短信" : "暂无短信";
    return;
  }
  list.className = "list sms-inbox-list";
  list.replaceChildren(...visible.map((message) => {
    const row = document.createElement("article");
    row.className = "item sms-item";
    const identity = document.createElement("div");
    identity.className = "sms-identity";
    const sender = document.createElement("strong");
    sender.textContent = message.sender || "未知号码";
    const time = document.createElement("time");
    time.textContent = new Date(message.timestamp).toLocaleString();
    identity.append(sender, time);
    const content = document.createElement("p");
    content.textContent = message.content;
    const actions = document.createElement("div");
    actions.className = "sms-actions";
    if (message.code) {
      const badge = document.createElement("button");
      badge.className = "code-badge sms-code-button";
      badge.type = "button";
      badge.textContent = `验证码 ${message.code}`;
      badge.title = "复制验证码";
      badge.addEventListener("click", () => copySMSCode(message.code));
      actions.append(badge);
    }
    const reply = document.createElement("button");
    reply.className = "secondary compact";
    reply.type = "button";
    reply.textContent = "回复";
    reply.addEventListener("click", () => {
	  const phoneInput = document.getElementById("phone");
	  const senderValue = String(message.sender || "");
	  phoneInput.value = senderValue;
	  phoneInput.setAttribute("value", senderValue);
	  phoneInput.dispatchEvent(new Event("input", { bubbles: true }));
      $("#message").focus();
      notice("已填入收件号码");
    });
    const copy = document.createElement("button");
    copy.className = "secondary compact";
    copy.type = "button";
    copy.textContent = "复制正文";
    copy.addEventListener("click", () => copySMSText(message.content || ""));
    const remove = document.createElement("button");
    remove.className = "secondary danger compact";
    remove.type = "button";
    remove.textContent = "删除";
    remove.disabled = !message.id;
    remove.addEventListener("click", async () => {
      remove.disabled = true;
      try {
        await deleteInboxSMS(message);
      } catch (error) {
        notice(`删除失败：${error.message}`);
      } finally {
        remove.disabled = false;
      }
    });
    actions.append(reply, copy, remove);
    row.append(identity, content, actions);
    return row;
  }));
}

function renderHardwareDetails(status) {
  const panel = $("#hardware-details");
  const device = status.usb_device;
  if (!device) {
    panel.hidden = true;
    panel.replaceChildren();
    return;
  }

  const title = document.createElement("strong");
  title.textContent = device ? "已检测到兼容 USB 设备" : "未检测到可用硬件";

  const detail = document.createElement("p");
  if (device) {
    const interfaceText = Array.isArray(device.interfaces)
      ? `${device.interfaces.length} 个 USB interface`
      : "interface 未知";
    detail.textContent = [
      `${device.vendor || "兼容设备"} ${device.product || ""}`.trim(),
      `${device.vendor_id}:${device.product_id}`,
      device.mode,
      interfaceText,
    ].filter(Boolean).join(" · ");
  } else {
    detail.textContent = status.discovery_error || "设备未枚举";
  }

  const hint = document.createElement("small");
  hint.textContent = status.discovery_error
    ? `当前限制：${status.discovery_error}`
    : "AT 串口可用后，短信和 eSIM/卡片操作会自动启用。";

  panel.hidden = false;
  panel.replaceChildren(title, detail, hint);
}

function setSidebarDeviceState(connected, device = null) {
  const panel = $("#sidebar-device");
  panel.classList.toggle("is-offline", !connected);
  $("#sidebar-device-name").textContent = connected
    ? (device?.product || "4G 模块")
    : "等待设备";
  $("#sidebar-device-state").textContent = connected ? "USB" : "未连接";
}

function setValue(id, text, tone = "") {
  const el = $(id);
  el.textContent = text || "--";
  el.className = tone;
}

function displayOperatorName(value) {
  const raw = String(value || "").trim();
  if (!raw) return "--";
  return operatorNames.get(raw.toUpperCase()) || raw;
}

function displayWorkMode(value) {
	if (value === null || value === undefined || value === "") {
	  return { label: "待读取", tone: "muted" };
	}
  switch (Number(value)) {
    case 0: return { label: "短信模式", tone: "neutral" };
    case 1: return { label: "上网模式", tone: "neutral" };
    case 2: return { label: "实验模式 2", tone: "warn" };
    case 3: return { label: "实验模式 3", tone: "warn" };
    default: return { label: "待读取", tone: "muted" };
  }
}

function setWorkModeControl(value) {
  const currentMode = value === null || value === undefined || value === "" ? -1 : Number(value);
  const smsButton = $("#workmode-sms");
  const networkButton = $("#workmode-network");
  smsButton.setAttribute("aria-pressed", currentMode === 0 ? "true" : "false");
  networkButton.setAttribute("aria-pressed", currentMode === 1 ? "true" : "false");
}

function setUSBNetModeSelector(value) {
  const currentMode = value === null || value === undefined || value === "" ? -1 : Number(value);
  [0, 1, 2, 3].forEach((mode) => {
    $("#usbnet-mode-" + mode).setAttribute("aria-pressed", currentMode === mode ? "true" : "false");
  });
}

function setHeaderDeviceState(connected, label = "设备在线") {
  const indicator = $("#header-device-state");
  indicator.classList.toggle("is-online", connected);
  indicator.classList.toggle("is-offline", !connected);
  indicator.querySelector("span").textContent = label;
}

async function loadSidebarConnection() {
  const panel = $("#sidebar-connection");
  try {
    const connection = await api("/api/network/local");
    if (!connection?.interface) {
      panel.hidden = true;
      return;
    }
    $("#sidebar-connection-detail").textContent = [connection.interface, connection.ipv4].filter(Boolean).join(" · ");
    const state = $("#sidebar-connection-state");
    state.textContent = connection.is_default ? "系统出口" : "已连接";
    state.classList.toggle("is-secondary", !connection.is_default);
    panel.hidden = false;
  } catch (_) {
    panel.hidden = true;
  }
}

function signalTone(dbm) {
  const value = Number(dbm);
  if (!Number.isFinite(value) || value === 0) return "muted";
  if (value >= -65) return "good";
  if (value >= -75) return "signal-fair";
  if (value >= -85) return "warn";
  if (value >= -95) return "orange";
  return "bad";
}

function renderSIMPhoneNumber(value, simInserted) {
  const phoneNumber = String(value || "").trim();
  const empty = $("#sim-phone-empty");
  const actions = $("#sim-phone-actions");
  const toggle = $("#sim-phone-toggle");
  const copy = $("#sim-phone-copy");

  if (!phoneNumber) {
    currentSIMPhoneNumber = "";
    simPhoneNumberRevealed = false;
    empty.textContent = simInserted ? "SIM 未存储号码" : "卡片状态";
    empty.hidden = false;
    actions.hidden = true;
    toggle.textContent = "--";
    copy.disabled = true;
    return;
  }

  if (phoneNumber !== currentSIMPhoneNumber) {
    currentSIMPhoneNumber = phoneNumber;
    simPhoneNumberRevealed = false;
  }
  empty.hidden = true;
  actions.hidden = false;
  toggle.textContent = simPhoneNumberRevealed ? phoneNumber : maskPhoneNumber(phoneNumber);
  toggle.title = simPhoneNumberRevealed ? "隐藏本机号码" : "显示完整本机号码";
  copy.disabled = false;
}

async function loadStatus(refresh = false) {
  try {
    const [health, status] = await Promise.all([
      api("/api/health"),
      api(refresh ? "/api/status?refresh=1" : "/api/status"),
    ]);
    const connected = Boolean(health.at_ready);
    const devicePresent = Boolean(health.device_present);
    setHeaderDeviceState(connected, health.overall_text || (connected ? "设备在线" : "等待设备"));
    setSidebarDeviceState(devicePresent, status.usb_device);
    postHostMessage({ type: "host-status", state: health.overall_state || "waiting_device", text: health.overall_text || "设备状态未知" });
    setValue("#operator", displayOperatorName(status.operator), status.operator ? "neutral" : "muted");
    setValue("#signal", status.signal_dbm ? `${status.signal_dbm} dBm` : "--", signalTone(status.signal_dbm));
    setValue("#network-mode", status.network_mode || status.reg_status_text || "--", status.network_mode ? "neutral" : "muted");
    setValue(
      "#sim",
      !connected ? "通信恢复中" : status.sim_inserted ? "已插入" : (status.sim_state_known === false || status.usb_device ? "待读取" : "未检测到"),
      !connected ? "warn" : status.sim_inserted ? "good" : (status.sim_state_known === false || status.usb_device ? "warn" : "bad"),
    );
    renderSIMPhoneNumber(status.phone_number, status.sim_inserted);
    window.DJSpatial?.summary({ready:connected,sim:status.sim_inserted,network:health.network_registered,cloudConnected:health.remote_connected});
    window.DJSpatial?.card(connected && status.sim_inserted ? displayOperatorName(status.operator) : !connected ? "通信恢复中" : "等待卡片就绪",
      [status.network_mode || status.reg_status_text, currentSIMPhoneNumber ? maskPhoneNumber(currentSIMPhoneNumber) : "SIM 未存储号码"].filter(Boolean).join(" · "));
    if (!spatialHostDevices.length && !window.chrome?.webview) {
      window.DJSpatial?.moduleCards($("#workspace-modules"), [{key:"current",label:status.usb_device?.name || "DJI 4G 模块",meta:health.overall_text || "本地设备",online:devicePresent}], "current", () => {});
    }
    const workMode = Object.prototype.hasOwnProperty.call(status, "usbnet_mode")
      ? displayWorkMode(status.usbnet_mode)
      : displayWorkMode(null);
    setValue("#work-mode", workMode.label, workMode.tone);
    setWorkModeControl(status.usbnet_mode);
    setUSBNetModeSelector(status.usbnet_mode);
    setOverviewDeviceSummary(connected ? "集中管理当前模块的卡片、短信与同步状态。" : "等待连接 4G 模块");
    renderHardwareDetails(status);
  } catch (error) {
    setOverviewDeviceSummary(error.message);
    setHeaderDeviceState(false, "设备离线");
    setSidebarDeviceState(false);
    postHostMessage({ type: "host-status", state: "waiting_device", text: `本地服务异常：${error.message}` });
    renderSIMPhoneNumber("", false);
    setWorkModeControl(null);
  }
}

function setMaintenanceState(selector, text, tone = "") {
  const element = $(selector);
  if (!element) return;
  element.textContent = text;
  element.className = tone ? `is-${tone}` : "";
}

async function loadSystemInfo() {
  const summary = $("#maintenance-health");
  if (!summary) return;
  summary.textContent = "正在检测 USB、AT、SIM、网络和云端链路…";
  try {
    const report = await api("/api/diagnostics");
    const health = report.health || {};
    $("#maintenance-version").textContent = report.app?.version || health.version || "--";
    setMaintenanceState("#maintenance-device", health.device_present ? "已检测" : "未连接", health.device_present ? "good" : "bad");
    setMaintenanceState("#maintenance-at", health.at_ready ? "可用" : "恢复中", health.at_ready ? "good" : "warn");
    setMaintenanceState("#maintenance-sim", health.sim_ready ? "已就绪" : "未就绪", health.sim_ready ? "good" : "warn");
    setMaintenanceState("#maintenance-network", health.network_registered ? "已注册" : "注册中", health.network_registered ? "good" : "warn");
    const cloudText = !health.remote_enabled ? "未启用" : (health.remote_connected ? "已连接" : "待连接");
    setMaintenanceState("#maintenance-cloud", cloudText, health.remote_connected ? "good" : (health.remote_enabled ? "warn" : ""));
    const syncDate = health.remote_last_sync && !String(health.remote_last_sync).startsWith("0001-")
      ? new Date(health.remote_last_sync).toLocaleString()
      : "暂无同步记录";
    $("#maintenance-cloud-detail").textContent = health.remote_error ? `最近错误：${health.remote_error}` : syncDate;
    summary.textContent = health.overall_text || "状态已更新";
    const badge = $("#maintenance-health-badge");
    badge.textContent = health.overall_state === "ready" ? "运行正常" : "需要关注";
    badge.classList.toggle("is-ready", health.overall_state === "ready");
  } catch (error) {
    summary.textContent = `检测失败：${error.message}`;
    $("#maintenance-health-badge").textContent = "检测失败";
  }
}

async function loadSMS() {
  try {
    const [messages, status] = await Promise.all([
      api("/api/sms"),
      api("/api/sms/status"),
    ]);
    const pollText = status.polling
      ? `自动轮询 ${status.poll_interval_s || 8}s`
      : "自动轮询未启用";
    const cleanupText = status.auto_cleanup_me ? "自动清理 ME 已开启" : "自动清理 ME 未开启";
    const errorText = status.last_poll_error ? ` · 最近错误：${status.last_poll_error}` : "";
    $("#sms-status").textContent = `当前缓存 ${messages.length} 条短信 · ${pollText} · ${cleanupText}${errorText}`;
    if (lastSMSCount !== null && messages.length > lastSMSCount) {
      notice(`收到 ${messages.length - lastSMSCount} 条新短信`);
    }
    lastSMSCount = messages.length;
    currentSMSMessages = Array.isArray(messages) ? messages : [];
    renderSMSInbox();
  } catch (error) {
    $("#sms-status").textContent = `读取列表失败：${error.message}`;
    notice(error.message);
  }
}

function profileRows(value) {
  const groups = Array.isArray(value) ? value : value?.profiles || [];
  return groups.flatMap((group) =>
    (group.profiles || []).map((profile) => ({ ...profile, aid: group.aid_hex || "" })),
  );
}

function profileDisplayName(profile) {
  return profile?.name || profile?.service_provider_name || profile?.iccid || "未命名 Profile";
}

function activeProfile(profiles) {
  return profiles.find((profile) => profile.state === 1) || null;
}

function maskIdentifier(value, keep = 4) {
  const text = String(value || "");
  if (text.length <= keep * 2) return text;
  return `${text.slice(0, keep)} ${"•".repeat(Math.max(4, text.length - keep * 2))} ${text.slice(-keep)}`;
}

function maskPhoneNumber(value) {
  const text = String(value || "").trim();
  const digitCount = [...text].filter((char) => /\d/.test(char)).length;
  if (digitCount <= 8) return text;
  let digitIndex = 0;
  return [...text].map((char) => {
    if (!/\d/.test(char)) return char;
    digitIndex += 1;
    return digitIndex > 4 && digitIndex <= digitCount - 4 ? "*" : char;
  }).join("");
}

async function copyIdentifier(value, label) {
  try {
    await navigator.clipboard.writeText(value);
    notice(`${label} 已复制`);
  } catch (error) {
    notice(`复制 ${label} 失败，请手动复制`);
  }
}

async function editProfileNote(profile, note) {
  const values = await showModal({
    title: "编辑模块资料",
    message: "这些资料保存在兼容模块中，并按 ICCID 与当前 Profile 关联。",
    confirmLabel: "保存",
    fields: [
      { name: "label", label: "模块内名称", value: note.label || "", placeholder: "可选" },
      { name: "phone", label: "模块号码", value: note.phone || "", placeholder: "可选" },
      { name: "tags", label: "用途标签", value: note.tags || "", placeholder: "例如：英国验证码" },
    ],
  });
  if (!values) return;
  try {
    await api("/api/esim/module-notes", {
      method: "PUT",
      body: JSON.stringify({ iccid: profile.iccid, label: values.label, phone: values.phone, tags: values.tags }),
    });
    notice("模块资料已保存");
    await loadESIM();
  } catch (error) {
    notice(error.message);
  }
}

function phonebookCheck(label, ok, detail) {
  const card = document.createElement("div");
  card.className = `phonebook-check ${ok ? "ok" : ""}`;
  const title = document.createElement("strong");
  title.textContent = label;
  const text = document.createElement("small");
  text.textContent = detail;
  card.append(title, text);
  return card;
}

async function probeESIMPhonebook() {
  const button = $("#probe-esim-phonebook");
  const status = $("#esim-phonebook-status");
  const resultPanel = $("#esim-phonebook-result");
  button.disabled = true;
  status.textContent = "正在检测卡内通讯录能力，不会写入联系人...";
  resultPanel.hidden = true;
  try {
    const result = await api("/api/esim/phonebook/probe", { method: "POST" });
    const supported = result.storage_supported && result.storage_selected;
    const portable = supported && result.read_supported && result.write_supported;
    status.textContent = portable
      ? "已确认当前 Profile 支持卡内通讯录读写；尚未写入任何联系人。"
      : "当前 Profile 未完整确认卡内通讯录读写能力；不会进行写入。";
    resultPanel.replaceChildren(
      phonebookCheck("SIM 通讯录", result.storage_supported, result.storage_supported ? "支持 SM 卡内存储" : "未发现 SM 卡内存储"),
      phonebookCheck("当前卡片", result.storage_selected, result.storage_selected ? "已安全选中 SM 存储" : "无法选中 SM 存储"),
      phonebookCheck("读取能力", result.read_supported, result.read_supported ? "模块支持读取卡内联系人" : "模块未确认读取命令"),
      phonebookCheck("写入接口", result.write_supported, result.write_supported ? "模块声明支持写入接口" : "模块未确认写入命令"),
      phonebookCheck("当前状态", supported, result.storage_status || "未返回容量信息"),
    );
    resultPanel.hidden = false;
  } catch (error) {
    status.textContent = `通讯录检测失败：${error.message}`;
  } finally {
    button.disabled = false;
  }
}

function esimEIDRows(value) {
  const eids = value?.chip_info?.eids;
  return Array.isArray(eids) ? eids : [];
}

function renderESIMChip(overview) {
  const panel = $("#esim-chip");
  const chip = overview?.chip_info || {};
  const eids = esimEIDRows(overview);
  if (!chip.sku_name && !chip.serial_number && !chip.firmware && !eids.length) {
    panel.hidden = true;
    panel.replaceChildren();
    return;
  }
  panel.hidden = false;
  panel.replaceChildren(
    diagnosticCard("卡类型", chip.sku_name || "eUICC/eSIM 卡片"),
    diagnosticCard("固件", chip.firmware || "--", chip.serial_number ? `序列号 ${chip.serial_number}` : ""),
    diagnosticCard("EID", eids.map((item) => item.eid).filter(Boolean).join(" · ") || "--"),
  );
}

function renderESIMEIDList(overview) {
  const eids = esimEIDRows(overview);
  if (!eids.length) return [];
  return eids.map((item) => {
    const row = document.createElement("article");
    row.className = "item esim-info-row";
    const name = document.createElement("strong");
    name.textContent = "已识别 eUICC";
    const detail = document.createElement("p");
    detail.textContent = [
      item.eid ? `EID ${item.eid}` : "",
      item.aid ? `AID ${item.aid}` : "",
      item.free_nvram ? `可用空间 ${item.free_nvram}` : "",
      item.firmware ? `固件 ${item.firmware}` : "",
    ].filter(Boolean).join("\n");
    const status = document.createElement("small");
    status.textContent = item.spec || item.spec_guess || "eSIM";
    row.append(name, detail, status);
    return row;
  });
}

function renderESIMEIDPanel(rows) {
  if (!rows.length) return null;
  const panel = document.createElement("details");
  panel.className = "esim-euicc-panel";
  const heading = document.createElement("summary");
  heading.className = "esim-euicc-heading";
  const title = document.createElement("strong");
  title.textContent = "已识别 eUICC";
  const hint = document.createElement("small");
  hint.textContent = rows.length > 1 ? `${rows.length} 张 eSIM 卡片` : "卡片信息";
  heading.append(title, hint);
  panel.append(heading, ...rows);
  return panel;
}

async function loadESIMHealth() {
  if (esimHealthInFlight) return;
  esimHealthInFlight = true;
  const section = $("#esim-runtime-section");
  const panel = $("#esim-runtime");
  section.hidden = false;
  panel.replaceChildren(diagnosticCard("Profile 检查", "正在检测"));
  try {
    const health = await api("/api/esim/health");
    if (health.card_type === "physical_sim") {
      section.hidden = true;
      return;
    }
    if (!health.active_profile) {
      panel.replaceChildren(diagnosticCard("Profile 检查", health.message || "未发现已启用 Profile"));
      return;
    }
    const profile = health.active_profile;
    const signal = Number.isFinite(health.signal_dbm) ? `${health.signal_dbm} dBm` : "--";
    panel.replaceChildren(
      diagnosticCard("当前启用", profileDisplayName(profile), profile.iccid ? `ICCID ${maskIdentifier(profile.iccid)}` : ""),
      diagnosticCard("模块实际卡", health.module_iccid ? maskIdentifier(health.module_iccid) : "--", health.imsi ? `IMSI ${health.imsi}` : ""),
      diagnosticCard("蜂窝注册", health.registration || "未注册", [displayOperatorName(health.operator), health.network_mode].filter(Boolean).join(" · ")),
      diagnosticCard("信号", signal, health.registered ? "模块已接管当前 Profile" : "等待网络注册"),
    );
  } catch (error) {
    panel.replaceChildren(diagnosticCard("Profile 检查", "暂时无法读取", error.message));
  } finally {
    esimHealthInFlight = false;
  }
}

function setESIMHealthPolling(enabled) {
  clearInterval(esimHealthPollTimer);
  esimHealthPollTimer = null;
  if (!enabled) return;
  esimHealthPollTimer = setInterval(() => {
    if ($("#esim").classList.contains("active")) void loadESIMHealth();
  }, 30000);
}

function showESIMCardState(title, detail, tone = "") {
  const panel = $("#esim-card-state");
  panel.className = `esim-card-state${tone ? ` ${tone}` : ""}`;
  $("#esim-card-state-title").textContent = title;
  $("#esim-card-state-detail").textContent = detail;
  panel.hidden = false;
}

function hideESIMCardState() {
  $("#esim-card-state").hidden = true;
}

function diagnosticCard(label, value, detail = "") {
  const card = document.createElement("div");
  card.className = "diagnostic-card";
  const span = document.createElement("span");
  span.textContent = label;
  const strong = document.createElement("strong");
  strong.textContent = value || "--";
  card.append(span, strong);
  if (detail) {
    const small = document.createElement("small");
    small.textContent = detail;
    card.append(small);
  }
  return card;
}

function networkPathStep(label, value, detail = "", tone = "") {
  const step = document.createElement("div");
  step.className = `network-path-step ${tone}`.trim();
  const labelNode = document.createElement("span");
  labelNode.textContent = label;
  const valueNode = document.createElement("strong");
  valueNode.textContent = value || "--";
  const detailNode = document.createElement("small");
  detailNode.textContent = detail;
  step.append(labelNode, valueNode, detailNode);
  return step;
}

function networkFact(label, value) {
  const item = document.createElement("div");
  const term = document.createElement("dt");
  term.textContent = label;
  const description = document.createElement("dd");
  description.textContent = value || "--";
  item.append(term, description);
  return item;
}

function renderNetworkCheck(label, result) {
  const list = $("#network-checks");
  list.className = "list";
  const row = document.createElement("article");
  row.className = `item check-item ${result.ok ? "ok" : "bad"}`;
  const name = document.createElement("strong");
  name.textContent = label;
  const detail = document.createElement("p");
  detail.textContent = result.detail || result.summary || "";
  const status = document.createElement("small");
  status.textContent = result.ok ? "通过" : "未通过";
  row.append(name, detail, status);
  const existing = [...list.querySelectorAll(".item")].filter((item) => item.dataset.label !== label);
  row.dataset.label = label;
  list.replaceChildren(row, ...existing);
}

async function runNetworkCheck(label, path, button) {
  button.disabled = true;
  try {
    const result = await api(path, { method: "POST" });
    renderNetworkCheck(label, result);
    notice(result.summary || "检测完成");
  } catch (error) {
    renderNetworkCheck(label, { ok: false, summary: "检测失败", detail: error.message });
    notice(error.message);
  } finally {
    button.disabled = false;
  }
}

function renderNetworkRecovery(diag) {
  const panel = $("#network-recovery");
  const service = diag.network_service;
  if (!service || diag.usb_network_ready) {
    panel.hidden = true;
    return;
  }

  const identity = [service.name, service.device].filter(Boolean).join(" · ");
  const disabled = Boolean(service.disabled);
  $("#network-recovery-title").textContent = disabled
    ? "macOS 已禁用当前 USB 网卡"
    : "USB 网卡尚未取得 DHCP 地址";
  $("#network-recovery-detail").textContent = disabled
    ? `${identity} 已存在，但网络服务处于禁用状态。启用后将重新请求地址。`
    : `${identity} 已启用，但当前没有 IPv4 地址。可以让 macOS 重新请求 DHCP。`;
  $("#enable-network-service").textContent = disabled ? "启用并获取 IP" : "重新请求 DHCP";
  panel.hidden = false;
}

async function loadNetwork() {
  const grid = $("#network-grid");
  const ifaceList = $("#network-interfaces");
  $("#network-status").textContent = "正在读取网络诊断...";
  try {
    const diag = await api("/api/network");
    setUSBNetModeSelector(diag.usbnet_mode);
    const active = Array.isArray(diag.active_contexts) ? diag.active_contexts.join(", ") : "";
    const apns = Array.isArray(diag.pdp_contexts)
      ? diag.pdp_contexts.map((ctx) => `${ctx.id}:${ctx.apn}`).join(" · ")
      : "";
    const addresses = Array.isArray(diag.pdp_addresses) ? diag.pdp_addresses.join(" · ") : "";
    const usb = diag.usb_device
      ? `${diag.usb_device.vendor || ""} ${diag.usb_device.product || ""} (${diag.usb_device.vendor_id}:${diag.usb_device.product_id})`
      : "未检测到";
    const service = diag.network_service;
    let usbNetworkValue = "未识别";
    let usbNetworkDetail = "macOS 网络接口";
    let usbNetworkTone = "is-bad";
    if (service?.disabled) {
      usbNetworkValue = "服务已禁用";
      usbNetworkDetail = [service.name, service.device].filter(Boolean).join(" · ");
    } else if (diag.usb_network_ready) {
      usbNetworkValue = service?.device ? `${service.device} 已连接` : "已连接";
      usbNetworkDetail = service?.ipv4 || "已取得 DHCP 地址";
      usbNetworkTone = "is-good";
    } else if (diag.usb_network_present) {
      usbNetworkValue = "等待 DHCP";
      usbNetworkDetail = service?.device || "macOS 已识别接口";
      usbNetworkTone = "is-warn";
    }
    const route = diag.default_route || {};
    const routeText = route.interface || "未知";
    const routeUsesUSB = Boolean(service?.device && route.interface === service.device);
    let routeDetail = route.gateway ? `网关 ${route.gateway}` : "macOS 当前默认路由";
    if (routeUsesUSB) {
      routeDetail += " · 公网尚未验证";
    }
    const path = document.createElement("div");
    path.className = "network-path";
    path.append(
      networkPathStep("蜂窝数据", active ? `已激活 ${active}` : "未激活", addresses || "等待分配蜂窝 IP", active ? "is-good" : "is-warn"),
      networkPathStep("USB 网卡", usbNetworkValue, usbNetworkDetail, usbNetworkTone),
      networkPathStep("macOS 出口", routeText, routeDetail, routeUsesUSB ? "is-good" : "is-warn"),
    );
    const facts = document.createElement("dl");
    facts.className = "network-facts";
    facts.append(
      networkFact("USBNET", diag.usbnet_mode ?? "未知"),
      networkFact("APN", apns || "无"),
      networkFact("macOS 服务", service ? `${service.name} · ${service.disabled ? "已禁用" : "已启用"}` : "未创建"),
      networkFact("USB 设备", usb),
    );
    grid.className = "network-summary";
    grid.replaceChildren(path, facts);

    const errorText = diag.errors ? ` · 错误：${Object.values(diag.errors).join("；")}` : "";
    if (service?.disabled) {
      $("#network-status").textContent = `macOS 网络服务已禁用${errorText}`;
    } else if (diag.usb_network_ready) {
      $("#network-status").textContent = `USB 网卡已连接并取得地址 · 公网尚未验证${errorText}`;
    } else if (diag.usb_network_present) {
      $("#network-status").textContent = `macOS 已识别 USB 网卡，正在等待 DHCP${errorText}`;
    } else {
      $("#network-status").textContent = `蜂窝侧可能已通，但 macOS 尚未识别 USB 网卡${errorText}`;
    }
    renderNetworkRecovery(diag);

    const interfaces = Array.isArray(diag.mac_interfaces) ? diag.mac_interfaces : [];
    if (!interfaces.length) {
      ifaceList.className = "list empty";
      ifaceList.textContent = "未读取到网络接口";
      return;
    }
    ifaceList.className = "list";
    ifaceList.replaceChildren(...interfaces.map((item) => {
      const row = document.createElement("article");
      row.className = "item";
      const name = document.createElement("strong");
      name.textContent = item.name;
      const detail = document.createElement("p");
      detail.textContent = [item.kind, item.status, item.ipv4].filter(Boolean).join(" · ");
      const status = document.createElement("small");
      status.textContent = item.status === "active" ? "active" : "inactive";
      row.append(name, detail, status);
      return row;
    }));
  } catch (error) {
    $("#network-status").textContent = `读取网络诊断失败：${error.message}`;
    grid.className = "network-summary network-summary-empty";
    grid.textContent = "网络摘要暂不可用";
    ifaceList.className = "list empty";
    ifaceList.textContent = "读取失败";
    $("#network-recovery").hidden = true;
    notice(error.message);
  }
}

async function enableNetworkService() {
  const confirmed = await showModal({
    title: "启用 macOS USB 网卡",
    message: "将启用当前模块对应的网络服务并重新请求 DHCP。macOS 可能要求管理员授权。",
    confirmLabel: "继续启用",
  });
  if (!confirmed) return;
  const button = $("#enable-network-service");
  const originalLabel = button.textContent;
  button.disabled = true;
  button.textContent = "正在启用…";
  try {
    const result = await api("/api/network/enable-service", { method: "POST" });
    notice(result.summary || "网络服务已处理");
    await Promise.all([loadNetwork(), loadSidebarConnection()]);
  } catch (error) {
    notice(error.message);
  } finally {
    button.disabled = false;
    button.textContent = originalLabel;
  }
}

function formatTrafficBytes(value) {
  const bytes = Math.max(0, Number(value || 0));
  const units = ["B", "KB", "MB", "GB", "TB"];
  let amount = bytes;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : (amount >= 100 ? 0 : amount >= 10 ? 1 : 2);
  return `${amount.toFixed(digits)} ${units[unit]}`;
}

async function loadNetworkTraffic() {
  if (networkTrafficInFlight) return;
  networkTrafficInFlight = true;
  try {
    const sample = await api("/api/network/traffic");
    if (!sample.available) {
      networkTrafficPrevious = null;
      setValue("#traffic-rx-rate", "--", "muted");
      setValue("#traffic-tx-rate", "--", "muted");
      setValue("#traffic-session-rx", "--", "muted");
      setValue("#traffic-session-tx", "--", "muted");
      setValue("#traffic-session-total", "--", "muted");
      return;
    }

    let rxRate = 0;
    let txRate = 0;
    const previous = networkTrafficPrevious;
    if (previous && previous.interface === sample.interface) {
      const elapsed = (Number(sample.sampled_at_ms) - Number(previous.sampled_at_ms)) / 1000;
      if (elapsed > 0) {
        rxRate = Math.max(0, Number(sample.rx_bytes) - Number(previous.rx_bytes)) / elapsed;
        txRate = Math.max(0, Number(sample.tx_bytes) - Number(previous.tx_bytes)) / elapsed;
      }
    }
    networkTrafficPrevious = sample;
    setValue("#traffic-rx-rate", `${formatTrafficBytes(rxRate)}/s`, "neutral");
    setValue("#traffic-tx-rate", `${formatTrafficBytes(txRate)}/s`, "neutral");
    setValue("#traffic-session-rx", formatTrafficBytes(sample.session_rx_bytes), "neutral");
    setValue("#traffic-session-tx", formatTrafficBytes(sample.session_tx_bytes), "neutral");
    setValue("#traffic-session-total", formatTrafficBytes(sample.session_total_bytes), "neutral");
    $("#traffic-session-total").title = "本次启动期间的下载与上传流量之和；关闭 DJ 4G Hub 后清零";
  } catch (error) {
    setValue("#traffic-rx-rate", "--", "muted");
    setValue("#traffic-tx-rate", "--", "muted");
    setValue("#traffic-session-total", "--", "muted");
  } finally {
    networkTrafficInFlight = false;
  }
}

function setNetworkTrafficPolling(enabled) {
  clearInterval(networkTrafficTimer);
  networkTrafficTimer = null;
  if (!enabled) {
    networkTrafficPrevious = null;
    return;
  }
  void loadNetworkTraffic();
  networkTrafficTimer = setInterval(loadNetworkTraffic, 1000);
}

function activityInitials(value) {
  return String(value || "?").trim().slice(0, 2).toUpperCase();
}

function renderNetworkActivityCountdown() {
  const label = $("#activity-updated");
  if (!label) return;
  label.textContent = networkActivityInFlight
    ? "正在刷新…"
    : `${networkActivityCountdown} 秒后刷新`;
}

async function loadNetworkActivity() {
  if (networkActivityInFlight) return;
  networkActivityInFlight = true;
  renderNetworkActivityCountdown();
  const list = $("#activity-list");
  try {
    const snapshot = await api("/api/network/activity");
    if (!snapshot.available) {
      $("#activity-physical").textContent = "未检测到 4G 网卡";
      $("#activity-tunnel").textContent = "--";
      $("#activity-count").textContent = "0 个连接";
      list.className = "activity-list empty";
      list.textContent = "当前没有可展示的 4G 联网活动";
      return;
    }
    $("#activity-physical").textContent = `${snapshot.physical_interface}${snapshot.physical_ipv4 ? ` · ${snapshot.physical_ipv4}` : ""}${snapshot.physical_active ? " · 活跃" : ""}`;
    $("#activity-tunnel").textContent = snapshot.tunnel_interface || "直连";
    const connections = Array.isArray(snapshot.connections) ? snapshot.connections : [];
    $("#activity-count").textContent = `${connections.length} 个连接`;
    if (!connections.length) {
      list.className = "activity-list empty";
      list.textContent = "当前没有活跃的应用连接";
      return;
    }
    list.className = "activity-list";
    list.replaceChildren(...connections.map((connection) => {
      const row = document.createElement("article");
      row.className = "activity-row";
      const app = document.createElement("div");
      app.className = "activity-app";
      const glyph = document.createElement("i");
      glyph.textContent = activityInitials(connection.process);
      const process = document.createElement("strong");
      process.textContent = connection.process || "系统";
      app.append(glyph, process);
      const target = document.createElement("div");
      target.className = "activity-target";
      const host = document.createElement("strong");
      host.textContent = connection.host || connection.ip;
      const detail = document.createElement("small");
      detail.textContent = connection.ip
        ? `${connection.ip}${connection.port ? `:${connection.port}` : ""}`
        : (connection.port ? `端口 ${connection.port}` : "目标主机");
      target.append(host, detail);
      const protocol = document.createElement("span");
      protocol.className = "activity-protocol";
      protocol.textContent = connection.protocol || "IP";
      const bytes = document.createElement("span");
      bytes.className = "activity-bytes";
      bytes.textContent = `↓ ${formatTrafficBytes(connection.rx_bytes)} · ↑ ${formatTrafficBytes(connection.tx_bytes)}`;
      row.append(app, target, protocol, bytes);
      return row;
    }));
  } catch (error) {
    list.className = "activity-list empty";
    list.textContent = `联网活动读取失败：${error.message}`;
  } finally {
    networkActivityInFlight = false;
    networkActivityCountdown = 5;
    renderNetworkActivityCountdown();
  }
}

function setNetworkActivityPolling(enabled) {
  clearInterval(networkActivityTimer);
  networkActivityTimer = null;
  if (!enabled) return;
  networkActivityCountdown = 5;
  void loadNetworkActivity();
  networkActivityTimer = setInterval(() => {
    if (networkActivityInFlight) {
      renderNetworkActivityCountdown();
      return;
    }
    networkActivityCountdown -= 1;
    if (networkActivityCountdown <= 0) {
      void loadNetworkActivity();
      return;
    }
    renderNetworkActivityCountdown();
  }, 1000);
}

async function setUSBNetMode(mode) {
  const label = `模式 ${mode}`;
  const confirmed = await showModal({
    title: `切换到${label}`,
    message: `将写入 usbnet=${mode}，重启模块后生效。`,
    confirmLabel: "继续切换",
  });
  if (!confirmed) return;
  try {
    const result = await api("/api/network/usbnet", {
      method: "POST",
      body: JSON.stringify({ mode }),
    });
    notice(`usbnet 已写入 ${result.mode}，请重启模块`);
    await loadNetwork();
  } catch (error) {
    notice(error.message);
  }
}

function wait(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function waitForUSBNetwork(status, timeoutMilliseconds = 60000) {
  const startedAt = Date.now();
  let lastState = "等待模块重新枚举";
  while (Date.now() - startedAt < timeoutMilliseconds) {
    try {
      const diag = await api("/api/network");
      setUSBNetModeSelector(diag.usbnet_mode);
      if (diag.usb_network_ready) return diag;
      if (diag.network_service?.disabled) {
        lastState = `${diag.network_service.name} 已识别，但 macOS 网络服务处于禁用状态`;
      } else if (diag.usb_network_present) {
        lastState = "USB 网卡已出现，正在等待 DHCP 地址";
      } else if (diag.usb_device) {
        lastState = "模块已重新连接，正在等待 USB 网卡出现";
      } else {
        lastState = "USB 正在重新枚举";
      }
    } catch (error) {
      lastState = `等待设备恢复：${error.message}`;
    }
    const secondsLeft = Math.max(1, Math.ceil((timeoutMilliseconds - (Date.now() - startedAt)) / 1000));
    status.textContent = `${lastState} · 最长等待 ${secondsLeft}s`;
    await wait(2500);
  }
  throw new Error(`${lastState}，未能在 60 秒内完成上网准备`);
}

async function switchWorkMode(mode, label, button) {
  if (button.getAttribute("aria-pressed") === "true") {
    notice(`当前已是${label}`);
    return;
  }
  const confirmed = await showModal({
    title: `切换到${label}`,
    message: mode === 1
      ? "将切换并重启模块，等待 macOS 网卡取得地址，再通过百度、Google 等地址自动验证公网。"
      : `将写入 usbnet=${mode} 并重启模块，USB 会短暂断开。`,
    confirmLabel: "确认切换",
  });
  if (!confirmed) return;
  const status = $("#workmode-status");
  const buttons = [$("#workmode-sms"), $("#workmode-network")];
  buttons.forEach((item) => { item.disabled = true; });
  status.hidden = false;
  status.textContent = `正在切到${label}...`;
  try {
    const result = await api("/api/network/usbnet", {
      method: "POST",
      body: JSON.stringify({ mode }),
    });
    status.textContent = `usbnet 已写入 ${result.mode}，正在重启模块...`;
    await api("/api/network/reboot-module", { method: "POST" });
    if (mode === 1) {
      status.textContent = "上网模式已写入，等待 USB 网卡和 DHCP...";
      notice("正在准备上网模式");
      const diag = await waitForUSBNetwork(status);
      const interfaceName = diag.network_service?.device || "USB 网卡";
      status.textContent = `${interfaceName} 已取得地址，正在验证公网...`;
      const connectivity = await api("/api/network/check-4g", { method: "POST" });
      renderNetworkCheck("4G 公网", connectivity);
      await Promise.all([loadStatus(), loadNetwork(), loadSidebarConnection()]);
      if (connectivity.ok) {
        status.textContent = `上网模式已就绪 · ${connectivity.summary}`;
        notice("上网模式已就绪");
      } else {
        status.textContent = `上网模式已切换，但${connectivity.summary}`;
        notice(connectivity.summary);
      }
    } else {
      status.textContent = `${label}已写入，等待模块重新枚举后自动刷新。`;
      notice(`${label}切换中`);
      await wait(9000);
      await Promise.all([loadStatus(), loadNetwork(), loadSidebarConnection()]);
      status.textContent = `${label}切换完成。`;
    }
  } catch (error) {
    status.hidden = false;
    status.textContent = `${label}切换失败：${error.message}`;
    notice(error.message);
  } finally {
    buttons.forEach((item) => { item.disabled = false; });
  }
}

async function rebootModule() {
  const confirmed = await showModal({
    title: "重启模块",
    message: "模块会重新枚举 USB，网页可能短暂断开。",
    confirmLabel: "确认重启",
  });
  if (!confirmed) return;
  try {
    await api("/api/network/reboot-module", { method: "POST" });
    notice("模块正在重启，稍后刷新状态");
    setTimeout(loadStatus, 8000);
    setTimeout(loadNetwork, 12000);
  } catch (error) {
    notice(error.message);
  }
}

async function loadESIM() {
  const list = $("#esim-list");
  const status = $("#esim-status");
  const download = $("#esim-download-section");
  const runtime = $("#esim-runtime-section");
  const profilePanel = $("#esim-profile-panel");
  const phonebook = $("#esim-phonebook-section");
  $("#esim-chip").hidden = true;
  $("#esim-chip").replaceChildren();
  runtime.hidden = true;
  download.hidden = true;
  profilePanel.hidden = true;
  phonebook.hidden = true;
  list.className = "list empty";
  list.textContent = "正在读取 eUICC";
  status.textContent = "正在通过 AT+CCHO/CGLA 读取 eUICC/eSIM 卡片";
  showESIMCardState("正在识别卡片", "正在确认当前卡片是否支持 eUICC Profile 管理。");
  try {
    const overview = await api("/api/esim");
    if (overview.card_type === "physical_sim") {
      status.textContent = overview.message;
      list.textContent = overview.message;
      showESIMCardState(
        "当前是实体 SIM 卡",
        "短信与蜂窝上网功能可以正常使用；这张卡不包含可管理的 eUICC Profile。",
        "is-physical",
      );
      setESIMHealthPolling(false);
      return;
    }
    hideESIMCardState();
    download.hidden = false;
    profilePanel.hidden = false;
    phonebook.hidden = false;
    let notes = {};
    let notesError = "";
    try {
      const notesResponse = await api("/api/esim/module-notes");
      notes = notesResponse.notes || {};
    } catch (error) {
      notesError = error.message || "模块资料库暂不可用";
    }
    const profiles = profileRows(overview);
    const eidRows = renderESIMEIDList(overview);
    const eidPanel = renderESIMEIDPanel(eidRows);
    renderESIMChip(overview);
    const profileCount = profiles.length;
    const eidCount = esimEIDRows(overview).length;
    const active = activeProfile(profiles);
    status.textContent = active
      ? `已读取：${eidCount} 个 eUICC，${profileCount} 个 Profile · 当前使用 ${profileDisplayName(active)}`
      : `已读取：${eidCount} 个 eUICC，${profileCount} 个 Profile · 未发现已启用 Profile`;
    if (notesError) {
      status.textContent += ` · 模块资料暂不可用：${notesError}`;
    }
    if (!profiles.length) {
      if (eidRows.length) {
        list.className = "list";
        list.replaceChildren(eidPanel);
        return;
      }
      list.textContent = "未发现 eUICC/eSIM 卡片参数";
      return;
    }
    list.className = "list";
    const profileItems = profiles.map((profile) => {
      const note = notes[profile.iccid] || {};
      const row = document.createElement("article");
      row.className = `item esim-profile ${profile.state === 1 ? "active" : ""}`;
      const name = document.createElement("strong");
      name.textContent = note.label || profileDisplayName(profile);
      const detail = document.createElement("p");
      detail.textContent = [
        note.label && note.label !== profileDisplayName(profile) ? `卡内名称：${profileDisplayName(profile)}` : "",
        profile.service_provider_name ? `服务商：${profile.service_provider_name}` : "",
        profile.class_text ? `类型：${profile.class_text}` : "",
        note.tags ? `标签：${note.tags}` : "",
      ].filter(Boolean).join("\n");
      const metadata = document.createElement("div");
      metadata.className = "profile-metadata";
      if (note.phone) {
        const phoneRow = document.createElement("div");
        phoneRow.className = "profile-identifier-row";
        const phone = document.createElement("code");
        phone.className = "profile-iccid";
        phone.textContent = `模块号码 ${maskPhoneNumber(note.phone)}`;
        const revealPhone = document.createElement("button");
        revealPhone.className = "secondary compact profile-toggle-button";
        revealPhone.type = "button";
        revealPhone.textContent = "显示";
        revealPhone.addEventListener("click", () => {
          const hidden = revealPhone.textContent === "显示";
          phone.textContent = `模块号码 ${hidden ? note.phone : maskPhoneNumber(note.phone)}`;
          revealPhone.textContent = hidden ? "隐藏" : "显示";
        });
        const copyPhone = document.createElement("button");
        copyPhone.className = "secondary compact profile-copy-button";
        copyPhone.type = "button";
        copyPhone.textContent = "复制号码";
        copyPhone.addEventListener("click", () => copyIdentifier(note.phone, "模块号码"));
        phoneRow.append(phone, revealPhone, copyPhone);
        metadata.append(phoneRow);
      }
      if (profile.iccid) {
        const iccidRow = document.createElement("div");
        iccidRow.className = "profile-identifier-row";
        const iccid = document.createElement("code");
        iccid.className = "profile-iccid";
        iccid.textContent = `ICCID ${maskIdentifier(profile.iccid)}`;
        const reveal = document.createElement("button");
        reveal.className = "secondary compact profile-toggle-button";
        reveal.type = "button";
        reveal.textContent = "显示";
        reveal.addEventListener("click", () => {
          const hidden = reveal.textContent === "显示";
          iccid.textContent = `ICCID ${hidden ? profile.iccid : maskIdentifier(profile.iccid)}`;
          reveal.textContent = hidden ? "隐藏" : "显示";
        });
        const copy = document.createElement("button");
        copy.className = "secondary compact profile-copy-button";
        copy.type = "button";
        copy.textContent = "复制 ICCID";
        copy.addEventListener("click", () => copyIdentifier(profile.iccid, "ICCID"));
        iccidRow.append(iccid, reveal, copy);
        metadata.append(iccidRow);
      }
      const actionBox = document.createElement("div");
      actionBox.className = "profile-actions";
      if (profile.state !== 1) {
        const button = document.createElement("button");
        button.className = "compact";
        button.textContent = "启用";
        button.addEventListener("click", async () => {
          const label = profileDisplayName(profile);
          const confirmed = await showModal({
            title: "启用 Profile",
            message: `确定启用 ${label} 吗？当前正在使用的 eSIM Profile 会被切换。`,
            confirmLabel: "启用",
          });
          if (!confirmed) {
            return;
          }
          button.disabled = true;
          button.textContent = "切换中";
          try {
            const result = await api("/api/esim/switch", {
              method: "POST",
              body: JSON.stringify({ iccid: profile.iccid, aid: profile.aid || "" }),
            });
            if (result.host_restart_scheduled) {
              status.textContent = `已切换到 ${label}，Windows 设备主机正在自动重连（约 ${result.reconnect_wait_seconds || 15} 秒）`;
              notice(`已切换 ${label}，设备主机正在自动重连`);
            } else if (result.identity_refreshed) {
              status.textContent = `已切换到 ${label}，Profile 和模块身份已更新`;
              notice(`已切换 ${label}`);
              await loadESIM();
              await loadStatus(true);
            } else {
              status.textContent = `Profile 已切换到 ${label}，模块身份仍在恢复：${result.module_reboot_warning || "稍后将自动重读"}`;
              notice("Profile 已切换，正在等待新卡注册");
              await loadESIM();
            }
          } catch (error) {
            status.textContent = `切换失败：${error.message}`;
            notice(error.message);
            button.disabled = false;
            button.textContent = "启用";
          }
        });
        actionBox.append(button);
      } else {
        const button = document.createElement("button");
        button.className = "secondary compact";
        button.type = "button";
        button.textContent = "启用";
        button.disabled = true;
        actionBox.append(button);
      }
      const rename = document.createElement("button");
      rename.className = "secondary compact";
      rename.type = "button";
      rename.textContent = "改名";
      rename.addEventListener("click", async () => {
        const values = await showModal({
          title: "修改 Profile 名称",
          message: "名称将写入 eUICC 卡片内部的 Profile nickname。",
          confirmLabel: "保存",
          fields: [{ name: "name", label: "Profile 名称", value: profileDisplayName(profile), required: true }],
        });
        if (!values?.name) return;
        rename.disabled = true;
        try {
          await api("/api/esim/profile", { method: "PATCH", body: JSON.stringify({ iccid: profile.iccid, aid: profile.aid || "", name: values.name }) });
          notice("Profile 名称已修改");
          await loadESIM();
        } catch (error) { notice(error.message); } finally { rename.disabled = false; }
      });
      const localNote = document.createElement("button");
      localNote.className = "secondary compact";
      localNote.type = "button";
      localNote.textContent = "模块资料";
      localNote.disabled = Boolean(notesError);
      if (notesError) localNote.title = notesError;
      localNote.addEventListener("click", () => editProfileNote(profile, note));
      const remove = document.createElement("button");
      remove.className = "secondary danger compact";
      remove.type = "button";
      remove.textContent = "删除";
      remove.disabled = profile.state === 1;
      remove.addEventListener("click", async () => {
        const last4 = String(profile.iccid || "").slice(-4);
        const values = await showModal({
          title: "删除 Profile",
          message: `删除不可恢复。请输入 ICCID 后四位 ${last4} 确认。`,
          confirmLabel: "删除",
          danger: true,
          fields: [{ name: "confirmation", label: "ICCID 后四位", required: true }],
        });
        if (!values) return;
        if (values.confirmation !== last4) {
          notice("ICCID 后四位不匹配，未执行删除");
          return;
        }
        remove.disabled = true;
        try {
          await api("/api/esim/profile", { method: "DELETE", body: JSON.stringify({ iccid: profile.iccid, aid: profile.aid || "" }) });
          notice("Profile 已删除");
          await loadESIM();
        } catch (error) { notice(error.message); } finally { remove.disabled = false; }
      });
      actionBox.append(localNote, rename, remove);
      const description = document.createElement("div");
      description.className = "profile-description";
      description.append(detail, metadata);
      row.append(name, description, actionBox);
      return row;
    });
    list.replaceChildren(...(eidPanel ? [eidPanel] : []), ...profileItems);
    void loadESIMHealth();
    setESIMHealthPolling(true);
  } catch (error) {
    status.textContent = `读取失败：${error.message}`;
    list.textContent = error.message;
    showESIMCardState("暂时无法读取卡片", error.message, "is-error");
    setESIMHealthPolling(false);
  }
}

document.querySelectorAll(".tab").forEach((tab) => {
  tab.addEventListener("click", () => {
    document.querySelectorAll(".tab").forEach((item) => {
      item.classList.remove("active");
      item.removeAttribute("aria-current");
    });
    document.querySelectorAll(".view").forEach((view) => view.classList.remove("active"));
    tab.classList.add("active");
    tab.setAttribute("aria-current", "page");
    $(`#${tab.dataset.view}`).classList.add("active");
    renderPageHeader(tab.dataset.view);
    setNetworkActivityPolling(tab.dataset.view === "overview");
    if (tab.dataset.view === "esim") loadESIM();
    else setESIMHealthPolling(false);
    if (tab.dataset.view === "network") loadNetwork();
    if (tab.dataset.view === "retention") loadRetention();
    if (tab.dataset.view === "settings") loadSystemInfo();
    if (tab.dataset.view === "at") {
      requestAnimationFrame(() => {
        const input = $("#at-command");
        input.focus();
        input.setSelectionRange(input.value.length, input.value.length);
      });
    }
  });
});

$("#esim-scan-qr").addEventListener("click", openESIMQRScanner);
$("#esim-qr-close").addEventListener("click", closeESIMQRScanner);
$("#esim-qr-cancel").addEventListener("click", closeESIMQRScanner);
$("#esim-qr-scanner").addEventListener("click", (event) => {
  if (event.target === event.currentTarget) closeESIMQRScanner();
});
$("#esim-choose-qr").addEventListener("click", () => $("#esim-qr-file").click());
$("#esim-qr-file").addEventListener("change", async (event) => {
  const [file] = event.currentTarget.files;
  event.currentTarget.value = "";
  if (!file) return;
  $("#esim-qr-status").textContent = "正在本机识别二维码图片…";
  try {
    await decodeESIMQRFile(file);
  } catch (error) {
    $("#esim-qr-status").textContent = `识别失败：${error.message}`;
    notice(error.message);
  }
});
$("#esim-parse-code").addEventListener("click", () => {
  try {
    applyESIMActivationCode($("#esim-activation-code").value);
  } catch (error) {
    $("#esim-qr-status").textContent = `解析失败：${error.message}`;
    notice(error.message);
  }
});

$("#esim-download-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const confirmed = await showModal({
    title: "下载新的 Profile",
    message: "将向 SM-DP+ 服务器下载并写入新的 eSIM Profile。写入期间请勿拔出模块。",
    confirmLabel: "开始下载",
  });
  if (!confirmed) return;
  const button = event.currentTarget.querySelector("button[type=submit]");
  const status = $("#esim-download-status");
  button.disabled = true;
  status.textContent = "正在下载并写入 Profile，请勿拔出模块...";
  try {
    const result = await api("/api/esim/download", { method: "POST", body: JSON.stringify({
      smdp: $("#esim-smdp").value, matching_id: $("#esim-matching-id").value,
      confirmation_code: $("#esim-confirmation-code").value, imei: $("#esim-imei").value, aid: $("#esim-aid").value,
    }) });
    status.textContent = result.message || "Profile 下载完成，正在重新读取卡片";
    notice("Profile 下载完成");
    await loadESIM();
  } catch (error) { status.textContent = `下载失败：${error.message}`; notice(error.message); } finally { button.disabled = false; }
});

const messageInput = $("#message");
const messageCounter = $("#message-counter");
const updateMessageCounter = () => {
  messageCounter.textContent = `${messageInput.value.length} 字 · 自动分片`;
};
messageInput.addEventListener("input", updateMessageCounter);
updateMessageCounter();

$("#send-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = event.submitter;
  const originalLabel = button.textContent;
  button.disabled = true;
  button.textContent = "发送中";
  try {
    const result = await api("/api/sms/send", {
      method: "POST",
      body: JSON.stringify({ phone: $("#phone").value, message: $("#message").value }),
    });
    messageInput.value = "";
    updateMessageCounter();
    const segments = Number(result.segments || 1);
    notice(segments > 1 ? `短信已发送（${segments} 个分片）` : "短信已发送");
  } catch (error) {
    notice(error.message);
  } finally {
    button.disabled = false;
    button.textContent = originalLabel;
  }
});

$("#notification-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = $("#notification-save");
  button.disabled = true;
  $("#notification-status").textContent = "正在保存转发设置…";
  try {
    await api("/api/notifications", {
      method: "PUT",
      body: JSON.stringify({
        telegram_enabled: $("#telegram-enabled").checked,
        telegram_bot_token: $("#telegram-token").value.trim(),
        telegram_chat_id: $("#telegram-chat-id").value.trim(),
        bark_enabled: $("#bark-enabled").checked,
        bark_url: $("#bark-url").value.trim(),
        bark_group: $("#bark-group").value.trim(),
      }),
    });
    await loadNotificationSettings();
    $("#notification-status").textContent = "已保存。只转发从现在开始新收到的短信。";
    notice("转发设置已保存");
  } catch (error) {
    $("#notification-status").textContent = `保存失败：${error.message}`;
    notice(error.message);
  } finally {
    button.disabled = false;
  }
});

document.querySelectorAll("[data-notification-test]").forEach((button) => {
  button.addEventListener("click", async () => {
    button.disabled = true;
    const channel = button.dataset.notificationTest;
    $("#notification-status").textContent = `正在测试 ${channel === "telegram" ? "Telegram" : "Bark"}…`;
    try {
      await api("/api/notifications/test", {
        method: "POST",
        body: JSON.stringify({ channel }),
      });
      $("#notification-status").textContent = "测试消息已发送。";
      notice("测试消息已发送");
    } catch (error) {
      $("#notification-status").textContent = `测试失败：${error.message}`;
      notice(error.message);
    } finally {
      button.disabled = false;
    }
  });
});

$("#at-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = $("#at-output");
  const input = $("#at-command");
  const command = input.value.trim();
  if (!command) {
    input.value = "";
    input.focus();
    return;
  }
  output.textContent = `› ${command}\n\n执行中...`;
  input.value = "";
  input.focus();
  try {
    const result = await api("/api/at", {
      method: "POST",
      body: JSON.stringify({ command }),
    });
    output.textContent = `› ${command}\n\n${result.response || "OK"}`;
  } catch (error) {
    output.textContent = `› ${command}\n\n${error.message}`;
  }
});

$("#refresh").addEventListener("click", async () => {
  await Promise.all([loadStatus(true), loadSMS(), loadSidebarConnection()]);
  notice("状态已刷新");
});
$("#sim-phone-toggle").addEventListener("click", () => {
  if (!currentSIMPhoneNumber) return;
  simPhoneNumberRevealed = !simPhoneNumberRevealed;
  renderSIMPhoneNumber(currentSIMPhoneNumber, true);
});
$("#sim-phone-copy").addEventListener("click", () => {
  if (currentSIMPhoneNumber) copyIdentifier(currentSIMPhoneNumber, "本机号码");
});
$("#refresh-sms").addEventListener("click", async () => {
  const button = $("#refresh-sms");
  button.disabled = true;
  $("#sms-status").textContent = "正在读取短信...";
  try {
    const result = await api("/api/sms/refresh", { method: "POST" });
    await loadSMS();
    $("#sms-status").textContent = `短信读取完成：${result.count ?? "未知"} 条`;
    notice("短信读取完成");
  } catch (error) {
    $("#sms-status").textContent = `读取短信失败：${error.message}`;
    notice(error.message);
  } finally {
    button.disabled = false;
  }
});
$("#sms-filter").addEventListener("input", renderSMSInbox);
$("#clear-module-sms").addEventListener("click", async () => {
  const confirmed = await showModal({
    title: "清空模块旧短信",
    message: "只会清空模块内部 ME 存储里的旧短信，不会删除 SIM 卡短信。",
    confirmLabel: "确认清空",
    danger: true,
  });
  if (!confirmed) return;
  const button = $("#clear-module-sms");
  button.disabled = true;
  $("#sms-status").textContent = "正在清空模块内部旧短信...";
  try {
    const result = await api("/api/sms/clear-module", { method: "POST" });
    $("#sms-status").textContent = `模块旧短信已清理：${result.before ?? 0} -> ${result.after ?? 0} 条`;
    await loadSMS();
    notice("模块旧短信已清理");
  } catch (error) {
    $("#sms-status").textContent = `清理模块旧短信失败：${error.message}`;
    notice(error.message);
  } finally {
    button.disabled = false;
  }
});
$("#refresh-esim").addEventListener("click", loadESIM);
$("#probe-esim-phonebook").addEventListener("click", probeESIMPhonebook);
$("#refresh-network").addEventListener("click", loadNetwork);
$("#retention-new").addEventListener("click", () => openRetentionForm());
$("#retention-cancel").addEventListener("click", closeRetentionForm);
$("#retention-refresh").addEventListener("click", () => loadRetention(true));
$("#retention-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = event.submitter;
  const payload = { id: retentionField("id").value, label: retentionField("label").value.trim(), country: retentionField("country").value.trim(), operator: retentionField("operator").value.trim(), phone: retentionField("phone").value.trim(), iccid: retentionField("iccid").value.trim(), plan: retentionField("plan").value.trim(), next_due: retentionField("next-due").value, grace_period_days: Number(retentionField("grace").value || 0), action_note: retentionField("action-note").value.trim(), source_url: retentionField("source-url").value.trim(), enabled: retentionField("enabled").checked };
  if (await runRetentionAction(button, "/api/retention", payload, "保号卡片已保存")) closeRetentionForm();
});
$("#enable-network-service").addEventListener("click", enableNetworkService);
$("#workmode-sms").addEventListener("click", () =>
  switchWorkMode(0, "短信模式", $("#workmode-sms")));
$("#workmode-network").addEventListener("click", () =>
  switchWorkMode(1, "上网模式", $("#workmode-network")));
$("#check-4g-route").addEventListener("click", () =>
  runNetworkCheck("4G 公网", "/api/network/check-4g", $("#check-4g-route")));
$("#check-proxy-route").addEventListener("click", () =>
  runNetworkCheck("代理", "/api/network/check-proxy", $("#check-proxy-route")));
$("#usbnet-mode-0").addEventListener("click", () => setUSBNetMode(0));
$("#usbnet-mode-1").addEventListener("click", () => setUSBNetMode(1));
$("#usbnet-mode-2").addEventListener("click", () => setUSBNetMode(2));
$("#usbnet-mode-3").addEventListener("click", () => setUSBNetMode(3));
$("#reboot-module").addEventListener("click", rebootModule);
$("#maintenance-refresh").addEventListener("click", async (event) => {
  event.currentTarget.disabled = true;
  try {
    await Promise.all([loadStatus(true), loadSystemInfo()]);
    notice("设备链路已重新检测");
  } finally {
    event.currentTarget.disabled = false;
  }
});

const requestedView = new URLSearchParams(window.location.search).get("view");
const requestedTab = requestedView && Object.prototype.hasOwnProperty.call(pageHeaders, requestedView)
  ? document.querySelector(`[data-view="${requestedView}"]`)
  : null;
if (requestedTab && !requestedTab.hidden) requestedTab.click();
else renderPageHeader("overview");
loadStatus();
loadSMS();
loadNotificationSettings();
loadRetention();
loadSidebarConnection();
loadSystemInfo();
setNetworkTrafficPolling(true);
setNetworkActivityPolling(true);
setInterval(loadStatus, 10000);
setInterval(loadSMS, 5000);
setInterval(loadSidebarConnection, 10000);
