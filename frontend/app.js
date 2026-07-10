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
      .then(function () { setDot("ok"); loadConnections().catch(function () {}); })
      .catch(function (e) { setDot("down"); showError(e.message); $("databases").innerHTML = ""; });
  }

  // ---- Connections ----
  function loadConnections() {
    return api("/api/connections").then(function (d) {
      var cards = (d.connections || []).map(function (c) {
        var actions =
          (c.active ? '<span class="tag ok-tag">Active</span>'
                    : '<button class="btn small primary" data-act="' + esc(c.name) + '">Connect</button>') +
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
      .then(function () { setDot("ok"); })
      .catch(function (e) { showError(e.message); });
  }

  function deleteConn(name) {
    api("/api/connections/delete", jsonPost({ name: name }))
      .then(loadConnections)
      .catch(function (e) { showError(e.message); });
  }

  function jsonPost(obj) {
    return { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(obj) };
  }

  // Add-connection modal
  var connDialog = $("conn-dialog"), connForm = $("conn-form");
  if ($("conn-add")) $("conn-add").addEventListener("click", function () { connForm.reset(); connDialog.showModal(); });
  if ($("conn-x")) $("conn-x").addEventListener("click", function () { connDialog.close(); });
  if ($("conn-cancel")) $("conn-cancel").addEventListener("click", function () { connDialog.close(); });
  if (connForm) connForm.addEventListener("submit", function (e) {
    e.preventDefault();
    var el = connForm.elements;
    var body = {
      name: el["name"].value.trim(), host: el["host"].value.trim(), port: el["port"].value.trim(),
      user: el["user"].value.trim(), password: el["password"].value, adminDB: el["adminDB"].value.trim(),
      sslmode: el["sslmode"].value, activate: el["activate"].checked
    };
    api("/api/connections/save", jsonPost(body))
      .then(function (res) {
        connDialog.close();
        if (res.error) showError(res.error);
        return res.activated ? Promise.all([loadConnections(), loadServer(), loadDatabases()]) : loadConnections();
      })
      .catch(function (err) { showError(err.message); });
  });

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

  // ---- Sidebar navigation (deep-linkable via #db / #conn / #sql) ----
  var TITLES = { db: "Databases", conn: "Connections", sql: "SQL Console" };
  function showView(view) {
    if (!TITLES[view]) view = "db";
    document.querySelectorAll(".side-nav a").forEach(function (x) {
      x.classList.toggle("active", x.getAttribute("data-view") === view);
    });
    $("tab-db").hidden = view !== "db";
    $("tab-conn").hidden = view !== "conn";
    $("tab-sql").hidden = view !== "sql";
    $("page-title").textContent = TITLES[view];
    if (view === "conn" && base) loadConnections().catch(function (e) {
      $("conn-list").innerHTML = '<div class="banner err">' + esc(e.message) + "</div>";
    });
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

  // Deep-link: ?addconn=1 opens the add-connection dialog on load.
  if (connDialog && new URLSearchParams(location.search).get("addconn")) {
    if (location.hash.slice(1) !== "conn") { location.hash = "conn"; showView("conn"); }
    connDialog.showModal();
  }

  // Auto-connect if a URL was already saved.
  if (base) connect();
})();
