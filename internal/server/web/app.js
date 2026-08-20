(() => {
  "use strict";

  const API_URL = "/api/v1/monitors";
  const EVENTS_URL = "/api/v1/events";

  const state = {
    monitors: new Map(),
    filter: "all",
    query: "",
    loading: true,
    refreshing: false,
    lastSyncedAt: null,
    updatedMonitorId: null,
    eventSource: null,
    snapshotVersion: 0,
  };

  const elements = {
    connectionStatus: document.querySelector("#connection-status"),
    connectionLabel: document.querySelector("#connection-label"),
    refreshButton: document.querySelector("#refresh-button"),
    lastSynced: document.querySelector("#last-synced"),
    totalCount: document.querySelector("#total-count"),
    healthyCount: document.querySelector("#healthy-count"),
    pendingCount: document.querySelector("#pending-count"),
    issueCount: document.querySelector("#issue-count"),
    latencyAverage: document.querySelector("#latency-average"),
    healthyCaption: document.querySelector("#healthy-caption"),
    pendingCaption: document.querySelector("#pending-caption"),
    issueCaption: document.querySelector("#issue-caption"),
    visibleCount: document.querySelector("#visible-count"),
    monitorGrid: document.querySelector("#monitor-grid"),
    emptyState: document.querySelector("#empty-state"),
    emptyTitle: document.querySelector("#empty-title"),
    emptyCopy: document.querySelector("#empty-copy"),
    clearFilters: document.querySelector("#clear-filters"),
    searchForm: document.querySelector("#search-form"),
    searchInput: document.querySelector("#monitor-search"),
    filterButtons: [...document.querySelectorAll("[data-filter]")],
    notice: document.querySelector("#notice"),
    noticeText: document.querySelector("#notice-text"),
    dismissNotice: document.querySelector("#dismiss-notice"),
    screenReaderUpdates: document.querySelector("#screen-reader-updates"),
  };

  const numberFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
  const compactFormatter = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });
  const exactDateFormatter = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  });

  function finiteNumber(value) {
    if (value === null || value === undefined || value === "") return null;
    const number = Number(value);
    return Number.isFinite(number) ? number : null;
  }

  function nonNegativeInteger(value) {
    const number = finiteNumber(value);
    return number === null ? 0 : Math.max(0, Math.trunc(number));
  }

  function normalizeMonitor(raw) {
    if (!raw || typeof raw !== "object" || raw.id === null || raw.id === undefined) return null;

    const url = typeof raw.url === "string" ? raw.url.trim() : "";
    const name = typeof raw.name === "string" && raw.name.trim() ? raw.name.trim() : hostFromUrl(url) || "Unnamed monitor";

    return {
      id: String(raw.id),
      name,
      url,
      pending: raw.pending === true,
      healthy: raw.healthy === true,
      statusCode: finiteNumber(raw.status_code),
      latencyMs: finiteNumber(raw.latency_ms),
      checkedAt: typeof raw.checked_at === "string" ? raw.checked_at : "",
      error: typeof raw.error === "string" ? raw.error.trim() : "",
      checks: nonNegativeInteger(raw.checks),
      failures: nonNegativeInteger(raw.failures),
    };
  }

  function monitorTimestamp(monitor) {
    const timestamp = Date.parse(monitor.checkedAt);
    return Number.isFinite(timestamp) ? timestamp : Number.NEGATIVE_INFINITY;
  }

  function compareMonitorFreshness(candidate, current) {
    if (candidate.checks !== current.checks) {
      return candidate.checks > current.checks ? 1 : -1;
    }

    const candidateTimestamp = monitorTimestamp(candidate);
    const currentTimestamp = monitorTimestamp(current);
    if (candidateTimestamp === currentTimestamp) return 0;
    return candidateTimestamp > currentTimestamp ? 1 : -1;
  }

  function replaceWithFreshestRestResponse(monitors) {
    const nextMonitors = new Map();

    monitors.forEach((candidate) => {
      const current = nextMonitors.get(candidate.id) || state.monitors.get(candidate.id);
      const freshest = current && compareMonitorFreshness(candidate, current) < 0
        ? current
        : candidate;
      nextMonitors.set(candidate.id, freshest);
    });

    state.monitors = nextMonitors;
  }

  function safeHttpUrl(value) {
    if (!value) return null;
    try {
      const parsed = new URL(value);
      return parsed.protocol === "http:" || parsed.protocol === "https:" ? parsed.href : null;
    } catch {
      return null;
    }
  }

  function hostFromUrl(value) {
    const safeUrl = safeHttpUrl(value);
    if (!safeUrl) return "";
    try {
      return new URL(safeUrl).hostname.replace(/^www\./, "");
    } catch {
      return "";
    }
  }

  function displayUrl(value) {
    const safeUrl = safeHttpUrl(value);
    if (!safeUrl) return value || "No URL provided";
    try {
      const parsed = new URL(safeUrl);
      const path = parsed.pathname === "/" ? "" : parsed.pathname.replace(/\/$/, "");
      return `${parsed.host}${path}`;
    } catch {
      return value;
    }
  }

  function initials(name) {
    const words = name.trim().split(/\s+/).filter(Boolean);
    if (!words.length) return "?";
    return words.length === 1
      ? words[0].slice(0, 2)
      : `${words[0][0]}${words[1][0]}`;
  }

  function successRate(monitor) {
    if (monitor.pending || monitor.checks <= 0) return null;
    const successful = Math.max(0, monitor.checks - Math.min(monitor.failures, monitor.checks));
    return (successful / monitor.checks) * 100;
  }

  function monitorCondition(monitor) {
    if (monitor.pending) return "pending";
    return monitor.healthy ? "healthy" : "issues";
  }

  function conditionCopy(condition) {
    if (condition === "pending") {
      return { badge: "Checking", aria: "checking, awaiting first check", announcement: "checking and awaiting its first result" };
    }
    if (condition === "healthy") {
      return { badge: "Operational", aria: "operational", announcement: "operational" };
    }
    return { badge: "Unavailable", aria: "unavailable", announcement: "unavailable" };
  }

  function relativeTime(value) {
    const timestamp = Date.parse(value);
    if (!Number.isFinite(timestamp)) return "Not checked yet";

    const elapsed = Math.max(0, Date.now() - timestamp);
    const seconds = Math.floor(elapsed / 1000);
    if (seconds < 5) return "Checked just now";
    if (seconds < 60) return `Checked ${seconds}s ago`;

    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `Checked ${minutes}m ago`;

    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `Checked ${hours}h ago`;

    const days = Math.floor(hours / 24);
    if (days < 7) return `Checked ${days}d ago`;
    return `Checked ${new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(timestamp)}`;
  }

  function exactTime(value) {
    const timestamp = Date.parse(value);
    return Number.isFinite(timestamp) ? exactDateFormatter.format(timestamp) : "No completed check";
  }

  function setConnection(mode, label) {
    elements.connectionStatus.classList.remove("is-live", "is-connecting", "is-offline");
    elements.connectionStatus.classList.add(`is-${mode}`);
    elements.connectionLabel.textContent = label;
    elements.connectionStatus.title = `Live updates: ${label.toLowerCase()}`;
  }

  function showNotice(message) {
    elements.noticeText.textContent = message;
    elements.notice.hidden = false;
  }

  function hideNotice() {
    elements.notice.hidden = true;
  }

  function announce(message) {
    elements.screenReaderUpdates.textContent = "";
    window.setTimeout(() => {
      elements.screenReaderUpdates.textContent = message;
    }, 30);
  }

  function setRefreshing(refreshing) {
    state.refreshing = refreshing;
    elements.refreshButton.disabled = refreshing;
    elements.refreshButton.classList.toggle("is-loading", refreshing);
    elements.refreshButton.setAttribute("aria-label", refreshing ? "Refreshing monitor data" : "Refresh monitor data");
  }

  function updateSummary() {
    const monitors = [...state.monitors.values()];
    const pending = monitors.filter((monitor) => monitorCondition(monitor) === "pending").length;
    const healthy = monitors.filter((monitor) => monitorCondition(monitor) === "healthy").length;
    const issues = monitors.filter((monitor) => monitorCondition(monitor) === "issues").length;
    const latencies = monitors
      .filter((monitor) => !monitor.pending)
      .map((monitor) => monitor.latencyMs)
      .filter((value) => value !== null && value >= 0);
    const averageLatency = latencies.length
      ? latencies.reduce((sum, value) => sum + value, 0) / latencies.length
      : null;

    elements.totalCount.textContent = state.loading ? "—" : String(monitors.length);
    elements.healthyCount.textContent = state.loading ? "—" : String(healthy);
    elements.pendingCount.textContent = state.loading ? "—" : String(pending);
    elements.issueCount.textContent = state.loading ? "—" : String(issues);
    elements.latencyAverage.textContent = state.loading || averageLatency === null
      ? "—"
      : `${numberFormatter.format(averageLatency)} ms`;
    if (state.loading) {
      elements.healthyCaption.textContent = "Checking status";
      elements.pendingCaption.textContent = "Awaiting first result";
      elements.issueCaption.textContent = "Checking status";
    } else {
      elements.healthyCaption.textContent = healthy > 0 && issues === 0
        ? "All checked services normal"
        : healthy === 0 && pending > 0 && issues === 0
          ? "No completed checks yet"
          : "Services online";
      elements.pendingCaption.textContent = pending === 0
        ? "All monitors checked"
        : `${pending} awaiting first result${pending === 1 ? "" : "s"}`;
      elements.issueCaption.textContent = issues === 0
        ? "No active incidents"
        : `${issues} service${issues === 1 ? "" : "s"} affected`;
    }
  }

  function filteredMonitors() {
    const query = state.query.toLocaleLowerCase();
    return [...state.monitors.values()].filter((monitor) => {
      const statusMatches = state.filter === "all"
        || state.filter === monitorCondition(monitor);
      const queryMatches = !query
        || monitor.name.toLocaleLowerCase().includes(query)
        || monitor.url.toLocaleLowerCase().includes(query);
      return statusMatches && queryMatches;
    });
  }

  function textElement(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    element.textContent = text;
    return element;
  }

  function metric(label, value, unit = "") {
    const wrapper = document.createElement("div");
    const term = textElement("dt", "", label);
    const description = document.createElement("dd");
    description.append(document.createTextNode(value));
    if (unit) description.append(textElement("span", "metric-unit", unit));
    wrapper.append(term, description);
    return wrapper;
  }

  function createMonitorCard(monitor) {
    const condition = monitorCondition(monitor);
    const copy = conditionCopy(condition);
    const card = document.createElement("article");
    const conditionClass = condition === "issues" ? "is-unhealthy" : `is-${condition}`;
    card.className = `monitor-card ${conditionClass}`;
    card.dataset.monitorId = monitor.id;
    card.dataset.status = condition;
    card.setAttribute("aria-label", `${monitor.name}: ${copy.aria}`);

    if (monitor.id === state.updatedMonitorId) {
      card.classList.add("is-updated");
    }

    const header = document.createElement("div");
    header.className = "monitor-header";
    const identity = document.createElement("div");
    identity.className = "monitor-identity";
    const avatar = textElement("span", "monitor-avatar", initials(monitor.name));
    avatar.setAttribute("aria-hidden", "true");
    const nameWrap = document.createElement("div");
    nameWrap.className = "monitor-name-wrap";
    const name = textElement("h3", "monitor-name", monitor.name);
    name.title = monitor.name;

    const safeUrl = safeHttpUrl(monitor.url);
    const url = document.createElement(safeUrl ? "a" : "span");
    url.className = "monitor-url";
    url.textContent = displayUrl(monitor.url);
    url.title = monitor.url || "No URL provided";
    if (safeUrl) {
      url.href = safeUrl;
      url.target = "_blank";
      url.rel = "noopener noreferrer";
      url.setAttribute("aria-label", `Open ${monitor.name} endpoint in a new tab`);
    }

    nameWrap.append(name, url);
    identity.append(avatar, nameWrap);
    const badge = textElement("span", "status-badge", copy.badge);
    header.append(identity, badge);

    const metrics = document.createElement("dl");
    metrics.className = "monitor-metrics";
    const latency = monitor.pending || monitor.latencyMs === null || monitor.latencyMs < 0 ? "—" : numberFormatter.format(monitor.latencyMs);
    const status = monitor.pending || monitor.statusCode === null ? "—" : String(Math.trunc(monitor.statusCode));
    const rate = successRate(monitor);
    metrics.append(
      metric("Response", latency, latency === "—" ? "" : "ms"),
      metric("HTTP status", status),
      metric("Success rate", rate === null ? "—" : numberFormatter.format(rate), rate === null ? "" : "%"),
    );

    const footer = document.createElement("div");
    footer.className = "monitor-footer";
    const checked = document.createElement("span");
    checked.className = "checked-time";
    const time = textElement("time", "", monitor.pending ? "Awaiting first check" : relativeTime(monitor.checkedAt));
    time.dateTime = monitor.checkedAt;
    time.title = monitor.pending ? "No completed check yet" : exactTime(monitor.checkedAt);
    checked.append(time);
    const checkLabel = `${compactFormatter.format(monitor.checks)} check${monitor.checks === 1 ? "" : "s"}`;
    const checkCount = textElement("span", "check-count", checkLabel);
    checkCount.title = `${monitor.checks.toLocaleString()} total checks, ${monitor.failures.toLocaleString()} failures`;
    footer.append(checked, checkCount);

    card.append(header, metrics, footer);

    if (condition === "issues" && monitor.error) {
      const error = document.createElement("div");
      error.className = "error-message";
      error.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="9"></circle><path d="M12 8v5M12 16.5v.1"></path></svg>';
      const message = textElement("span", "", monitor.error);
      message.title = monitor.error;
      error.append(message);
      card.append(error);
    }

    return card;
  }

  function renderMonitors() {
    if (state.loading) return;

    const monitors = filteredMonitors();
    elements.monitorGrid.replaceChildren(...monitors.map(createMonitorCard));
    elements.monitorGrid.setAttribute("aria-busy", "false");
    elements.visibleCount.textContent = String(monitors.length);
    elements.visibleCount.setAttribute("aria-label", `${monitors.length} monitor${monitors.length === 1 ? "" : "s"} shown`);

    const filtersActive = state.filter !== "all" || Boolean(state.query);
    const isEmpty = monitors.length === 0;
    elements.monitorGrid.hidden = isEmpty;
    elements.emptyState.hidden = !isEmpty;

    if (isEmpty) {
      elements.emptyTitle.textContent = filtersActive ? "No matching monitors" : "No monitors yet";
      elements.emptyCopy.textContent = filtersActive
        ? "Try a different search or status filter."
        : "Configured monitors will appear here after their first check.";
      elements.clearFilters.hidden = !filtersActive;
    }

    if (state.updatedMonitorId) {
      window.setTimeout(() => {
        state.updatedMonitorId = null;
      }, 1000);
    }
  }

  function render() {
    updateSummary();
    renderMonitors();
    updateRelativeTimes();
  }

  function updateRelativeTimes() {
    document.querySelectorAll(".monitor-card time").forEach((time) => {
      if (time.closest(".is-pending")) {
        time.textContent = "Awaiting first check";
        time.title = "No completed check yet";
        return;
      }
      time.textContent = relativeTime(time.dateTime);
      time.title = exactTime(time.dateTime);
    });

    if (state.lastSyncedAt) {
      const elapsed = Math.max(0, Date.now() - state.lastSyncedAt);
      if (elapsed < 5000) elements.lastSynced.textContent = "Just now";
      else if (elapsed < 60000) elements.lastSynced.textContent = `${Math.floor(elapsed / 1000)} seconds ago`;
      else elements.lastSynced.textContent = `${Math.floor(elapsed / 60000)} minutes ago`;
      elements.lastSynced.title = exactDateFormatter.format(state.lastSyncedAt);
    }
  }

  async function loadMonitors({ announceResult = false } = {}) {
    if (state.refreshing) return;
    setRefreshing(true);
    const snapshotVersionAtRequest = state.snapshotVersion;

    try {
      const response = await fetch(API_URL, {
        headers: { Accept: "application/json" },
        cache: "no-store",
      });

      if (state.snapshotVersion !== snapshotVersionAtRequest) return;
      if (!response.ok) throw new Error(`Server returned HTTP ${response.status}`);
      const payload = await response.json();
      if (state.snapshotVersion !== snapshotVersionAtRequest) return;
      if (!payload || !Array.isArray(payload.monitors)) throw new Error("The monitor response was not in the expected format");

      const monitors = payload.monitors.map(normalizeMonitor).filter(Boolean);
      replaceWithFreshestRestResponse(monitors);
      state.loading = false;
      state.lastSyncedAt = Date.now();
      hideNotice();
      render();
      if (announceResult) announce(`Monitor data refreshed. ${state.monitors.size} monitors loaded.`);
    } catch (error) {
      if (state.snapshotVersion !== snapshotVersionAtRequest) return;
      state.loading = false;
      showNotice(`Could not refresh monitor data. ${error instanceof Error ? error.message : "Please try again."}`);
      render();
      if (announceResult) announce("Monitor refresh failed.");
    } finally {
      setRefreshing(false);
    }
  }

  function handleCheckEvent(event) {
    try {
      const monitor = normalizeMonitor(JSON.parse(event.data));
      if (!monitor) throw new Error("Invalid monitor event");

      const previous = state.monitors.get(monitor.id);
      if (previous && compareMonitorFreshness(monitor, previous) < 0) {
        setConnection("live", "Live");
        return;
      }

      state.monitors.set(monitor.id, monitor);
      state.loading = false;
      state.lastSyncedAt = Date.now();
      state.updatedMonitorId = monitor.id;
      setConnection("live", "Live");
      render();

      const condition = monitorCondition(monitor);
      if (!previous || monitorCondition(previous) !== condition) {
        announce(`${monitor.name} is now ${conditionCopy(condition).announcement}.`);
      }
    } catch {
      showNotice("A live update could not be read. The dashboard will continue listening for new checks.");
    }
  }

  function handleSnapshotEvent(event) {
    try {
      const payload = JSON.parse(event.data);
      if (!payload || !Array.isArray(payload.monitors)) throw new Error("Invalid monitor snapshot");

      const monitors = payload.monitors.map(normalizeMonitor).filter(Boolean);
      state.snapshotVersion += 1;
      state.monitors = new Map(monitors.map((monitor) => [monitor.id, monitor]));
      state.updatedMonitorId = null;
      state.loading = false;
      state.lastSyncedAt = Date.now();
      setConnection("live", "Live");
      hideNotice();
      render();
    } catch {
      showNotice("A live snapshot could not be read. The dashboard will continue listening for new checks.");
    }
  }

  function connectEvents() {
    if (!("EventSource" in window)) {
      setConnection("offline", "Live unavailable");
      showNotice("This browser does not support live updates. Use refresh to load the latest checks.");
      return;
    }

    state.eventSource?.close();
    setConnection("connecting", "Connecting");
    const source = new EventSource(EVENTS_URL);
    state.eventSource = source;

    source.addEventListener("open", () => {
      setConnection("live", "Live");
    });

    source.addEventListener("snapshot", handleSnapshotEvent);
    source.addEventListener("check", handleCheckEvent);

    source.addEventListener("error", () => {
      if (!navigator.onLine) setConnection("offline", "Offline");
      else if (source.readyState === EventSource.CLOSED) setConnection("offline", "Disconnected");
      else setConnection("connecting", "Reconnecting");
    });
  }

  function clearFilters() {
    state.query = "";
    state.filter = "all";
    elements.searchInput.value = "";
    elements.filterButtons.forEach((button) => {
      const active = button.dataset.filter === "all";
      button.classList.toggle("is-active", active);
      button.setAttribute("aria-pressed", String(active));
    });
    renderMonitors();
    elements.searchInput.focus();
  }

  elements.searchForm.addEventListener("submit", (event) => event.preventDefault());

  elements.searchInput.addEventListener("input", (event) => {
    state.query = event.currentTarget.value.trim();
    renderMonitors();
  });

  elements.filterButtons.forEach((button) => {
    button.addEventListener("click", () => {
      state.filter = button.dataset.filter;
      elements.filterButtons.forEach((candidate) => {
        const active = candidate === button;
        candidate.classList.toggle("is-active", active);
        candidate.setAttribute("aria-pressed", String(active));
      });
      renderMonitors();
    });
  });

  elements.refreshButton.addEventListener("click", () => loadMonitors({ announceResult: true }));
  elements.dismissNotice.addEventListener("click", hideNotice);
  elements.clearFilters.addEventListener("click", clearFilters);

  window.addEventListener("offline", () => setConnection("offline", "Offline"));
  window.addEventListener("online", () => {
    setConnection("connecting", "Reconnecting");
    if (!state.eventSource || state.eventSource.readyState === EventSource.CLOSED) connectEvents();
    loadMonitors();
  });
  window.addEventListener("beforeunload", () => state.eventSource?.close());

  window.setInterval(updateRelativeTimes, 30000);
  loadMonitors();
  connectEvents();
})();
