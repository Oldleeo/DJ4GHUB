(function (root) {
  "use strict";

  function decodeField(value) {
    const text = String(value || "").trim();
    try {
      return decodeURIComponent(text);
    } catch (_) {
      return text;
    }
  }

  function parseActivationCode(value) {
    let text = String(value || "")
      .replace(/[\u200B-\u200D\uFEFF]/g, "")
      .trim()
      .replace(/^['"]|['"]$/g, "");
    const lpaIndex = text.toUpperCase().indexOf("LPA:1$");
    if (lpaIndex >= 0) text = text.slice(lpaIndex + 4);
    if (!text.startsWith("1$")) {
      throw new Error("不是有效的 LPA eSIM 激活码");
    }
    const fields = text.split("$");
    if (fields[0] !== "1" || fields.length < 2 || fields.length > 4) {
      throw new Error("eSIM 激活码格式不完整");
    }
    const smdp = decodeField(fields[1]);
    const matchingID = decodeField(fields[2]);
    const confirmationCode = decodeField(fields[3]);
    if (!smdp || /\s/.test(smdp)) {
      throw new Error("激活码缺少有效的 SM-DP+ 地址");
    }
    return { smdp, matchingID, confirmationCode, raw: `LPA:${text}` };
  }

  const api = { parseActivationCode };
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  if (root) root.DJ4HubESIMQR = api;
})(typeof window !== "undefined" ? window : undefined);
