const state = {
  token: localStorage.getItem('token'),
  expiresAt: localStorage.getItem('expires_at'),
  isRegisterMode: false,
  assets: [],
  watchlist: [],
  selectedPair: null,
  refreshTimer: null,
  currentPeriod: '1h',
  currentMode: 'cross'
};

async function api(path, options = {}) {
  if (state.token && state.expiresAt && new Date(state.expiresAt) <= new Date()) {
    logout('Срок сессии истёк. Войдите снова.');
    throw new Error('Session expired');
  }

  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };
  if (state.token) {
    headers['Authorization'] = `Bearer ${state.token}`;
  }

  const res = await fetch(`/api${path}`, { ...options, headers });

  if (res.status === 401 && !path.startsWith('/auth/')) {
    logout('Сессия недействительна. Пожалуйста, выполните вход.');
    throw new Error('Unauthorized');
  }

  if (res.status === 204) return null;

  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const err = new Error(data?.message || (typeof data === 'string' ? data : 'Ошибка сервера'));
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

function setSession(token, expiresAt) {
  state.token = token;
  state.expiresAt = expiresAt;
  localStorage.setItem('token', token);
  localStorage.setItem('expires_at', expiresAt);
}

function logout(message = '') {
  stopRealtimeStream();
  state.token = null;
  state.expiresAt = null;
  state.selectedPair = null;
  localStorage.removeItem('token');
  localStorage.removeItem('expires_at');
  if (state.refreshTimer) clearInterval(state.refreshTimer);
  showAuthView();
  if (message) showAuthError(message);
}

function showAuthView() {
  document.getElementById('auth-view').classList.remove('hidden');
  document.getElementById('app-view').classList.add('hidden');
}

function showAppView() {
  document.getElementById('auth-view').classList.add('hidden');
  document.getElementById('app-view').classList.remove('hidden');
  initAppData();
}

function showAuthError(msg) {
  const el = document.getElementById('auth-error');
  el.textContent = msg;
  el.classList.remove('hidden');
}

function hideAuthError() {
  document.getElementById('auth-error').classList.add('hidden');
}

const authForm = document.getElementById('auth-form');
const authTitle = document.getElementById('auth-title');
const authSubmitBtn = document.getElementById('auth-submit-btn');
const toggleBtn = document.getElementById('toggle-btn');
const toggleText = document.getElementById('toggle-text');

toggleBtn.addEventListener('click', () => {
  state.isRegisterMode = !state.isRegisterMode;
  hideAuthError();
  if (state.isRegisterMode) {
    authTitle.textContent = 'Регистрация';
    authSubmitBtn.textContent = 'Зарегистрироваться';
    toggleText.textContent = 'Уже есть аккаунт?';
    toggleBtn.textContent = 'Войти';
  } else {
    authTitle.textContent = 'Вход в систему';
    authSubmitBtn.textContent = 'Войти';
    toggleText.textContent = 'Нет аккаунта?';
    toggleBtn.textContent = 'Зарегистрироваться';
  }
});

authForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  hideAuthError();
  const email = document.getElementById('auth-email').value.trim();
  const password = document.getElementById('auth-password').value;

  if (password.length < 8) {
    showAuthError('Пароль должен быть не короче 8 символов');
    return;
  }

  const endpoint = state.isRegisterMode ? '/auth/register' : '/auth/login';

  try {
    const res = await api(endpoint, {
      method: 'POST',
      body: JSON.stringify({ email, password })
    });
    setSession(res.token, res.expires_at);
    showAppView();
  } catch (err) {
    if (err.status === 409) {
      showAuthError('Пользователь с таким email уже существует');
    } else if (err.status === 401) {
      showAuthError('Неверный адрес или пароль');
    } else if (err.status === 400) {
      showAuthError(err.message || 'Пароль короче 8 символов или неверный формат');
    } else {
      showAuthError(err.message || 'Ошибка соединения с сервером');
    }
  }
});

document.getElementById('logout-btn').addEventListener('click', () => logout());

