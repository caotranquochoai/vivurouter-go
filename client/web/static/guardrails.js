document.addEventListener("DOMContentLoaded", () => {
  const textarea = document.getElementById("guardrails-text");
  if (!textarea) return;
  const vi = (document.documentElement.lang || "vi") === "vi";
  const $ = (id) => document.getElementById(id);
  const f = {
    name: $("guardrail-name"), description: $("guardrail-description"), enabled: $("guardrail-enabled"),
    optimizerEnabled: $("guardrail-optimizer-enabled"), validatorEnabled: $("guardrail-validator-enabled"), optimizer: $("guardrail-optimizer-target"), main: $("guardrail-main-target"), validator: $("guardrail-validator-target"),
    customPolicy: $("guardrail-custom-policy"), optimizerTimeout: $("guardrail-optimizer-timeout"), mainTimeout: $("guardrail-main-timeout"), validatorTimeout: $("guardrail-validator-timeout"),
    maxPatches: $("guardrail-max-patches"), maxPatchBytes: $("guardrail-max-patch-bytes"), maxBufferedBytes: $("guardrail-max-buffered-bytes"),
    optimizerFailOpen: $("guardrail-optimizer-fail-open"), validatorFailOpen: $("guardrail-validator-fail-open"),
  };
  const msg = {
    edit: vi ? "Sửa" : "Edit", duplicate: vi ? "Nhân bản" : "Duplicate", remove: vi ? "Xóa" : "Delete", enabled: vi ? "đang bật" : "enabled", disabled: vi ? "đã tắt" : "disabled",
    empty: vi ? "Chưa có Guardrail. Dùng biểu mẫu phía trên để tạo cấu hình đầu tiên." : "No Guardrails yet. Use the builder above to create one.",
    name: vi ? "Cần nhập tên Guardrail." : "Guardrail name is required.", main: vi ? "Cần chọn model chính." : "Main model is required.", validator: vi ? "Cần chọn model kiểm tra." : "Validator model is required.", optimizer: vi ? "Cần chọn optimizer khi bật tối ưu đầu vào." : "Optimizer target is required when optimization is enabled.", duplicateName: vi ? "Tên Guardrail đã tồn tại." : "That Guardrail name already exists.", self: vi ? "Guardrail không được trỏ stage về chính nó." : "A Guardrail cannot target itself.", saved: vi ? "Đã thêm Guardrail vào danh sách. Nhấn Lưu để ghi cấu hình." : "Guardrail added. Click Save to persist changes.", confirm: vi ? "Xóa Guardrail" : "Delete Guardrail",
  };
  let items = parseJSON(textarea.value);
  let editingName = "";
  const policies = () => Array.from(document.querySelectorAll("[data-guardrail-policy]:checked")).map((x) => x.value);
  const num = (field, fallback) => Number(field.value || fallback);

  function reset() {
    editingName = ""; f.name.value = ""; f.description.value = ""; f.enabled.checked = true; f.optimizerEnabled.checked = true; f.validatorEnabled.checked = true;
    setSelect(f.optimizer, ""); setSelect(f.main, ""); setSelect(f.validator, ""); f.customPolicy.value = "";
    f.optimizerTimeout.value = 30000; f.mainTimeout.value = 120000; f.validatorTimeout.value = 30000; f.maxPatches.value = 128; f.maxPatchBytes.value = 262144; f.maxBufferedBytes.value = 4194304;
    f.optimizerFailOpen.checked = true; f.validatorFailOpen.checked = true;
    document.querySelectorAll("[data-guardrail-policy]").forEach((x) => { x.checked = true; }); updateStages(); updateCount();
  }
  function read() {
    return {
      name: f.name.value.trim(), description: f.description.value.trim(), enabled: f.enabled.checked,
      schema_version: 1, optimizer_enabled: f.optimizerEnabled.checked, validator_enabled: f.validatorEnabled.checked, optimizer_target: f.optimizer.value.trim(), main_target: f.main.value.trim(), validator_target: f.validator.value.trim(), response_mode: "buffered-stream",
      policy_presets: policies(), custom_policy: f.customPolicy.value.trim(), optimizer_timeout_ms: num(f.optimizerTimeout, 30000), main_timeout_ms: num(f.mainTimeout, 120000), validator_timeout_ms: num(f.validatorTimeout, 30000),
      max_patch_count: num(f.maxPatches, 128), max_patch_bytes: num(f.maxPatchBytes, 262144), max_buffered_bytes: num(f.maxBufferedBytes, 4194304), optimizer_fail_open: f.optimizerFailOpen.checked, validator_fail_open: f.validatorFailOpen.checked,
    };
  }
  function load(item) {
    editingName = item.name || ""; f.name.value = item.name || ""; f.description.value = item.description || ""; f.enabled.checked = item.enabled !== false; f.optimizerEnabled.checked = item.optimizer_enabled !== false; f.validatorEnabled.checked = item.schema_version ? item.validator_enabled !== false : true;
    setSelect(f.optimizer, item.optimizer_target || ""); setSelect(f.main, item.main_target || ""); setSelect(f.validator, item.validator_target || ""); f.customPolicy.value = item.custom_policy || "";
    f.optimizerTimeout.value = item.optimizer_timeout_ms || 30000; f.mainTimeout.value = item.main_timeout_ms || 120000; f.validatorTimeout.value = item.validator_timeout_ms || 30000; f.maxPatches.value = item.max_patch_count || 128; f.maxPatchBytes.value = item.max_patch_bytes || 262144; f.maxBufferedBytes.value = item.max_buffered_bytes || 4194304;
    f.optimizerFailOpen.checked = item.optimizer_fail_open !== false; f.validatorFailOpen.checked = item.validator_fail_open !== false;
    const selected = new Set(item.policy_presets || ["safety", "quality", "format", "privacy"]); document.querySelectorAll("[data-guardrail-policy]").forEach((x) => { x.checked = selected.has(x.value); }); updateStages(); updateCount(); window.scrollTo({ top: 0, behavior: "smooth" });
  }
  function validate(item) {
    if (!item.name) return msg.name; if (!item.main_target) return msg.main; if (item.validator_enabled && !item.validator_target) return msg.validator; if (item.optimizer_enabled && !item.optimizer_target) return msg.optimizer;
    if (item.main_target === item.name || (item.optimizer_enabled && item.optimizer_target === item.name) || (item.validator_enabled && item.validator_target === item.name)) return msg.self;
    if (items.some((x) => x.name === item.name && x.name !== editingName)) return msg.duplicateName;
    return "";
  }
  function saveEditor() {
    const item = read(); const error = validate(item); if (error) return toast(error, false);
    items = items.filter((x) => x.name !== (editingName || item.name)); items.push(item); editingName = item.name; sync(); load(item); toast(msg.saved, true);
  }
  function sync() {
    textarea.value = JSON.stringify(items, null, 2); $("guardrail-json-preview").textContent = textarea.value; $("guardrail-count").textContent = String(items.length); render();
  }
  function render() {
    const grid = $("guardrail-card-grid");
    if (!items.length) { grid.innerHTML = `<article class="guardrail-empty">${esc(msg.empty)}</article>`; return; }
    grid.innerHTML = items.map((x) => `<article class="guardrail-card ${x.enabled === false ? "is-disabled" : ""}"><div class="api-key-card-head"><div><span class="badge">${x.enabled === false ? msg.disabled : msg.enabled}</span><h3>${esc(x.name)}</h3></div><div class="inline-actions"><button type="button" class="btn-secondary" data-edit-guardrail="${attr(x.name)}">${msg.edit}</button><button type="button" class="btn-secondary" data-copy-guardrail="${attr(x.name)}">${msg.duplicate}</button><button type="button" class="btn-danger" data-delete-guardrail="${attr(x.name)}">${msg.remove}</button></div></div><p>${esc(x.description || "")}</p><div class="guardrail-card-pipeline"><code>${esc(x.optimizer_enabled === false ? "Optimizer off" : x.optimizer_target || "Optimizer")}</code><b>→</b><code>${esc(x.main_target || "Main")}</code><b>→</b><code>${esc(x.validator_enabled === false ? "Validator off" : x.validator_target || "Validator")}</code></div><div class="guardrail-policy-chips">${(x.policy_presets || []).map((p) => `<span>${esc(p)}</span>`).join("")}</div></article>`).join("");
  }
  function updateStages() {
    f.optimizer.disabled = !f.optimizerEnabled.checked; f.optimizer.closest(".guardrail-stage")?.classList.toggle("is-disabled", !f.optimizerEnabled.checked);
    f.validator.disabled = !f.validatorEnabled.checked; f.validator.closest(".guardrail-stage")?.classList.toggle("is-disabled", !f.validatorEnabled.checked);
    document.querySelectorAll("[data-guardrail-policy], #guardrail-custom-policy, #guardrail-validator-timeout, #guardrail-validator-fail-open").forEach((control) => { control.disabled = !f.validatorEnabled.checked; });
    document.querySelector(".guardrail-policy-section")?.classList.toggle("is-disabled", !f.validatorEnabled.checked);
    document.querySelector(".guardrail-custom-policy")?.classList.toggle("is-disabled", !f.validatorEnabled.checked);
  }
  function updateCount() { $("guardrail-policy-count").textContent = String(f.customPolicy.value.length); }

  $("guardrail-reset").addEventListener("click", reset); $("guardrail-save").addEventListener("click", saveEditor); f.optimizerEnabled.addEventListener("change", updateStages); f.validatorEnabled.addEventListener("change", updateStages); f.customPolicy.addEventListener("input", updateCount);
  document.addEventListener("click", (event) => {
    const edit = event.target.closest("[data-edit-guardrail]"); if (edit) { const item = items.find((x) => x.name === edit.dataset.editGuardrail); if (item) load(item); return; }
    const copy = event.target.closest("[data-copy-guardrail]"); if (copy) { const item = items.find((x) => x.name === copy.dataset.copyGuardrail); if (item) load({ ...item, name: `${item.name}-copy`, enabled: false }); editingName = ""; return; }
    const del = event.target.closest("[data-delete-guardrail]"); if (del) { if (!confirm(`${msg.confirm} ${del.dataset.deleteGuardrail}?`)) return; items = items.filter((x) => x.name !== del.dataset.deleteGuardrail); if (editingName === del.dataset.deleteGuardrail) reset(); sync(); }
  });
  $("guardrail-form").addEventListener("submit", (event) => { if (f.name.value.trim()) { const item = read(); const error = validate(item); if (error) { event.preventDefault(); return toast(error, false); } items = items.filter((x) => x.name !== (editingName || item.name)); items.push(item); } sync(); });
  sync(); if (items[0]) load(items[0]); else reset();
});

function parseJSON(raw) { try { const value = JSON.parse(raw || "[]"); return Array.isArray(value) ? value : []; } catch { return []; } }
function setSelect(select, value) { if (value && !Array.from(select.options).some((x) => x.value === value)) select.add(new Option(value, value)); select.value = value; }
function esc(value) { return String(value ?? "").replace(/[&<>'"]/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;",'"':"&quot;"}[c])); }
function attr(value) { return esc(value).replace(/"/g, "&quot;"); }
function toast(message, ok) { if (typeof showToast === "function") showToast(message, ok); else alert(message); }
