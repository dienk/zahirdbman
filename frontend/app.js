/* Zahir Data Console — a static frontend for a zahirdbman JSON API.
   Feature parity with the main app: Databases (+ create/drop, table detail),
   Backup & Restore, Connections, SQL Console. API base is user-provided. */
(function () {
  "use strict";

  var KEY = "zdbm-api";
  var fromQuery = new URLSearchParams(location.search).get("api");
  var base = (fromQuery || localStorage.getItem(KEY) || "").trim();
  if (fromQuery) localStorage.setItem(KEY, base);

  var state = { databases: [] };

  var $ = function (id) { return document.getElementById(id); };
  var apiInput = $("api"), dot = $("dot"), banner = $("banner");
  apiInput.value = base;

  function setDot(s) { dot.className = "dot" + (s ? " " + s : ""); }
  function showError(m) { banner.hidden = false; banner.className = "banner err"; banner.textContent = m; }
  function clearError() { banner.hidden = true; }
  function esc(s) {
    return String(s == null ? "" : s).replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }
  function baseURL() { return base.replace(/\/+$/, ""); }
  function jsonPost(obj) { return { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(obj) }; }

  // fetchFailHint turns a bare "Failed to fetch" (network / CORS / mixed-content
  // / private-network block) into an actionable message.
  function fetchFailHint() {
    var pageHttps = location.protocol === "https:";
    var isHttp = /^http:\/\//i.test(base);
    var isLocal = /^https?:\/\/(localhost|127\.0\.0\.1|\[?::1)/i.test(base);
    if (pageHttps && isLocal)
      return "Can't reach a localhost backend from this HTTPS page — browsers block public→private-network requests. Run the console locally, or deploy the backend over HTTPS.";
    if (pageHttps && isHttp)
      return "This page is HTTPS but the API URL is HTTP, which the browser blocks (mixed content). Use an https:// backend URL.";
    return "Couldn't reach the API at " + base + ". Check the URL is correct and the backend is running, and that it allows this site via ZDBM_CORS_ORIGIN=" + location.origin + " (a CORS block also shows as “Failed to fetch”).";
  }

  function api(path, opts) {
    if (!base) return Promise.reject(new Error("Set the API URL in the top bar, then click Connect."));
    return fetch(baseURL() + path, opts).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (body) {
        if (!r.ok) throw new Error(body.error || (r.status + " " + r.statusText));
        return body;
      });
    }, function () {
      // fetch itself rejected (no response reached the browser).
      throw new Error(fetchFailHint());
    });
  }

  function table(cols, rows, mono) {
    var h = '<div class="scroll"><table class="grid"><thead><tr>';
    cols.forEach(function (c) { h += "<th>" + esc(c) + "</th>"; });
    h += "</tr></thead><tbody>";
    if (!rows.length) h += '<tr><td class="muted" colspan="' + cols.length + '">No rows.</td></tr>';
    rows.forEach(function (row) {
      h += "<tr>";
      row.forEach(function (cell) { h += "<td" + (mono ? ' class="mono"' : "") + ">" + cell + "</td>"; });
      h += "</tr>";
    });
    return h + "</tbody></table></div>";
  }

  // ---- Databases ----
  function loadServer() {
    return api("/api/server").then(function (d) {
      state.server = d;
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
      state.databases = d.databases || [];
      var rows = state.databases.map(function (db) {
        return [
          '<a class="link" data-db="' + esc(db.name) + '">' + esc(db.name) + "</a>",
          esc(db.owner), '<span class="mono">' + esc(db.size) + "</span>",
          '<button class="btn small danger" data-drop="' + esc(db.name) + '">Drop</button>'
        ];
      });
      $("databases").innerHTML = table(["Name", "Owner", "Size", ""], rows);
      $("databases").querySelectorAll("a[data-db]").forEach(function (a) {
        a.addEventListener("click", function () { loadTables(a.getAttribute("data-db")); });
      });
      $("databases").querySelectorAll("[data-drop]").forEach(function (b) {
        b.addEventListener("click", function () {
          var n = b.getAttribute("data-drop");
          if (confirm("Drop database " + n + "? This cannot be undone.")) dropDatabase(n);
        });
      });
      populateBackupDbs();
    });
  }

  function loadTables(db) {
    $("tabledetail").innerHTML = "";
    $("tables").innerHTML = "<h2>Tables in " + esc(db) + '</h2><div class="muted">Loading…</div>';
    api("/api/tables?db=" + encodeURIComponent(db)).then(function (d) {
      var rows = (d.tables || []).map(function (t) {
        return [
          esc(t.schema),
          '<a class="link" data-tbl="' + esc(t.schema) + "|" + esc(t.name) + '">' + esc(t.name) + "</a>",
          '<span class="tag">' + esc(t.kind) + "</span>",
          '<span class="mono">' + (t.rows < 0 ? "—" : t.rows) + "</span>",
          '<span class="mono">' + esc(t.size) + "</span>"
        ];
      });
      $("tables").innerHTML = "<h2>Tables in " + esc(db) + "</h2>" +
        table(["Schema", "Table", "Type", "Est. rows", "Size"], rows);
      $("tables").querySelectorAll("[data-tbl]").forEach(function (a) {
        a.addEventListener("click", function () {
          var p = a.getAttribute("data-tbl").split("|");
          loadTableDetail(db, p[0], p[1]);
        });
      });
    }).catch(function (e) { $("tables").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>"; });
  }

  function loadTableDetail(db, schema, name) {
    $("tabledetail").innerHTML = '<div class="muted">Loading ' + esc(schema) + "." + esc(name) + "…</div>";
    api("/api/table?db=" + encodeURIComponent(db) + "&schema=" + encodeURIComponent(schema) + "&name=" + encodeURIComponent(name))
      .then(function (d) {
        var cols = (d.columns || []).map(function (c) {
          return ['<span class="mono strong">' + esc(c.name) + "</span>", '<span class="mono">' + esc(c.type) + "</span>",
            c.nullable ? '<span class="muted">yes</span>' : '<span class="tag">NOT NULL</span>',
            '<span class="mono muted">' + (c.default ? esc(c.default) : "—") + "</span>"];
        });
        var pv = d.preview || { columns: [], rows: [] };
        $("tabledetail").innerHTML =
          '<h2>' + esc(schema) + "." + esc(name) + "</h2>" +
          "<h3>Columns</h3>" + table(["Column", "Type", "Nullable", "Default"], cols) +
          '<h3>Data preview <span class="muted small">(first 100 rows)</span></h3>' +
          table(pv.columns || [], (pv.rows || []).map(function (r) { return r.map(esc); }), true);
      }).catch(function (e) { $("tabledetail").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>"; });
  }

  function dropDatabase(name) {
    api("/api/databases/drop", jsonPost({ name: name })).then(function () {
      $("tables").innerHTML = ""; $("tabledetail").innerHTML = "";
      return loadDatabases();
    }).catch(function (e) { showError(e.message); });
  }

  // New database modal
  var dbDialog = $("db-dialog"), dbForm = $("db-form");
  $("db-add").addEventListener("click", function () { dbForm.reset(); dbDialog.showModal(); });
  $("db-x").addEventListener("click", function () { dbDialog.close(); });
  $("db-cancel").addEventListener("click", function () { dbDialog.close(); });
  dbForm.addEventListener("submit", function (e) {
    e.preventDefault();
    api("/api/databases/create", jsonPost({ name: dbForm.elements["name"].value.trim() }))
      .then(function () { dbDialog.close(); return loadDatabases(); })
      .catch(function (err) { showError(err.message); });
  });

  // ---- Backup & Restore ----
  function populateBackupDbs() {
    var sel = $("bk-db");
    if (!sel) return;
    sel.innerHTML = state.databases.map(function (db) {
      return '<option value="' + esc(db.name) + '">' + esc(db.name) + " (" + esc(db.size) + ")</option>";
    }).join("");
  }
  function refreshBackupWarn() {
    var ok = state.server && state.server.toolsAvailable;
    $("backup-warn").innerHTML = (state.server && !ok)
      ? '<div class="banner err">The server is missing pg_dump/pg_restore/psql — Backup &amp; Restore is unavailable.</div>' : "";
  }
  $("bk-download").addEventListener("click", function () {
    var db = $("bk-db").value;
    if (!base || !db) { showError("Connect and pick a database first."); return; }
    var url = baseURL() + "/backup?db=" + encodeURIComponent(db) + "&format=" + $("bk-format").value;
    var a = document.createElement("a"); a.href = url; a.rel = "noopener"; document.body.appendChild(a); a.click(); a.remove();
  });
  $("rs-form").addEventListener("submit", function (e) {
    e.preventDefault();
    if (!base) { showError("Set the API URL first."); return; }
    $("rs-result").innerHTML = '<div class="muted">Restoring…</div>';
    fetch(baseURL() + "/api/restore", { method: "POST", body: new FormData(e.target) })
      .then(function (r) { return r.json().then(function (b) { if (!r.ok) throw new Error(b.error || r.status); return b; }); })
      .then(function (res) {
        $("rs-result").innerHTML = '<div class="banner ok">Restored into ' + esc(res.restored) + "</div>";
        loadDatabases();
      })
      .catch(function (err) { $("rs-result").innerHTML = '<div class="banner err">' + esc(err.message) + "</div>"; });
  });

  // ---- Connections ----
  function loadConnections() {
    return api("/api/connections").then(function (d) {
      state.connections = d.connections || [];
      var cards = state.connections.map(function (c) {
        var actions =
          (c.active ? '<span class="tag ok-tag">Active</span>'
                    : '<button class="btn small primary" data-act="' + esc(c.name) + '">Connect</button>') +
          ' <button class="btn small" data-edit="' + esc(c.name) + '">Edit</button>' +
          ' <button class="btn small danger" data-del="' + esc(c.name) + '">Delete</button>';
        return '<div class="conn-card' + (c.active ? " is-active" : "") + '">' +
          '<div class="conn-head"><span class="conn-name">' + esc(c.name) + "</span>" +
          (c.active ? '<span class="dot ok"></span>' : "") + "</div>" +
          '<div class="conn-meta mono">' + esc(c.user) + "@" + esc(c.host) + ":" + esc(c.port) +
          "/" + esc(c.adminDB) + " · sslmode=" + esc(c.sslmode || "prefer") + "</div>" +
          '<div class="conn-actions">' + actions + "</div></div>";
      });
      $("conn-list").innerHTML = cards.length
        ? '<div class="conn-grid">' + cards.join("") + "</div>"
        : '<p class="muted">No connection profiles yet. Click “+ Add connection”.</p>';
      $("conn-list").querySelectorAll("[data-act]").forEach(function (b) {
        b.addEventListener("click", function () { activateConn(b.getAttribute("data-act")); });
      });
      $("conn-list").querySelectorAll("[data-edit]").forEach(function (b) {
        b.addEventListener("click", function () { openConnModal(b.getAttribute("data-edit")); });
      });
      $("conn-list").querySelectorAll("[data-del]").forEach(function (b) {
        b.addEventListener("click", function () {
          if (confirm("Delete profile " + b.getAttribute("data-del") + "?")) deleteConn(b.getAttribute("data-del"));
        });
      });
    });
  }
  function activateConn(name) {
    clearError();
    api("/api/connections/activate", jsonPost({ name: name }))
      .then(function () { return Promise.all([loadConnections(), loadServer(), loadDatabases()]); })
      .then(function () { setDot("ok"); }).catch(function (e) { showError(e.message); });
  }
  function deleteConn(name) {
    api("/api/connections/delete", jsonPost({ name: name })).then(loadConnections).catch(function (e) { showError(e.message); });
  }

  // Add / edit connection modal
  var connDialog = $("conn-dialog"), connForm = $("conn-form");
  function openConnModal(editName) {
    connForm.reset();
    var el = connForm.elements;
    if (editName) {
      var p = state.connections.filter(function (c) { return c.name === editName; })[0];
      if (p) {
        el["name"].value = p.name; el["host"].value = p.host; el["port"].value = p.port;
        el["user"].value = p.user; el["adminDB"].value = p.adminDB; el["sslmode"].value = p.sslmode || "prefer";
        el["password"].placeholder = "(unchanged)";
      }
      $("conn-modal-title").textContent = "Edit “" + editName + "”";
      $("conn-submit").textContent = "Save changes";
    } else {
      el["password"].placeholder = "";
      $("conn-modal-title").textContent = "Add connection";
      $("conn-submit").textContent = "Save";
    }
    connDialog.showModal();
  }
  $("conn-add").addEventListener("click", function () { openConnModal(null); });
  $("conn-x").addEventListener("click", function () { connDialog.close(); });
  $("conn-cancel").addEventListener("click", function () { connDialog.close(); });
  connForm.addEventListener("submit", function (e) {
    e.preventDefault();
    var el = connForm.elements;
    var body = {
      name: el["name"].value.trim(), host: el["host"].value.trim(), port: el["port"].value.trim(),
      user: el["user"].value.trim(), password: el["password"].value, adminDB: el["adminDB"].value.trim(),
      sslmode: el["sslmode"].value, activate: el["activate"].checked
    };
    api("/api/connections/save", jsonPost(body)).then(function (res) {
      connDialog.close();
      if (res.error) showError(res.error);
      return res.activated ? Promise.all([loadConnections(), loadServer(), loadDatabases()]) : loadConnections();
    }).catch(function (err) { showError(err.message); });
  });

  // ---- SQL Console ----
  function runQuery() {
    var sql = $("sql").value.trim();
    if (!sql) return;
    clearError();
    $("qresult").innerHTML = '<div class="muted">Running…</div>';
    api("/api/query", jsonPost({ db: $("qdb").value.trim(), sql: sql })).then(function (res) {
      var rows = (res.rows || []).map(function (r) { return r.map(esc); });
      $("qresult").innerHTML = '<p class="muted small">' + res.rowCount + " row(s)</p>" +
        table(res.columns || [], rows, true);
    }).catch(function (e) { $("qresult").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>"; });
  }

  // ---- Connect ----
  function connect() {
    base = apiInput.value.trim();
    localStorage.setItem(KEY, base);
    clearError(); setDot("");
    $("databases").innerHTML = '<div class="muted">Loading…</div>';
    Promise.all([loadServer(), loadDatabases()])
      .then(function () { setDot("ok"); refreshBackupWarn(); loadConnections().catch(function () {}); })
      .catch(function (e) { setDot("down"); showError(e.message); $("databases").innerHTML = ""; });
  }

  // ---- Sidebar navigation (deep-linkable via #db / #backup / #conn / #sql) ----
  var TITLES = { db: "Databases", backup: "Backup & Restore", conn: "Connections", sql: "SQL Console" };
  function showView(view) {
    if (!TITLES[view]) view = "db";
    document.querySelectorAll(".side-nav a").forEach(function (x) {
      x.classList.toggle("active", x.getAttribute("data-view") === view);
    });
    ["db", "backup", "conn", "sql"].forEach(function (v) { $("tab-" + v).hidden = v !== view; });
    $("page-title").textContent = TITLES[view];
    if (!base) return;
    if (view === "backup") { populateBackupDbs(); refreshBackupWarn(); }
    if (view === "conn") loadConnections().catch(function (e) { $("conn-list").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>"; });
  }
  document.querySelectorAll(".side-nav a[data-view]").forEach(function (a) {
    a.addEventListener("click", function () { var v = a.getAttribute("data-view"); location.hash = v; showView(v); });
  });
  showView((location.hash || "#db").slice(1));
  window.addEventListener("hashchange", function () { showView(location.hash.slice(1)); });

  $("connect").addEventListener("click", connect);
  apiInput.addEventListener("keydown", function (e) { if (e.key === "Enter") connect(); });
  $("run").addEventListener("click", runQuery);
  $("sql").addEventListener("keydown", function (e) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); runQuery(); }
  });

  if (connDialog && new URLSearchParams(location.search).get("addconn")) {
    if (location.hash.slice(1) !== "conn") { location.hash = "conn"; showView("conn"); }
    openConnModal(null);
  }
  if (base) connect();
})();