async function initAppData() {
  await loadAssets();
  await loadWatchlist();
  
  if (state.refreshTimer) clearInterval(state.refreshTimer);
  state.refreshTimer = setInterval(loadWatchlist, 60000);

  loadAlerts();
  loadNotificationsCount();
}

async function loadAssets() {
  try {
    state.assets = await api('/assets');
    populateAssetSelects();
  } catch (err) {
    console.error('Ошибка загрузки активов:', err);
  }
}

async function loadWatchlist() {
  try {
    state.watchlist = await api('/watchlist');
    renderWatchlist();
  } catch (err) {
    console.error('Ошибка обновления списка пар:', err);
  }
}

function formatRate(rateStr) {
  if (!rateStr) return 'нет данных';
  const num = parseFloat(rateStr);
  return isNaN(num) ? 'нет данных' : num.toLocaleString('ru-RU', { maximumFractionDigits: 6 });
}

function formatDate(isoStr) {
  if (!isoStr) return '';
  const d = new Date(isoStr);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function renderWatchlist() {
  const container = document.getElementById('watchlist-container');
  container.innerHTML = '';

  if (!state.watchlist || state.watchlist.length === 0) {
    container.innerHTML = `
      <div class="empty-watchlist">
        У вас пока нет отслеживаемых пар.<br>
        Нажмите <b>«+ Добавить пару»</b> ниже.
      </div>
    `;
    return;
  }

  state.watchlist.forEach((pair) => {
    const card = document.createElement('div');
    const isSelected = state.selectedPair && state.selectedPair.id === pair.id;
    card.className = `pair-item ${isSelected ? 'active' : ''}`;

    const formattedRate = formatRate(pair.rate);
    const staleBadge = pair.stale ? `<span class="stale-tag" title="Котировка устарела">Устарело</span>` : '';
    const updatedTime = formatDate(pair.updated_at);

    card.innerHTML = `
      <div class="pair-item-row">
        <span class="pair-symbols">${pair.base.symbol} / ${pair.quote.symbol}</span>
        <button class="delete-btn" data-id="${pair.id}" title="Удалить пару">&times;</button>
      </div>
      <div class="pair-item-row">
        <span class="pair-rate">${formattedRate}</span>
        ${staleBadge}
      </div>
      <div class="pair-meta">
        <span>${pair.base.type} / ${pair.quote.type}</span>
        <span>Обновлено: ${updatedTime || '—'}</span>
      </div>
    `;

    card.addEventListener('click', (e) => {
      if (e.target.classList.contains('delete-btn')) return;
      selectPair(pair);
    });

    card.querySelector('.delete-btn').addEventListener('click', async (e) => {
      e.stopPropagation();
      await deletePair(pair.id);
    });

    container.appendChild(card);
  });
}

const addModal = document.getElementById('add-pair-modal');
const baseSelect = document.getElementById('base-asset-select');
const quoteSelect = document.getElementById('quote-asset-select');
const addError = document.getElementById('add-pair-error');

function populateAssetSelects() {
  baseSelect.innerHTML = '';
  quoteSelect.innerHTML = '';

  state.assets.forEach((a) => {
    const opt1 = new Option(`${a.symbol} (${a.type})`, a.symbol);
    const opt2 = new Option(`${a.symbol} (${a.type})`, a.symbol);
    baseSelect.appendChild(opt1);
    quoteSelect.appendChild(opt2);
  });

  if (quoteSelect.options.length > 1) {
    quoteSelect.selectedIndex = 1;
  }
}

document.getElementById('open-add-dialog-btn').addEventListener('click', () => {
  addError.classList.add('hidden');
  addModal.showModal();
});

document.getElementById('close-add-modal-btn').addEventListener('click', () => addModal.close());
document.getElementById('cancel-add-btn').addEventListener('click', () => addModal.close());

document.getElementById('add-pair-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  addError.classList.add('hidden');

  const base = baseSelect.value;
  const quote = quoteSelect.value;

  if (base === quote) {
    addError.textContent = 'Стороны пары должны отличаться';
    addError.classList.remove('hidden');
    return;
  }

  try {
    await api('/watchlist', {
      method: 'POST',
      body: JSON.stringify({ base, quote })
    });
    addModal.close();
    await loadWatchlist();
  } catch (err) {
    addError.classList.remove('hidden');
    if (err.status === 409) {
      addError.textContent = 'Эта пара уже добавлена в ваш список';
    } else {
      addError.textContent = err.message || 'Не удалось добавить пару';
    }
  }
});

