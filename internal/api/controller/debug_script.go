package controller

const debugPageScript = `<script>
const events = [];
const container = document.getElementById("events");
const state = document.getElementById("state");
const dot = document.getElementById("dot");
const count = document.getElementById("count");
const total = document.getElementById("total");
const correlation = document.getElementById("correlation");
const from = document.getElementById("from");
const to = document.getElementById("to");
let expanded = false;
let newestFirst = true;
let clearedAt = 0;
let emptyTitle = "No events displayed.";
let emptyBody = "The stream is ready. New events will appear here.";

function visible() {
  const filtered = events.filter((item) => {
    if (!eventIsAfterClear(item)) return false;
    const event = item.event || {};
    if (correlation.value && event.correlation_id !== correlation.value) return false;
    if (from.value && item.streamed_at < new Date(from.value).toISOString()) return false;
    if (to.value && item.streamed_at > new Date(to.value).toISOString()) return false;
    return true;
  });
  return newestFirst ? filtered.slice().reverse() : filtered;
}

function render() {
  const shown = visible();
  count.textContent = shown.length + " visible";
  total.textContent = events.length + " buffered";
  if (!shown.length) {
    container.innerHTML = events.length
      ? renderEmpty("No matching events.", "Adjust filters or wait for the next signal.")
      : renderEmpty(emptyTitle, emptyBody);
    return;
  }
  container.innerHTML = shown.map((item, index) => renderEvent(item, index)).join("");
  for (const button of document.querySelectorAll(".event-copy")) {
    button.onclick = () => navigator.clipboard.writeText(JSON.stringify(shown[Number(button.dataset.index)].event, null, 2));
  }
}

function renderEvent(item, index) {
  const event = item.event || {};
  const source = item.source || {};
  const batch = source.batch_index === undefined ? "" : " #" + source.batch_index;
  const detailsOpen = expanded ? " open" : "";
  const sentAt = formatTimestamp(item.streamed_at);
  return '<article class="event"><div class="event-top"><div><span class="event-name">' +
    escapeHTML(item.event_name || event.event_name || "unnamed event") +
    '</span><span class="event-id">' + escapeHTML(item.event_id || event.event_id || "missing event_id") +
    '</span></div><div class="event-meta"><div class="event-time"><span class="event-time-label">sent at</span><span class="event-time-value">' +
    escapeHTML(sentAt) +
    '</span></div><button class="event-copy" type="button" data-index="' + index + '">Copy event JSON</button></div></div>' +
    '<div class="chips">' +
    chip("producer", item.producer_service || event.producer_service) +
    chip("endpoint", (source.endpoint || "unknown") + batch) +
    chip("correlation", event.correlation_id) +
    chip("streamed", shortTime(item.streamed_at)) +
    '</div><details' + detailsOpen + '><summary>Envelope JSON</summary><pre>' +
    escapeHTML(JSON.stringify(event, null, 2)) + '</pre></details></article>';
}

function renderEmpty(title, body) {
  return '<div class="empty"><strong>' + escapeHTML(title) + '</strong>' + escapeHTML(body) + '</div>';
}

function eventIsAfterClear(item) {
  if (!clearedAt) return true;
  const streamedAt = Date.parse(item.streamed_at || "");
  return Number.isNaN(streamedAt) || streamedAt > clearedAt;
}

function chip(name, value) {
  if (!value) return "";
  return '<span class="chip">' + escapeHTML(name) + ' <strong>' + escapeHTML(value) + '</strong></span>';
}

function shortTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function formatTimestamp(value) {
  if (!value) return "unknown";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString([], {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    fractionalSecondDigits: 3
  });
}

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;"
  })[char]);
}

async function refresh() {
  const response = await fetch("/debug/history");
  if (!response.ok) return;
  const history = await response.json();
  events.splice(0, events.length, ...history.filter(eventIsAfterClear));
  render();
}

async function clearEvents() {
  const button = document.getElementById("clear");
  button.disabled = true;
  clearedAt = Date.now();
  emptyTitle = "Events cleared.";
  emptyBody = "New events from the next smoke run will appear here.";
  events.splice(0, events.length);
  render();
  try {
    const response = await fetch("/debug/history", { method: "DELETE" });
    if (!response.ok) state.textContent = "clear failed";
  } catch {
    state.textContent = "clear failed";
  } finally {
    button.disabled = false;
  }
}

document.getElementById("refresh").onclick = refresh;
document.getElementById("clear").onclick = clearEvents;
document.getElementById("copy").onclick = () => navigator.clipboard.writeText(JSON.stringify(visible().map((item) => item.event), null, 2));
document.getElementById("toggleOrder").onclick = (event) => {
  newestFirst = !newestFirst;
  event.target.textContent = newestFirst ? "Order: newest first" : "Order: oldest first";
  render();
};
document.getElementById("toggleEvents").onclick = (event) => {
  expanded = !expanded;
  event.target.textContent = expanded ? "Collapse all events" : "Open all events";
  render();
};
for (const input of [correlation, from, to]) input.oninput = render;

async function stream() {
  try {
    const response = await fetch("/debug/stream");
    if (!response.ok || !response.body) {
      state.textContent = "stream unavailable";
      return;
    }
    state.textContent = "connected";
    dot.classList.add("connected");
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop();
      for (const line of lines) {
        if (!line) continue;
        const item = JSON.parse(line);
        if (!eventIsAfterClear(item)) continue;
        events.push(item);
        if (events.length > 1000) events.shift();
      }
      render();
    }
  } catch {
    state.textContent = "disconnected";
    dot.classList.remove("connected");
  }
}
refresh();
stream();
</script>
</body>
</html>`
