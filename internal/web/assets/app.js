"use strict";

/* ===================== Theme ===================== */
const THEMES = [
  { id: "light", color: "#4f46e5" },
  { id: "dark", color: "#6366f1" },
  { id: "midnight", color: "#38bdf8" },
  { id: "emerald", color: "#0d9488" },
];

function applyTheme(id) {
  document.documentElement.dataset.theme = id;
  localStorage.setItem("pg_theme", id);
  renderThemeDots();
}
function renderThemeDots() {
  const cur = localStorage.getItem("pg_theme") || "light";
  document.getElementById("themeDots").innerHTML = THEMES.map(
    (t) => `<button data-theme-btn="${t.id}" class="${t.id === cur ? "sel" : ""}" style="background:${t.color}" title="${t.id}"></button>`
  ).join("");
}

/* ===================== Token ===================== */
const getToken = () => localStorage.getItem("pg_token") || "";
const setToken = (t) => localStorage.setItem("pg_token", t);

function openTokenModal() {
  modal("توکن دسترسی API", `
    <div class="field">
      <label>Bearer Token</label>
      <input id="tokenInput" type="password" placeholder="PANEL_API_TOKEN" value="${getToken()}" />
      <p class="muted" style="font-size:12.5px;margin-top:8px">توکن در مرورگر شما ذخیره می‌شود و در هدر Authorization ارسال می‌گردد.</p>
    </div>
    <div class="actions">
      <button class="btn primary" id="saveToken">ذخیره</button>
      <button class="btn ghost" id="cancelToken">انصراف</button>
    </div>`, () => {
    document.getElementById("saveToken").onclick = () => {
      setToken(document.getElementById("tokenInput").value.trim());
      closeModal();
      toast("توکن ذخیره شد");
      route();
    };
    document.getElementById("cancelToken").onclick = closeModal;
  });
}