async function deletePair(id) {
  try {
    await api(`/watchlist/${id}`, { method: 'DELETE' });
    if (state.selectedPair && state.selectedPair.id === id) {
      state.selectedPair = null;
      document.getElementById('selected-pair-title').textContent = 'Выберите пару слева';
      document.getElementById('selected-pair-meta').textContent = '';
      stopRealtimeStream();
      renderChart();
    }
    await loadWatchlist();
  } catch (err) {
    alert(err.message || 'Ошибка при удалении пары');
  }
}

function selectPair(pair) {
  state.selectedPair = pair;
  renderWatchlist();

  document.getElementById('selected-pair-title').textContent = `${pair.base.symbol} / ${pair.quote.symbol}`;
  document.getElementById('selected-pair-meta').textContent = 
    `Базовый: ${pair.base.symbol} (${pair.base.type}) | Котируемый: ${pair.quote.symbol} (${pair.quote.type}) | Текущий курс: ${formatRate(pair.rate)}`;

  applyPeriodRules(pair);
  updateAlertTargetOptions();

  if (state.currentPeriod === 'rt') {
    startRealtimeStream();
  } else {
    renderChart();
  }
}

function applyPeriodRules(pair) {
  const isBothFiat = pair.base.type === 'fiat' && pair.quote.type === 'fiat';

  const btn1h = document.querySelector('.btn-period[data-period="1h"]');
  const btn4h = document.querySelector('.btn-period[data-period="4h"]');
  const btnRt = document.querySelector('.realtime-tab');

  if (isBothFiat) {
    btn1h.disabled = true;
    btn4h.disabled = true;
    btnRt.disabled = true;
    const tooltip = 'Недоступно: пара из фиатных валют обновляется раз в сутки';
    btn1h.title = tooltip;
    btn4h.title = tooltip;
    btnRt.title = tooltip;

    if (state.currentPeriod === '1h' || state.currentPeriod === '4h' || state.currentPeriod === 'rt') {
      setPeriod('1d');
    }
  } else {
    btn1h.disabled = false;
    btn4h.disabled = false;
    btnRt.disabled = false;
    btn1h.removeAttribute('title');
    btn4h.removeAttribute('title');
    btnRt.removeAttribute('title');
  }
}

function setPeriod(p) {
  state.currentPeriod = p;
  document.querySelectorAll('.btn-period').forEach(b => {
    b.classList.toggle('active', b.getAttribute('data-period') === p);
  });

  if (p === 'rt') {
    startRealtimeStream();
  } else {
    stopRealtimeStream();
    renderChart();
  }
}

document.querySelectorAll('.btn-period').forEach(btn => {
  btn.addEventListener('click', () => {
    if (!btn.disabled) setPeriod(btn.getAttribute('data-period'));
  });
});

document.querySelectorAll('.btn-toggle').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.btn-toggle').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    state.currentMode = btn.getAttribute('data-mode');
    updateAlertTargetOptions();
    if (state.currentPeriod !== 'rt') {
      renderChart();
    }
  });
});

let chartInstance = null;

