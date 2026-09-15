# Playground

`web/playground/` is a development-only overlay for `web/`. Enable it with
`APP_ENV=dev` (or `APP_ENV=Development`).

Put files here using the same paths as under `web/`. For example,
`web/playground/pages/index.gosh` replaces `web/pages/index.gosh`; a new
`web/playground/pages/about.gosh` adds `/about`. When both trees contain the
same path, the playground file wins. Production builds and binaries ignore
this directory completely.

Settings remain project-level: `websettings.json` is followed by
`websettings.Development.json` when the playground is enabled.
