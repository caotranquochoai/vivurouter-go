document.addEventListener("DOMContentLoaded", () => {
  document.querySelectorAll("td small").forEach((el) => {
    const text = el.textContent || "";
    if (/^\d{4}-\d{2}-\d{2}T/.test(text)) {
      const d = new Date(text);
      if (!Number.isNaN(d.getTime())) el.textContent = d.toLocaleString();
    }
  });
  initProviderActions();
  initProviderCatalog();
  initProxySearch();
  initAPIKeyManager();
  initPricingManager();
  initComboBuilder();
  initRequestFilters();
  initRequestDebugModal();
  initNotices();
  initAPIKeyQuotas();
  initPricingFilter();
  initCodexResetCredits();
  initProviderModelCollapse();
  initRestoreGuard();
  initClearRequestDebug();
  initRTKStatus();
  initTokenOptimizationPresets();
  initBudgetBars();
  initUsageChart();
  initUsageStatsTable();
  initDashboardQuickStart();
  initRecentRequestsLive();
  initPromptRouterRoles();
  initFusionEditor();
  initSidebar();
});

function t(key, fallback) { return window.VivuRouterI18n?.messages?.[key] || fallback || key; }
function safeJSON(res) { return res.json().catch(() => ({})); }
function escapeHtml(value) { return String(value ?? "").replace(/[&<>'"]/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;",'"':"&quot;"}[c])); }
function showToast(message, ok = true) {
  const toast = document.createElement("div");
  toast.className = `ui-toast ${ok ? "" : "error"}`;
  toast.textContent = message;
  document.body.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add("is-visible"));
  setTimeout(() => { toast.classList.remove("is-visible"); setTimeout(() => toast.remove(), 300); }, 3000);
}
function setChecked(name, checked) { const el = document.querySelector(`[name="${name}"]`); if (el) el.checked = checked; }
function setValue(name, value) { const el = document.querySelector(`[name="${name}"]`); if (el) el.value = value; }

function initSidebar() {
  const toggle = document.querySelector("[data-toggle-sidebar]");
  if (!toggle) return;
  const open = () => { document.body.classList.add("sidebar-open"); toggle.setAttribute("aria-expanded", "true"); };
  const close = () => { document.body.classList.remove("sidebar-open"); toggle.setAttribute("aria-expanded", "false"); };
  toggle.addEventListener("click", () => {
    document.body.classList.contains("sidebar-open") ? close() : open();
  });
  document.querySelectorAll("[data-close-sidebar]").forEach((el) => el.addEventListener("click", close));
  document.querySelectorAll(".sidebar-nav a").forEach((a) => a.addEventListener("click", close));
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") close(); });
}
function csvEscape(value) { const text = String(value ?? "").trim(); return /[",\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text; }
function splitCSV(value) { return String(value || "").split(",").map((x) => x.trim()).filter(Boolean); }

function initProviderCatalog() {
  const search = document.getElementById("provider-catalog-search");
  const cards = [...document.querySelectorAll("[data-provider-card-search]")];
  if (search) search.addEventListener("input", () => {
    const q = search.value.trim().toLowerCase();
    cards.forEach((card) => { card.hidden = !!q && !(card.dataset.providerCardSearch || card.textContent || "").toLowerCase().includes(q); });
  });
  document.addEventListener("click", (event) => {
    const toggle = event.target.closest(".js-provider-add-toggle");
    if (toggle) {
      event.preventDefault();
      const menu = toggle.parentElement?.querySelector(".provider-preset-dropdown");
      if (!menu) return;
      const open = menu.hidden;
      document.querySelectorAll(".provider-preset-dropdown").forEach((m) => m.hidden = true);
      menu.hidden = !open;
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
      return;
    }
    if (!event.target.closest(".provider-add-menu")) document.querySelectorAll(".provider-preset-dropdown").forEach((m) => m.hidden = true);
  });
}

function initProxySearch() {
  const search = document.getElementById("proxy-search");
  const cards = [...document.querySelectorAll("[data-proxy-card-search]")];
  if (search) search.addEventListener("input", () => {
    const q = search.value.trim().toLowerCase();
    cards.forEach((card) => { card.hidden = !!q && !(card.dataset.proxyCardSearch || card.textContent || "").toLowerCase().includes(q); });
  });
}

function openModal(id) { const el = document.getElementById(id); if (el) { el.classList.add("is-open"); el.setAttribute("aria-hidden", "false"); document.body.classList.add("modal-open"); } }
function closeModals() { document.querySelectorAll(".provider-modal").forEach((el) => { el.classList.remove("is-open"); el.setAttribute("aria-hidden", "true"); }); document.body.classList.remove("modal-open"); }
function focusOAuthSection(sectionID) {
  if (!sectionID) return;
  requestAnimationFrame(() => {
    const section = document.getElementById(sectionID);
    if (!section) return;
    section.scrollIntoView({ behavior: "smooth", block: "start" });
    section.focus?.({ preventScroll: true });
  });
}
function initProviderActions() {
  const resultBox = (providerID) => document.querySelector(`[data-provider-result="${CSS.escape(providerID || "")}"]`) || document.querySelector(".provider-action-result");
  const setResult = (providerID, message, ok = true) => { const box = resultBox(providerID); if (box) { box.textContent = message; box.classList.toggle("error", !ok); } showToast(message, ok); };
  const setResultHTML = (providerID, html, toastMessage, ok = true) => { const box = resultBox(providerID); if (box) { box.innerHTML = html; box.classList.toggle("error", !ok); } showToast(toastMessage, ok); };
  const postProviderAction = async (url, payload) => {
    const res = await fetch(url, { method: "POST", cache: "no-store", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify(payload || {}) });
    const data = await safeJSON(res);
    if (!res.ok) {
      const errMsg = data.message || (data.error && typeof data.error === "object" ? data.error.message : data.error) || `HTTP ${res.status}`;
      throw new Error(errMsg);
    }
    return data;
  };
  document.addEventListener("click", async (event) => {
    const resetKeyModal = () => {
      const vi = document.documentElement.lang === "vi";
      const title = document.querySelector("#add-key-modal-title");
      const id = document.getElementById("provider-key-modal-id");
      const name = document.getElementById("provider-key-modal-name");
      const priority = document.getElementById("provider-key-modal-priority");
      const value = document.getElementById("provider-key-modal-value");
      const enabled = document.getElementById("provider-key-modal-enabled");
      const submit = document.getElementById("provider-key-modal-submit");
      if (title) title.textContent = vi ? "Thêm key mới" : "Add new key";
      if (id) id.value = "";
      if (name) name.value = "";
      if (priority) priority.value = "1";
      if (value) { value.value = ""; value.required = true; value.placeholder = "sk-..."; }
      if (enabled) enabled.checked = true;
      if (submit) submit.textContent = vi ? "Thêm key" : "Add key";
    };
    const editKey = event.target.closest(".js-edit-key-btn");
    if (editKey) {
      event.preventDefault();
      const vi = document.documentElement.lang === "vi";
      const title = document.querySelector("#add-key-modal-title");
      const id = document.getElementById("provider-key-modal-id");
      const name = document.getElementById("provider-key-modal-name");
      const priority = document.getElementById("provider-key-modal-priority");
      const value = document.getElementById("provider-key-modal-value");
      const enabled = document.getElementById("provider-key-modal-enabled");
      const submit = document.getElementById("provider-key-modal-submit");
      if (title) title.textContent = vi ? "Sửa key" : "Edit key";
      if (id) id.value = editKey.dataset.keyId || "";
      if (name) name.value = editKey.dataset.keyName || "";
      if (priority) priority.value = editKey.dataset.keyPriority || "1";
      if (value) { value.value = ""; value.required = false; value.placeholder = vi ? "để trống để giữ nguyên" : "leave blank to keep current key"; }
      if (enabled) enabled.checked = editKey.dataset.keyEnabled === "true";
      if (submit) submit.textContent = vi ? "Lưu key" : "Save key";
      openModal("add-key-modal");
      return;
    }
    const addKey = event.target.closest(".js-btn-add-key");
    if (addKey) resetKeyModal();
    const open = event.target.closest("[data-open-modal]");
    if (open) { event.preventDefault(); applyProviderPreset(open.dataset.providerPreset || ""); openModal(open.dataset.openModal); focusOAuthSection(open.dataset.oauthSection || ""); return; }
    if (event.target.closest("[data-close-modal], .provider-modal-close")) { event.preventDefault(); closeModals(); return; }
    const copy = event.target.closest(".js-copy-field");
    if (copy) { const input = copy.parentElement?.querySelector("input,textarea"); if (input) navigator.clipboard?.writeText(input.value || ""); return; }
    const regen = event.target.closest(".js-regenerate-local-key");
    if (regen) { const input = regen.parentElement?.querySelector("input"); if (input) input.value = `sk-local-${crypto.randomUUID?.() || Date.now()}`; return; }
    const createOAuth = event.target.closest(".js-create-oauth-link");
    if (createOAuth) {
      event.preventDefault();
      const form = createOAuth.closest("form") || document;
      const params = new URLSearchParams();
      const providerID = form.querySelector('[name="provider_id"]')?.value || "";
      const proxyURL = form.querySelector('[name="proxy_url"], [data-proxy-input]')?.value || "";
      const proxyID = form.querySelector('[name="proxy_id"]')?.value || "";
      if (providerID) params.set("provider_id", providerID);
      if (proxyID) params.set("proxy_id", proxyID);
      else if (proxyURL) params.set("proxy_url", proxyURL);
      params.set("json", "1");
      const startURL = createOAuth.dataset.oauthStart || "/api/codex/oauth/start";
      createOAuth.disabled = true; createOAuth.textContent = t("providers.create_link", "Create link to copy");
      try {
        const res = await fetch(`${startURL}?${params}`, { method: "POST", cache: "no-store", headers: { Accept: "application/json" } });
        const data = await safeJSON(res);
        if (!res.ok) throw new Error(data.message || data.error || `HTTP ${res.status}`);
        const box = document.getElementById(createOAuth.dataset.oauthBox || "oauth-manual-box");
        const link = document.getElementById(createOAuth.dataset.oauthAuthLink || "oauth-auth-link");
        if (link) link.value = data.auth_url || data.AuthURL || data.status?.auth_url || "";
        if (box) box.hidden = false;
        showToast(t("js.oauth_link_created", "OAuth link created. Copy it to another browser, sign in, then paste the callback/auth link below."));
      } catch (err) { showToast(`${t("js.oauth_link_failed", "Could not create OAuth link")}: ${err.message || err}`, false); }
      finally { createOAuth.disabled = false; createOAuth.textContent = t("providers.create_link", "Create link to copy"); }
      return;
    }
    const copyOAuth = event.target.closest(".js-copy-oauth-link");
    if (copyOAuth) {
      event.preventDefault();
      const link = document.getElementById(copyOAuth.dataset.oauthAuthLink || "oauth-auth-link");
      if (link) navigator.clipboard?.writeText(link.value || "");
      showToast(t("js.oauth_link_copied", "Authorization link copied."));
      return;
    }
    const completeOAuth = event.target.closest(".js-complete-oauth-link");
    if (completeOAuth) {
      event.preventDefault();
      const callback = document.getElementById(completeOAuth.dataset.oauthCallbackLink || "oauth-callback-link")?.value || "";
      completeOAuth.disabled = true;
      try {
        const res = await fetch(completeOAuth.dataset.oauthComplete || "/api/codex/oauth/complete", { method: "POST", cache: "no-store", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ callback_url: callback }) });
        const data = await safeJSON(res);
        if (!res.ok) throw new Error(data.message || data.error || `HTTP ${res.status}`);
        showToast(t("js.oauth_completed", "OAuth completed. Token and proxy were saved to the provider."));
      } catch (err) { showToast(`${t("js.oauth_failed", "OAuth failed")}: ${err.message || err}`, false); }
      finally { completeOAuth.disabled = false; }
      return;
    }
    const proxyTest = event.target.closest(".js-test-proxy");
    if (proxyTest) {
      event.preventDefault();
      const form = proxyTest.closest("form") || document;
      const proxyURL = form.querySelector('[name="proxy_url"], [data-proxy-input]')?.value || "";
      const proxyID = form.querySelector('[name="proxy_id"]')?.value || "";
      proxyTest.disabled = true; proxyTest.textContent = "Testing...";
      try {
        const data = await postProviderAction("/api/providers/proxy-test", { proxy_url: proxyURL, proxy_id: proxyID });
        const ok = !!data.ok;
        const message = ok ? `Proxy OK ${data.status || ""} · ${data.latency_ms || 0}ms` : `Proxy failed: ${data.error || "unknown error"}`;
        setResult("", message, ok);
      } catch (err) { setResult("", err.message || String(err), false); }
      finally { proxyTest.disabled = false; proxyTest.textContent = t("providers.proxy_test", "Test proxy"); }
      return;
    }
    const proxyCardTest = event.target.closest(".js-test-proxy-card");
    if (proxyCardTest) {
      event.preventDefault();
      const proxyID = proxyCardTest.dataset.proxyId || "";
      proxyCardTest.disabled = true;
      const originalText = proxyCardTest.textContent;
      proxyCardTest.textContent = "...";
      try {
        const data = await postProviderAction("/api/providers/proxy-test", { proxy_id: proxyID });
        const ok = !!data.ok;
        const message = ok ? `Proxy OK ${data.status || ""} · ${data.latency_ms || 0}ms` : `Proxy failed: ${data.error || "unknown error"}`;
        showToast(message, ok);
      } catch (err) {
        showToast(err.message || String(err), false);
      } finally {
        proxyCardTest.disabled = false;
        proxyCardTest.textContent = originalText;
      }
      return;
    }
    const fetchModels = event.target.closest(".js-fetch-models");
    if (fetchModels) {
      event.preventDefault();
      const providerID = fetchModels.dataset.providerId || "";
      fetchModels.disabled = true; fetchModels.textContent = "Loading...";
      try {
        const data = await postProviderAction("/api/providers/models", { provider_id: providerID, apply: true });
        const count = Number(data.count || data.models?.length || 0);
        const modelCount = document.querySelector(`[data-model-count="${CSS.escape(providerID)}"]`);
        if (modelCount) modelCount.textContent = String(count);
        setResult(providerID, `Fetched ${count} models${data.saved ? " and saved" : ""}.`, true);
      } catch (err) { setResult(providerID, err.message || String(err), false); }
      finally { fetchModels.disabled = false; fetchModels.textContent = t("providers.fetch_models", "Fetch models"); }
      return;
    }
    const quota = event.target.closest(".js-refresh-claude-quota, .js-refresh-codex-quota, .js-refresh-antigravity-quota");
    if (quota) {
      event.preventDefault();
      const providerID = quota.dataset.providerId || "";
      const isClaude = quota.classList.contains("js-refresh-claude-quota");
      const isAntigravity = quota.classList.contains("js-refresh-antigravity-quota");
      const quotaName = isClaude ? "Claude" : (isAntigravity ? "Antigravity" : "Codex");
      quota.disabled = true; quota.textContent = "Loading...";
      try {
        const quotaEndpoint = isClaude ? "/api/claude/quota" : (isAntigravity ? "/api/antigravity/quota" : "/api/codex/quota");
        const data = await postProviderAction(quotaEndpoint, { provider_id: providerID });
        const quotas = Array.isArray(data.quotas) ? data.quotas : [];
        const cards = quotas.map((q) => {
          const total = Number(q.total || 0), remaining = Number(q.remaining || 0), used = Number(q.used || 0);
          const pctUsed = q.unlimited || total <= 0 ? 0 : Math.max(0, Math.min(100, used / total * 100));
          const pctLeft = q.unlimited || total <= 0 ? 100 : Math.max(0, Math.min(100, remaining / total * 100));
          const low = !q.unlimited && pctLeft <= 15;
          return `<article class="quota-card ${low ? "is-low" : ""}"><div class="quota-card-head"><h4>${escapeHtml(q.name || q.key || "Quota")}</h4><span class="badge badge-${low ? "error" : "ok"}">${q.unlimited ? "Unlimited" : `${Math.round(pctLeft)}% left`}</span></div><strong>${q.unlimited ? "Unlimited" : `${escapeHtml(remaining)}/${escapeHtml(total)} left`}</strong><div class="quota-progress ${low ? "is-low" : ""}" title="${Math.round(pctUsed)}% used"><span style="width:${pctUsed}%"></span></div>${q.reset_at ? `<small>Reset: ${escapeHtml(q.reset_at)}</small>` : ""}</article>`;
        }).join("");
        const modelNote = isAntigravity && Array.isArray(data.models) && data.models.length ? `<p class="muted-text">${escapeHtml(data.models.length)} models available.</p>` : "";
        const unavailable = !!data.message && !cards && !(Array.isArray(data.models) && data.models.length);
        const available = !(data.limit_reached || data.review_limit_reached || unavailable);
        const statusLabel = unavailable ? "Unavailable" : (available ? "Available" : "Limit reached");
        const resetCreditCount = Number(data.reset_credits?.available_count || 0);
        document.querySelectorAll(`.js-codex-reset[data-provider-id="${CSS.escape(providerID)}"]:not([data-account-id])`).forEach((button) => { button.dataset.creditCount = String(resetCreditCount); button.disabled = resetCreditCount < 1; button.title = resetCreditCount ? `${resetCreditCount} reset credit(s) available` : "No reset credits available"; });
        const resetNote = !isClaude && !isAntigravity ? `<p class="muted-text"><strong>${resetCreditCount}</strong> Codex reset credit(s) available.</p>` : "";
        const html = `<div class="quota-result"><div class="quota-header"><div><strong>${quotaName} quota</strong><small>${escapeHtml(data.plan || "unknown plan")}</small></div><span class="badge badge-${available ? "ok" : "error"}">${statusLabel}</span></div>${cards ? `<div class="quota-grid">${cards}</div>` : `<p class="muted-text">${escapeHtml(data.message || "No quota windows returned.")}</p>`}${resetNote}${modelNote}</div>`;
        setResultHTML(providerID, html, `${quotaName} quota updated`, available);
      } catch (err) { setResult(providerID, err.message || String(err), false); }
      finally { quota.disabled = false; quota.textContent = t("providers.quota", "Quota"); }
      return;
    }
    const hide = event.target.closest(".js-hide-model, .js-unhide-model");
    if (hide) {
      event.preventDefault();
      const hidden = hide.classList.contains("js-hide-model");
      const providerID = hide.dataset.providerId || "";
      const model = hide.dataset.model || "";
      hide.disabled = true;
      try {
        await postProviderAction("/api/providers/model-visibility", { provider_id: providerID, model, hidden });
        window.location.reload();
      } catch (err) { setResult(providerID, err.message || String(err), false); hide.disabled = false; }
      return;
    }
    const test = event.target.closest(".js-test-model");
    if (test) {
      event.preventDefault();
      const providerID = test.dataset.providerId || "";
      const model = test.dataset.model || "";
      const chipResult = test.parentElement?.querySelector(".model-test-result");
      test.disabled = true; test.textContent = "Testing..."; if (chipResult) chipResult.textContent = "";
      try {
        const data = await postProviderAction("/api/providers/test-model", { provider_id: providerID, model });
        const ok = !!data.ok && !data.error;
        const attempted = Array.isArray(data.attempted_account_ids) && data.attempted_account_ids.length ? ` · accounts: ${data.attempted_account_ids.join(" → ")}` : "";
        const message = ok ? `OK ${data.status || ""} · ${data.latency_ms || 0}ms${attempted}` : `Failed ${data.status || ""}: ${data.error || "unknown error"}${attempted}`;
        if (chipResult) { chipResult.textContent = message; chipResult.classList.toggle("ok", ok); chipResult.classList.toggle("error", !ok); }
        setResult(providerID, `${model}: ${message}`, ok);
      } catch (err) {
        if (chipResult) { chipResult.textContent = err.message || String(err); chipResult.classList.add("error"); }
        setResult(providerID, err.message || String(err), false);
      } finally { test.disabled = false; test.textContent = t("providers.test", "Test"); }
      return;
    }
  });
}
function applyProviderPreset(preset) {
  if (!preset) return;
  const presets = {
    custom: {id:"custom", name:"Custom OpenAI-compatible", base_url:"", models:""},
    openai: {id:"openai", name:"OpenAI", base_url:"https://api.openai.com/v1", models:"gpt-4.1, gpt-4o-mini"},
    openrouter: {id:"openrouter", name:"OpenRouter", base_url:"https://openrouter.ai/api/v1", models:"openai/gpt-4o-mini"},
    groq: {id:"groq", name:"Groq", base_url:"https://api.groq.com/openai/v1", models:"llama-3.3-70b-versatile"},
    ollama: {id:"ollama", name:"Ollama", base_url:"http://localhost:11434/v1", models:"llama3.1"},
    "vivurouter": {id:"vivurouter", name:"vivurouter upstream", base_url:"", models:""},
    "mimo-free": {id:"mimo-free", type:"mimo-free", name:"MiMo Code Free", base_url:"https://api.xiaomimimo.com/api/free-ai/openai/chat", models:"mimo-auto"},
    "opencode": {id:"opencode", type:"opencode", name:"OpenCode Free", base_url:"https://opencode.ai", models:"big-pickle"},
    "antigravity": {id:"antigravity", type:"antigravity", name:"Antigravity", base_url:"https://daily-cloudcode-pa.googleapis.com", models:"gemini-3-flash-agent, gemini-3.5-flash-low, gemini-3.5-flash-extra-low, gemini-pro-agent, gemini-3.1-pro-low, claude-sonnet-4-6, claude-opus-4-6-thinking, gpt-oss-120b-medium, gemini-3-flash"},
    "anthropic-proxy": {id:"anthropic-proxy", name:"Anthropic proxy", base_url:"", models:"claude-sonnet-4-5"},
    "gemini-proxy": {id:"gemini-proxy", name:"Gemini proxy", base_url:"", models:"gemini-2.5-pro"},
  }[preset];
  if (!presets) return;
  setValue("id", presets.id); setValue("name", presets.name); setValue("base_url", presets.base_url); setValue("models", presets.models);
  const type = document.querySelector('[name="type"]'); if (type) type.value = presets.type || "openai-compatible";
}

function initTokenOptimizationPresets() {
  document.addEventListener("click", (event) => {
    const btn = event.target.closest(".js-token-preset");
    if (!btn) return;
    event.preventDefault();
    const p = btn.dataset.tokenPreset;
    ["token_optimize_system","token_optimize_developer","token_optimize_text","token_optimize_tool_schemas","token_optimize_tool_calls"].forEach((n) => setChecked(n, false));
    if (p === "safe") { setChecked("token_optimize_tool_results", false); setChecked("rtk_enabled", false); setChecked("save_raw_prompt", false); setChecked("save_raw_tool_result", false); }
    if (p === "balanced") { setChecked("token_optimize_tool_results", true); setChecked("rtk_enabled", false); setValue("token_optimize_min_chars", 20000); setValue("token_optimize_max_chars", 12000); setChecked("save_raw_prompt", false); setChecked("save_raw_tool_result", false); }
    if (p === "aggressive") { setChecked("token_optimize_tool_results", true); setChecked("rtk_enabled", true); setValue("token_optimize_min_chars", 12000); setValue("token_optimize_max_chars", 8000); }
    if (p === "debug") { setChecked("save_raw_prompt", true); setChecked("save_raw_tool_result", true); setChecked("save_raw_response", true); setChecked("mask_debug_secrets", true); setChecked("compact_debug_payloads", true); }
    showToast(`Applied ${p} preset`);
  });
}

function initRequestFilters() { const input = document.getElementById("request-filter"); if (!input) return; input.addEventListener("input", () => { const q = input.value.toLowerCase(); document.querySelectorAll(".request-log-card").forEach((el) => el.hidden = q && !el.textContent.toLowerCase().includes(q)); }); }
function initRequestDebugModal() {
  const modal = document.getElementById("request-debug-modal"), raw = document.getElementById("request-debug-raw"), meta = document.getElementById("request-debug-meta");
  let payload = null, activeTab = "compact-prompt";
  const tabText = (d) => activeTab === "raw-prompt" ? d.raw_prompt : activeTab === "compact-tool" ? d.compact_tool_result : activeTab === "raw-tool" ? d.raw_tool_result : activeTab === "compact-response" ? d.compact_response : activeTab === "raw-response" ? d.raw_response : d.compact_prompt;
  const render = () => { const d = payload?.debug || {}; if (raw) raw.textContent = tabText(d) || "No payload stored for this section."; if (meta) meta.textContent = [payload?.id, d.redacted && "redacted", d.raw_prompt_truncated || d.raw_tool_truncated || d.raw_response_truncated ? "truncated" : ""].filter(Boolean).join(" · "); document.querySelectorAll("[data-debug-tab]").forEach((b) => b.classList.toggle("active", b.dataset.debugTab === activeTab)); };
  document.addEventListener("click", async (event) => {
    const toggle = event.target.closest(".js-request-debug-toggle"); if (toggle) { const body = toggle.closest(".request-debug-panel")?.querySelector(".request-debug-body"); if (body) body.hidden = !body.hidden; return; }
    const load = event.target.closest(".js-request-debug-load"); if (load) { const old = load.textContent; load.disabled = true; load.textContent = "Loading..."; try { const res = await fetch(`/api/admin/request-debug?id=${encodeURIComponent(load.dataset.requestId || "")}`, {cache:"no-store"}); payload = await safeJSON(res); activeTab = "compact-prompt"; if (modal) modal.hidden = false; render(); } catch(e) { showToast(String(e), false); } finally { load.disabled = false; load.textContent = old; } return; }
    const tab = event.target.closest("[data-debug-tab]"); if (tab) { activeTab = tab.dataset.debugTab; render(); return; }
    if (event.target.closest(".js-request-debug-close, .request-debug-backdrop")) { if (modal) modal.hidden = true; return; }
    if (event.target.closest(".js-request-debug-copy")) navigator.clipboard?.writeText(raw?.textContent || "");
  });
}
function initRTKStatus() { const btn = document.querySelector(".js-check-rtk"), summary = document.getElementById("rtk-status-summary"), details = document.getElementById("rtk-status-details"); if (!btn) return; const run = async () => { const params = new URLSearchParams(); const en = document.querySelector('[name="rtk_enabled"]'); const path = document.querySelector('[name="rtk_path"]'); if (en) params.set("rtk_enabled", en.checked ? "1" : "0"); if (path) params.set("rtk_path", path.value || ""); btn.disabled = true; try { const res = await fetch(`/api/admin/rtk/status?${params}`, {cache:"no-store"}); const data = await safeJSON(res); const token = !!data.token_optimize_tool_results; summary.textContent = data.enabled ? (data.can_run_now ? `RTK ready: ${data.version || data.path || "detected"}` : data.message || "RTK enabled but not runnable") : (token ? "Native tool-result optimization is enabled. RTK bridge is disabled." : "RTK bridge is disabled."); if (details) { details.hidden = false; details.innerHTML = Object.entries({"Tool-result optimizer": token ? "enabled" : "disabled", "RTK bridge": data.enabled ? "enabled" : "disabled", Found: data.found ? "yes" : "no", Source: data.source || "-", Path: data.path || "-", Version: data.version || "-"}).map(([k,v]) => `<div><dt>${escapeHtml(k)}</dt><dd>${escapeHtml(v)}</dd></div>`).join(""); } } catch(e) { summary.textContent = String(e); } finally { btn.disabled = false; } }; btn.addEventListener("click", run); if (document.getElementById("rtk-status-card")?.dataset.autoCheck === "1") run(); }
function initClearRequestDebug() { const btn = document.querySelector(".js-clear-request-debug"); if (!btn) return; btn.addEventListener("click", async () => { if (!confirm(t("js.clear_debug_confirm", "Clear saved raw debug payloads?"))) return; const res = await fetch("/api/admin/request-debug/clear", {method:"POST", cache:"no-store"}); const data = await safeJSON(res); showToast(res.ok ? `Deleted ${data.deleted || 0} debug payloads` : (data.message || "Cleanup failed"), res.ok); }); }
function initNotices() { setTimeout(() => document.querySelectorAll(".notice").forEach((n) => n.classList.add("is-hiding")), 2500); }
function initCodexResetCredits() {
  const modal = document.getElementById("codex-reset-credits-modal");
  if (!modal) return;
  const summary = modal.querySelector("[data-codex-reset-summary]");
  const body = modal.querySelector("[data-codex-reset-credits-body]");
  const label = modal.querySelector("[data-codex-reset-account-label]");
  const isVI = document.documentElement.lang === "vi";
  const text = (vi, en) => isVI ? vi : en;
  const endpoint = (button) => {
    const params = new URLSearchParams({ provider_id: button.dataset.providerId || "" });
    if (button.dataset.accountId) params.set("account_id", button.dataset.accountId);
    return `/api/codex/reset-credits?${params}`;
  };
  const dateText = (value) => value && !Number.isNaN(new Date(value).getTime()) ? new Date(value).toLocaleString() : "—";
  const remainingText = (value) => {
    const ms = value ? new Date(value).getTime() - Date.now() : NaN;
    if (!Number.isFinite(ms)) return "—";
    if (ms <= 0) return text("Đã hết hạn", "Expired");
    const days = Math.floor(ms / 86400000), hours = Math.floor(ms % 86400000 / 3600000);
    return days > 0 ? `${days}d ${hours}h` : `${Math.max(1, hours)}h`;
  };
  const applyCount = (source, count) => {
    document.querySelectorAll(`.js-codex-reset[data-provider-id="${CSS.escape(source.dataset.providerId || "")}"]`).forEach((button) => {
      if ((button.dataset.accountId || "") !== (source.dataset.accountId || "")) return;
      button.dataset.creditCount = String(count);
      button.disabled = count < 1;
      button.title = count ? `${count} reset credit(s) available` : "No reset credits available";
    });
  };
  const load = async (button, show = true) => {
    if (show) {
      label.textContent = button.dataset.accountLabel || button.dataset.accountId || text("Tài khoản mặc định", "Default account");
      summary.textContent = text("Đang tải reset credit…", "Loading reset credits…");
      body.replaceChildren();
      openModal("codex-reset-credits-modal");
    }
    const response = await fetch(endpoint(button), { headers: { Accept: "application/json" }, cache: "no-store" });
    const data = await safeJSON(response);
    if (!response.ok) throw new Error(data.error || data.message || `HTTP ${response.status}`);
    const credits = Array.isArray(data.credits) ? data.credits : [];
    const count = Number(data.available_count || 0);
    applyCount(button, count);
    if (show) {
      summary.innerHTML = `<strong>${count}</strong> ${escapeHtml(text("credit khả dụng", "available credit(s)"))} · ${credits.length} ${escapeHtml(text("credit tổng cộng", "total credit(s)"))}`;
      const rows = credits.map((credit) => {
        const row = document.createElement("tr");
        [credit.status || "unknown", dateText(credit.granted_at), dateText(credit.expires_at), remainingText(credit.expires_at)].forEach((value) => { const cell = document.createElement("td"); cell.textContent = value; row.append(cell); });
        return row;
      });
      if (rows.length) body.replaceChildren(...rows);
      else { const row = document.createElement("tr"), cell = document.createElement("td"); cell.colSpan = 4; cell.textContent = text("Không có chi tiết reset credit.", "No reset credit details returned."); row.append(cell); body.replaceChildren(row); }
    }
    return data;
  };
  document.addEventListener("click", async (event) => {
    const expiry = event.target.closest(".js-codex-credit-expiry");
    if (expiry) {
      event.preventDefault();
      expiry.disabled = true;
      try { await load(expiry, true); } catch (error) { summary.textContent = error.message || String(error); showToast(summary.textContent, false); }
      finally { expiry.disabled = false; }
      return;
    }
    const reset = event.target.closest(".js-codex-reset");
    if (!reset) return;
    event.preventDefault();
    const count = Number(reset.dataset.creditCount || 0);
    if (count < 1) return;
    const account = reset.dataset.accountLabel || reset.dataset.accountId || text("tài khoản mặc định", "default account");
    if (!window.confirm(text(`Dùng 1 Codex reset credit cho ${account}? Còn ${count} credit. Hành động này không thể hoàn tác.`, `Use 1 Codex reset credit for ${account}? ${count} credit(s) remain. This cannot be undone.`))) return;
    reset.disabled = true;
    try {
      const response = await fetch(endpoint(reset), { method: "POST", headers: { Accept: "application/json" } });
      const data = await safeJSON(response);
      if (!response.ok) throw new Error(data.error || data.message || `HTTP ${response.status}`);
      showToast(text(`Đã reset ${Number(data.windows_reset || 0)} cửa sổ quota.`, `Reset ${Number(data.windows_reset || 0)} quota window(s).`));
      await load(reset, false);
      const quotaButton = reset.dataset.accountId ? null : document.querySelector(`.js-refresh-codex-quota[data-provider-id="${CSS.escape(reset.dataset.providerId || "")}"]`);
      quotaButton?.click();
    } catch (error) {
      if (String(error.message || error).includes("No Codex reset credits")) applyCount(reset, 0);
      showToast(error.message || String(error), false);
    } finally { reset.disabled = Number(reset.dataset.creditCount || 0) < 1; }
  });
}

function initProviderModelCollapse() {
  document.addEventListener("click", async (e) => {
    const connect = e.target.closest(".js-connect-codex-account");
    if (!connect) return;
    e.preventDefault();
    const params = new URLSearchParams({ json: "1", provider_id: connect.dataset.providerId || "", account_id: connect.dataset.accountId || "" });
    if (connect.dataset.proxyUrl) params.set("proxy_url", connect.dataset.proxyUrl);
    connect.disabled = true;
    const originalText = connect.textContent;
    connect.textContent = "Starting OAuth…";
    try {
      const res = await fetch(`/api/codex/oauth/start?${params}`, { method: "POST", cache: "no-store", headers: { Accept: "application/json" } });
      const data = await safeJSON(res);
      if (!res.ok) throw new Error(data.message || data.error || `HTTP ${res.status}`);
      const authURL = data.auth_url || data.AuthURL || "";
      if (!authURL) throw new Error("OAuth authorization URL was not returned");
      window.open(authURL, "_blank", "noopener,noreferrer");
      showToast("OAuth opened in a new tab. Complete it to connect this account.");
    } catch (err) {
      showToast(`Could not start account OAuth: ${err.message || err}`, false);
    } finally {
      connect.disabled = false;
      connect.textContent = originalText;
    }
  });
  document.addEventListener("click", (e) => {
    const btn = e.target.closest(".js-expand-provider-models");
    if (!btn) return;
    const section = btn.closest(".provider-detail-section") || document;
    const scroll = section.querySelector("[data-provider-model-scroll]");
    if (scroll) scroll.classList.remove("is-collapsed");
    section.querySelectorAll(".provider-model-extra").forEach((el) => el.hidden = false);
    btn.hidden = true;
  });
  document.querySelectorAll("[data-provider-account-pool]").forEach((pool) => {
    const providerID = pool.dataset.providerId;
    const list = pool.querySelector("[data-managed-provider-accounts]");
    const form = document.querySelector(`[data-provider-account-form][data-provider-id="${CSS.escape(providerID || "")}"]`);
    const result = form?.querySelector("[data-provider-account-result]");
    const oauthPanel = form?.querySelector("[data-provider-account-oauth]");
    const oauthOpen = form?.querySelector(".js-account-oauth-open");
    const oauthCreateLink = form?.querySelector(".js-account-oauth-create-link");
    const oauthManual = form?.querySelector(".account-oauth-manual");
    const oauthAuthLink = form?.querySelector("[data-account-oauth-auth-link]");
    const oauthCallbackLink = form?.querySelector("[data-account-oauth-callback-link]");
    const oauthStartURL = oauthPanel?.dataset.oauthStart || "/api/codex/oauth/start";
    const oauthCompleteURL = oauthPanel?.dataset.oauthComplete || "/api/codex/oauth/complete";
    const supportsAccountOAuth = ["claude", "codex"].includes(pool.dataset.providerType);
    const accountID = () => form?.querySelector('[name="id"]')?.value || "";
    const accountProxyID = () => form?.querySelector('[name="proxy_id"]')?.value || "";
    const accountProxy = () => form?.querySelector('[name="proxy_url"]')?.value || "";
    const setOAuthEnabled = (enabled) => {
      if (oauthPanel) oauthPanel.hidden = false;
      if (oauthOpen) oauthOpen.disabled = !enabled;
      if (oauthCreateLink) oauthCreateLink.disabled = !enabled;
    };
    const ensureAccountID = () => {
      const input = form?.querySelector('[name="id"]');
      if (input && !input.value) input.value = `account-${crypto.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`}`;
      return input?.value || "";
    };
    const resetForm = () => {
      if (!form) return;
      form.reset();
      form.querySelector('[name="id"]').value = "";
      form.querySelector('[name="priority"]').value = "1";
      const quotaLimit = form.querySelector('[name="quota_limit_percent"]');
      if (quotaLimit) quotaLimit.value = "0";
      form.querySelector('[name="enabled"]').checked = true;
      syncAuthFields();
      if (oauthManual) oauthManual.hidden = true;
      if (oauthAuthLink) oauthAuthLink.value = "";
      if (oauthCallbackLink) oauthCallbackLink.value = "";
      setOAuthEnabled(false);
      form.querySelector("[data-provider-account-submit]").textContent = document.documentElement.lang === "vi" ? "Lưu tài khoản" : "Save account";
      if (result) result.textContent = "";
    };
    const isVI = document.documentElement.lang === "vi";
    const text = (vi, en) => isVI ? vi : en;
    const setResult = (message, ok = true) => {
      if (!result) return;
      result.textContent = message;
      result.classList.toggle("error", !ok);
    };
    const syncAuthFields = () => {
      const authType = form?.querySelector('[name="auth_type"]')?.value;
      const apiKeyField = form?.querySelector("[data-account-api-key-field]");
      const bearerField = form?.querySelector("[data-account-bearer-field]");
      if (apiKeyField) apiKeyField.hidden = authType !== "api_key";
      if (bearerField) bearerField.hidden = authType !== "bearer";
    };
    form?.querySelector('[name="auth_type"]')?.addEventListener("change", syncAuthFields);
    const populateForm = (account) => {
      if (!form) return;
      resetForm();
      form.querySelector('[name="id"]').value = account.id || "";
      form.querySelector('[name="name"]').value = account.name || "";
      form.querySelector('[name="auth_type"]').value = account.auth_type || form.querySelector('[name="auth_type"]').value;
      syncAuthFields();
      form.querySelector('[name="priority"]').value = String(account.priority || 1);
      const quotaLimit = form.querySelector('[name="quota_limit_percent"]');
      if (quotaLimit) quotaLimit.value = String(Number(account.quota_limit_percent || 0));
      form.querySelector('[name="proxy_id"]').value = account.proxy_id || "";
      form.querySelector('[name="proxy_url"]').value = "";
      form.querySelector('[name="enabled"]').checked = !!account.enabled;
      form.querySelector("[data-provider-account-submit]").textContent = text("Cập nhật tài khoản", "Update account");
      setOAuthEnabled(supportsAccountOAuth);
    };
    const requestAccountUpdate = async (account, method = "POST") => {
      const response = await fetch(`/api/provider-accounts?provider_id=${encodeURIComponent(providerID)}${method === "DELETE" ? `&id=${encodeURIComponent(account.id)}` : ""}`, {
        method,
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: method === "DELETE" ? undefined : JSON.stringify(account)
      });
      const payload = await safeJSON(response);
      if (!response.ok) throw new Error(payload.error || payload.message || `HTTP ${response.status}`);
      return payload;
    };
    const exportForm = document.querySelector(`[data-provider-account-export-form][data-provider-id="${CSS.escape(providerID || "")}"]`);
    const importForm = document.querySelector(`[data-provider-account-import-form][data-provider-id="${CSS.escape(providerID || "")}"]`);
    const downloadBackup = (bytes, filename) => { const url = URL.createObjectURL(new Blob([bytes], { type: "application/json" })); const link = document.createElement("a"); link.href = url; link.download = filename; link.click(); setTimeout(() => URL.revokeObjectURL(url), 0); };
    exportForm?.querySelector("[data-account-backup-format]")?.addEventListener("change", (event) => { const raw = event.currentTarget.value === "json"; exportForm.querySelector("[data-account-backup-encrypted-fields]").hidden = raw; exportForm.querySelector("[data-account-backup-plaintext-warning]").hidden = !raw; });
    exportForm?.addEventListener("submit", async (event) => { event.preventDefault(); const data = Object.fromEntries(new FormData(exportForm).entries()); const output = exportForm.querySelector("[data-account-backup-export-result]"); const accountIDs = data.scope === "selected" ? [...list.querySelectorAll('.provider-account-card input[type="checkbox"]:checked')].map((input) => input.closest(".provider-account-card")?.dataset.accountId).filter(Boolean) : []; if (data.scope === "selected" && accountIDs.length === 0) { output.textContent = text("Chọn ít nhất một tài khoản hoặc chọn Toàn bộ Account Pool.", "Select at least one account or choose the entire Account Pool."); return; } if (data.format === "encrypted" && (!data.passphrase || data.passphrase !== data.passphrase_confirm)) { output.textContent = text("Passphrase không khớp.", "Passphrases do not match."); return; } if (data.format === "json" && !exportForm.querySelector('[name="confirm_plaintext"]').checked) { output.textContent = text("Bạn cần xác nhận cảnh báo JSON plaintext.", "You must acknowledge the plaintext JSON warning."); return; } try { const response = await fetch("/api/provider-accounts/export-encrypted", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ provider_id: providerID, account_ids: accountIDs, scope: data.scope, format: data.format, passphrase: data.passphrase || "", confirm_plaintext: exportForm.querySelector('[name="confirm_plaintext"]').checked }) }); if (!response.ok) throw new Error("Backup export failed"); downloadBackup(await response.arrayBuffer(), data.format === "json" ? `vivurouter-accounts-${providerID}-UNENCRYPTED.json` : `vivurouter-accounts-${providerID}.vrbackup`); output.textContent = text("Đã tải backup.", "Backup downloaded."); } catch (error) { output.textContent = error.message || "Backup export failed."; } });
    const readImport = async () => { const file = importForm?.querySelector('[name="file"]')?.files?.[0]; if (!file) throw new Error(text("Chọn file backup trước.", "Choose a backup file first.")); return JSON.parse(await file.text()); };
    importForm?.querySelector('[name="policy"]')?.addEventListener("change", (event) => { importForm.querySelector("[data-account-backup-replace-confirm]").hidden = event.currentTarget.value !== "replace"; });
    importForm?.querySelector(".js-preview-account-backup")?.addEventListener("click", async () => { const output = importForm.querySelector("[data-account-backup-import-result]"); try { const bundle = await readImport(); const response = await fetch("/api/provider-accounts/import-preview", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ bundle, passphrase: importForm.querySelector('[name="passphrase"]').value, confirm_plaintext: importForm.querySelector('[name="confirm_plaintext"]').checked }) }); const preview = await safeJSON(response); if (!response.ok) throw new Error(preview.error || "Backup preview failed"); output.textContent = `${text("Tìm thấy", "Found")} ${preview.accounts?.length || 0} ${text("tài khoản", "accounts")} · ${preview.proxy_count || 0} ${text("proxy", "proxies")}`; } catch (error) { output.textContent = error.message || "Backup preview failed."; } });
    importForm?.addEventListener("submit", async (event) => { event.preventDefault(); const output = importForm.querySelector("[data-account-backup-import-result]"); try { const bundle = await readImport(); const data = Object.fromEntries(new FormData(importForm).entries()); const response = await fetch("/api/provider-accounts/import-encrypted", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ bundle, passphrase: data.passphrase || "", provider_id: providerID, policy: data.policy, confirm: data.confirm || "", confirm_plaintext: importForm.querySelector('[name="confirm_plaintext"]').checked }) }); const result = await safeJSON(response); if (!response.ok) throw new Error(result.error || "Backup import failed"); output.textContent = `${text("Đã nhập", "Imported")} ${result.imported || 0} · ${text("bỏ qua", "skipped")} ${result.skipped || 0}`; refresh(); } catch (error) { output.textContent = error.message || "Backup import failed."; } });
    const loadQuota = async (account, target, button) => {
      if (!['claude', 'codex', 'antigravity'].includes(pool.dataset.providerType) || !account.enabled) return;
      if (button) button.disabled = true;
      target.textContent = text("Đang lấy quota…", "Loading quota…");
      try {
        const response = await fetch(`/api/provider-accounts/quota?provider_id=${encodeURIComponent(providerID)}&account_id=${encodeURIComponent(account.id)}`, { headers: { Accept: "application/json" } });
        const payload = await safeJSON(response);
        if (!response.ok) throw new Error(payload.error || payload.message || `HTTP ${response.status}`);
        const report = payload.report || {};
        const quotas = Array.isArray(report.quotas) ? report.quotas : [];
        const summary = quotas.map((q) => `${q.name || q.key}: ${Number(q.used || 0).toFixed(0)}%`).join(" · ");
        const limit = Number(account.quota_limit_percent || 0);
        // Claude bills each model family's weekly window separately, so a
        // per-family window at the limit only pauses that family. Name the
        // affected windows instead of implying the whole account is paused.
        const routingQuotas = quotas.filter((q) => ["session", "weekly"].includes(q.key) || (pool.dataset.providerType === "claude" && String(q.key || "").startsWith("weekly_")));
        const atLimit = limit > 0 ? routingQuotas.filter((q) => Number(q.used || 0) >= limit) : [];
        let limitStatus = "";
        if (atLimit.length) {
          const shared = atLimit.some((q) => q.key === "session" || q.key === "weekly");
          limitStatus = shared || pool.dataset.providerType !== "claude"
            ? text(`Đã chạm ngưỡng ${limit.toFixed(0)}% — dừng tác vụ mới`, `Limit ${limit.toFixed(0)}% reached — new tasks paused`)
            : text(`Đã chạm ngưỡng ${limit.toFixed(0)}% ở ${atLimit.map((q) => q.name || q.key).join(", ")} — chỉ model thuộc họ đó bị dừng`, `Limit ${limit.toFixed(0)}% reached on ${atLimit.map((q) => q.name || q.key).join(", ")} — only those model families are paused`);
        }
        const resetCount = Number(report.reset_credits?.available_count || 0);
        target.textContent = [summary, limitStatus, pool.dataset.providerType === "codex" ? `${resetCount} reset credit(s)` : ""].filter(Boolean).join(" · ") || report.message || text("Chưa có dữ liệu quota", "No quota data returned");
        const resetButton = target.closest(".provider-account-card")?.querySelector(".js-codex-reset");
        if (resetButton) { resetButton.dataset.creditCount = String(resetCount); resetButton.disabled = resetCount < 1; }
        target.title = report.plan ? `${text("Gói", "Plan")}: ${report.plan}` : "";
      } catch (error) {
        target.textContent = error.message || text("Không thể lấy quota", "Unable to load quota");
        target.classList.add("is-error");
      } finally { if (button) button.disabled = false; }
    };
    const refresh = () => {
      if (!providerID || !list) return;
      list.classList.add("is-loading");
      list.textContent = text("Đang tải tài khoản…", "Loading accounts…");
      fetch(`/api/provider-accounts?provider_id=${encodeURIComponent(providerID)}`, { headers: { Accept: "application/json" } })
        .then((response) => response.ok ? response.json() : Promise.reject(new Error(`HTTP ${response.status}`)))
        .then((accounts) => {
          list.classList.remove("is-loading");
          const count = pool.querySelector(".provider-account-count");
          if (count) count.textContent = String((Array.isArray(accounts) ? accounts.length : 0) + 1);
          if (!Array.isArray(accounts) || accounts.length === 0) {
            const empty = document.createElement("div");
            empty.className = "provider-account-empty";
            empty.innerHTML = `<strong>${text("Chưa có tài khoản được quản lý", "No managed accounts")}</strong><span>${text("Thêm tài khoản để dùng credential, proxy và cooldown riêng.", "Add an account for its own credentials, proxy, and cooldown.")}</span>`;
            list.replaceChildren(empty);
            return;
          }
          const rows = accounts.map((account) => {
            const row = document.createElement("article");
            row.className = `provider-account-card${account.enabled ? "" : " is-disabled"}`;
            row.dataset.accountId = account.id || "";
            const state = account.enabled ? text("Đang hoạt động", "Active") : text("Đã tắt", "Disabled");
            const cooldown = account.cooldown_until && new Date(account.cooldown_until) > new Date();
            const proxy = account.proxy_id ? `${text("Pool", "Pool")}: ${account.proxy_id}` : (account.proxy_url ? text("Proxy riêng", "Custom proxy") : text("Kết nối trực tiếp", "Direct route"));
            const health = cooldown ? `${text("Cooldown đến", "Cooldown until")} ${new Date(account.cooldown_until).toLocaleTimeString()}` : (account.failure_streak ? `${text("Lỗi liên tiếp", "Failure streak")}: ${account.failure_streak}` : text("Sẵn sàng", "Ready"));
            row.innerHTML = `<div class="provider-account-select"><input type="checkbox" aria-label="Select account"></div><div class="provider-account-identity"><span class="provider-account-status ${account.enabled && !cooldown ? "is-active" : ""}"></span><div><strong></strong><small></small></div></div><div class="provider-account-meta"><span class="badge badge-info"></span><span></span><span></span></div><div class="provider-account-health ${cooldown ? "is-cooldown" : ""}"></div><div class="provider-account-actions"></div><div class="provider-account-quota" hidden></div>`;
            row.querySelector("strong").textContent = account.name || account.id;
            row.querySelector("small").textContent = `${account.id || "—"} · ${state}`;
            row.querySelector(".badge").textContent = account.auth_type || "—";
            const meta = row.querySelectorAll(".provider-account-meta span");
            meta[1].textContent = account.quota_limit_percent > 0 ? `${text("Ưu tiên", "Priority")} ${account.priority || 1} · ${text("Ngưỡng", "Limit")} ${Number(account.quota_limit_percent).toFixed(0)}%` : `${text("Ưu tiên", "Priority")} ${account.priority || 1}`;
            meta[2].textContent = proxy;
            row.querySelector(".provider-account-health").textContent = health;
            const actions = row.querySelector(".provider-account-actions");
            const quota = row.querySelector(".provider-account-quota");
            if (['claude', 'codex', 'antigravity'].includes(pool.dataset.providerType)) {
              quota.hidden = false;
              const quotaButton = document.createElement("button"); quotaButton.type = "button"; quotaButton.className = "btn-secondary btn-sm"; quotaButton.textContent = text("Quota", "Quota");
              quotaButton.addEventListener("click", () => loadQuota(account, quota, quotaButton));
              actions.append(quotaButton);
              if (pool.dataset.providerType === "codex") {
                const resetButton = document.createElement("button"); resetButton.type = "button"; resetButton.className = "btn-secondary btn-sm js-codex-reset"; resetButton.textContent = "Reset"; resetButton.dataset.providerId = providerID; resetButton.dataset.accountId = account.id || ""; resetButton.dataset.accountLabel = account.name || account.id || "Codex account"; resetButton.dataset.creditCount = "0"; resetButton.disabled = true;
                const expiryButton = document.createElement("button"); expiryButton.type = "button"; expiryButton.className = "btn-secondary btn-sm js-codex-credit-expiry"; expiryButton.textContent = text("Hạn credit", "Expiry"); expiryButton.dataset.providerId = providerID; expiryButton.dataset.accountId = account.id || ""; expiryButton.dataset.accountLabel = account.name || account.id || "Codex account";
                actions.append(resetButton, expiryButton);
              }
            }
            const edit = document.createElement("button"); edit.type = "button"; edit.className = "btn-secondary btn-sm"; edit.textContent = text("Sửa", "Edit");
            edit.addEventListener("click", () => { populateForm(account); openModal("provider-account-modal"); });
            const toggle = document.createElement("button"); toggle.type = "button"; toggle.className = "btn-secondary btn-sm"; toggle.textContent = account.enabled ? text("Tắt", "Disable") : text("Bật", "Enable");
            toggle.addEventListener("click", async () => { try { await requestAccountUpdate({ ...account, enabled: !account.enabled }); refresh(); showToast(text("Đã cập nhật trạng thái tài khoản.", "Account status updated.")); } catch (error) { showToast(error.message || "Unable to update account.", false); } });
            const remove = document.createElement("button"); remove.type = "button"; remove.className = "btn-danger btn-sm"; remove.textContent = text("Xóa", "Delete");
            remove.addEventListener("click", async () => { if (!window.confirm(text(`Xóa tài khoản ${account.name || account.id}?`, `Delete account ${account.name || account.id}?`))) return; try { await requestAccountUpdate(account, "DELETE"); refresh(); showToast(text("Đã xóa tài khoản.", "Account deleted.")); } catch (error) { showToast(error.message || "Unable to delete account.", false); } });
            actions.append(edit, toggle, remove);
            return row;
          });
          list.replaceChildren(...rows);
          if (['codex', 'antigravity'].includes(pool.dataset.providerType)) {
            accounts.filter((account) => account.enabled).forEach((account) => {
              const card = list.querySelector(`[data-account-id="${CSS.escape(account.id || "")}"]`);
              const quota = card?.querySelector(".provider-account-quota");
              if (quota) loadQuota(account, quota);
            });
          }
        })
        .catch(() => { list.classList.remove("is-loading"); list.textContent = text("Không thể tải tài khoản.", "Unable to load accounts."); });
    };
    pool.querySelector(".js-add-provider-account")?.addEventListener("click", resetForm);
    pool.querySelector(".js-refresh-provider-accounts")?.addEventListener("click", refresh);
    form?.addEventListener("submit", async (event) => {
      event.preventDefault();
      const data = Object.fromEntries(new FormData(form).entries());
      if (supportsAccountOAuth) data.id = ensureAccountID();
      data.enabled = form.querySelector('[name="enabled"]').checked;
      data.priority = Number(data.priority) || 1;
      if (Object.prototype.hasOwnProperty.call(data, "quota_limit_percent")) data.quota_limit_percent = Number(data.quota_limit_percent) || 0;
      try {
        const response = await fetch(`/api/provider-accounts?provider_id=${encodeURIComponent(providerID)}`, { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify(data) });
        const payload = await safeJSON(response);
        if (!response.ok) throw new Error(payload.error || payload.message || `HTTP ${response.status}`);
        if (result) result.textContent = "Account saved.";
        refresh();
        if (supportsAccountOAuth) {
          setOAuthEnabled(true);
          showToast("Account saved. Continue with OAuth below.");
        } else {
          closeModals();
          showToast("Account saved.");
        }
      } catch (error) {
        if (result) result.textContent = error.message || "Unable to save account.";
        showToast(error.message || "Unable to save account.", false);
      }
    });
    oauthOpen?.addEventListener("click", async () => {
      const id = accountID();
      if (!id) return;
      oauthOpen.disabled = true;
      try {
        const params = new URLSearchParams({ json: "1", provider_id: providerID, account_id: id });
        if (accountProxyID()) params.set("proxy_id", accountProxyID());
        else if (accountProxy()) params.set("proxy_url", accountProxy());
        const res = await fetch(`${oauthStartURL}?${params}`, { method: "POST", cache: "no-store", headers: { Accept: "application/json" } });
        const payload = await safeJSON(res);
        if (!res.ok || !payload.auth_url) throw new Error(payload.message || payload.error || "OAuth authorization URL was not returned");
        window.open(payload.auth_url, "_blank", "noopener,noreferrer");
        showToast("OAuth opened in a new tab for this account.");
      } catch (error) { showToast(error.message || "Unable to start OAuth.", false); }
      finally { oauthOpen.disabled = false; }
    });
    oauthCreateLink?.addEventListener("click", async () => {
      const id = accountID();
      if (!id) return;
      oauthCreateLink.disabled = true;
      try {
        const params = new URLSearchParams({ json: "1", provider_id: providerID, account_id: id });
        if (accountProxyID()) params.set("proxy_id", accountProxyID());
        else if (accountProxy()) params.set("proxy_url", accountProxy());
        const res = await fetch(`${oauthStartURL}?${params}`, { method: "POST", cache: "no-store", headers: { Accept: "application/json" } });
        const payload = await safeJSON(res);
        if (!res.ok || !payload.auth_url) throw new Error(payload.message || payload.error || "OAuth authorization URL was not returned");
        if (oauthAuthLink) oauthAuthLink.value = payload.auth_url;
        if (oauthManual) oauthManual.hidden = false;
      } catch (error) { showToast(error.message || "Unable to create OAuth link.", false); }
      finally { oauthCreateLink.disabled = false; }
    });
    form?.querySelector(".js-account-oauth-copy-link")?.addEventListener("click", () => navigator.clipboard?.writeText(oauthAuthLink?.value || ""));
    form?.querySelector(".js-account-oauth-complete")?.addEventListener("click", async (event) => {
      const callbackURL = oauthCallbackLink?.value || "";
      event.currentTarget.disabled = true;
      try {
        const res = await fetch(oauthCompleteURL, { method: "POST", cache: "no-store", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ callback_url: callbackURL }) });
        const payload = await safeJSON(res);
        if (!res.ok) throw new Error(payload.message || payload.error || "OAuth completion failed");
        refresh();
        closeModals();
        showToast("OAuth connected to this account.");
      } catch (error) { showToast(error.message || "OAuth completion failed.", false); }
      finally { event.currentTarget.disabled = false; }
    });
    refresh();
  });

  document.querySelectorAll(".provider-detail-model-filter").forEach((input) => {
    input.addEventListener("input", () => {
      const section = input.closest(".provider-detail-section") || document;
      const q = input.value.trim().toLowerCase();
      const list = section.querySelector("[data-provider-model-list]");
      const toggle = section.querySelector(".js-expand-provider-models");
      if (list && q) list.classList.remove("is-collapsed");
      section.querySelectorAll("[data-model-chip]").forEach((chip) => {
        const hit = !q || (chip.dataset.modelChip || chip.textContent || "").toLowerCase().includes(q);
        chip.hidden = !hit;
      });
      if (toggle) toggle.hidden = !!q;
    });
  });
}
function initPricingFilter() { const input = document.querySelector(".pricing-model-search"); if (input) input.addEventListener("input", () => { const q = input.value.toLowerCase(); document.querySelectorAll("[data-pricing-model]").forEach((el) => el.hidden = q && !el.dataset.pricingModel.toLowerCase().includes(q)); }); }
function initBudgetBars() { document.querySelectorAll(".budget-bar-fill[data-pct]").forEach((el) => el.style.width = `${Math.max(0, Math.min(100, Number(el.dataset.pct) || 0))}%`); }
function initUsageChart() {
  const canvas = document.getElementById("usage-chart-canvas");
  const empty = document.getElementById("usage-chart-empty");
  const rangeTabs = document.getElementById("usage-range-tabs");
  const metricTabs = document.getElementById("usage-metric-tabs");
  const tooltip = document.getElementById("usage-chart-tooltip");
  if (!canvas) return;
  const ctx = canvas.getContext("2d");
  const state = { series: window.VivuRouterUsageSeries || { buckets: [] }, metric: "cost_usd" };
  let geometry = [];
  const compact = (n) => Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(Number(n) || 0);
  const metricKey = () => state.metric;
  const metricLabel = () => state.metric === "cost_usd" ? "Cost" : state.metric === "requests" ? "Requests" : "Tokens";
  const updateKPIs = () => {
    const totals = state.series?.totals || {};
    const setText = (selector, value, title) => {
      const el = document.querySelector(selector);
      if (!el) return;
      el.textContent = value;
      if (title !== undefined) el.title = title;
    };
    setText('[data-kpi="requests"]', compact(totals.requests || 0), String(totals.requests || 0));
    setText('[data-kpi="prompt_tokens"]', compact(totals.prompt_tokens || 0), String(totals.prompt_tokens || 0));
    setText('[data-kpi="completion_tokens"]', compact(totals.completion_tokens || 0), String(totals.completion_tokens || 0));
    setText('[data-kpi="total_tokens"]', compact(totals.total_tokens || 0));
    setText('[data-kpi="estimated"]', String(totals.estimated || 0));
    setText('[data-kpi="cost_usd"]', `$${Number(totals.cost_usd || 0).toFixed(6)}`);
    setText('[data-kpi="strip_prompt_tokens"]', compact(totals.prompt_tokens || 0), String(totals.prompt_tokens || 0));
    setText('[data-kpi="strip_completion_tokens"]', compact(totals.completion_tokens || 0), String(totals.completion_tokens || 0));
    setText('[data-kpi="strip_cached_tokens"]', compact(totals.cached_tokens || 0), String(totals.cached_tokens || 0));
    setText('[data-kpi="strip_reasoning_tokens"]', compact(totals.reasoning_tokens || 0), String(totals.reasoning_tokens || 0));
  };
  const draw = () => {
    updateKPIs();
    const buckets = Array.isArray(state.series?.buckets) ? state.series.buckets : [];
    const width = canvas.clientWidth || canvas.parentElement?.clientWidth || 720;
    const height = Number(canvas.getAttribute("height")) || 220;
    canvas.width = Math.max(1, width * window.devicePixelRatio);
    canvas.height = Math.max(1, height * window.devicePixelRatio);
    ctx.setTransform(window.devicePixelRatio, 0, 0, window.devicePixelRatio, 0, 0);
    ctx.clearRect(0, 0, width, height);
    geometry = [];
    if (!buckets.length) { if (empty) empty.hidden = false; return; }
    if (empty) empty.hidden = true;
    const values = buckets.map((b) => Number(b[metricKey()] || 0));
    const max = Math.max(...values, 1);
    const padX = 34, padY = 24;
    const chartW = Math.max(1, width - padX * 2);
    const chartH = Math.max(1, height - padY * 2);
    ctx.strokeStyle = "rgba(148,163,184,.22)";
    ctx.lineWidth = 1;
    ctx.fillStyle = "rgba(203,213,225,.72)";
    ctx.font = "11px system-ui, sans-serif";
    for (let i = 0; i <= 3; i++) {
      const y = padY + i * (chartH / 3);
      ctx.beginPath(); ctx.moveTo(padX, y); ctx.lineTo(width - padX, y); ctx.stroke();
      const label = compact(max - i * (max / 3));
      ctx.fillText(label, 4, y + 4);
    }
    const slot = chartW / buckets.length;
    values.forEach((v, i) => {
      const h = Math.max(v > 0 ? 2 : 0, (v / max) * chartH);
      const x = padX + i * slot + Math.max(2, slot * .16);
      const barW = Math.max(3, slot * .68);
      const y = height - padY - h;
      const grad = ctx.createLinearGradient(0, y, 0, height - padY);
      grad.addColorStop(0, "#5cc0ea"); grad.addColorStop(1, "#6a74f0");
      ctx.fillStyle = grad;
      ctx.fillRect(x, y, barW, h);
      geometry.push({ x, y, w: barW, h, bucket: buckets[i], value: v });
      if (buckets.length <= 12 || i % Math.ceil(buckets.length / 8) === 0) {
        ctx.fillStyle = "rgba(203,213,225,.7)";
        ctx.fillText(String(buckets[i].label || ""), x, height - 6);
      }
    });
  };
  const loadRange = async (range) => {
    try {
      const res = await fetch(`/api/usage/timeseries?range=${encodeURIComponent(range)}`, { cache: "no-store", headers: { Accept: "application/json" } });
      const data = await safeJSON(res);
      if (res.ok && data.series) state.series = data.series;
      if (res.ok && data.table && typeof window.updateUsageStatsTable === "function") {
        window.updateUsageStatsTable(data.table);
      }
    } catch (err) { showToast(err.message || String(err), false); }
    draw();
  };
  rangeTabs?.addEventListener("click", (e) => {
    const tab = e.target.closest("[data-range]");
    if (!tab) return;
    rangeTabs.querySelectorAll("[data-range]").forEach((el) => el.classList.remove("active"));
    tab.classList.add("active");
    loadRange(tab.dataset.range || "24h");
  });
  metricTabs?.addEventListener("click", (e) => {
    const tab = e.target.closest("[data-metric]");
    if (!tab) return;
    metricTabs.querySelectorAll("[data-metric]").forEach((el) => el.classList.remove("active"));
    tab.classList.add("active");
    state.metric = tab.dataset.metric === "cost" ? "cost_usd" : tab.dataset.metric || "total_tokens";
    draw();
  });
  canvas.addEventListener("mousemove", (e) => {
    if (!tooltip || !geometry.length) return;
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX - rect.left, my = e.clientY - rect.top;
    const hit = geometry.find((g) => mx >= g.x && mx <= g.x + g.w);
    if (!hit) { tooltip.hidden = true; return; }
    tooltip.hidden = false;
    tooltip.innerHTML = `<strong>${escapeHtml(hit.bucket.label || "")}</strong><span>${escapeHtml(metricLabel())}: ${escapeHtml(compact(hit.value))}</span><span>Requests: ${escapeHtml(hit.bucket.requests || 0)}</span><span>Tokens: ${escapeHtml(compact(hit.bucket.total_tokens || 0))}</span><span>Cost: $${Number(hit.bucket.cost_usd || 0).toFixed(4)}</span>`;
    let left = mx + 14;
    if (left + tooltip.offsetWidth > canvas.clientWidth) left = mx - tooltip.offsetWidth - 14;
    tooltip.style.left = Math.max(0, left) + "px";
    tooltip.style.top = Math.max(0, my - 8) + "px";
  });
  canvas.addEventListener("mouseleave", () => { if (tooltip) tooltip.hidden = true; });
  window.addEventListener("resize", draw);
  draw();
}
function initRecentRequestsLive() { /* Recent requests are server-rendered; refresh the page for latest rows. */ }
function randomHex(bytes) {
  const arr = new Uint8Array(bytes);
  if (crypto?.getRandomValues) crypto.getRandomValues(arr);
  else for (let i = 0; i < arr.length; i++) arr[i] = Math.floor(Math.random() * 256);
  return [...arr].map((b) => b.toString(16).padStart(2, "0")).join("");
}
function generateSecretKey() { return `sk-${randomHex(24)}`; }
function defaultAPIKeyID() { return `key-${Date.now()}`; }

function initAPIKeyManager() {
  const form = document.getElementById("api-keys-form");
  const textarea = document.getElementById("api-keys-text");
  if (!form || !textarea) return;
  const rows = () => (textarea.value || "").split(/\r?\n/).map((l) => l.trim()).filter(Boolean).map((l) => l.split("|"));
  const write = (list) => { textarea.value = list.map((r) => r.join("|")).join("\n"); };
  const field = (name) => document.querySelector(`[data-new-api-key="${name}"]`);
  const list = document.querySelector("[data-api-key-list]");
  const directory = document.querySelector(".api-key-directory");
  const search = document.querySelector("[data-api-key-search]");
  const filter = document.querySelector("[data-api-key-filter]");
  const sort = document.querySelector("[data-api-key-sort]");
  const noResults = document.querySelector("[data-api-key-no-results]");
  const resetPolicyModal = () => {
    ["id", "key", "models", "requests", "tokens", "usd", "rpm", "concurrent"].forEach((name) => { const input = field(name); if (input) input.value = ""; });
    const editing = field("editing");
    const enabled = field("enabled");
    if (editing) editing.value = "";
    if (enabled) enabled.value = "true";
  };
  document.querySelectorAll('[data-open-modal="api-key-policy-modal"]').forEach((button) => button.addEventListener("click", () => {
    if (!button.closest("[data-api-key-card]")) resetPolicyModal();
  }));
  const format = (value) => new Intl.NumberFormat().format(Number(value || 0));
  const updateKPIs = () => {
    const cards = Array.from(document.querySelectorAll("[data-api-key-card]"));
    const total = cards.length;
    const active = cards.filter((card) => card.dataset.apiKeyEnabled === "true").length;
    const aggregate = (name) => cards.reduce((sum, card) => sum + Number(card.dataset[`apiKey${name}`] || 0), 0);
    const set = (name, value) => { const node = document.querySelector(`[data-api-key-kpi="${name}"]`); if (node) node.textContent = value; };
    set("total", format(total));
    set("active", format(active));
    set("requests", format(aggregate("Requests")));
    set("tokens", format(aggregate("Tokens")));
  };
  const refreshDirectory = () => {
    if (!list) return;
    const query = (search?.value || "").trim().toLowerCase();
    const state = filter?.value || "all";
    const cards = Array.from(list.querySelectorAll("[data-api-key-card]"));
    cards.forEach((card) => {
      const enabled = card.dataset.apiKeyEnabled === "true";
      const matchesQuery = !query || (card.dataset.apiKeySearch || "").toLowerCase().includes(query);
      const matchesState = state === "all" || (state === "active" && enabled) || (state === "disabled" && !enabled);
      card.hidden = !(matchesQuery && matchesState);
    });
    const sortBy = sort?.value || "default";
    const visible = cards.filter((card) => !card.hidden);
    if (sortBy !== "default") {
      visible.sort((a, b) => {
        if (sortBy === "id") return (a.dataset.apiKeyId || "").localeCompare(b.dataset.apiKeyId || "");
        const names = { requests: "apiKeyRequests", tokens: "apiKeyTokens", cost: "apiKeyCost" };
        return Number(b.dataset[names[sortBy]] || 0) - Number(a.dataset[names[sortBy]] || 0);
      }).forEach((card) => list.appendChild(card));
    }
    if (noResults) noResults.hidden = visible.length > 0 || cards.length === 0;
  };
  updateKPIs();
  refreshDirectory();
  search?.addEventListener("input", refreshDirectory);
  filter?.addEventListener("change", refreshDirectory);
  sort?.addEventListener("change", refreshDirectory);
  document.querySelectorAll("[data-api-key-view]").forEach((button) => button.addEventListener("click", () => {
    const view = button.dataset.apiKeyView || "list";
    directory?.classList.toggle("is-grid", view === "grid");
    document.querySelectorAll("[data-api-key-view]").forEach((node) => {
      const active = node === button;
      node.classList.toggle("is-active", active);
      node.setAttribute("aria-pressed", String(active));
    });
  }));
  document.addEventListener("click", (event) => {
    const copy = event.target.closest(".js-copy-api-key");
    if (copy) { navigator.clipboard?.writeText(copy.closest("[data-api-key-card]")?.querySelector(".api-key-secret")?.textContent || ""); showToast("API key copied"); return; }
    const edit = event.target.closest(".js-edit-api-key");
    if (edit) { const c = edit.closest("[data-api-key-card]"); ["id","key","models","requests","tokens","usd","enabled"].forEach((k) => { const el = field(k); const old = c?.querySelector(`[data-api-key-field="${k}"]`); if (el && old) el.value = old.value; }); const editing = field("editing"); if (editing) editing.value = field("id")?.value || ""; openModal("api-key-policy-modal"); return; }
    const toggle = event.target.closest(".js-toggle-api-key");
    if (toggle) { const id = toggle.closest("[data-api-key-card]")?.querySelector('[data-api-key-field="id"]')?.value || ""; write(rows().map((r) => { if (r[0] === id) r[6] = String(String(r[6]).toLowerCase() !== "true"); return r; })); form.requestSubmit(); return; }
    const remove = event.target.closest(".js-remove-api-key");
    if (remove) { const id = remove.closest("[data-api-key-card]")?.querySelector('[data-api-key-field="id"]')?.value || ""; if (!confirm(`Delete API key ${id}?`)) return; write(rows().filter((r) => r[0] !== id)); form.requestSubmit(); return; }
    const generate = event.target.closest(".js-generate-api-secret");
    if (generate) { const key = field("key"); if (key) key.value = generateSecretKey(); return; }
    const add = event.target.closest(".js-add-api-key-policy");
    if (add) {
      const idInput = field("id");
      const keyInput = field("key");
      const id = (idInput?.value || "").trim() || defaultAPIKeyID();
      const key = (keyInput?.value || "").trim() || generateSecretKey();
      if (idInput) idInput.value = id;
      if (keyInput) keyInput.value = key;
      const editing = field("editing")?.value || id;
      const models = (field("models")?.value || "*").trim() || "*";
      const requests = (field("requests")?.value || "0").trim() || "0";
      const tokens = (field("tokens")?.value || "0").trim() || "0";
      const usd = (field("usd")?.value || "0").trim() || "0";
      const rpm = (field("rpm")?.value || "0").trim() || "0";
      const concurrent = (field("concurrent")?.value || "0").trim() || "0";
      const enabled = (field("enabled")?.value || "true").trim() || "true";
      const list = rows().filter((r) => r[0] !== editing && r[0] !== id);
      list.push([id, key, models, requests, tokens, usd, enabled, rpm, concurrent]);
      write(list);
      form.requestSubmit();
    }
  });
}
function initPricingManager() {
  const form = document.getElementById("pricing-form");
  const textarea = document.getElementById("model-prices-text");
  if (!form || !textarea) return;
  const field = (name) => document.querySelector(`[data-new-pricing="${name}"]`);
  const rows = () => (textarea.value || "").split(/\r?\n/).map((l) => l.trim()).filter(Boolean).map((l) => l.split("|"));
  const write = (list) => { textarea.value = list.map((r) => r.join("|")).join("\n"); };
  const fill = (data) => Object.entries(data).forEach(([k, v]) => { const el = field(k); if (el) el.value = v || ""; });
  let pendingPricingPairs = null;
  const selectedModels = (block) => Array.from(block?.querySelectorAll(".js-configure-pricing.is-selected") || []).map((el) => el.dataset.pricingModel).filter(Boolean);
  const selectedPairs = () => Array.from(document.querySelectorAll(".js-configure-pricing.is-selected")).map((el) => ({provider: el.dataset.pricingProvider, model: el.dataset.pricingModel})).filter((x) => x.provider && x.model);
  const refreshAllButton = () => {
    const count = selectedPairs().length;
    const button = document.querySelector(".js-configure-pricing-all");
    if (!button) return;
    button.hidden = count === 0;
    const badge = button.querySelector("[data-pricing-all-selected-count]");
    if (badge) badge.textContent = String(count);
  };
  const refreshBulkButton = (block) => {
    const count = selectedModels(block).length;
    const button = block?.querySelector(".js-configure-pricing-bulk");
    if (button) {
      button.hidden = count === 0;
      const badge = button.querySelector("[data-pricing-selected-count]");
      if (badge) badge.textContent = String(count);
    }
    refreshAllButton();
  };
  const cardPairs = (card) => (card?.querySelector('[data-pricing-field="pairs"]')?.value || "").split(/\r?\n/).map((line) => line.trim()).filter(Boolean).map((line) => { const [provider, model] = line.split("|"); return {provider: provider || "", model}; }).filter((x) => x.model);
  const clearSelection = (block) => {
    block?.querySelectorAll(".js-configure-pricing.is-selected").forEach((el) => el.classList.remove("is-selected"));
    refreshBulkButton(block);
  };
  document.addEventListener("click", (event) => {
    if (event.target.closest('[data-open-modal="model-pricing-modal"]')) pendingPricingPairs = null;
    const all = event.target.closest(".js-configure-pricing-all");
    if (all) { const pairs = selectedPairs(); if (pairs.length) { pendingPricingPairs = pairs; fill({provider: [...new Set(pairs.map((p) => p.provider))].join(", "), model: [...new Set(pairs.map((p) => p.model))].join(", ")}); openModal("model-pricing-modal"); } return; }
    const bulk = event.target.closest(".js-configure-pricing-bulk");
    if (bulk) { const block = bulk.closest("[data-pricing-provider-block]"); const models = selectedModels(block); if (models.length) { pendingPricingPairs = models.map((model) => ({provider: bulk.dataset.pricingProvider, model})); fill({provider: bulk.dataset.pricingProvider, model: models.join(", ")}); openModal("model-pricing-modal"); } return; }
    const pick = event.target.closest(".js-configure-pricing");
    if (pick) { const block = pick.closest("[data-pricing-provider-block]"); if (event.ctrlKey || event.metaKey) { event.preventDefault(); pick.classList.toggle("is-selected"); refreshBulkButton(block); return; } pendingPricingPairs = null; clearSelection(block); fill({provider: pick.dataset.pricingProvider, model: pick.dataset.pricingModel}); openModal("model-pricing-modal"); return; }
    const edit = event.target.closest(".js-edit-pricing");
    if (edit) { const c = edit.closest("[data-pricing-card]"); pendingPricingPairs = cardPairs(c); const data = {}; ["provider","model","input","output","cached","reasoning","context","rpm","tpm","daily_requests","daily_tokens"].forEach((k) => data[k] = c?.querySelector(`[data-pricing-field="${k}"]`)?.value || ""); fill(data); openModal("model-pricing-modal"); return; }
    const remove = event.target.closest(".js-remove-pricing");
    if (remove) { event.preventDefault(); const pairs = cardPairs(remove.closest("[data-pricing-card]")); write(rows().filter((r) => !pairs.some((p) => p.provider === r[0] && p.model === r[1]))); form.requestSubmit(); return; }
    const add = event.target.closest(".js-add-model-pricing");
    if (add) { event.preventDefault(); const providerValues = splitCSV(field("provider")?.value); const modelValues = splitCSV(field("model")?.value); const pairs = pendingPricingPairs?.length ? pendingPricingPairs : (providerValues.length ? providerValues : [""]).flatMap((provider) => modelValues.map((model) => ({provider, model}))); if (!pairs.length) { showToast("Model is required", false); return; } const list = rows().filter((r) => !pairs.some((p) => (p.provider || "") === (r[0] || "") && p.model === r[1])); for (const {provider, model} of pairs) list.push([provider || "", model, field("input")?.value || "0", field("output")?.value || "0", field("cached")?.value || "0", field("reasoning")?.value || "0", field("context")?.value || "0", field("rpm")?.value || "0", field("tpm")?.value || "0", field("daily_requests")?.value || "0", field("daily_tokens")?.value || "0"]); write(list); form.requestSubmit(); }
  });
  field("provider")?.addEventListener("input", () => { pendingPricingPairs = null; });
  field("model")?.addEventListener("input", () => { pendingPricingPairs = null; });
}
function initComboBuilder() {
  const form = document.getElementById("combo-builder-form");
  const textarea = document.getElementById("combos-text");
  const selected = document.getElementById("combo-selected-list");
  if (!form || !textarea || !selected) return;
  let models = [];
  const rows = () => (textarea.value || "").split(/\r?\n/).map((l) => l.trim()).filter(Boolean).map((l) => l.split("|"));
  const write = (list) => { textarea.value = list.map((r) => r.join("|")).join("\n"); };
  const render = () => { selected.innerHTML = models.length ? models.map((m) => `<span class="combo-selected-chip"><code>${escapeHtml(m)}</code><button type="button" data-remove-selected="${escapeHtml(m)}">×</button></span>`).join("") : `<div class="combo-selected-empty-card"><strong>No model selected yet</strong></div>`; };
  const set = (id, v) => { const el = document.getElementById(id); if (el) el.value = v || ""; };
  document.addEventListener("click", (event) => {
    const opt = event.target.closest("[data-combo-model]"); if (opt) { if (!models.includes(opt.dataset.comboModel)) models.push(opt.dataset.comboModel); render(); return; }
    const rm = event.target.closest("[data-remove-selected]"); if (rm) { models = models.filter((m) => m !== rm.dataset.removeSelected); render(); return; }
    if (event.target.closest("#combo-clear-selection")) { models = []; render(); return; }
    if (event.target.closest("#combo-reset-editor")) { ["combo-name-input","combo-description-input","combo-context-input"].forEach((id) => set(id, "")); set("combo-sticky-input", "1"); models = []; render(); return; }
    if (event.target.closest("#combo-add-button")) { const name = document.getElementById("combo-name-input")?.value.trim(); if (!name || !models.length) { showToast("Combo name and selected models are required", false); return; } const list = rows().filter((r) => r[0] !== name); list.push([name, models.join(","), document.getElementById("combo-strategy-input")?.value || "fallback", document.getElementById("combo-sticky-input")?.value || "1", document.getElementById("combo-context-input")?.value || "0", document.getElementById("combo-enabled-input")?.checked ? "true" : "false", document.getElementById("combo-description-input")?.value || ""]); write(list); form.requestSubmit(); return; }
    const edit = event.target.closest(".js-edit-combo"); if (edit) { const c = edit.closest("[data-combo-card]"); set("combo-name-input", c?.querySelector('[data-combo-field="name"]')?.value); set("combo-description-input", c?.querySelector('[data-combo-field="description"]')?.value); set("combo-strategy-input", c?.querySelector('[data-combo-field="strategy"]')?.value || "fallback"); set("combo-sticky-input", c?.querySelector('[data-combo-field="sticky"]')?.value || "1"); set("combo-context-input", c?.querySelector('[data-combo-field="context"]')?.value || ""); const enabled = document.getElementById("combo-enabled-input"); if (enabled) enabled.checked = c?.querySelector('[data-combo-field="enabled"]')?.value !== "false"; models = splitCSV(c?.querySelector('[data-combo-field="models"]')?.value || ""); render(); return; }
    const del = event.target.closest(".js-remove-combo"); if (del) { const name = del.closest("[data-combo-card]")?.querySelector('[data-combo-field="name"]')?.value || ""; if (!confirm(`Delete combo ${name}?`)) return; write(rows().filter((r) => r[0] !== name)); form.requestSubmit(); }
  });
  document.getElementById("combo-model-filter")?.addEventListener("input", (e) => { const q = e.target.value.toLowerCase(); document.querySelectorAll("[data-combo-model]").forEach((el) => el.hidden = q && !el.textContent.toLowerCase().includes(q)); });
  render();
}
function initAPIKeyQuotas() { document.querySelectorAll(".api-key-quota").forEach((el) => { const used = Number(el.dataset.used || 0), limit = Number(el.dataset.limit || 0); const pct = limit > 0 ? Math.max(0, Math.min(100, used / limit * 100)) : 0; const bar = el.querySelector("i"); if (bar) { bar.style.display = "block"; bar.style.width = `${pct}%`; } }); }
function initDashboardQuickStart() {
  const base = document.querySelector("[data-dashboard-base-url]");
  if (!base) return;
  const baseURL = window.location.origin;
  base.textContent = baseURL;
  const status = document.querySelector("[data-dashboard-copy-status]");
  const copy = async (value, button) => {
    try {
      await navigator.clipboard.writeText(value);
      document.querySelectorAll(".dashboard-copy-button.is-copied").forEach((el) => el.classList.remove("is-copied"));
      button?.classList.add("is-copied");
      if (status) status.textContent = t("dashboard.copied", "Copied");
      setTimeout(() => { button?.classList.remove("is-copied"); if (status) status.textContent = ""; }, 1800);
    } catch (_) {
      if (status) status.textContent = value;
    }
  };
  document.querySelector("[data-copy-base]")?.addEventListener("click", (event) => copy(baseURL, event.currentTarget));
  document.querySelectorAll("[data-copy-endpoint]").forEach((button) => button.addEventListener("click", () => copy(`${baseURL}${button.dataset.copyEndpoint}`, button)));
}
function initRestoreGuard() {
  document.querySelectorAll('form[action*="restore"], form[data-restore-form]').forEach((form) => form.addEventListener("submit", (event) => { if (!confirm("Restore database? This will overwrite current data after creating a safety backup.")) event.preventDefault(); }));
  document.querySelectorAll('form[action*="reset-data"], form[data-reset-data-form]').forEach((form) => form.addEventListener("submit", (event) => {
    const input = form.querySelector('input[name="confirm"]') || document.querySelector('input[name="confirm"][form="reset-data-form"]');
    if (!input || input.value !== "DELETE" || !confirm("Reset all data? This permanently deletes the database contents.")) event.preventDefault();
  }));
}


function initPromptRouterRoles() {
  const list = document.getElementById("router-role-list");
  const count = document.getElementById("router-role-count");
  const template = document.getElementById("router-role-template");
  if (!list || !count || !template) return;
  const setNamed = (name, value) => { const el = document.querySelector(`[name="${name}"]`); if (el) el.value = value || ""; };
  const renumber = () => {
    const rows = [...list.querySelectorAll("[data-router-role-row]")];
    rows.forEach((row, idx) => {
      const n = idx + 1;
      row.querySelector('[data-role-field="role"], input[name^="role_"]')?.setAttribute("name", `role_${n}`);
      row.querySelector('[data-role-field="complexity"], select[name^="complexity_"]')?.setAttribute("name", `complexity_${n}`);
      row.querySelector('[data-role-field="risk"], select[name^="risk_"]')?.setAttribute("name", `risk_${n}`);
      row.querySelector('[data-role-field="target"], input[name^="target_"]')?.setAttribute("name", `target_${n}`);
      row.querySelector('[data-role-field="inject"], input[name^="inject_"]')?.setAttribute("name", `inject_${n}`);
      row.querySelector('[data-role-field="instruction"], textarea[name^="instruction_"]')?.setAttribute("name", `instruction_${n}`);
    });
    count.value = String(rows.length);
  };
  const getRouteVal = (route, key) => route?.[key] ?? route?.[key.charAt(0).toUpperCase() + key.slice(1)] ?? "";
  const getRouteBool = (route, key, fallback = false) => {
    const pascal = key.split("_").map((p) => p.charAt(0).toUpperCase() + p.slice(1)).join("");
    return route?.[key] ?? route?.[pascal] ?? fallback;
  };
  const appendRoute = (route = {}) => {
    const node = template.content.firstElementChild.cloneNode(true);
    list.appendChild(node);
    const setInput = (sel, val) => { const el = node.querySelector(sel); if (el) el.value = val || ""; };
    setInput('[data-role-field="role"], input[name^="role_"]', getRouteVal(route, "role"));
    setInput('[data-role-field="complexity"], select[name^="complexity_"]', getRouteVal(route, "complexity"));
    setInput('[data-role-field="risk"], select[name^="risk_"]', getRouteVal(route, "risk"));
    setInput('[data-role-field="target"], input[name^="target_"]', getRouteVal(route, "target"));
    const inject = node.querySelector('[data-role-field="inject"], input[name^="inject_"]');
    if (inject) inject.checked = getRouteBool(route, "inject_instruction", true);
    setInput('[data-role-field="instruction"], textarea[name^="instruction_"]', getRouteVal(route, "instruction"));
    renumber();
    return node;
  };
  const routeFromRow = (row) => {
    const get = (prefix) => row.querySelector(`[name^="${prefix}_"]`)?.value || "";
    return {
      role: get("role"),
      complexity: get("complexity"),
      risk: get("risk"),
      target: get("target"),
      inject_instruction: row.querySelector('input[name^="inject_"]')?.checked !== false,
      instruction: row.querySelector('textarea[name^="instruction_"]')?.value || "",
    };
  };
  document.addEventListener("click", (event) => {
    const clearImport = event.target.closest("#prompt-router-import-clear");
    if (clearImport) {
      event.preventDefault();
      const input = document.getElementById("prompt-router-import-json");
      if (input) input.value = "";
      return;
    }
    const copyRouter = event.target.closest(".js-copy-prompt-router-json");
    if (copyRouter) {
      event.preventDefault();
      const card = copyRouter.closest("[data-prompt-router-card]");
      const raw = card?.querySelector(".js-prompt-router-json")?.textContent || "{}";
      try {
        const pretty = JSON.stringify(JSON.parse(raw), null, 2);
        navigator.clipboard?.writeText(pretty);
        showToast("Prompt Router JSON copied");
      } catch {
        showToast("Could not copy router config", false);
      }
      return;
    }
    const edit = event.target.closest(".js-edit-prompt-router");
    if (edit) {
      event.preventDefault();
      const card = edit.closest("[data-prompt-router-card]");
      const raw = card?.querySelector(".js-prompt-router-json")?.textContent || "{}";
      let router = {};
      try { router = JSON.parse(raw); } catch { showToast("Could not read router config", false); return; }
      setNamed("name", router.name || "");
      setNamed("description", router.description || "");
      setNamed("classifier_model", router.classifier_model || "");
      setNamed("fallback_target", router.fallback_target || "");
      setNamed("fallback_role", router.fallback_role || "");
      const enabled = document.querySelector('[name="enabled"]');
      if (enabled) enabled.checked = router.enabled !== false;
      const useRaw = document.querySelector('[name="use_raw_prompt"]');
      if (useRaw) useRaw.checked = !!router.use_raw_prompt;
      setNamed("classifier_prompt_template", router.classifier_prompt_template || "");
      list.innerHTML = "";
      (router.routes || []).forEach((route) => appendRoute(route));
      if (!(router.routes || []).length) appendRoute({role:"planner", inject_instruction:true});
      renumber();
      document.querySelector('[name="name"]')?.scrollIntoView({behavior:"smooth", block:"center"});
      showToast(`Loaded ${router.name || "router"} for editing`);
      return;
    }
    const copyRole = event.target.closest(".js-copy-router-role");
    if (copyRole) {
      event.preventDefault();
      const row = copyRole.closest("[data-router-role-row]");
      if (!row) return;
      const copied = appendRoute(routeFromRow(row));
      copied.scrollIntoView({behavior:"smooth", block:"center"});
      showToast("Prompt Router role copied");
      return;
    }
    const add = event.target.closest(".js-add-router-role");
    if (add) {
      event.preventDefault();
      const node = appendRoute({inject_instruction:true});
      node.querySelector('input[name^="role_"]')?.focus();
      return;
    }
    const remove = event.target.closest(".js-remove-router-role");
    if (remove) {
      event.preventDefault();
      remove.closest("[data-router-role-row]")?.remove();
      renumber();
    }
  });
  const importForm = document.getElementById("prompt-router-import-form");
  if (importForm) importForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const input = document.getElementById("prompt-router-import-json");
    const result = document.getElementById("prompt-router-import-result");
    const json = input?.value.trim() || "";
    if (!json) { showToast("Missing Prompt Router JSON", false); return; }
    try { JSON.parse(json); } catch (err) { showToast(`Invalid JSON: ${err.message}`, false); return; }
    const button = importForm.querySelector('button[type="submit"]');
    if (button) { button.disabled = true; button.textContent = "Importing..."; }
    try {
      const res = await fetch("/api/prompt-routers/import", { method: "POST", cache: "no-store", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: json });
      const data = await safeJSON(res);
      if (!res.ok) throw new Error(data.message || data.error || `HTTP ${res.status}`);
      const message = `Imported ${data.imported || 0} router(s). Total: ${data.total || 0}.`;
      if (result) { result.textContent = message; result.classList.remove("error"); }
      showToast(message);
      window.location.href = "/prompt-routers?saved=1";
    } catch (err) {
      const message = err.message || String(err);
      if (result) { result.textContent = message; result.classList.add("error"); }
      showToast(message, false);
    } finally {
      if (button) { button.disabled = false; button.textContent = t("prompt_routers.import", "Add / update from JSON"); }
    }
  });
  renumber();
}

function initFusionEditor() {
  const textarea = document.getElementById("fusions-text");
  const example = document.getElementById("fusion-example-json");
  const button = document.getElementById("fusion-insert-example");
  if (!textarea || !example || !button) return;
  button.addEventListener("click", () => {
    if (textarea.value.trim() && !confirm("Replace current Fusion JSON with the example?")) return;
    try {
      textarea.value = JSON.stringify(JSON.parse(example.textContent || "[]"), null, 2);
    } catch {
      textarea.value = example.textContent || "[]";
    }
  });
  document.getElementById("fusion-form")?.addEventListener("submit", (event) => {
    try {
      JSON.parse(textarea.value || "[]");
    } catch (err) {
      event.preventDefault();
      showToast(`Invalid Fusion JSON: ${err.message}`, false);
    }
  });
}

function initUsageStatsTable() {
  const table = document.getElementById("usage-stats-table");
  const tbody = document.getElementById("usage-stats-table-body");
  const groupSelect = document.getElementById("usage-stats-group-select");
  const metricTabs = document.getElementById("usage-stats-metric-tabs");
  const emptyMessage = document.getElementById("usage-stats-empty");
  const headerRow = document.getElementById("usage-stats-header-row");
  if (!table || !tbody || !groupSelect || !metricTabs || !headerRow) return;

  let tableData = window.VivuRouterUsageTable || { by_model: [], by_account: [], by_api_key: [], by_endpoint: [] };

  const state = {
    groupBy: 'model',
    metric: 'cost',
    sortBy: 'total_cost',
    sortDesc: true,
    expanded: {}
  };

  const getGroupData = () => {
    switch (state.groupBy) {
      case 'model': return tableData.by_model || [];
      case 'account': return tableData.by_account || [];
      case 'api_key': return tableData.by_api_key || [];
      case 'endpoint': return tableData.by_endpoint || [];
      default: return [];
    }
  };

  window.updateUsageStatsTable = (newTableData) => {
    tableData = newTableData || { by_model: [], by_account: [], by_api_key: [], by_endpoint: [] };
    state.expanded = {};
    updateTable();
  };

  const formatCost = (val) => '$' + (Number(val) || 0).toFixed(4);
  const formatTokens = (val) => Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(Number(val) || 0);

  const getSortValue = (row, col) => {
    switch (col) {
      case 'name': return row.name;
      case 'provider': return row.provider;
      case 'requests': return row.requests;
      case 'last_used': return row.last_used_secs_ago;
      case 'input_cost': return state.metric === 'cost' ? row.input_cost : row.prompt_tokens;
      case 'output_cost': return state.metric === 'cost' ? row.output_cost : row.completion_tokens;
      case 'total_cost': return state.metric === 'cost' ? row.total_cost : row.total_tokens;
      default: return 0;
    }
  };

  const renderHeaders = () => {
    let nameLabel = 'Model';
    if (state.groupBy === 'account') nameLabel = 'Account';
    if (state.groupBy === 'api_key') nameLabel = 'API Key';
    if (state.groupBy === 'endpoint') nameLabel = 'Endpoint';

    const costOrTokensHeaders = state.metric === 'cost' ? `
      <th class="text-right pointer" data-col="input_cost">Input Cost</th>
      <th class="text-right pointer" data-col="output_cost">Output Cost</th>
      <th class="text-right pointer active-sort" data-col="total_cost">Total Cost</th>
    ` : `
      <th class="text-right pointer" data-col="input_cost">Input Tokens</th>
      <th class="text-right pointer" data-col="output_cost">Output Tokens</th>
      <th class="text-right pointer active-sort" data-col="total_cost">Total Tokens</th>
    `;

    headerRow.innerHTML = `
      <th style="width: 40px;"></th>
      <th class="pointer" data-col="name">${nameLabel}</th>
      <th class="pointer" data-col="provider">Provider</th>
      <th class="text-right pointer" data-col="requests">Requests</th>
      <th class="pointer" data-col="last_used">Last Used</th>
      ${costOrTokensHeaders}
    `;

    headerRow.querySelectorAll('th[data-col]').forEach(th => {
      const col = th.dataset.col;
      th.classList.remove('active-sort');
      if (col === state.sortBy) {
        th.classList.add('active-sort');
        th.innerHTML += state.sortDesc ? ' ↓' : ' ↑';
      }
    });
  };

  const renderRows = () => {
    const list = getGroupData();
    if (!list.length) {
      tbody.innerHTML = '';
      emptyMessage.hidden = false;
      table.style.display = 'none';
      return;
    }
    emptyMessage.hidden = true;
    table.style.display = '';

    const sortedParents = [...list].sort((a, b) => {
      let va = getSortValue(a, state.sortBy);
      let vb = getSortValue(b, state.sortBy);

      if (typeof va === 'string') {
        return state.sortDesc ? vb.localeCompare(va) : va.localeCompare(vb);
      }
      return state.sortDesc ? vb - va : va - vb;
    });

    let html = '';
    sortedParents.forEach((pRow) => {
      const parentId = pRow.name;
      const isExpanded = !!state.expanded[parentId];
      const hasChildren = Array.isArray(pRow.children) && pRow.children.length > 0;
      const caret = hasChildren ? (isExpanded ? '▼' : '▶') : '';

      const inputVal = state.metric === 'cost' ? formatCost(pRow.input_cost) : formatTokens(pRow.prompt_tokens);
      const outputVal = state.metric === 'cost' ? formatCost(pRow.output_cost) : formatTokens(pRow.completion_tokens);
      const totalVal = state.metric === 'cost' ? formatCost(pRow.total_cost) : formatTokens(pRow.total_tokens);

      html += `
        <tr class="parent-row" data-parent-id="${escapeHtml(parentId)}">
          <td class="col-expand pointer text-center">${caret}</td>
          <td class="col-name font-semibold pointer">${escapeHtml(pRow.name)}</td>
          <td class="col-provider text-muted">${escapeHtml(pRow.provider)}</td>
          <td class="col-requests text-right">${pRow.requests}</td>
          <td class="col-last-used text-muted">${escapeHtml(pRow.last_used_label || '—')}</td>
          <td class="col-input-cost text-right text-muted">${inputVal}</td>
          <td class="col-output-cost text-right text-muted">${outputVal}</td>
          <td class="col-total-cost text-right font-bold text-orange">${totalVal}</td>
        </tr>
      `;

      if (hasChildren && isExpanded) {
        const sortedChildren = [...pRow.children].sort((a, b) => {
          let va = getSortValue(a, state.sortBy);
          let vb = getSortValue(b, state.sortBy);
          if (typeof va === 'string') {
            return state.sortDesc ? vb.localeCompare(va) : va.localeCompare(vb);
          }
          return state.sortDesc ? vb - va : va - vb;
        });

        sortedChildren.forEach((cRow) => {
          const cInputVal = state.metric === 'cost' ? formatCost(cRow.input_cost) : formatTokens(cRow.prompt_tokens);
          const cOutputVal = state.metric === 'cost' ? formatCost(cRow.output_cost) : formatTokens(cRow.completion_tokens);
          const cTotalVal = state.metric === 'cost' ? formatCost(cRow.total_cost) : formatTokens(cRow.total_tokens);

          html += `
            <tr class="child-row">
              <td class="col-expand"></td>
              <td class="col-name text-muted" style="padding-left: 1.5rem;">↳ ${escapeHtml(cRow.name)}</td>
              <td class="col-provider text-muted">${escapeHtml(cRow.provider)}</td>
              <td class="col-requests text-right text-muted">${cRow.requests}</td>
              <td class="col-last-used text-muted">${escapeHtml(cRow.last_used_label || '—')}</td>
              <td class="col-input-cost text-right text-muted">${cInputVal}</td>
              <td class="col-output-cost text-right text-muted">${cOutputVal}</td>
              <td class="col-total-cost text-right text-muted">${cTotalVal}</td>
            </tr>
          `;
        });
      }
    });

    tbody.innerHTML = html;
  };

  const updateTable = () => {
    renderHeaders();
    renderRows();
  };

  groupSelect.addEventListener("change", () => {
    state.groupBy = groupSelect.value;
    state.expanded = {};
    state.sortBy = 'total_cost';
    state.sortDesc = true;
    updateTable();
  });

  metricTabs.addEventListener("click", (e) => {
    const tab = e.target.closest("[data-metric]");
    if (!tab) return;
    metricTabs.querySelectorAll("[data-metric]").forEach((el) => el.classList.remove("active"));
    tab.classList.add("active");
    state.metric = tab.dataset.metric;
    updateTable();
  });

  headerRow.addEventListener("click", (e) => {
    const th = e.target.closest("th[data-col]");
    if (!th) return;
    const col = th.dataset.col;
    if (state.sortBy === col) {
      state.sortDesc = !state.sortDesc;
    } else {
      state.sortBy = col;
      state.sortDesc = true;
    }
    updateTable();
  });

  tbody.addEventListener("click", (e) => {
    const tr = e.target.closest("tr.parent-row");
    if (!tr) return;
    const cell = e.target.closest("td");
    if (cell && (cell.classList.contains("col-expand") || cell.classList.contains("col-name"))) {
      const parentId = tr.dataset.parentId;
      state.expanded[parentId] = !state.expanded[parentId];
      renderRows();
    }
  });

  updateTable();
}