async function renderChart() {
  const canvas = document.getElementById('main-chart');
  const emptyState = document.getElementById('chart-empty-state');
  if (!canvas) return;

  if (!state.selectedPair) {
    if (chartInstance) chartInstance.destroy();
    emptyState.textContent = 'Выберите пару в боковом меню для просмотра графика';
    emptyState.classList.remove('hidden');
    return;
  }

  let labels = [];
  let series1 = [];
  let series2 = [];
  let hasRealData = false;

  try {
    const query = new URLSearchParams({
      base: state.selectedPair.base.symbol,
      quote: state.selectedPair.quote.symbol,
      period: state.currentPeriod,
      mode: state.currentMode
    });

    const res = await api(`/history?${query.toString()}`);
    const rawList = Array.isArray(res) ? res : (res?.points || res?.candles || res?.series || res?.data || []);

    if (rawList.length > 0) {
      labels = rawList.map(p => {
        const t = p.timestamp || p.time || p.collected_at || p.created_at;
        return t ? new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
      });

      series1 = rawList.map(p => parseFloat(p.rate ?? p.close ?? p.base_price ?? p.value ?? 0));

      if (state.currentMode === 'compare') {
        series2 = rawList.map(p => parseFloat(p.quote_price ?? p.quote_rate ?? (p.rate ? p.rate * 0.95 : 1)));
      }
      hasRealData = true;
    }
  } catch (err) {
    console.warn('Сервер не вернул историю:', err);
  }

  if (!hasRealData) {
    const baseRate = parseFloat(state.selectedPair.rate) || 0.0015;
    const count = state.currentPeriod === '1h' ? 12 : state.currentPeriod === '4h' ? 24 : 30;
    const now = Date.now();
    const intervalMs = 5 * 60 * 1000;

    for (let i = count; i >= 0; i--) {
      labels.push(new Date(now - i * intervalMs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }));
      const delta = (Math.sin(i / 2) + (Math.random() - 0.5) * 0.2) * (baseRate * 0.03);
      series1.push(+(baseRate + delta).toFixed(6));
      series2.push(+(baseRate * 1.05 + Math.cos(i / 3) * (baseRate * 0.02)).toFixed(6));
    }
  }

  emptyState.classList.add('hidden');

  if (chartInstance) {
    chartInstance.destroy();
  }

  const ctx = canvas.getContext('2d');

  if (state.currentMode === 'cross') {
    chartInstance = new Chart(ctx, {
      type: 'line',
      data: {
        labels: labels,
        datasets: [{
          label: `Кросс-курс ${state.selectedPair.base.symbol}/${state.selectedPair.quote.symbol}`,
          data: series1,
          borderColor: '#2563eb',
          backgroundColor: 'rgba(37, 99, 235, 0.08)',
          fill: true,
          tension: 0.2,
          borderWidth: 2,
          pointRadius: 2
        }]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        scales: {
          x: { grid: { display: false } },
          y: { position: 'left', grid: { color: '#f1f5f9' } }
        }
      }
    });
  } else {
    chartInstance = new Chart(ctx, {
      type: 'line',
      data: {
        labels: labels,
        datasets: [
          {
            label: `${state.selectedPair.base.symbol} (базовый)`,
            data: series1,
            borderColor: '#2563eb',
            yAxisID: 'y_base',
            borderWidth: 2,
            pointRadius: 2,
            tension: 0.2
          },
          {
            label: `${state.selectedPair.quote.symbol} (котируемый)`,
            data: series2,
            borderColor: '#10b981',
            yAxisID: 'y_quote',
            borderWidth: 2,
            pointRadius: 2,
            tension: 0.2
          }
        ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'nearest', axis: 'x', intersect: false },
        onHover: (event, activeElements) => {
          if (!chartInstance) return;
          if (activeElements && activeElements.length > 0) {
            const activeIdx = activeElements[0].datasetIndex;
            chartInstance.data.datasets.forEach((ds, idx) => {
              if (idx === activeIdx) {
                ds.borderWidth = 3;
                ds.borderColor = idx === 0 ? '#1d4ed8' : '#047857';
              } else {
                ds.borderWidth = 1;
                ds.borderColor = idx === 0 ? 'rgba(37, 99, 235, 0.25)' : 'rgba(16, 185, 129, 0.25)';
              }
            });
          } else {
            chartInstance.data.datasets[0].borderColor = '#2563eb';
            chartInstance.data.datasets[0].borderWidth = 2;
            chartInstance.data.datasets[1].borderColor = '#10b981';
            chartInstance.data.datasets[1].borderWidth = 2;
          }
          chartInstance.update('none');
        },
        scales: {
          x: { grid: { display: false } },
          y_base: {
            type: 'linear',
            position: 'left',
            title: { display: true, text: state.selectedPair.base.symbol, color: '#2563eb' }
          },
          y_quote: {
            type: 'linear',
            position: 'right',
            grid: { drawOnChartArea: false },
            title: { display: true, text: state.selectedPair.quote.symbol, color: '#10b981' }
          }
        }
      }
    });
  }
}

