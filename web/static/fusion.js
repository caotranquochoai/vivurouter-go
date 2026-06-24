document.addEventListener("DOMContentLoaded", () => {
  const textarea = document.getElementById("fusions-text");
  if (!textarea) return;

  const isVI = (document.documentElement.lang || "vi") === "vi";
  const msg = {
    expert: isVI ? "Chuyên gia" : "Expert",
    remove: isVI ? "Xóa" : "Remove",
    name: isVI ? "Tên" : "Name",
    target: isVI ? "Model/target" : "Target",
    weight: isVI ? "Trọng số" : "Weight",
    enabled: isVI ? "Bật" : "Enabled",
    role: isVI ? "Vai trò" : "Role",
    prompt: isVI ? "Prompt riêng cho expert" : "Expert prompt override",
    rolePlaceholder: isVI ? "Tìm rủi ro bảo mật" : "Find security risks",
    promptPlaceholder: isVI ? "Tùy chọn. Để trống sẽ dùng prompt mặc định." : "Optional. Leave blank for default expert prompt.",
    unnamed: isVI ? "chưa đặt tên" : "unnamed",
    disabled: isVI ? "tắt" : "disabled",
    enabledText: isVI ? "bật" : "enabled",
    edit: isVI ? "Sửa" : "Edit",
    delete: isVI ? "Xóa" : "Delete",
    mode: isVI ? "Chế độ" : "Mode",
    experts: isVI ? "Experts" : "Experts",
    minSuccess: isVI ? "Tối thiểu" : "Min success",
    synth: isVI ? "Thư ký" : "Synth",
    reviewer: isVI ? "Quản lý" : "Reviewer",
    timeout: "Timeout",
    none: isVI ? "chưa chọn" : "none",
    emptyTitle: isVI ? "Chưa có Fusion" : "No Fusion definitions yet",
    emptyDesc: isVI ? "Tạo Fusion bằng form trực quan phía trên." : "Create one with the visual builder above.",
    nameRequired: isVI ? "Cần nhập tên Fusion" : "Fusion name is required",
    expertRequired: isVI ? "Cần ít nhất một expert có target" : "At least one expert target is required",
    synthRequired: isVI ? "Cần chọn model thư ký tổng hợp" : "Synthesizer target is required",
    saved: isVI ? "Đã thêm Fusion vào danh sách" : "Fusion saved to list",
    confirmDelete: isVI ? "Xóa Fusion" : "Delete Fusion",
    selectTarget: isVI ? "Chọn model..." : "Select model...",
  };

  const grid = document.getElementById("fusion-card-grid");
  const count = document.getElementById("fusion-count");
  const preview = document.getElementById("fusion-json-preview");
  const expertList = document.getElementById("fusion-expert-list");
  const field = (id) => document.getElementById(id);
  const fields = {
    name: field("fusion-name-input"),
    description: field("fusion-description-input"),
    mode: field("fusion-mode-input"),
    timeout: field("fusion-timeout-input"),
    minSuccess: field("fusion-min-success-input"),
    maxOutput: field("fusion-max-output-input"),
    synthTarget: field("fusion-synth-target-input"),
    reviewerTarget: field("fusion-reviewer-target-input"),
    enabled: field("fusion-enabled-input"),
    requireReviewer: field("fusion-require-reviewer-input"),
    synthPrompt: field("fusion-synth-prompt-input"),
    reviewerPrompt: field("fusion-reviewer-prompt-input"),
  };
  const targetOptionsHTML = buildTargetOptionsHTML();
  let fusions = parseFusionJSON(textarea.value);
  let editingName = "";

  function buildTargetOptionsHTML(selected = "") {
    const base = `<option value="">${escapeHtml(msg.selectTarget)}</option>`;
    const source = document.getElementById("fusion-synth-target-input") || document.querySelector("[data-fusion-target-select]");
    const options = Array.from(source?.querySelectorAll("option") || [])
      .filter((option) => option.value)
      .map((option) => `<option value="${escapeAttr(option.value)}" ${option.value === selected ? "selected" : ""}>${escapeHtml(option.textContent || option.value)}</option>`)
      .join("");
    return base + options;
  }
  function sync() {
    textarea.value = JSON.stringify(fusions, null, 2);
    if (preview) preview.textContent = textarea.value;
    if (count) count.textContent = String(fusions.length);
    renderCards();
  }
  function addExpertRow(expert = {}) {
    if (!expertList) return;
    const row = document.createElement("article");
    row.className = "fusion-expert-row";
    row.innerHTML = `
      <div class="fusion-expert-row-head">
        <strong>${escapeHtml(expert.name || msg.expert)}</strong>
        <button type="button" class="btn-danger" data-remove-fusion-expert>${msg.remove}</button>
      </div>
      <div class="provider-form-grid">
        <label>${msg.name}<input data-fusion-expert="name" value="${escapeAttr(expert.name || "")}" placeholder="security"></label>
        <label>${msg.target}<select data-fusion-expert="target">${buildTargetOptionsHTML(expert.target || "")}</select></label>
        <label>${msg.weight}<input data-fusion-expert="weight" type="number" min="0" value="${escapeAttr(expert.weight || "")}" placeholder="1"></label>
        <label class="checkbox fusion-toggle-card"><span><strong>${msg.enabled}</strong></span><input data-fusion-expert="enabled" type="checkbox" ${expert.enabled === false ? "" : "checked"}></label>
        <label class="wide">${msg.role}<textarea data-fusion-expert="role" rows="2" placeholder="${escapeAttr(msg.rolePlaceholder)}">${escapeHtml(expert.role || "")}</textarea></label>
        <label class="wide">${msg.prompt}<textarea data-fusion-expert="prompt" rows="3" placeholder="${escapeAttr(msg.promptPlaceholder)}">${escapeHtml(expert.prompt_template || "")}</textarea></label>
      </div>`;
    expertList.appendChild(row);
  }
  function resetEditor() {
    editingName = "";
    fields.name.value = "";
    fields.description.value = "";
    fields.mode.value = "parallel";
    fields.timeout.value = "120000";
    fields.minSuccess.value = "0";
    fields.maxOutput.value = "4096";
    fields.synthTarget.value = "";
    fields.reviewerTarget.value = "";
    fields.enabled.checked = true;
    fields.requireReviewer.checked = true;
    fields.synthPrompt.value = "";
    fields.reviewerPrompt.value = "";
    expertList.innerHTML = "";
    addExpertRow({ name: "security", role: isVI ? "Tìm rủi ro bảo mật" : "Find security risks", enabled: true });
    addExpertRow({ name: "performance", role: isVI ? "Tìm vấn đề hiệu năng" : "Find performance issues", enabled: true });
    addExpertRow({ name: "architecture", role: isVI ? "Đánh giá kiến trúc và khả năng bảo trì" : "Review architecture and maintainability", enabled: true });
  }
  function loadEditor(fusion) {
    editingName = fusion.name || "";
    fields.name.value = fusion.name || "";
    fields.description.value = fusion.description || "";
    fields.mode.value = fusion.mode || "parallel";
    fields.timeout.value = fusion.timeout_ms || 120000;
    fields.minSuccess.value = fusion.min_successful_experts || 0;
    fields.maxOutput.value = fusion.max_output_tokens || 0;
    setSelectValue(fields.synthTarget, fusion.synthesizer_target || "");
    setSelectValue(fields.reviewerTarget, fusion.reviewer_target || "");
    fields.enabled.checked = fusion.enabled !== false;
    fields.requireReviewer.checked = fusion.require_reviewer !== false;
    fields.synthPrompt.value = fusion.synthesis_prompt_template || "";
    fields.reviewerPrompt.value = fusion.reviewer_prompt_template || "";
    expertList.innerHTML = "";
    (fusion.experts || []).forEach(addExpertRow);
    if (!(fusion.experts || []).length) addExpertRow({ enabled: true });
  }
  function readEditor() {
    const experts = Array.from(expertList.querySelectorAll(".fusion-expert-row")).map((row) => cleanObject({
      name: row.querySelector('[data-fusion-expert="name"]')?.value.trim(),
      target: row.querySelector('[data-fusion-expert="target"]')?.value.trim(),
      role: row.querySelector('[data-fusion-expert="role"]')?.value.trim(),
      prompt_template: row.querySelector('[data-fusion-expert="prompt"]')?.value.trim(),
      enabled: row.querySelector('[data-fusion-expert="enabled"]')?.checked ?? true,
      weight: Number(row.querySelector('[data-fusion-expert="weight"]')?.value || 0) || undefined,
    })).filter((x) => x.target);
    return cleanObject({
      name: fields.name.value.trim(),
      description: fields.description.value.trim(),
      enabled: fields.enabled.checked,
      mode: fields.mode.value || "parallel",
      timeout_ms: Number(fields.timeout.value || 120000),
      min_successful_experts: Number(fields.minSuccess.value || 0),
      max_output_tokens: Number(fields.maxOutput.value || 0),
      synthesizer_target: fields.synthTarget.value.trim(),
      reviewer_target: fields.reviewerTarget.value.trim(),
      require_reviewer: fields.requireReviewer.checked,
      synthesis_prompt_template: fields.synthPrompt.value.trim(),
      reviewer_prompt_template: fields.reviewerPrompt.value.trim(),
      experts,
    });
  }
  function renderCards() {
    if (!grid) return;
    grid.innerHTML = fusions.length ? fusions.map((fusion) => `
      <article class="pricing-rule-card fusion-card">
        <div class="api-key-card-head">
          <div><span class="badge">${fusion.enabled === false ? msg.disabled : msg.enabledText}</span><h3>${escapeHtml(fusion.name || msg.unnamed)}</h3></div>
          <div class="inline-actions"><button type="button" class="btn-secondary" data-edit-fusion="${escapeAttr(fusion.name || "")}">${msg.edit}</button><button type="button" class="btn-danger" data-delete-fusion="${escapeAttr(fusion.name || "")}">${msg.delete}</button></div>
        </div>
        <div class="api-key-limits pricing-friendly-limits">
          <span><small>${msg.mode}</small><strong>${escapeHtml(fusion.mode || "parallel")}</strong></span>
          <span><small>${msg.experts}</small><strong>${(fusion.experts || []).length}</strong></span>
          <span><small>${msg.minSuccess}</small><strong>${fusion.min_successful_experts || 0}</strong></span>
          <span><small>${msg.synth}</small><strong>${escapeHtml(fusion.synthesizer_target || msg.none)}</strong></span>
          <span><small>${msg.reviewer}</small><strong>${escapeHtml(fusion.reviewer_target || msg.none)}</strong></span>
          <span><small>${msg.timeout}</small><strong>${fusion.timeout_ms || 120000}ms</strong></span>
        </div>
        ${fusion.description ? `<p class="muted-text">${escapeHtml(fusion.description)}</p>` : ""}
      </article>`).join("") : `<article class="api-key-empty"><h3>${msg.emptyTitle}</h3><p>${msg.emptyDesc}</p></article>`;
  }

  field("fusion-add-expert")?.addEventListener("click", (event) => { event.preventDefault(); addExpertRow({ enabled: true }); });
  field("fusion-reset-editor")?.addEventListener("click", (event) => { event.preventDefault(); resetEditor(); });
  field("fusion-save-editor")?.addEventListener("click", (event) => {
    event.preventDefault();
    const fusion = readEditor();
    if (!fusion.name) return showToast(msg.nameRequired, false);
    if (!fusion.experts?.length) return showToast(msg.expertRequired, false);
    if (!fusion.synthesizer_target) return showToast(msg.synthRequired, false);
    fusions = fusions.filter((x) => x.name !== (editingName || fusion.name));
    fusions.push(fusion);
    sync();
    loadEditor(fusion);
    showToast(msg.saved);
  });
  document.addEventListener("click", (event) => {
    const rm = event.target.closest("[data-remove-fusion-expert]");
    if (rm) { rm.closest(".fusion-expert-row")?.remove(); return; }
    const edit = event.target.closest("[data-edit-fusion]");
    if (edit) { const fusion = fusions.find((x) => x.name === edit.dataset.editFusion); if (fusion) loadEditor(fusion); return; }
    const del = event.target.closest("[data-delete-fusion]");
    if (del) { if (!confirm(`${msg.confirmDelete} ${del.dataset.deleteFusion}?`)) return; fusions = fusions.filter((x) => x.name !== del.dataset.deleteFusion); sync(); }
  });
  field("fusion-form")?.addEventListener("submit", (event) => {
    const current = readEditor();
    if (current.name || current.experts?.length || current.synthesizer_target || current.reviewer_target) {
      if (!current.name) { event.preventDefault(); return showToast(msg.nameRequired, false); }
      if (!current.experts?.length) { event.preventDefault(); return showToast(msg.expertRequired, false); }
      if (!current.synthesizer_target) { event.preventDefault(); return showToast(msg.synthRequired, false); }
      fusions = fusions.filter((x) => x.name !== (editingName || current.name));
      fusions.push(current);
    }
    sync();
  });
  sync();
  if (fusions[0]) loadEditor(fusions[0]); else resetEditor();
});

function parseFusionJSON(raw) { try { const v = JSON.parse(raw || "[]"); return Array.isArray(v) ? v : []; } catch { return []; } }
function cleanObject(obj) { Object.keys(obj).forEach((k) => { if (obj[k] === undefined || obj[k] === "") delete obj[k]; }); return obj; }
function setSelectValue(select, value) { if (!select) return; if (value && !Array.from(select.options).some((x) => x.value === value)) select.add(new Option(value, value)); select.value = value; }
function escapeHtml(value) { return String(value ?? "").replace(/[&<>'"]/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;",'"':"&quot;"}[c])); }
function escapeAttr(value) { return escapeHtml(value).replace(/"/g, "&quot;"); }
