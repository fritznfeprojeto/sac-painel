# Support Desk Frontend

Vanilla JS/HTML/CSS SPA with no build step.

## Local development

Serve this directory with any static server, for example:

```bash
python -m http.server 5500
```

The frontend assumes the API is on the same origin. For a separate local API, open the page with a query-free origin and set before loading the module, or edit `assets/js/api.js` to use `http://localhost:8080`.

A simple option is to add this before `app.js` in `index.html`:

```html
<script>window.SUPPORT_API_BASE = 'http://localhost:8080';</script>
```

For production, deploy the frontend and backend behind the same domain/reverse proxy when possible.
