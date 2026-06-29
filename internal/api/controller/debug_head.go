package controller

const debugPageHead = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>sigint debug</title>
  <link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Ctext y='.9em' font-size='90'%3E%F0%9F%9A%B0%3C/text%3E%3C/svg%3E">
  <style>
    :root {
      color-scheme: dark;
      --bg: #0b1020;
      --panel: #121a2a;
      --panel-2: #18243a;
      --row: #111827;
      --line: #2d3b52;
      --text: #f8fafc;
      --muted: #a8b3c7;
      --accent: #38bdf8;
      --mint: #6ee7b7;
      --sun: #facc15;
      --ink: #050816;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      font: 14px/1.45 Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(1180px, calc(100vw - 24px));
      margin: 0 auto;
      padding: 14px 0 28px;
    }
    header, .toolbar, .filters, .event, .empty {
      border: 1px solid var(--line);
      background: var(--panel);
      border-radius: 8px;
      box-shadow: 0 14px 32px rgba(0, 0, 0, 0.22);
    }
    header {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      padding: 14px 16px;
      border-top: 3px solid var(--accent);
    }
    h1 { margin: 0; font-size: 21px; letter-spacing: 0; }
    .subtitle { color: var(--muted); font-size: 13px; }
    .status-line {
      display: flex;
      flex-wrap: wrap;
      justify-content: flex-end;
      gap: 8px;
      align-items: center;
    }
    .pill {
      display: inline-flex;
      gap: 7px;
      align-items: center;
      min-height: 30px;
      padding: 5px 9px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--ink);
      color: var(--muted);
      white-space: nowrap;
    }
    .dot {
      width: 9px;
      height: 9px;
      border-radius: 50%;
      background: var(--sun);
      box-shadow: 0 0 0 3px rgba(250, 204, 21, 0.14);
    }
    .dot.connected {
      background: var(--mint);
      box-shadow: 0 0 0 3px rgba(110, 231, 183, 0.14);
    }
    .toolbar, .filters {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
      margin-top: 10px;
      padding: 10px;
      background: var(--panel-2);
    }
    button, input {
      border: 1px solid var(--line);
      border-radius: 6px;
      background: var(--ink);
      color: var(--text);
      min-height: 34px;
      padding: 7px 10px;
      font: inherit;
    }
    button {
      cursor: pointer;
      font-weight: 650;
    }
    button:hover { border-color: var(--accent); color: white; }
    .copy-all { border-color: rgba(110, 231, 183, 0.5); }
    label {
      display: grid;
      gap: 4px;
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
    }
    input {
      min-width: 210px;
      color: var(--text);
    }
    .events {
      display: grid;
      gap: 8px;
      margin-top: 10px;
    }
    .event {
      padding: 12px;
      background: var(--row);
      border-left: 4px solid var(--accent);
    }
    .event:nth-child(3n + 2) { border-left-color: var(--mint); }
    .event:nth-child(3n + 3) { border-left-color: var(--sun); }
    .event-top {
      display: flex;
      justify-content: space-between;
      gap: 10px;
      align-items: flex-start;
    }
    .event-meta {
      display: grid;
      gap: 8px;
      justify-items: end;
    }
    .event-time {
      min-width: 260px;
      padding: 8px 10px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--ink);
      text-align: right;
    }
    .event-time-label {
      display: block;
      color: var(--muted);
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.06em;
      text-transform: uppercase;
    }
    .event-time-value {
      display: block;
      margin-top: 3px;
      color: var(--text);
      font: 15px/1.35 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-weight: 800;
    }
    .event-name {
      display: block;
      font-size: 16px;
      font-weight: 800;
      line-height: 1.25;
      overflow-wrap: anywhere;
    }
    .event-id {
      display: block;
      margin-top: 3px;
      color: var(--muted);
      font: 12px/1.4 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      overflow-wrap: anywhere;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      margin-top: 10px;
    }
    .chip {
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 3px 7px;
      background: var(--panel);
      color: var(--muted);
      font-size: 12px;
    }
    .chip strong { color: var(--text); }
    details {
      margin-top: 10px;
      border-top: 1px solid var(--line);
      padding-top: 8px;
    }
    summary {
      cursor: pointer;
      color: var(--muted);
      font-weight: 700;
    }
    pre {
      overflow: auto;
      margin: 8px 0 0;
      padding: 10px;
      max-height: 45vh;
      background: #020617;
      border: 1px solid #1e293b;
      border-radius: 6px;
      color: #dbeafe;
      font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }
    .empty {
      padding: 24px;
      text-align: center;
      color: var(--muted);
      background: var(--panel);
    }
    .empty strong {
      display: block;
      color: var(--text);
      font-size: 16px;
      margin-bottom: 3px;
    }
    @media (max-width: 720px) {
      main { width: calc(100vw - 12px); padding-top: 6px; }
      header, .event-top { display: grid; }
      .event-meta { justify-items: start; }
      .event-time { min-width: 0; width: 100%; text-align: left; }
      .status-line { justify-content: flex-start; }
      input, button { width: 100%; }
      label { width: 100%; }
    }
  </style>
</head>
`