/* ===================== API client ===================== */
async function api(path, opts = {}) {
  const token = getToken();
  const res = await fetch(path, {
    ...opts,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: "Bearer " + token } : {}),
      ...(opts.headers || {}),
    },
  });
  if (res.status === 401) {
    openTokenModal();
    throw new Error("توکن نامعتبر یا غایب است");
  }
  if (!res.ok) {
    let msg = "خطا " + res.status;
    try { msg = (await res.json()).error || msg; } catch (_) {}
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

/* ===================== UI helpers ===================== */
function toast(msg, type = "ok") {
  const wrap = document.getElementById("toastWrap");
  const t = document.createElement("div");
  t.className = "toast " + type;
  t.textContent = msg;
  wrap.appendChild(t);
  setTimeout(() => t.remove(), 3800);
}
function modal(title, bodyHTML, onMount) {
  const root = document.getElementById("modalRoot");
  root.innerHTML = `<div class="modal-bg"><div class="modal"><h3>${title}</h3><div>${bodyHTML}</div></div></div>`;
  const bg = root.firstElementChild;
  bg.addEventListener("click", (e) => { if (e.target === bg) closeModal(); });
  if (onMount) onMount(root);
}
const closeModal = () => (document.getElementById("modalRoot").innerHTML = "");

const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
function fmtBytes(n) {
  n = Number(n || 0);
  if (n <= 0) return "0";
  const u = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return (n / Math.pow(1024, i)).toFixed(i ? 1 : 0) + " " + u[i];
}
function fmtDate(sec) {
  if (!sec) return "—";
  try { return new Date(sec * 1000).toLocaleString("fa-IR"); } catch { return String(sec); }
}
// daysLeftLabel returns a small colored " (N روز)" suffix for an expiry epoch.
function daysLeftLabel(endAt) {
  if (!endAt) return "";
  const days = Math.floor((endAt - Date.now() / 1000) / 86400);
  if (days < 0) return ` <span style="color:var(--danger);font-size:12px">(منقضی)</span>`;
  const col = days <= 3 ? "var(--danger)" : days <= 7 ? "var(--warn)" : "var(--muted, #94a3b8)";
  return ` <span style="color:${col};font-size:12px">(${days} روز)</span>`;
}
const STATUS_FA = { active: "فعال", suspended: "معلق", expired: "منقضی", online: "آنلاین", offline: "آفلاین", unknown: "نامشخص" };
const badge = (s) => `<span class="badge ${esc(s)}">${STATUS_FA[s] || esc(s)}</span>`;
function progress(used, quota) {
  const pct = quota > 0 ? Math.min(100, Math.round((used / quota) * 100)) : (used > 0 ? 100 : 0);
  const col = pct >= 100 ? "var(--danger)" : pct >= 80 ? "var(--warn)" : "var(--ok)";
  return `<div style="background:var(--surface-2);border-radius:999px;height:8px;overflow:hidden;min-width:120px">
    <div style="width:${pct}%;height:100%;background:${col}"></div></div>
    <div class="muted" style="font-size:12px;margin-top:3px">${fmtBytes(used)} / ${fmtBytes(quota)} (${pct}%)</div>`;
}

/* ===================== Navigation / router ===================== */
const ICONS = {
  dashboard: '<path d="M3 13h8V3H3zM13 21h8V3h-8zM3 21h8v-6H3z"/>',
  nodes: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/>',
  customers: '<circle cx="9" cy="8" r="3.5"/><path d="M3 20c0-3 3-5 6-5s6 2 6 5"/><path d="M16 8a3 3 0 0 1 0 6"/>',
  plans: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M8 4v16"/>',
  subscriptions: '<path d="M4 7h16M4 12h16M4 17h10"/>',
  webhooks: '<circle cx="6" cy="18" r="2.5"/><circle cx="12" cy="6" r="2.5"/><circle cx="18" cy="14" r="2.5"/><path d="M8 16l3-7M14 7l3 5"/>',
};
const NAV = [
  { id: "dashboard", label: "داشبورد", render: viewDashboard },
  { id: "nodes", label: "نودها", render: viewNodes },
  { id: "customers", label: "مشتری‌ها", render: viewCustomers },
  { id: "plans", label: "پلن‌ها", render: viewPlans },
  { id: "subscriptions", label: "اشتراک‌ها", render: viewSubscriptions },
  { id: "webhooks", label: "وب‌هوک‌ها", render: viewWebhooks },
];

function renderNav() {
  document.getElementById("nav").innerHTML = NAV.map(
    (n) => `<button class="nav-item" data-route="${n.id}">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${ICONS[n.id]}</svg>
      ${n.label}</button>`
  ).join("");
}

async function route() {
  const id = (location.hash.replace(/^#\/?/, "").split("?")[0]) || "dashboard";
  const item = NAV.find((n) => n.id === id) || NAV[0];
  document.querySelectorAll(".nav-item[data-route]").forEach((b) =>
    b.classList.toggle("active", b.dataset.route === item.id));
  document.getElementById("pageTitle").textContent = item.label;
  document.getElementById("sidebar").classList.remove("open");
  const view = document.getElementById("view");
  view.innerHTML = `<div class="empty">در حال بارگذاری…</div>`;
  try {
    await item.render(view);
  } catch (e) {
    view.innerHTML = `<div class="card"><p class="muted">${esc(e.message)}</p>
      <button class="btn primary" onclick="openTokenModal()">تنظیم توکن</button></div>`;
  }
}

/* ===================== Bootstrap ===================== */
function boot() {
  applyTheme(localStorage.getItem("pg_theme") || "light");
  renderNav();
  document.getElementById("nav").addEventListener("click", (e) => {
    const b = e.target.closest(".nav-item[data-route]");
    if (b) location.hash = "#/" + b.dataset.route;
  });
  document.getElementById("themeDots").addEventListener("click", (e) => {
    const b = e.target.closest("[data-theme-btn]");
    if (b) applyTheme(b.dataset.themeBtn);
  });
  document.getElementById("tokenBtn").onclick = openTokenModal;
  document.getElementById("docsBtn").onclick = () => window.open("/docs", "_blank");
  document.getElementById("hamburger").onclick = () =>
    document.getElementById("sidebar").classList.toggle("open");
  window.addEventListener("hashchange", route);
  if (!getToken()) openTokenModal();
  route();
}
document.addEventListener("DOMContentLoaded", boot);

/* ===================== Shared view helpers ===================== */
function sectionHead(title, actionsHTML) {
  return `<div class="section-head"><h2>${esc(title)}</h2><div class="spacer"></div>${actionsHTML || ""}</div>`;
}
function statCard(label, value, sub) {
  return `<div class="card stat"><div class="label">${esc(label)}</div><div class="value">${esc(value)}</div><div class="sub">${esc(sub || "")}</div></div>`;
}
function table(headers, rows) {
  const hs = headers.filter((h) => h !== null);
  const head = `<thead><tr>${hs.map((h) => `<th>${h}</th>`).join("")}</tr></thead>`;
  const body = rows
    .map((r) => `<tr>${r.filter((c) => c !== null).map((c) => `<td>${c}</td>`).join("")}</tr>`)
    .join("");
  return `<table>${head}<tbody>${body}</tbody></table>`;
}

/* Generic form modal. fields: {name,label,type,placeholder,value,hint,options}.
   onSubmit(values) is awaited; the modal closes and the view re-renders on success. */
function formModal(title, fields, onSubmit, submitLabel = "ذخیره") {
  const body = fields
    .map((f) => {
      const label = `<label>${esc(f.label)}</label>`;
      const hint = f.hint ? `<p class="muted" style="font-size:12px;margin-top:6px">${esc(f.hint)}</p>` : "";
      let control;
      if (f.type === "select") {
        const opts = (f.options || [])
          .map((o) => `<option value="${esc(o.value)}" ${o.value === f.value ? "selected" : ""}>${esc(o.label)}</option>`)
          .join("");
        control = `<select data-f="${f.name}">${opts}</select>`;
      } else if (f.type === "textarea") {
        control = `<textarea data-f="${f.name}" rows="5" placeholder="${esc(f.placeholder || "")}">${esc(f.value || "")}</textarea>`;
      } else {
        control = `<input data-f="${f.name}" type="${f.type || "text"}" placeholder="${esc(f.placeholder || "")}" value="${esc(f.value ?? "")}" />`;
      }
      return `<div class="field">${label}${control}${hint}</div>`;
    })
    .join("");

  modal(
    title,
    `${body}<div class="actions"><button class="btn primary" id="fmOk">${esc(submitLabel)}</button><button class="btn ghost" id="fmCancel">انصراف</button></div>`,
    (root) => {
      root.querySelector("#fmCancel").onclick = closeModal;
      root.querySelector("#fmOk").onclick = async () => {
        const values = {};
        root.querySelectorAll("[data-f]").forEach((el) => (values[el.dataset.f] = el.value.trim()));
        const btn = root.querySelector("#fmOk");
        btn.disabled = true;
        try {
          await onSubmit(values);
          closeModal();
          route();
        } catch (e) {
          toast(e.message, "err");
          btn.disabled = false;
        }
      };
    }
  );
}

async function guard(fn) {
  try { await fn(); } catch (e) { toast(e.message, "err"); }
}
const GB = 1024 * 1024 * 1024;

// confirmModal asks for confirmation before a destructive action.
function confirmModal(message, onYes) {
  modal(
    "تایید",
    `<p style="margin:0 0 4px">${esc(message)}</p>
     <div class="actions"><button class="btn danger" id="cfYes">تایید</button><button class="btn ghost" id="cfNo">انصراف</button></div>`,
    (root) => {
      root.querySelector("#cfNo").onclick = closeModal;
      root.querySelector("#cfYes").onclick = () =>
        guard(async () => { await onYes(); closeModal(); toast("انجام شد"); route(); });
    }
  );
}

/* ===================== Dashboard ===================== */
async function viewDashboard(view) {
  const [nodes, customers, plans] = await Promise.all([
    api("/api/v1/nodes"),
    api("/api/v1/customers"),
    api("/api/v1/plans"),
  ]);
  const online = nodes.filter((n) => n.status === "online").length;

  // Gather subscriptions across customers for the alert lists.
  const custName = Object.fromEntries(customers.map((c) => [c.id, c.name]));
  const subs = [];
  for (const c of customers) {
    try { (await api(`/api/v1/customers/${c.id}/subscriptions`)).forEach((s) => subs.push(s)); } catch (_) {}
  }
  const now = Date.now() / 1000;
  const active = subs.filter((s) => s.status === "active").length;
  const expiring = subs
    .filter((s) => s.end_at && s.end_at - now > 0 && s.end_at - now <= 7 * 86400)
    .sort((a, b) => a.end_at - b.end_at);
  const nearQuota = subs
    .filter((s) => s.quota_bytes > 0 && s.used_bytes / s.quota_bytes >= 0.8)
    .sort((a, b) => b.used_bytes / b.quota_bytes - a.used_bytes / a.quota_bytes);
  const totalUsed = subs.reduce((a, s) => a + (s.used_bytes || 0), 0);

  const listCard = (title, rows) =>
    `<div class="card"><h3>${esc(title)}</h3>${
      rows.length
        ? table(["مشتری", "وضعیت", "مصرف", "انقضا"],
            rows.slice(0, 8).map((s) => [
              esc(custName[s.customer_id] || "—"),
              badge(s.status),
              progress(s.used_bytes, s.quota_bytes),
              fmtDate(s.end_at) + daysLeftLabel(s.end_at),
            ]))
        : `<div class="empty">موردی نیست</div>`
    }</div>`;

  view.innerHTML = `
    <div class="grid cols-4">
      ${statCard("نودها", nodes.length, `${online} آنلاین`)}
      ${statCard("مشتری‌ها", customers.length, "")}
      ${statCard("اشتراک‌های فعال", active, `از ${subs.length}`)}
      ${statCard("مصرف کل", fmtBytes(totalUsed), "")}
    </div>
    <div class="grid cols-2" style="margin-top:18px">
      ${listCard("رو به انقضا (۷ روز آینده)", expiring)}
      ${listCard("نزدیک به اتمام حجم (≥۸۰٪)", nearQuota)}
    </div>
    <div class="card" style="margin-top:18px">
      <h3>نودها</h3>
      ${nodesTable(nodes, false)}
    </div>`;
}

/* ===================== Nodes ===================== */
function nodesTable(nodes, withActions) {
  if (!nodes.length) return `<div class="empty">نودی ثبت نشده است</div>`;
  return table(
    ["نام", "آدرس", "وضعیت", "نسخه", "آخرین بازدید", withActions ? "عملیات" : null],
    nodes.map((n) => [
      esc(n.name),
      `<span class="mono">${esc(n.address)}</span>`,
      badge(n.status),
      esc(n.version || "—"),
      fmtDate(n.last_seen_at),
      withActions
        ? `<div class="row" style="gap:6px">
            <button class="btn sm ghost" data-detail="${n.id}">جزئیات</button>
            <button class="btn sm ghost" data-health="${n.id}">سلامت</button>
            <button class="btn sm ghost" data-cfg="${n.id}">کانفیگ</button>
            <button class="btn sm danger" data-delnode="${n.id}">حذف</button>
          </div>`
        : null,
    ])
  );
}
async function viewNodes(view) {
  const nodes = await api("/api/v1/nodes");
  view.innerHTML =
    sectionHead("نودها", `<button class="btn primary" id="addNode">+ ثبت نود</button>`) +
    `<div class="card">${nodesTable(nodes, true)}</div>`;
  document.getElementById("addNode").onclick = nodeModal;
  view.querySelectorAll("[data-health]").forEach((b) => {
    b.onclick = () =>
      guard(async () => {
        const h = await api(`/api/v1/nodes/${b.dataset.health}/health`);
        toast(`هسته ${h.core_version || "?"} — ${h.core_started ? "فعال" : "خاموش"}`);
        route();
      });
  });
  view.querySelectorAll("[data-delnode]").forEach((b) => {
    b.onclick = () => confirmModal("این نود حذف شود؟", () => api(`/api/v1/nodes/${b.dataset.delnode}`, { method: "DELETE" }));
  });
  view.querySelectorAll("[data-cfg]").forEach((b) => {
    b.onclick = () => guard(() => nodeConfigModal(b.dataset.cfg));
  });
  view.querySelectorAll("[data-detail]").forEach((b) => {
    b.onclick = () => guard(() => nodeDetailModal(b.dataset.detail));
  });
}

// nodeDetailModal shows a node's connection info (host, ports, gRPC address and
// certificate) with copy buttons, plus a shortcut to the Xray config editor.
async function nodeDetailModal(nodeID) {
  const d = await api(`/api/v1/nodes/${nodeID}`);
  const field = (label, value, btnId) =>
    `<div class="field"><label>${esc(label)}</label>
      <div class="row" style="gap:6px;align-items:stretch">
        <input readonly value="${esc(value)}" style="flex:1" />
        ${btnId ? `<button class="btn ghost" id="${btnId}">کپی</button>` : ""}
      </div></div>`;
  modal(
    "جزئیات نود — " + esc(d.name),
    `${field("IP / هاست", d.host, "cpHost")}
     ${field("آدرس سرویس (کنترل، HTTPS)", d.address, "cpAddr")}
     ${field("پورت سرویس (HTTP)", d.service_port)}
     ${field("آدرس gRPC (برای پنل مشتری)", d.grpc_address, "cpGrpc")}
     ${field("پورت gRPC", d.grpc_port)}
     ${field("پروتکل", d.protocol)}
     ${field("وضعیت", (STATUS_FA[d.status] || d.status) + (d.version ? " — " + d.version : ""))}
     <div class="field"><label>گواهی نود (Certificate)</label>
       <textarea id="dCert" rows="6" readonly spellcheck="false" style="font-family:ui-monospace,monospace;font-size:12px">${esc(d.cert_pem || "—")}</textarea></div>
     <p class="muted" style="font-size:12px;margin:0 0 8px">این مقادیر را برای افزودن نود در پنل PasarGuard مشتری استفاده کن. کانفیگ Xray را با دکمه‌ی زیر ببین/ویرایش کن.</p>
     <div class="actions">
       <button class="btn primary" id="dCfg">کانفیگ Xray</button>
       <button class="btn ghost" id="dCertCopy">کپی گواهی</button>
       <button class="btn ghost" id="dClose">بستن</button>
     </div>`,
    (root) => {
      const copy = (t) => { if (navigator.clipboard) navigator.clipboard.writeText(t); toast("کپی شد"); };
      root.querySelector("#cpHost").onclick = () => copy(d.host);
      root.querySelector("#cpAddr").onclick = () => copy(d.address);
      root.querySelector("#cpGrpc").onclick = () => copy(d.grpc_address);
      root.querySelector("#dCertCopy").onclick = () => copy(d.cert_pem || "");
      root.querySelector("#dClose").onclick = closeModal;
      root.querySelector("#dCfg").onclick = () => { closeModal(); guard(() => nodeConfigModal(nodeID)); };
    }
  );
}

// nodeConfigModal lets the operator edit and push a node's fixed Xray config.
async function nodeConfigModal(nodeID) {
  const res = await fetch(`/api/v1/nodes/${nodeID}/config`, {
    headers: { Authorization: "Bearer " + getToken() },
  });
  if (res.status === 401) { openTokenModal(); return; }
  const current = await res.text();
  const pretty = (() => { try { return JSON.stringify(JSON.parse(current), null, 2); } catch { return current; } })();

  modal(
    "کانفیگ هسته‌ی نود",
    `<p class="muted" style="font-size:12.5px;margin:0 0 8px">کانفیگ ثابت Xray (JSON). با اعمال، روی نود push و هسته راه‌اندازی می‌شود.</p>
     <div class="field"><textarea id="cfgEditor" rows="16" spellcheck="false" style="font-family:ui-monospace,monospace;font-size:12.5px">${esc(pretty)}</textarea></div>
     <div class="actions"><button class="btn primary" id="cfgApply">اعمال روی نود</button><button class="btn ghost" id="cfgCancel">انصراف</button></div>`,
    (root) => {
      root.querySelector("#cfgCancel").onclick = closeModal;
      root.querySelector("#cfgApply").onclick = () =>
        guard(async () => {
          const raw = root.querySelector("#cfgEditor").value.trim();
          if (!raw) throw new Error("کانفیگ خالی است");
          try { JSON.parse(raw); } catch { throw new Error("JSON نامعتبر است"); }
          const r = await fetch(`/api/v1/nodes/${nodeID}/config`, {
            method: "PUT",
            headers: { "Content-Type": "application/json", Authorization: "Bearer " + getToken() },
            body: raw,
          });
          if (!r.ok) {
            let msg = "خطا " + r.status;
            try { msg = (await r.json()).error || msg; } catch (_) {}
            throw new Error(msg);
          }
          closeModal();
          toast("کانفیگ روی نود اعمال شد");
          route();
        });
    }
  );
}
function nodeModal() {
  formModal(
    "ثبت نود",
    [
      { name: "name", label: "نام" },
      { name: "address", label: "آدرس", placeholder: "https://1.2.3.4:8090" },
      { name: "master_key", label: "کلید مستر", type: "password" },
      { name: "grpc_port", label: "پورت gRPC (پیش‌فرض 62050)", type: "number", value: "62050" },
      { name: "cert_pem", label: "گواهی نود (PEM، اختیاری)", type: "textarea", hint: "خالی بگذارید تا پنل خودکار گواهی نود را دریافت و pin کند (TOFU)." },
      { name: "config", label: "کانفیگ ثابت Xray (JSON، اختیاری)", type: "textarea", hint: "در صورت ورود، روی نود اعمال و هسته راه‌اندازی می‌شود." },
    ],
    async (v) => {
      const body = { name: v.name, address: v.address, master_key: v.master_key };
      if (v.grpc_port) body.grpc_port = parseInt(v.grpc_port, 10);
      if (v.cert_pem) body.cert_pem = v.cert_pem;
      if (v.config) {
        try { body.config = JSON.parse(v.config); } catch { throw new Error("کانفیگ JSON نامعتبر است"); }
      }
      await api("/api/v1/nodes", { method: "POST", body: JSON.stringify(body) });
      toast("نود ثبت شد");
    }
  );
}

/* ===================== Customers ===================== */
async function viewCustomers(view) {
  const customers = await api("/api/v1/customers");
  view.innerHTML =
    sectionHead("مشتری‌ها", `<button class="btn primary" id="addCust">+ مشتری جدید</button>`) +
    `<div class="card">${
      customers.length
        ? table(
            ["نام", "وضعیت", "شناسه در ربات", "ساخته‌شده", "عملیات"],
            customers.map((c) => [
              esc(c.name),
              badge(c.status),
              `<span class="mono">${esc(c.external_ref || "—")}</span>`,
              fmtDate(c.created_at),
              `<div class="row" style="gap:6px">
                <button class="btn sm ghost" data-cust="${c.id}">جزئیات</button>
                ${c.status === "disabled"
                  ? `<button class="btn sm ghost" data-encust="${c.id}">فعال‌سازی</button>`
                  : `<button class="btn sm ghost" data-discust="${c.id}">غیرفعال</button>`}
                <button class="btn sm danger" data-delcust="${c.id}">حذف</button>
              </div>`,
            ])
          )
        : `<div class="empty">مشتری‌ای ثبت نشده است</div>`
    }</div>`;
  document.getElementById("addCust").onclick = () =>
    formModal(
      "مشتری جدید",
      [
        { name: "name", label: "نام" },
        { name: "external_ref", label: "شناسه در ربات فروش (اختیاری)" },
      ],
      async (v) => {
        await api("/api/v1/customers", { method: "POST", body: JSON.stringify(v) });
        toast("مشتری ساخته شد");
      }
    );
  view.querySelectorAll("[data-cust]").forEach((b) => (b.onclick = () => guard(() => customerDetail(b.dataset.cust))));
  view.querySelectorAll("[data-encust]").forEach((b) =>
    (b.onclick = () => guard(async () => { await api(`/api/v1/customers/${b.dataset.encust}/enable`, { method: "POST" }); toast("فعال شد"); route(); })));
  view.querySelectorAll("[data-discust]").forEach((b) =>
    (b.onclick = () => guard(async () => { await api(`/api/v1/customers/${b.dataset.discust}/disable`, { method: "POST" }); toast("غیرفعال شد"); route(); })));
  view.querySelectorAll("[data-delcust]").forEach((b) =>
    (b.onclick = () => confirmModal("این مشتری و همه‌ی اشتراک‌هایش حذف شوند؟", () => api(`/api/v1/customers/${b.dataset.delcust}`, { method: "DELETE" }))));
}
async function customerDetail(id) {
  const [usage, subs] = await Promise.all([
    api(`/api/v1/customers/${id}/usage`),
    api(`/api/v1/customers/${id}/subscriptions`),
  ]);
  modal(
    "جزئیات مشتری",
    `<div class="row" style="gap:24px;margin-bottom:16px">
      <div><div class="muted" style="font-size:12px">مصرف کل</div><b>${fmtBytes(usage.used_bytes)} / ${fmtBytes(usage.quota_bytes)}</b></div>
      <div><div class="muted" style="font-size:12px">مصرف اضافه</div><b style="color:var(--danger)">${fmtBytes(usage.overage_bytes)}</b></div>
    </div>
    ${
      subs.length
        ? table(
            ["وضعیت", "مصرف", "انقضا", ""],
            subs.map((s) => [
              badge(s.status),
              progress(s.used_bytes, s.quota_bytes),
              fmtDate(s.end_at),
              s.sub_token ? `<button class="btn sm ghost" onclick="subLinkModal('${esc(s.sub_token)}')">لینک</button>` : "",
            ])
          )
        : `<div class="empty">اشتراکی ندارد</div>`
    }
    <div class="actions"><button class="btn ghost" id="cdClose">بستن</button></div>`,
    (root) => (root.querySelector("#cdClose").onclick = closeModal)
  );
}

/* ===================== Plans ===================== */
async function viewPlans(view) {
  const plans = await api("/api/v1/plans");
  view.innerHTML =
    sectionHead("پلن‌ها", `<button class="btn primary" id="addPlan">+ پلن جدید</button>`) +
    `<div class="card">${
      plans.length
        ? table(
            ["نام", "حجم", "مدت (روز)", "سقف کاربر", "ساخته‌شده", "عملیات"],
            plans.map((p) => [
              esc(p.name),
              fmtBytes(p.quota_bytes),
              esc(p.duration_days),
              esc(p.max_users ? p.max_users : "∞"),
              fmtDate(p.created_at),
              `<button class="btn sm danger" data-delplan="${p.id}">حذف</button>`,
            ])
          )
        : `<div class="empty">پلنی تعریف نشده است</div>`
    }</div>`;
  document.getElementById("addPlan").onclick = () =>
    formModal(
      "پلن جدید",
      [
        { name: "name", label: "نام" },
        { name: "quota_gb", label: "حجم (گیگابایت)", type: "number", placeholder: "100" },
        { name: "duration_days", label: "مدت (روز)", type: "number", placeholder: "30" },
        { name: "max_users", label: "سقف کاربر (۰ = نامحدود)", type: "number", value: "0" },
      ],
      async (v) => {
        const quota_bytes = Math.round(parseFloat(v.quota_gb || "0") * GB);
        if (quota_bytes <= 0) throw new Error("حجم باید بزرگ‌تر از صفر باشد");
        await api("/api/v1/plans", {
          method: "POST",
          body: JSON.stringify({
            name: v.name,
            quota_bytes,
            duration_days: parseInt(v.duration_days || "0", 10),
            max_users: parseInt(v.max_users || "0", 10),
          }),
        });
        toast("پلن ساخته شد");
      }
    );
  view.querySelectorAll("[data-delplan]").forEach((b) =>
    (b.onclick = () => confirmModal("این پلن حذف شود؟", () => api(`/api/v1/plans/${b.dataset.delplan}`, { method: "DELETE" }))));
}

/* ===================== Subscriptions ===================== */
function subActions(s) {
  const toggle =
    s.status === "suspended"
      ? `<button class="btn sm ghost" data-resume="${s.id}">فعال‌سازی</button>`
      : `<button class="btn sm ghost" data-suspend="${s.id}">تعلیق</button>`;
  return `<div class="row" style="gap:6px">${toggle}
    <button class="btn sm ghost" data-conn="${s.id}">اتصال مشتری</button>
    ${s.sub_token ? `<button class="btn sm ghost" data-sublink="${s.sub_token}">لینک</button>` : ""}
    <button class="btn sm ghost" data-topup="${s.id}">شارژ حجم</button>
    <button class="btn sm ghost" data-renew="${s.id}">تمدید</button>
    <button class="btn sm danger" data-delsub="${s.id}">حذف</button></div>`;
}
async function viewSubscriptions(view) {
  const [customers, nodes, plans] = await Promise.all([
    api("/api/v1/customers"),
    api("/api/v1/nodes"),
    api("/api/v1/plans"),
  ]);
  const nodeName = Object.fromEntries(nodes.map((n) => [n.id, n.name]));
  const planName = Object.fromEntries(plans.map((p) => [p.id, p.name]));
  const all = [];
  for (const c of customers) {
    const subs = await api(`/api/v1/customers/${c.id}/subscriptions`);
    subs.forEach((s) => all.push({ ...s, _customer: c.name }));
  }
  const rowsHTML = (list) =>
    list.length
      ? table(
          ["مشتری", "نود", "پلن", "وضعیت", "مصرف", "انقضا", "عملیات"],
          list.map((s) => [
            esc(s._customer),
            esc(nodeName[s.node_id] || "—"),
            esc(planName[s.plan_id] || "—"),
            badge(s.status),
            progress(s.used_bytes, s.quota_bytes),
            fmtDate(s.end_at) + daysLeftLabel(s.end_at),
            subActions(s),
          ])
        )
      : `<div class="empty">موردی یافت نشد</div>`;

  view.innerHTML =
    sectionHead("اشتراک‌ها", `<button class="btn primary" id="addSub">+ اشتراک جدید</button>`) +
    `<div class="card">
       <input id="subSearch" class="search" placeholder="جست‌وجو: مشتری / نود / پلن / وضعیت" />
       <div id="subTable">${rowsHTML(all)}</div>
     </div>`;

  const searchEl = document.getElementById("subSearch");
  searchEl.oninput = () => {
    const q = searchEl.value.trim().toLowerCase();
    const f = q
      ? all.filter((s) =>
          [s._customer, nodeName[s.node_id], planName[s.plan_id], STATUS_FA[s.status] || s.status]
            .some((x) => String(x || "").toLowerCase().includes(q)))
      : all;
    document.getElementById("subTable").innerHTML = rowsHTML(f);
    wireSubActions(view);
  };

  document.getElementById("addSub").onclick = () => guard(() => subscriptionModal(customers));
  wireSubActions(view);
}

// wireSubActions binds the per-row action buttons (re-run after re-rendering rows).
function wireSubActions(view) {
  view.querySelectorAll("[data-suspend]").forEach((b) =>
    (b.onclick = () => guard(async () => { await api(`/api/v1/subscriptions/${b.dataset.suspend}/suspend`, { method: "POST" }); toast("اشتراک معلق شد"); route(); }))
  );
  view.querySelectorAll("[data-resume]").forEach((b) =>
    (b.onclick = () => guard(async () => { await api(`/api/v1/subscriptions/${b.dataset.resume}/resume`, { method: "POST" }); toast("اشتراک فعال شد"); route(); }))
  );
  view.querySelectorAll("[data-topup]").forEach((b) =>
    (b.onclick = () =>
      formModal("شارژ حجم", [{ name: "gb", label: "افزودن حجم (گیگابایت)", type: "number", placeholder: "50" }], async (v) => {
        const add_bytes = Math.round(parseFloat(v.gb || "0") * GB);
        if (add_bytes <= 0) throw new Error("حجم باید بزرگ‌تر از صفر باشد");
        await api(`/api/v1/subscriptions/${b.dataset.topup}/topup-quota`, { method: "POST", body: JSON.stringify({ add_bytes }) });
        toast("حجم شارژ شد");
      }))
  );
  view.querySelectorAll("[data-renew]").forEach((b) =>
    (b.onclick = () => confirmModal("اشتراک تمدید شود؟ (مصرف صفر و انقضا تمدید می‌شود)", () => api(`/api/v1/subscriptions/${b.dataset.renew}/renew`, { method: "POST" }))));
  view.querySelectorAll("[data-conn]").forEach((b) =>
    (b.onclick = () => guard(() => connectionModal(b.dataset.conn))));
  view.querySelectorAll("[data-sublink]").forEach((b) =>
    (b.onclick = () => subLinkModal(b.dataset.sublink)));
  view.querySelectorAll("[data-delsub]").forEach((b) =>
    (b.onclick = () => confirmModal("این اشتراک حذف و از نود حذف شود؟", () => api(`/api/v1/subscriptions/${b.dataset.delsub}`, { method: "DELETE" }))));
}

// subLinkModal shows the public subscription link with copy/open actions.
function subLinkModal(token) {
  const link = location.origin + "/sub/" + token;
  modal(
    "لینک اشتراک مشتری",
    `<p class="muted" style="font-size:12.5px;margin:0 0 8px">این لینک را به مشتری بده؛ کانفیگ نود و وضعیت حجم/انقضا را می‌بیند (نیازی به توکن ادمین ندارد).</p>
     <div class="field"><input id="slv" readonly value="${esc(link)}" /></div>
     <div class="actions">
       <button class="btn primary" id="slCopy">کپی لینک</button>
       <button class="btn ghost" id="slOpen">باز کردن</button>
       <button class="btn ghost" id="slClose">بستن</button>
     </div>`,
    (root) => {
      root.querySelector("#slCopy").onclick = () => { const i = root.querySelector("#slv"); i.select(); if (navigator.clipboard) navigator.clipboard.writeText(link); toast("کپی شد"); };
      root.querySelector("#slOpen").onclick = () => window.open(link, "_blank");
      root.querySelector("#slClose").onclick = closeModal;
    }
  );
}
async function subscriptionModal(customers) {
  const [plans, nodes] = await Promise.all([api("/api/v1/plans"), api("/api/v1/nodes")]);
  if (!customers.length || !plans.length || !nodes.length) {
    toast("ابتدا باید مشتری، پلن و نود داشته باشید", "err");
    return;
  }
  formModal(
    "اشتراک جدید",
    [
      { name: "customer_id", label: "مشتری", type: "select", options: customers.map((c) => ({ value: c.id, label: c.name })) },
      { name: "plan_id", label: "پلن", type: "select", options: plans.map((p) => ({ value: p.id, label: `${p.name} (${fmtBytes(p.quota_bytes)})` })) },
      { name: "node_id", label: "نود", type: "select", options: nodes.map((n) => ({ value: n.id, label: `${n.name} — ${STATUS_FA[n.status] || n.status}` })) },
    ],
    async (v) => {
      const res = await api(`/api/v1/customers/${v.customer_id}/subscriptions`, {
        method: "POST",
        body: JSON.stringify({ plan_id: v.plan_id, node_id: v.node_id }),
      });
      toast("اشتراک provision شد");
      // Defer so the key modal opens after formModal closes its own modal.
      setTimeout(() => showApiKey(res.api_key, res.node_address, res.sub_token), 60);
    },
    "ساخت و provision"
  );
}
function showApiKey(key, nodeAddr, subToken) {
  const subLink = subToken ? location.origin + "/sub/" + subToken : "";
  modal(
    "کلید مشتری ساخته شد",
    `<p class="muted" style="font-size:13px">این کلید فقط همین یک‌بار نمایش داده می‌شود؛ آن را ذخیره و به مشتری بدهید.</p>
     <div class="field"><label>API Key</label><input id="akv" readonly value="${esc(key)}" /></div>
     <div class="field"><label>آدرس نود</label><input readonly value="${esc(nodeAddr)}" /></div>
     ${subLink ? `<div class="field"><label>لینک اشتراک مشتری (کانفیگ + وضعیت حجم/انقضا)</label><input id="aksub" readonly value="${esc(subLink)}" /></div>` : ""}
     <div class="actions">
       <button class="btn primary" id="akCopy">کپی کلید</button>
       ${subLink ? `<button class="btn ghost" id="akSubCopy">کپی لینک</button><button class="btn ghost" id="akSubOpen">باز کردن</button>` : ""}
       <button class="btn ghost" id="akClose">بستن</button>
     </div>`,
    (root) => {
      root.querySelector("#akCopy").onclick = () => {
        const inp = root.querySelector("#akv");
        inp.select();
        if (navigator.clipboard) navigator.clipboard.writeText(key);
        toast("کپی شد");
      };
      if (subLink) {
        root.querySelector("#akSubCopy").onclick = () => { if (navigator.clipboard) navigator.clipboard.writeText(subLink); toast("کپی شد"); };
        root.querySelector("#akSubOpen").onclick = () => window.open(subLink, "_blank");
      }
      root.querySelector("#akClose").onclick = closeModal;
    }
  );
}

// connectionModal shows everything the customer needs to add this node in their
// own PasarGuard panel: gRPC address, protocol, certificate and the node's real
// inbound(s). The inbound must be replicated exactly so end-user links work.
async function connectionModal(subID) {
  const info = await api(`/api/v1/subscriptions/${subID}/connection`);
  let inboundsPretty;
  try { inboundsPretty = JSON.stringify(info.inbounds, null, 2); }
  catch { inboundsPretty = String(info.inbounds || ""); }

  modal(
    "اطلاعات اتصال مشتری",
    `<p class="muted" style="font-size:12.5px;margin:0 0 10px">این مقادیر را به مشتری بدهید تا نود را در پنل PasarGuard خودش اضافه کند. کلید مشتری فقط هنگام ساخت اشتراک نمایش داده می‌شود.</p>
     ${info.sub_token ? `<div class="field"><label>🔗 لینک اشتراک مشتری (کانفیگ + وضعیت حجم/انقضا)</label>
       <div class="row" style="gap:6px;align-items:stretch"><input id="connSub" readonly value="${esc(location.origin + "/sub/" + info.sub_token)}" style="flex:1" /><button class="btn ghost" id="connSubOpen">باز</button></div></div>` : ""}
     <div class="field"><label>آدرس gRPC</label><input id="connAddr" readonly value="${esc(info.grpc_address)}" /></div>
     <div class="field"><label>پروتکل</label><input readonly value="${esc(info.protocol)}" /></div>
     <div class="field"><label>گواهی نود (Certificate)</label><textarea id="connCert" rows="5" readonly spellcheck="false" style="font-family:ui-monospace,monospace;font-size:12px">${esc(info.cert_pem || "—")}</textarea></div>
     <div class="field"><label>inboundها (دقیقاً همین را در پنل خود بسازید)</label><textarea id="connIn" rows="10" readonly spellcheck="false" style="font-family:ui-monospace,monospace;font-size:12px">${esc(inboundsPretty)}</textarea></div>
     <p class="muted" style="font-size:12px;margin:0 0 8px">${esc(info.note || "")}</p>
     <div class="actions">
       <button class="btn primary" id="connCopyAddr">کپی آدرس</button>
       <button class="btn ghost" id="connCopyCert">کپی گواهی</button>
       <button class="btn ghost" id="connCopyIn">کپی inbounds</button>
       ${info.sub_token ? `<button class="btn ghost" id="connCopySub">کپی لینک</button>` : ""}
       <button class="btn ghost" id="connClose">بستن</button>
     </div>`,
    (root) => {
      const copy = (text) => { if (navigator.clipboard) navigator.clipboard.writeText(text); toast("کپی شد"); };
      root.querySelector("#connCopyAddr").onclick = () => copy(info.grpc_address);
      root.querySelector("#connCopyCert").onclick = () => copy(info.cert_pem || "");
      root.querySelector("#connCopyIn").onclick = () => copy(inboundsPretty);
      if (info.sub_token) {
        const subLink = location.origin + "/sub/" + info.sub_token;
        root.querySelector("#connCopySub").onclick = () => copy(subLink);
        root.querySelector("#connSubOpen").onclick = () => window.open(subLink, "_blank");
      }
      root.querySelector("#connClose").onclick = closeModal;
    }
  );
}

/* ===================== Webhooks ===================== */
async function viewWebhooks(view) {
  const hooks = await api("/api/v1/webhooks");
  view.innerHTML =
    sectionHead("وب‌هوک‌ها", `<button class="btn primary" id="addWh">+ وب‌هوک جدید</button>`) +
    `<div class="card">${
      hooks.length
        ? table(
            ["URL", "رویدادها", "وضعیت"],
            hooks.map((h) => [`<span class="mono">${esc(h.url)}</span>`, esc(h.events), badge(h.status)])
          )
        : `<div class="empty">وب‌هوکی ثبت نشده است</div>`
    }</div>
    <div class="card" style="margin-top:16px">
      <h3>رویدادهای قابل ارسال</h3>
      <p class="muted" style="font-size:13px;line-height:2">
        <span class="mono">usage.threshold</span> (۸۰٪/۹۵٪) ·
        <span class="mono">usage.over_quota</span> ·
        <span class="mono">subscription.suspended</span> ·
        <span class="mono">subscription.resumed</span> ·
        <span class="mono">subscription.expired</span><br/>
        هر درخواست با هدر <span class="mono">X-PG-Signature: sha256=...</span> امضا می‌شود.
      </p>
    </div>`;
  document.getElementById("addWh").onclick = () =>
    formModal(
      "وب‌هوک جدید",
      [
        { name: "url", label: "URL", placeholder: "https://bot.example/webhook" },
        { name: "secret", label: "Secret (برای امضای HMAC)", type: "password" },
        { name: "events", label: "رویدادها (کاما-جدا یا *)", value: "*" },
      ],
      async (v) => {
        await api("/api/v1/webhooks", { method: "POST", body: JSON.stringify(v) });
        toast("وب‌هوک ثبت شد");
      }
    );
}
