"use strict";
(() => {
  const charts = {};
  const controllers = {};
  const money = new Intl.NumberFormat("ru-RU", {style: "currency", currency: "RUB"});
  const colors = ["#176b52", "#83bca2", "#efb965", "#849bd1", "#d38e91", "#93b9c3", "#b7a4cc"];
  function table(target, rows) {
    const t = document.createElement("table");
    const head = t.createTHead().insertRow();
    ["Период / категория", "Сумма", "Трат"].forEach(label => {
      const th = document.createElement("th"); th.scope = "col"; th.textContent = label; head.append(th);
    });
    const body = t.createTBody();
    rows.forEach(row => {
      const tr = body.insertRow();
      [row.label, money.format(row.amount / 100), row.count].forEach(value => {tr.insertCell().textContent = value;});
    });
    target.replaceChildren(t);
  }
  async function load(kind) {
    if (controllers[kind]) controllers[kind].abort();
    const controller = new AbortController(); controllers[kind] = controller;
    const form = document.getElementById(kind + "-filters");
    const msg = document.getElementById(kind + "-message");
    const target = document.getElementById(kind + "-table");
    msg.textContent = "Загружаем данные…";
    if (charts[kind]) {charts[kind].destroy(); delete charts[kind];}
    target.replaceChildren();
    try {
      const query = new URLSearchParams(new FormData(form));
      const response = await fetch("/api/stats/by-" + kind + "?" + query, {credentials: "same-origin", signal: controller.signal});
      const rows = await response.json();
      if (!response.ok) throw new Error(rows.error || "Не удалось загрузить статистику.");
      if (controller.signal.aborted) return;
      table(target, rows);
      const total = rows.reduce((sum, row) => sum + row.amount, 0);
      msg.textContent = total ? "Всего: " + money.format(total / 100) : "За выбранный период трат нет.";
      if (typeof Chart === "undefined") {msg.textContent += " Графики недоступны: проверьте подключение к интернету. Данные показаны в таблице."; return;}
      if (!total && kind === "category") return;
      charts[kind] = new Chart(document.getElementById(kind + "-chart"), {
        type: kind === "category" ? "doughnut" : "bar",
        data: {labels: rows.map(row => row.label), datasets: [{label: "Расходы, ₽", data: rows.map(row => row.amount / 100), backgroundColor: kind === "category" ? colors : "#176b52", borderWidth: 0, borderRadius: kind === "month" ? 5 : 0}]},
        options: {responsive: true, maintainAspectRatio: false, plugins: {legend: {display: kind === "category", position: "bottom"}, tooltip: {callbacks: {label: ctx => money.format(kind === "category" ? ctx.parsed : ctx.parsed.y)}}}, ...(kind === "month" ? {scales: {y: {beginAtZero: true}}} : {})}
      });
    } catch (error) {if (error.name !== "AbortError") msg.textContent = error.message || "Ошибка загрузки данных.";}
  }
  ["category", "month"].forEach(kind => {document.getElementById(kind + "-filters").addEventListener("submit", event => {event.preventDefault(); load(kind);}); load(kind);});
})();
