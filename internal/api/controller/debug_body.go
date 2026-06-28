package controller

const debugPageBody = `<body>
<main>
  <header>
    <div>
      <h1>sigint debug</h1>
      <div class="subtitle">live event telescope</div>
    </div>
    <div class="status-line">
      <span class="pill"><span id="dot" class="dot"></span><span id="state">connecting</span></span>
      <span class="pill"><span id="count">0 visible</span></span>
      <span class="pill"><span id="total">0 buffered</span></span>
    </div>
  </header>

  <section class="toolbar" aria-label="debug controls">
    <button id="refresh" type="button">Refresh</button>
    <button id="toggleOrder" type="button">Order: newest first</button>
    <button id="toggleEvents" type="button">Open all events</button>
    <button id="copy" type="button" class="copy-all">Copy visible JSON</button>
    <button id="clear" type="button">Clear events</button>
  </section>

  <section class="filters" aria-label="debug filters">
    <label>correlation_id <input id="correlation" placeholder="corr-001"></label>
    <label>from <input id="from" type="datetime-local"></label>
    <label>to <input id="to" type="datetime-local"></label>
  </section>

  <section class="events" id="events"><div class="empty"><strong>No events yet.</strong>The stream is ready.</div></section>
</main>
`
