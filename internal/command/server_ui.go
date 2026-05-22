// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package command

const serverIndexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Terraform Server</title>
<style>
:root {
  color-scheme: light;
  --bg: #f6f8fb;
  --canvas: #f8fafc;
  --grid: #e6edf5;
  --panel: #ffffff;
  --panel-2: #f1f5f9;
  --text: #18212f;
  --muted: #667386;
  --line: #d6dee8;
  --node: #ffffff;
  --node-border: #d8e1ed;
  --create: #0f8f5f;
  --update: #b7791f;
  --delete: #c53030;
  --read: #2b6cb0;
  --focus: #2f6fed;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  background: var(--bg);
  color: var(--text);
  font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
button, input {
  font: inherit;
}
.app {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  min-height: 100vh;
}
.main {
  min-width: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
}
.toolbar {
  min-height: 58px;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--line);
  background: rgba(255,255,255,.92);
}
.brand {
  font-weight: 700;
  margin-right: auto;
}
.meta {
  color: var(--muted);
  font-size: 12px;
}
.btn {
  border: 1px solid var(--line);
  background: var(--panel);
  color: var(--text);
  border-radius: 6px;
  padding: 7px 10px;
  cursor: pointer;
}
.btn:hover {
  border-color: var(--focus);
}
.btn.primary {
  color: white;
  background: var(--focus);
  border-color: var(--focus);
}
.stage {
  position: relative;
  min-width: 0;
  overflow: hidden;
  background: var(--canvas);
}
svg {
  width: 100%;
  height: 100%;
  display: block;
}
.graph-bg {
  fill: url(#graph-grid);
}
.column-guide {
  stroke: #dbe4ef;
  stroke-width: 1;
  stroke-dasharray: 4 10;
}
.edge-halo {
  stroke: #ffffff;
  stroke-width: 7;
  fill: none;
  opacity: .9;
}
.edge {
  stroke: #8fa1b5;
  stroke-width: 2;
  fill: none;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.edge.changed {
  stroke: var(--update);
}
.node {
  cursor: pointer;
}
.node .card {
  fill: var(--node);
  stroke: var(--node-border);
  stroke-width: 1.2;
  filter: url(#node-shadow);
}
.node .accent {
  fill: var(--focus);
}
.node .handle {
  fill: #f8fafc;
  stroke: #9eacbc;
  stroke-width: 2;
}
.node .resource-icon {
  fill: #eef4ff;
  stroke: #c8d7f4;
  stroke-width: 1;
}
.node .resource-icon-text {
  fill: #2654a8;
  font-size: 11px;
  font-weight: 800;
}
.node text {
  pointer-events: none;
  fill: var(--text);
}
.node .title {
  font-size: 15px;
  font-weight: 800;
}
.node .address {
  fill: var(--muted);
  font-size: 11px;
}
.node .type {
  fill: #42516a;
  font-size: 12px;
  font-weight: 650;
}
.node .count {
  fill: var(--muted);
  font-size: 11px;
}
.node .badge {
  fill: var(--panel-2);
  stroke: #dbe4ee;
  stroke-width: 1;
}
.node .badge-text {
  fill: #4d5c70;
  font-size: 11px;
  font-weight: 700;
}
.node .expand {
  fill: #eef4ff;
  stroke: #b9cbef;
  stroke-width: 1;
}
.node .expand-text {
  fill: #2f5fab;
  font-size: 12px;
  font-weight: 800;
}
.node.selected .card {
  stroke: var(--focus);
  stroke-width: 2.2;
}
.node.create .accent { fill: var(--create); }
.node.update .accent { fill: var(--update); }
.node.delete .accent { fill: var(--delete); }
.node.read .accent { fill: var(--read); }
.node.create .badge { fill: #e9f8f0; stroke: #bfe7d2; }
.node.update .badge { fill: #fff7e8; stroke: #eed39b; }
.node.delete .badge { fill: #fff0f0; stroke: #edbbbb; }
.node.read .badge { fill: #eef6ff; stroke: #bfd7f3; }
.node.create .badge-text { fill: var(--create); }
.node.update .badge-text { fill: var(--update); }
.node.delete .badge-text { fill: var(--delete); }
.node.read .badge-text { fill: var(--read); }
.node.create .handle { stroke: var(--create); }
.node.update .handle { stroke: var(--update); }
.node.delete .handle { stroke: var(--delete); }
.node.read .handle { stroke: var(--read); }
.node:hover .card {
  stroke: #9db1c7;
}
.node.selected:hover .card {
  stroke: var(--focus);
}
.empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: var(--muted);
}
.sidebar {
  border-left: 1px solid var(--line);
  background: var(--panel);
  min-width: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
}
.side-head {
  min-height: 58px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--line);
  display: flex;
  align-items: center;
  gap: 8px;
}
.side-title {
  min-width: 0;
  font-weight: 700;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.side-body {
  padding: 14px;
  overflow: auto;
}
.closed {
  display: none;
}
.field {
  margin-bottom: 14px;
}
.label {
  display: block;
  color: var(--muted);
  font-size: 12px;
  margin-bottom: 5px;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace;
}
.value, .attr-input {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--panel-2);
  color: var(--text);
  padding: 8px;
  min-height: 36px;
  overflow-wrap: anywhere;
}
.attr-input {
  background: #fff;
}
.section-title {
  margin: 18px 0 8px;
  font-weight: 700;
}
.pill {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  margin: 0 6px 6px 0;
  padding: 4px 7px;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--muted);
  background: var(--panel-2);
  font-size: 12px;
}
.status {
  position: absolute;
  left: 14px;
  bottom: 14px;
  max-width: min(760px, calc(100% - 28px));
  padding: 8px 10px;
  border: 1px solid var(--line);
  border-radius: 6px;
  background: rgba(255,255,255,.94);
  color: var(--muted);
  box-shadow: 0 10px 28px rgba(15, 23, 42, .08);
}
@media (max-width: 900px) {
  .app {
    grid-template-columns: 1fr;
  }
  .sidebar {
    position: fixed;
    inset: auto 0 0 0;
    max-height: 58vh;
    border-left: 0;
    border-top: 1px solid var(--line);
    box-shadow: 0 -14px 32px rgba(15, 23, 42, .16);
  }
}
</style>
</head>
<body>
<div class="app">
  <main class="main">
    <div class="toolbar">
      <div class="brand">Terraform Server</div>
      <div class="meta" id="meta">Loading graph</div>
      <button class="btn" id="collapse">Collapse</button>
      <button class="btn" id="refresh">Refresh</button>
      <button class="btn primary" id="plan">Plan</button>
      <button class="btn" id="apply">Apply</button>
    </div>
    <div class="stage">
      <svg id="graph" role="img" aria-label="Terraform managed resource graph"></svg>
      <div class="empty closed" id="empty">No managed resources in this configuration.</div>
      <div class="status closed" id="status"></div>
    </div>
  </main>
  <aside class="sidebar closed" id="sidebar">
    <div class="side-head">
      <div class="side-title" id="side-title"></div>
      <button class="btn" id="close">Close</button>
    </div>
    <div class="side-body" id="side-body"></div>
  </aside>
</div>
<script>
const state = {
  graph: null,
  expanded: new Set(),
  selected: null,
};

const els = {
  svg: document.getElementById("graph"),
  meta: document.getElementById("meta"),
  empty: document.getElementById("empty"),
  status: document.getElementById("status"),
  sidebar: document.getElementById("sidebar"),
  sideTitle: document.getElementById("side-title"),
  sideBody: document.getElementById("side-body"),
  refresh: document.getElementById("refresh"),
  plan: document.getElementById("plan"),
  apply: document.getElementById("apply"),
  collapse: document.getElementById("collapse"),
  close: document.getElementById("close"),
};

function escapeText(value) {
  return String(value ?? "").replace(/[&<>"']/g, ch => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  })[ch]);
}

function truncateText(value, max) {
  value = String(value ?? "");
  if (value.length <= max) return value;
  return value.slice(0, Math.max(0, max - 1)) + "...";
}

function resourceInitials(type) {
  const parts = String(type || "rs").split("_").filter(Boolean);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

function nodeMap() {
  return new Map((state.graph?.nodes || []).map(node => [node.id, node]));
}

function visibleIds() {
  if (!state.graph) return [];
  const ids = new Set(state.graph.roots || []);
  const nodes = nodeMap();
  const visit = id => {
    const node = nodes.get(id);
    if (!node || !state.expanded.has(id)) return;
    for (const dep of node.dependencies || []) {
      ids.add(dep);
      visit(dep);
    }
  };
  for (const root of Array.from(ids)) visit(root);
  return Array.from(ids);
}

function depthFor(id, nodes, memo = new Map()) {
  if (memo.has(id)) return memo.get(id);
  const node = nodes.get(id);
  if (!node || !node.dependents?.length) {
    memo.set(id, 0);
    return 0;
  }
  const depth = 1 + Math.max(...node.dependents.map(dep => depthFor(dep, nodes, memo)));
  memo.set(id, depth);
  return depth;
}

function layout(ids) {
  const nodes = nodeMap();
  const columns = new Map();
  const memo = new Map();
  const nodeWidth = 252;
  const nodeHeight = 96;
  const colGap = 118;
  const rowGap = 34;
  const margin = 54;
  for (const id of ids) {
    const depth = depthFor(id, nodes, memo);
    if (!columns.has(depth)) columns.set(depth, []);
    columns.get(depth).push(id);
  }
  const positions = new Map();
  const maxRows = Math.max(1, ...Array.from(columns.values(), c => c.length));
  const contentHeight = maxRows * nodeHeight + Math.max(0, maxRows - 1) * rowGap;
  const depths = Array.from(columns.keys()).sort((a, b) => a - b);
  for (const depth of depths) {
    const columnIds = columns.get(depth);
    columnIds.sort();
    const columnHeight = columnIds.length * nodeHeight + Math.max(0, columnIds.length - 1) * rowGap;
    const yOffset = (contentHeight - columnHeight) / 2;
    columnIds.forEach((id, index) => {
      positions.set(id, {
        x: margin + depth * (nodeWidth + colGap),
        y: margin + yOffset + index * (nodeHeight + rowGap),
      });
    });
  }
  const maxDepth = Math.max(0, ...depths);
  let width = Math.max(960, margin * 2 + (maxDepth + 1) * nodeWidth + maxDepth * colGap);
  let height = Math.max(560, margin * 2 + contentHeight);
  return { positions, width, height, columns, nodeWidth, nodeHeight };
}

function graphDefs() {
  return '<defs>' +
    '<pattern id="graph-grid" width="28" height="28" patternUnits="userSpaceOnUse">' +
      '<path d="M 28 0 L 0 0 0 28" fill="none" stroke="#e6edf5" stroke-width="1"></path>' +
    '</pattern>' +
    '<filter id="node-shadow" x="-18%" y="-26%" width="136%" height="160%">' +
      '<feDropShadow dx="0" dy="8" stdDeviation="10" flood-color="#22324a" flood-opacity=".14"></feDropShadow>' +
    '</filter>' +
    '<marker id="arrow" viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">' +
      '<path d="M 0 0 L 10 5 L 0 10 z" fill="#8fa1b5"></path>' +
    '</marker>' +
    '<marker id="arrow-changed" viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">' +
      '<path d="M 0 0 L 10 5 L 0 10 z" fill="#b7791f"></path>' +
    '</marker>' +
  '</defs>';
}

function edgePath(from, to, nodeWidth, nodeHeight) {
  const x1 = from.x + nodeWidth;
  const y1 = from.y + nodeHeight / 2;
  const x2 = to.x;
  const y2 = to.y + nodeHeight / 2;
  const curve = Math.max(70, Math.abs(x2 - x1) * .48);
  return "M " + x1 + " " + y1 + " C " + (x1 + curve) + " " + y1 + ", " + (x2 - curve) + " " + y2 + ", " + x2 + " " + y2;
}

function render() {
  const graph = state.graph;
  els.svg.innerHTML = graphDefs();
  if (!graph || graph.nodes.length === 0) {
    els.empty.classList.remove("closed");
    els.meta.textContent = "No managed resources";
    return;
  }
  els.empty.classList.add("closed");
  els.meta.textContent = graph.nodes.length + " resources, " + graph.edges.length + " edges";
  const nodes = nodeMap();
  const ids = visibleIds();
  const visible = new Set(ids);
  const { positions, width, height, columns, nodeWidth, nodeHeight } = layout(ids);
  els.svg.setAttribute("viewBox", "0 0 " + width + " " + height);

  const bg = document.createElementNS("http://www.w3.org/2000/svg", "rect");
  bg.setAttribute("class", "graph-bg");
  bg.setAttribute("width", width);
  bg.setAttribute("height", height);
  els.svg.appendChild(bg);

  const guideLayer = document.createElementNS("http://www.w3.org/2000/svg", "g");
  els.svg.appendChild(guideLayer);
  for (const depth of Array.from(columns.keys()).sort((a, b) => a - b)) {
    const x = positions.get(columns.get(depth)[0])?.x + nodeWidth / 2;
    if (!Number.isFinite(x)) continue;
    const guide = document.createElementNS("http://www.w3.org/2000/svg", "line");
    guide.setAttribute("class", "column-guide");
    guide.setAttribute("x1", x);
    guide.setAttribute("x2", x);
    guide.setAttribute("y1", 36);
    guide.setAttribute("y2", height - 36);
    guideLayer.appendChild(guide);
  }

  const edgeLayer = document.createElementNS("http://www.w3.org/2000/svg", "g");
  els.svg.appendChild(edgeLayer);
  for (const edge of graph.edges) {
    if (!visible.has(edge.from) || !visible.has(edge.to)) continue;
    const from = positions.get(edge.from);
    const to = positions.get(edge.to);
    if (!from || !to) continue;
    const changed = Boolean(nodes.get(edge.from)?.change || nodes.get(edge.to)?.change);
    const d = edgePath(from, to, nodeWidth, nodeHeight);
    const halo = document.createElementNS("http://www.w3.org/2000/svg", "path");
    halo.setAttribute("class", "edge-halo");
    halo.setAttribute("d", d);
    edgeLayer.appendChild(halo);
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
    path.setAttribute("class", "edge");
    if (changed) path.classList.add("changed");
    path.setAttribute("marker-end", changed ? "url(#arrow-changed)" : "url(#arrow)");
    path.setAttribute("d", d);
    edgeLayer.appendChild(path);
  }

  const nodeLayer = document.createElementNS("http://www.w3.org/2000/svg", "g");
  els.svg.appendChild(nodeLayer);
  for (const id of ids) {
    const node = nodes.get(id);
    const pos = positions.get(id);
    const g = document.createElementNS("http://www.w3.org/2000/svg", "g");
    const actionClass = (node.change?.action || "").toLowerCase();
    g.setAttribute("class", "node " + (state.selected === id ? "selected" : "") + " " + actionClass);
    g.setAttribute("transform", "translate(" + pos.x + "," + pos.y + ")");
    g.setAttribute("tabindex", "0");
    g.setAttribute("role", "button");
    g.setAttribute("aria-label", node.address);
    g.dataset.id = id;
    const expanded = state.expanded.has(id);
    const changeLabel = node.change ? node.change.action : "Config";
    const expandLabel = node.dependencies.length > 0 ? (expanded ? "-" : "+") + node.dependencies.length : "";
    g.innerHTML =
      '<rect class="card" width="' + nodeWidth + '" height="' + nodeHeight + '" rx="8"></rect>' +
      '<rect class="accent" width="6" height="' + nodeHeight + '" rx="3"></rect>' +
      '<circle class="handle" cx="0" cy="' + (nodeHeight / 2) + '" r="5"></circle>' +
      '<circle class="handle" cx="' + nodeWidth + '" cy="' + (nodeHeight / 2) + '" r="5"></circle>' +
      '<circle class="resource-icon" cx="29" cy="30" r="15"></circle>' +
      '<text class="resource-icon-text" x="29" y="34" text-anchor="middle">' + escapeText(resourceInitials(node.type)) + '</text>' +
      '<text class="title" x="54" y="27">' + escapeText(truncateText(node.name, 23)) + '</text>' +
      '<text class="address mono" x="54" y="45">' + escapeText(truncateText(node.address, 31)) + '</text>' +
      '<text class="type" x="18" y="73">' + escapeText(truncateText(node.type, 24)) + '</text>' +
      '<text class="count" x="18" y="88">' + node.dependency_count + ' deps, ' + node.inputs.length + ' inputs</text>' +
      '<rect class="badge" x="' + (nodeWidth - 78) + '" y="' + (nodeHeight - 31) + '" width="60" height="20" rx="8"></rect>' +
      '<text class="badge-text" x="' + (nodeWidth - 48) + '" y="' + (nodeHeight - 17) + '" text-anchor="middle">' + escapeText(truncateText(changeLabel, 8)) + '</text>' +
      (expandLabel ? '<rect class="expand" x="' + (nodeWidth - 39) + '" y="14" width="24" height="22" rx="7"></rect><text class="expand-text" x="' + (nodeWidth - 27) + '" y="29" text-anchor="middle">' + escapeText(expandLabel) + '</text>' : '');
    g.addEventListener("click", () => {
      state.selected = id;
      if (node.dependencies.length > 0) {
        if (state.expanded.has(id)) state.expanded.delete(id);
        else state.expanded.add(id);
      }
      showNode(id);
      render();
    });
    g.addEventListener("keydown", event => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        g.dispatchEvent(new MouseEvent("click"));
      }
    });
    nodeLayer.appendChild(g);
  }
}

function showNode(id) {
  const node = nodeMap().get(id);
  if (!node) return;
  els.sidebar.classList.remove("closed");
  els.sideTitle.textContent = node.address;
  const attrs = node.attributes.map(attr =>
    '<div class="field">' +
      '<label class="label">' + escapeText(attr.name) + '</label>' +
      '<input class="attr-input mono" data-attr="' + escapeText(attr.name) + '" value="' + escapeText(attr.expression) + '">' +
    '</div>'
  ).join("") || '<div class="value">No direct attributes found.</div>';
  const inputs = node.inputs.map(input => '<span class="pill mono">' + escapeText(input.kind) + ': ' + escapeText(input.address) + '</span>').join("") || '<div class="value">No non-managed inputs found.</div>';
  const deps = node.dependencies.map(dep => '<span class="pill mono">' + escapeText(dep) + '</span>').join("") || '<div class="value">No managed dependencies.</div>';
  const change = node.change ? '<div class="field"><span class="label">Planned Change</span><div class="value mono">' + escapeText(node.change.action) + ' ' + escapeText((node.change.instances || []).join(", ")) + '</div></div>' : "";
  els.sideBody.innerHTML =
    '<div class="field"><span class="label">Address</span><div class="value mono">' + escapeText(node.address) + '</div></div>' +
    '<div class="field"><span class="label">Source</span><div class="value mono">' + escapeText(node.source_range.filename || "") + ':' + (node.source_range.start_line || "") + '</div></div>' +
    change +
    '<div class="section-title">Attributes</div>' +
    attrs +
    '<div class="section-title">Managed Dependencies</div>' +
    deps +
    '<div class="section-title">Non-Managed Inputs</div>' +
    inputs;
  for (const input of els.sideBody.querySelectorAll(".attr-input")) {
    input.addEventListener("change", async () => {
      await runEdit(node.address, input.dataset.attr, input.value);
    });
  }
}

function showStatus(message) {
  els.status.textContent = message;
  els.status.classList.remove("closed");
}

async function loadGraph() {
  const res = await fetch("/api/graph");
  state.graph = await res.json();
  state.expanded = new Set(state.graph.roots || []);
  render();
}

async function runPlan() {
  showStatus("Planning with refresh...");
  const res = await fetch("/api/plan", { method: "POST" });
  const body = await res.json();
  state.graph = body.graph;
  if (!res.ok) {
    const message = (body.diagnostics || []).map(d => d.summary).join("; ") || "Plan failed";
    showStatus(message);
  } else {
    showStatus(body.empty ? "Plan complete: no changes." : "Plan complete: changed resources are color coded.");
  }
  render();
  if (state.selected) showNode(state.selected);
}

async function runEdit(address, attribute, expression) {
  showStatus("Planning draft edit...");
  const res = await fetch("/api/edit", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ address, attribute, expression }),
  });
  const body = await res.json();
  if (body.graph) state.graph = body.graph;
  if (!res.ok) {
    const message = body.error || (body.diagnostics || []).map(d => d.summary).join("; ") || "Draft edit failed";
    showStatus(message);
  } else {
    showStatus(body.empty ? "Draft plan complete: no changes." : "Draft plan complete: changed resources are color coded.");
  }
  render();
  if (state.selected) showNode(state.selected);
}

async function runApply() {
  showStatus("Applying last server plan...");
  const res = await fetch("/api/apply", { method: "POST" });
  const body = await res.json();
  if (!res.ok) {
    showStatus(body.error || "Apply failed");
    return;
  }
  showStatus("Apply complete.");
  await loadGraph();
}

els.refresh.addEventListener("click", loadGraph);
els.plan.addEventListener("click", runPlan);
els.apply.addEventListener("click", runApply);
els.collapse.addEventListener("click", () => {
  state.expanded = new Set(state.graph?.roots || []);
  render();
});
els.close.addEventListener("click", () => {
  els.sidebar.classList.add("closed");
  state.selected = null;
  render();
});

loadGraph().catch(err => {
  showStatus("Failed to load graph: " + err.message);
});
</script>
</body>
</html>`
