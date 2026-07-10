# Zahir Data Console — Vercel frontend

A dependency-free static single-page app (plain HTML/CSS/JS) that talks to a
running **zahirdbman** instance over its JSON API. Deploy it to Vercel; point it
at your zahirdbman URL at runtime (stored in the browser).

## What it does

- Enter your zahirdbman API base URL (top bar) — saved in `localStorage`.
- **Databases** tab: server version + connection, the list of databases, and
  the tables/views inside any database.
- **SQL Console** tab: run a query against a chosen database and view results.

It uses these API endpoints (added to zahirdbman): `GET /api/server`,
`GET /api/databases`, `GET /api/tables?db=`, `POST /api/query`.

## Deploy to Vercel

1. In Vercel: **Add New → Project**, import the `zahirdbman` repo.
2. Set **Root Directory** to `frontend`. Framework preset: **Other** (it's static
   — no build step, no install command).
3. Deploy. You get a URL like `https://your-app.vercel.app`.

## Connect it to zahirdbman (required)

The API is **CORS-gated and off by default**. On the zahirdbman server, set:

```
ZDBM_CORS_ORIGIN=https://your-app.vercel.app
```

and restart it. zahirdbman must be reachable over **HTTPS** (a Vercel page can't
call an `http://` API — mixed content is blocked).

## ⚠️ Security

The `/api/query` endpoint runs arbitrary SQL, and zahirdbman has **no login of
its own**. Exposing its API to a browser frontend means anyone who can reach
that URL can read or destroy data. Only do this behind a private network, a
reverse proxy with authentication, or for a throwaway/demo database. Setting
`ZDBM_CORS_ORIGIN` limits *which site's* JS may call it, but not *who* can reach
the server directly — add real auth for anything sensitive.
