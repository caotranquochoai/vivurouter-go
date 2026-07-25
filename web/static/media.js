(() => {
  const root = document.querySelector("[data-media-studio]");
  if (!root) return;
  const form = root.querySelector("[data-media-form]");
  const tabs = [...root.querySelectorAll("[data-media-tab]")];
  const panels = [...root.querySelectorAll("[data-media-panel]")];
  const provider = form.elements.provider;
  const modelSelect = form.elements.model_select;
  const modelInput = form.elements.model;
  const runButton = root.querySelector("[data-media-run]");
  const cancelButton = root.querySelector("[data-media-cancel]");
  const statusBox = root.querySelector("[data-media-status]");
  const emptyBox = root.querySelector("[data-media-empty]");
  const errorBox = root.querySelector("[data-media-error]");
  const output = root.querySelector("[data-media-output]");
  const image = root.querySelector("[data-media-image]");
  const audio = root.querySelector("[data-media-audio]");
  const text = root.querySelector("[data-media-text]");
  const meta = root.querySelector("[data-media-meta]");
  const download = root.querySelector("[data-media-download]");
  const inputAudio = root.querySelector("[data-media-input-audio]");
  let kind = "image", controller = null, resultURL = "", inputURLs = [];
  const vi = root.dataset.lang === "vi";

  const revoke = (url) => { if (url) try { URL.revokeObjectURL(url); } catch {} };
  const clearResult = () => {
    revoke(resultURL); resultURL = ""; image.hidden = true; image.removeAttribute("src"); audio.hidden = true; audio.removeAttribute("src"); audio.load(); text.hidden = true; text.textContent = ""; meta.textContent = ""; output.hidden = true; errorBox.hidden = true; errorBox.textContent = ""; download.hidden = true; download.onclick = null; emptyBox.hidden = false;
  };
  const resetInputURLs = () => { inputURLs.forEach(revoke); inputURLs = []; };
  const setBusy = (busy) => { runButton.disabled = busy; cancelButton.disabled = !busy; tabs.forEach((tab) => { if (!tab.disabled) tab.disabled = busy; }); };
  const setError = (message) => { emptyBox.hidden = true; output.hidden = true; errorBox.textContent = message; errorBox.hidden = false; statusBox.textContent = vi ? "Thất bại" : "Failed"; };
  const setDownload = (url, filename) => { download.hidden = false; download.onclick = () => { const a = document.createElement("a"); a.href = url; a.download = filename; a.click(); }; };

  tabs.forEach((tab) => tab.addEventListener("click", () => {
    if (tab.disabled) return;
    kind = tab.dataset.mediaTab;
    tabs.forEach((item) => { const active = item === tab; item.classList.toggle("is-active", active); item.setAttribute("aria-selected", active ? "true" : "false"); });
    panels.forEach((panel) => { panel.hidden = panel.dataset.mediaPanel !== kind; });
    clearResult(); statusBox.textContent = vi ? "Sẵn sàng" : "Ready"; filterModels();
  }));

  const filterModels = () => {
    const providerID = provider.value;
    const selectedProvider = provider.selectedOptions?.[0];
    const codex = selectedProvider?.dataset.providerType === "codex";
    [...modelSelect.options].forEach((option, index) => {
      if (!index) return;
      const operationOK = option.dataset.operation === "all" || ((kind === "image" || kind === "image-edit") && option.dataset.operation === "image");
      option.hidden = option.dataset.provider !== providerID || !operationOK;
    });
    modelSelect.value = "";
    if (codex && kind !== "image" && kind !== "image-edit") {
      modelInput.value = "";
      statusBox.textContent = vi ? "Codex OAuth chỉ hỗ trợ tạo/sửa ảnh" : "Codex OAuth supports image operations only";
    }
  };
  provider.addEventListener("change", filterModels);
  modelSelect.addEventListener("change", () => { if (modelSelect.value) modelInput.value = modelSelect.value; });

  const previewFile = (input, target) => input.addEventListener("change", () => {
    const file = input.files?.[0]; if (!file) { target.hidden = true; return; }
    const url = URL.createObjectURL(file); inputURLs.push(url); target.src = url; target.hidden = false;
  });
  previewFile(form.elements.image_file, root.querySelector('[data-media-input-preview="image"]'));
  previewFile(form.elements.mask_file, root.querySelector('[data-media-input-preview="mask"]'));
  form.elements.audio_file.addEventListener("change", () => {
    const file = form.elements.audio_file.files?.[0]; if (!file) { inputAudio.hidden = true; return; }
    const url = URL.createObjectURL(file); inputURLs.push(url); inputAudio.src = url; inputAudio.hidden = false;
  });

  const buildRequest = () => {
    const model = modelInput.value.trim();
    if (!provider.value || !model) throw new Error(vi ? "Hãy chọn provider và model." : "Select a provider and model.");
    if (kind === "image") {
      const prompt = form.elements.image_prompt.value.trim(); if (!prompt) throw new Error(vi ? "Hãy nhập mô tả ảnh." : "Enter an image prompt.");
      return { body: JSON.stringify({ model, prompt, size: form.elements.image_size.value, quality: form.elements.image_quality.value, output_format: form.elements.image_format.value }), headers: { "Content-Type": "application/json" } };
    }
    if (kind === "tts") {
      const input = form.elements.tts_input.value.trim(); if (!input) throw new Error(vi ? "Hãy nhập nội dung đọc." : "Enter speech input.");
      return { body: JSON.stringify({ model, input, voice: form.elements.tts_voice.value.trim() || "alloy", response_format: form.elements.tts_format.value, speed: Number(form.elements.tts_speed.value) || 1 }), headers: { "Content-Type": "application/json" } };
    }
    const fd = new FormData(); fd.append("model", model);
    if (kind === "image-edit") {
      const source = form.elements.image_file.files?.[0], prompt = form.elements.edit_prompt.value.trim();
      if (!source || !prompt) throw new Error(vi ? "Chọn ảnh nguồn và nhập yêu cầu chỉnh sửa." : "Choose a source image and enter an edit prompt.");
      fd.append("image", source, source.name); const mask = form.elements.mask_file.files?.[0]; if (mask) fd.append("mask", mask, mask.name); fd.append("prompt", prompt);
    } else {
      const file = form.elements.audio_file.files?.[0]; if (!file) throw new Error(vi ? "Hãy chọn tệp âm thanh." : "Choose an audio file.");
      fd.append("file", file, file.name); const language = form.elements.stt_language.value.trim(), prompt = form.elements.stt_prompt.value.trim(); if (language) fd.append("language", language); if (prompt) fd.append("prompt", prompt); fd.append("response_format", form.elements.stt_format.value);
    }
    return { body: fd, headers: {} };
  };

  const renderJSON = (data, elapsed) => {
    const first = Array.isArray(data?.data) ? data.data[0] : null;
    const source = first?.url || (first?.b64_json ? `data:image/png;base64,${first.b64_json}` : "");
    if ((kind === "image" || kind === "image-edit") && source) { image.src = source; image.hidden = false; setDownload(source, `vivurouter-${Date.now()}.png`); }
    else { text.textContent = typeof data?.text === "string" ? data.text : JSON.stringify(data, (key, value) => key === "b64_json" && typeof value === "string" ? `<${value.length} base64 chars>` : value, 2); text.hidden = false; }
    output.hidden = false; emptyBox.hidden = true; meta.textContent = `${elapsed} ms · JSON`; statusBox.textContent = vi ? "Hoàn tất" : "Completed";
  };

  form.addEventListener("submit", async (event) => {
    event.preventDefault(); clearResult(); let request;
    try { request = buildRequest(); } catch (err) { setError(err.message); return; }
    controller = new AbortController(); setBusy(true); const started = performance.now(); statusBox.textContent = vi ? "Đang xử lý…" : "Processing…";
    try {
      const response = await fetch(`/api/media/run/${kind}`, { method: "POST", headers: { ...request.headers, "X-VivuRouter-Provider": provider.value }, body: request.body, signal: controller.signal });
      if (!response.ok) { const data = await response.json().catch(() => ({})); throw new Error(data.error || data.message || `HTTP ${response.status}`); }
      const contentType = response.headers.get("content-type") || ""; const elapsed = Math.round(performance.now() - started);
      if (contentType.includes("application/json")) renderJSON(await response.json(), elapsed);
      else if (contentType.startsWith("audio/")) { const blob = await response.blob(); resultURL = URL.createObjectURL(blob); audio.src = resultURL; audio.hidden = false; output.hidden = false; emptyBox.hidden = true; meta.textContent = `${elapsed} ms · ${contentType} · ${blob.size.toLocaleString()} bytes`; setDownload(resultURL, `vivurouter-audio.${form.elements.tts_format.value || "mp3"}`); statusBox.textContent = vi ? "Hoàn tất" : "Completed"; }
      else { const value = await response.text(); text.textContent = value; text.hidden = false; output.hidden = false; emptyBox.hidden = true; meta.textContent = `${elapsed} ms · ${contentType || "text"}`; statusBox.textContent = vi ? "Hoàn tất" : "Completed"; }
    } catch (err) { if (err.name === "AbortError") { setError(vi ? "Đã hủy yêu cầu." : "Request cancelled."); } else setError(err.message || "Request failed"); }
    finally { controller = null; setBusy(false); }
  });
  cancelButton.addEventListener("click", () => controller?.abort());
  window.addEventListener("beforeunload", () => { controller?.abort(); revoke(resultURL); resetInputURLs(); });
  filterModels();
})();
