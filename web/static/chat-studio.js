(() => {
  const root = document.querySelector('[data-chat-studio]');
  if (!root) return;

  const lang = root.dataset.lang || 'vi';
  const text = lang === 'vi' ? {
    model: 'Vui lòng chọn model.', empty: 'Vui lòng nhập nội dung.', thinking: 'Đang tạo câu trả lời…',
    ready: 'Sẵn sàng', stopped: 'Đã dừng', error: 'Không thể hoàn tất yêu cầu.', user: 'Bạn', assistant: 'Trợ lý'
  } : {
    model: 'Please select a model.', empty: 'Please enter a message.', thinking: 'Generating response…',
    ready: 'Ready', stopped: 'Stopped', error: 'Unable to complete the request.', user: 'You', assistant: 'Assistant'
  };
  const $ = selector => root.querySelector(selector);
  const model = $('[data-chat-model]');
  const modelSearch = $('[data-chat-model-search]');
  const system = $('[data-chat-system]');
  const temperature = $('[data-chat-temperature]');
  const maxTokens = $('[data-chat-max-tokens]');
  const disableFallback = $('[data-chat-disable-fallback]');
  const messagesEl = $('[data-chat-messages]');
  const emptyEl = $('[data-chat-empty]');
  const form = $('[data-chat-form]');
  const input = $('[data-chat-input]');
  const send = $('[data-chat-send]');
  const stop = $('[data-chat-stop]');
  const status = $('[data-chat-status]');
  const newChat = $('[data-chat-new]');
  let messages = [];
  let controller = null;

  function filterModels() {
    const query = (modelSearch?.value || '').trim().toLowerCase();
    model?.querySelectorAll('option').forEach(option => {
      if (!option.value) {
        option.hidden = false;
        return;
      }
      option.hidden = Boolean(query) && !option.textContent.toLowerCase().includes(query);
    });
    model?.querySelectorAll('optgroup').forEach(group => {
      group.hidden = Boolean(query) && !Array.from(group.options).some(option => !option.hidden);
    });
    if (model?.selectedOptions[0]?.hidden) model.value = '';
  }

  modelSearch?.addEventListener('input', filterModels);

  function addMessage(role, content = '') {
    emptyEl.hidden = true;
    const article = document.createElement('article');
    article.className = `chat-message is-${role}`;
    const label = document.createElement('strong');
    label.textContent = role === 'user' ? text.user : text.assistant;
    const body = document.createElement('div');
    body.className = 'chat-message-content';
    body.textContent = content;
    article.append(label, body);
    messagesEl.appendChild(article);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return body;
  }

  function setBusy(busy) {
    send.disabled = busy;
    model.disabled = busy;
    if (disableFallback) disableFallback.disabled = busy;
    stop.disabled = !busy;
    input.disabled = busy;
  }

  function errorMessage(payload, fallback) {
    if (payload && typeof payload === 'object') {
      return payload.error?.message || payload.error || payload.message || fallback;
    }
    return fallback;
  }

  function consumeEvent(raw, assistantBody, state) {
    const dataLines = raw.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trim());
    if (!dataLines.length) return;
    const data = dataLines.join('\n');
    if (data === '[DONE]') return;
    let payload;
    try { payload = JSON.parse(data); } catch { return; }
    if (payload.error) throw new Error(errorMessage(payload, text.error));
    const delta = payload.choices?.[0]?.delta?.content;
    if (typeof delta === 'string') {
      state.content += delta;
      assistantBody.textContent = state.content;
      messagesEl.scrollTop = messagesEl.scrollHeight;
    }
  }

  async function readStream(response, assistantBody) {
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const state = { content: '' };
    let buffer = '';
    while (true) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done }).replace(/\r\n/g, '\n');
      const events = buffer.split('\n\n');
      buffer = events.pop() || '';
      for (const event of events) consumeEvent(event, assistantBody, state);
      if (done) break;
    }
    if (buffer.trim()) consumeEvent(buffer, assistantBody, state);
    return state.content;
  }

  form.addEventListener('submit', async event => {
    event.preventDefault();
    const content = input.value.trim();
    if (!model.value) { status.textContent = text.model; model.focus(); return; }
    if (!content) { status.textContent = text.empty; input.focus(); return; }

    messages.push({ role: 'user', content });
    addMessage('user', content);
    input.value = '';
    const assistantBody = addMessage('assistant');
    controller = new AbortController();
    setBusy(true);
    status.textContent = text.thinking;

    const requestMessages = [];
    if (system.value.trim()) requestMessages.push({ role: 'system', content: system.value.trim() });
    requestMessages.push(...messages);
    const payload = {
      model: model.value,
      messages: requestMessages,
      stream: true,
      temperature: Number(temperature.value || 0.7),
      max_tokens: Math.max(1, Number(maxTokens.value || 2048)),
      vivurouter_disable_fallback: Boolean(disableFallback?.checked)
    };

    try {
      const response = await fetch('/api/chat/completions', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload), signal: controller.signal
      });
      if (!response.ok) {
        let detail;
        try { detail = await response.json(); } catch { detail = null; }
        throw new Error(errorMessage(detail, `${text.error} HTTP ${response.status}`));
      }
      const answer = await readStream(response, assistantBody);
      if (answer) messages.push({ role: 'assistant', content: answer });
      else if (!assistantBody.textContent) assistantBody.textContent = text.error;
      status.textContent = text.ready;
    } catch (error) {
      if (error.name === 'AbortError') {
        status.textContent = text.stopped;
        if (!assistantBody.textContent) assistantBody.closest('article').remove();
        else messages.push({ role: 'assistant', content: assistantBody.textContent });
      } else {
        assistantBody.textContent = error.message || text.error;
        assistantBody.closest('article').classList.add('is-error');
        status.textContent = text.error;
      }
    } finally {
      controller = null;
      setBusy(false);
      input.focus();
    }
  });

  stop.addEventListener('click', () => controller?.abort());
  newChat.addEventListener('click', () => {
    controller?.abort();
    messages = [];
    messagesEl.querySelectorAll('.chat-message').forEach(node => node.remove());
    emptyEl.hidden = false;
    status.textContent = text.ready;
    input.focus();
  });
  input.addEventListener('keydown', event => {
    if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); form.requestSubmit(); }
  });
})();
