# Examples

## Structure

Structure for template rendering:

```bash
templates/
  layouts/
    base.html
  partials/
  pages/
```

## Initiate

Initiate:

```go
tm := goserver.NewTemplateManager() // always necessary
server.TemplatesMiddleware(tm) // OPTIONALLY: only if we need static templates
```

And we may overwrite certain static endpoints to dynamic - just make additional endpoint after the NewTemplateManager call.

Endpoints are created in the same order as they are in the code, so newer endpoints will overwrite older ones.

This Go template system provides a flexible way to manage layouts, partials, global/page-specific assets, and dynamic variables for HTML pages. It supports default values in templates and allows easy route registration.

### Folder Structure

```bash
templates/
├── layouts/         # layouts (e.g., base.html, alt.html)
├── pages/           # page templates (e.g., index.html, contact.html, about@alt.html)
├── partials/        # reusable partials (e.g., footer.html, header.html)
├── head/            # page-specific <head> additions (e.g., contact.html)
├── styles/          # page-specific CSS (e.g., index.html)
└── scripts/         # page-specific JS (e.g., index.html, global.html)
```

- **Global assets**: `scripts/global.html`, `styles/global.html`, `head/global.html`
- **Page-specific assets**: placed in `scripts/`, `styles/`, or `head/` with the same filename as the page.

## Base

Do not delete or modify base layout `base.html`, as it is used (and will be overwritten by updates). If You need - simply override it with `page@alt.html` (where alt is a layout name - alt.html).

## Page example

pages/demo.html (`define "content` should be for every page!)

```html
{{define "content"}}
<h1>Demo Contact Us</h1>

<p>Phone: {{var . "phone"}}</p>

<ul>
 {{range (var . "emails")}}
 <li>{{.}}</li>
 {{end}}
</ul>

<ul>
 {{range (var . "contacts")}}
 <li>{{index . "name"}} - {{index . "email"}}</li>
 {{end}}
</ul>
{{end}}
```

## Layout example

layouts/alt.html

```html
{{define "alt"}}
<!DOCTYPE html>
<html lang="en">
 <head>
  <meta charset="UTF-8" />
  <title>{{.Title}}</title>
  <meta name="description" content="{{.Description}}" />
  {{.GlobalHead}} {{.PageHead}} {{.GlobalStyles}} {{.PageStyles}}
 </head>

 <body>
  <header>
   <h1>Alt Layout</h1>
  </header>

  <main>{{template "content" .}}</main>

  {{template "footer.html" .}} {{.GlobalScripts}} {{.PageScripts}}
 </body>
</html>
{{end}}
```

## Partials

```html
{{define "footer.html"}}
<footer>
 <p>© 2025 MyelophOne</p>
</footer>
{{end}}
```

## Scripts and styles

They do not include script and style tags by default, so files should contain `<script>` and `<style>` tags.

## Includes

`GlobalHead` - incldues global head content.
`GlobalStyles` - includes global styles.
`GlobalScripts` - includes global scripts.

`PageHead` - incldues page head content.
`PageStyles` - includes page styles.
`PageScripts` - includes page scripts.

`{{template "footer.html" .}}` - will include file from partials folder with footer.html name.

## Variables

You can use `{{.Variables}}` in layouts and pages to access variables from context.

```html
{{define "content"}}
<p>Phone: {{index .Variables "phone"}}</p>
<ul>
 {{range index .Variables "emails"}}
 <li>{{.}}</li>
 {{end}}
</ul>
{{end}}
```

And example of using dynamic variables in templates:

```go
 server.GET("/demo", func(w http.ResponseWriter, r *http.Request) {
  data := goserver.PageData{
   Title: "Contact",
   Variables: map[string]interface{}{
    "phone":  "+1-234-567-890",
    "emails": []string{"info@example.com", "support@example.com"},
    "contacts": []map[string]string{
     {"name": "Alice", "email": "alice@example.com"},
     {"name": "Bob", "email": "bob@example.com"},
    },
   },
  }

  htmlContent, err := tm.RenderHTML("demo.html", data)
  if err != nil {
   http.Error(w, err.Error(), http.StatusInternalServerError)
   return
  }

  server.RespondHTML(w, htmlContent)
 })
```

## Tips

Script out-from-box replaces `nojs` to `js` class in `<html>` tag, so You may use classes `nojs` and `js` by default.