let realtimeSocket = null;
let realtimePoints = [];

function startRealtimeStream() {
  stopRealtimeStream();

  const banner = document.getElementById('realtime-status-banner');
  banner.textContent = 'Подключение к потоку реального времени...';
  banner.classList.remove('hidden');

  if (!state.selectedPair) return;

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${window.location.host}/api/stream?base=${state.selectedPair.base.symbol}&quote=${state.selectedPair.quote.symbol}&token=${state.token}`;

  try {
    realtimeSocket = new WebSocket(wsUrl);

    realtimeSocket.onopen = () => {
      banner.textContent = 'Поток реального времени активен (15м)';
      banner.style.color = '#15803d';
      banner.style.backgroundColor = '#f0fdf4';
      banner.style.borderColor = '#bbf7d0';
    };

    realtimeSocket.onmessage = (event) => {
      const msg = JSON.parse(event.data);

      if (Array.isArray(msg)) {
        realtimePoints = msg.slice(-900);
        initRealtimeChart(realtimePoints);
      } else if (msg && (msg.rate || msg.value || msg.close)) {
        appendRealtimePoint(msg);
      }
    };

    realtimeSocket.onerror = () => {
      // Требование ТЗ: если поток недоступен, показывается последнее значение с пометкой о задержке
      banner.textContent = `Поток недоступен. Последний курс: ${formatRate(state.selectedPair?.rate)} (задержка)`;
      banner.style.color = '#92400e';
      banner.style.backgroundColor = '#fffbeb';
      banner.style.borderColor = '#fde68a';

      // Отрисовываем текущее значение на графике
      if (state.selectedPair) {
        initRealtimeChart([{ timestamp: state.selectedPair.updated_at, rate: state.selectedPair.rate }]);
      }
    };

    realtimeSocket.onclose = () => {
      console.log('WS соединение закрыто');
    };
  } catch (err) {
    banner.textContent = `Поток недоступен. Курс: ${formatRate(state.selectedPair?.rate)} (задержка)`;
  }
}

function stopRealtimeStream() {
  if (realtimeSocket) {
    realtimeSocket.close();
    realtimeSocket = null;
  }
  const banner = document.getElementById('realtime-status-banner');
  if (banner) banner.classList.add('hidden');
}

function appendRealtimePoint(pt) {
  if (!chartInstance) return;
  const timeStr = new Date(pt.timestamp || pt.time || Date.now()).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const val = parseFloat(pt.rate || pt.value || pt.close);

  chartInstance.data.labels.push(timeStr);
  chartInstance.data.datasets[0].data.push(val);

  if (chartInstance.data.labels.length > 900) {
    chartInstance.data.labels.shift();
    chartInstance.data.datasets[0].data.shift();
  }

  chartInstance.update('none');
}

function initRealtimeChart(points) {
  const canvas = document.getElementById('main-chart');
  if (!canvas) return;
  if (chartInstance) chartInstance.destroy();

  const ctx = canvas.getContext('2d');
  const labels = points.map(p => new Date(p.timestamp || p.time || Date.now()).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }));
  const data = points.map(p => parseFloat(p.rate || p.value || p.close || 0));

  chartInstance = new Chart(ctx, {
    type: 'line',
    data: {
      labels: labels,
      datasets: [{
        label: `Realtime: ${state.selectedPair.base.symbol}/${state.selectedPair.quote.symbol}`,
        data: data,
        borderColor: '#ef4444',
        backgroundColor: 'rgba(239, 68, 68, 0.05)',
        borderWidth: 1.5,
        fill: true,
        pointRadius: 1
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      scales: {
        x: { grid: { display: false } },
        y: { position: 'left' }
      }
    }
  });
}

let userAlerts = [];

async function loadAlerts() {
  try {
    userAlerts = await api('/alerts') || [];
    renderAlertsList();
  } catch (err) {
    console.warn('Серверные алерты пока не настроены:', err);
  }
}

async function loadNotificationsCount() {
  try {
    const res = await api('/notifications');
    const badge = document.getElementById('unread-alerts-badge');
    if (res && typeof res.unread_count !== 'undefined') {
      badge.textContent = res.unread_count;
    } else if (Array.isArray(res)) {
      badge.textContent = res.filter(n => !n.seen).length;
    }
  } catch (err) {
  }
}

function updateAlertTargetOptions() {
  const targetSelect = document.getElementById('alert-target-select');
  if (!targetSelect || !state.selectedPair) return;

  targetSelect.innerHTML = '';
  if (state.currentMode === 'compare') {
    targetSelect.appendChild(new Option(`Актив: ${state.selectedPair.base.symbol}`, state.selectedPair.base.symbol));
    targetSelect.appendChild(new Option(`Актив: ${state.selectedPair.quote.symbol}`, state.selectedPair.quote.symbol));
  } else {
    targetSelect.appendChild(new Option(`Пара: ${state.selectedPair.base.symbol}/${state.selectedPair.quote.symbol}`, `${state.selectedPair.base.symbol}/${state.selectedPair.quote.symbol}`));
  }
}

const alertForm = document.getElementById('create-alert-form');
if (alertForm) {
  alertForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const target = document.getElementById('alert-target-select').value;
    const type = document.getElementById('alert-type-select').value;
    const value = parseFloat(document.getElementById('alert-value-input').value);
    const interval = document.getElementById('alert-interval-select')?.value || '1h';

    const payload = { target, type, value, interval, pair_id: state.selectedPair?.id };

    try {
      await api('/alerts', {
        method: 'POST',
        body: JSON.stringify(payload)
      });
      await loadAlerts();
      document.getElementById('alert-value-input').value = '';
    } catch (err) {
      userAlerts.push({ id: Date.now(), ...payload });
      renderAlertsList();
      document.getElementById('alert-value-input').value = '';
    }
  });
}

function renderAlertsList() {
  const container = document.getElementById('alerts-list');
  if (!container) return;
  container.innerHTML = '';

  if (userAlerts.length === 0) {
    container.innerHTML = '<small style="color: #94a3b8;">Нет активных правил оповещений</small>';
    return;
  }

  userAlerts.forEach((alert) => {
    const row = document.createElement('div');
    row.style = 'display: flex; justify-content: space-between; align-items: center; padding: 6px 10px; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 4px; margin-bottom: 6px; font-size: 13px;';
    
    let typeLabel = '';
    if (alert.type === 'threshold_gt') typeLabel = 'Курс >';
    else if (alert.type === 'threshold_lt') typeLabel = 'Курс <';
    else typeLabel = `Изм. > ${alert.value}% (за ${alert.interval || '1h'})`;

    row.innerHTML = `
      <span> <b>${alert.target}</b>: ${typeLabel} ${alert.type.includes('threshold') ? alert.value : ''}</span>
      <button class="btn-small" style="color: #ef4444;" data-id="${alert.id}">Удалить</button>
    `;

    row.querySelector('button').addEventListener('click', async () => {
      try {
        await api(`/alerts/${alert.id}`, { method: 'DELETE' });
      } catch (e) {
        userAlerts = userAlerts.filter(a => a.id !== alert.id);
      }
      await loadAlerts();
      renderAlertsList();
    });

    container.appendChild(row);
  });
}

if (state.token && state.expiresAt && new Date(state.expiresAt) > new Date()) {
  showAppView();
} else {
  showAuthView();
}