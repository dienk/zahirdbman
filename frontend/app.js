/* Zahir Data Console — a static frontend for a zahirdbman JSON API.
   The API base URL is provided by the user and stored in localStorage. */
(function () {
  "use strict";

  var KEY = "zdbm-api";
  // Precedence: ?api= query param (shareable link) > saved value.
  var fromQuery = new URLSearchParams(location.search).get("api");
  var base = (fromQuery || localStorage.getItem(KEY) || "").trim();
  if (fromQuery) localStorage.setItem(KEY, base);

  var $ = function (id) { return document.getElementById(id); };
  var apiInput = $("api"), dot = $("dot"), banner = $("banner");

  apiInput.value = base;

  function setDot(state) { dot.className = "dot" + (state ? " " + state : ""); }
  function showError(msg) { banner.hidden = false; banner.className = "banner err"; banner.textContent = msg; }
  function clearError() { banner.hidden = true; }

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // Fetch JSON from the API; throws on non-2xx with the server's error message.
  function api(path, opts) {
    if (!base) return Promise.reject(new Error("Set the API URL first."));
    var url = base.replace(/\/+$/, "") + path;
    return fetch(url, opts).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (body) {
        if (!r.ok) throw new Error(body.error || (r.status + " " + r.statusText));
        return body;
      });
    });
  }

  function table(cols, rows, cellFns) {
    var h = '<div class="scroll"><table class="grid"><thead><tr>';
    cols.forEach(function (c) { h += "<th>" + esc(c) + "</th>"; });
    h += "</tr></thead><tbody>";
    if (!rows.length) h += '<tr><td class="muted" colspan="' + cols.length + '">No rows.</td></tr>';
    rows.forEach(function (row) {
      h += "<tr>";
      row.forEach(function (cell, i) {
        h += "<td" + (cellFns && cellFns[i] ? ' class="mono"' : "") + ">" + cell + "</td>";
      });
      h += "</tr>";
    });
    return h + "</tbody></table></div>";
  }

  // ---- Databases tab ----
  function loadServer() {
    return api("/api/server").then(function (d) {
      var s = d.server || {}, c = d.connection || {};
      $("serverinfo").innerHTML =
        '<div class="card server-row">' +
          '<div><div class="muted small">PostgreSQL</div><div class="server-ver">' + esc(s.version || "?") + "</div></div>" +
          '<div class="kv"><span class="k">Server</span><span class="v mono">' + esc(c.user) + "@" + esc(c.host) + ":" + esc(c.port) + "</span></div>" +
          '<div class="kv"><span class="k">Profile</span><span class="v">' + esc(c.profile || "—") + "</span></div>" +
        "</div>";
    });
  }

  function loadDatabases() {
    return api("/api/databases").then(function (d) {
      var rows = (d.databases || []).map(function (db) {
        return [
          '<a class="link" data-db="' + esc(db.name) + '">' + esc(db.name) + "</a>",
          esc(db.owner), '<span class="mono">' + esc(db.size) + "</span>"
        ];
      });
      $("databases").innerHTML = table(["Name", "Owner", "Size"], rows);
      $("databases").querySelectorAll("a[data-db]").forEach(function (a) {
        a.addEventListener("click", function () { loadTables(a.getAttribute("data-db")); });
      });
    });
  }

  function loadTables(db) {
    $("tables").innerHTML = '<h2>Tables in ' + esc(db) + '</h2><div class="muted">Loading…</div>';
    api("/api/tables?db=" + encodeURIComponent(db)).then(function (d) {
      var rows = (d.tables || []).map(function (t) {
        return [
          esc(t.schema), esc(t.name),
          '<span class="tag">' + esc(t.kind) + "</span>",
          '<span class="mono">' + (t.rows < 0 ? "—" : t.rows) + "</span>",
          '<span class="mono">' + esc(t.size) + "</span>"
        ];
      });
      $("tables").innerHTML = "<h2>Tables in " + esc(db) + "</h2>" +
        table(["Schema", "Table", "Type", "Est. rows", "Size"], rows);
    }).catch(function (e) { $("tables").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>"; });
  }

  function connect() {
    base = apiInput.value.trim();
    localStorage.setItem(KEY, base);
    clearError();
    setDot("");
    $("databases").innerHTML = '<div class="muted">Loading…</div>';
    Promise.all([loadServer(), loadDatabases()])
      .then(function () { setDot("ok"); })
      .catch(function (e) { setDot("down"); showError(e.message); $("databases").innerHTML = ""; });
  }

  // ---- SQL tab ----
  function runQuery() {
    var sql = $("sql").value.trim();
    if (!sql) return;
    clearError();
    $("qresult").innerHTML = '<div class="muted">Running…</div>';
    api("/api/query", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ db: $("qdb").value.trim(), sql: sql })
    }).then(function (res) {
      var rows = (res.rows || []).map(function (r) { return r.map(esc); });
      $("qresult").innerHTML = '<p class="muted small">' + res.rowCount + " row(s)</p>" +
        table(res.columns || [], rows, (res.columns || []).map(function () { return true; }));
    }).catch(function (e) { $("qresult").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>"; });
  }

  // ---- Sidebar navigation (deep-linkable via #db / #sql) ----
  var TITLES = { db: "Databases", sql: "SQL Console" };
  function showView(view) {
    if (!TITLES[view]) view = "db";
    document.querySelectorAll(".side-nav a").forEach(function (x) {
      x.classList.toggle("active", x.getAttribute("data-view") === view);
    });
    $("tab-db").hidden = view !== "db";
    $("tab-sql").hidden = view !== "sql";
    $("page-title").textContent = TITLES[view];
  }
  document.querySelectorAll(".side-nav a[data-view]").forEach(function (a) {
    a.addEventListener("click", function () {
      var view = a.getAttribute("data-view");
      location.hash = view;
      showView(view);
    });
  });
  showView((location.hash || "#db").slice(1));
  window.addEventListener("hashchange", function () { showView(location.hash.slice(1)); });

  $("connect").addEventListener("click", connect);
  apiInput.addEventListener("keydown", function (e) { if (e.key === "Enter") connect(); });
  $("run").addEventListener("click", runQuery);
  $("sql").addEventListener("keydown", function (e) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); runQuery(); }
  });

  // Auto-connect if a URL was already saved.
  if (base) connect();
})();
