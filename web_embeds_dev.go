//go:build !myelophone_prod

package goserver

import "embed"

//go:embed all:web/pages all:web/components all:web/layouts all:web/global all:web/stores all:web/plugins all:web/css all:web/content all:web/teleport all:web/server all:web/system/css all:web/system/templates all:web/system/teleport all:web/system/runtime web/system/logic/*.go web/logic/*.go web/system/client/*.js web/system/tailwind/global.css
var embeddedWeb embed.FS

//go:embed web/system/client/package.json web/system/client/yarn.lock web/system/client/.yarnrc.yml web/system/client/.yarn/releases/*.cjs web/system/tailwind/package.json web/system/tailwind/yarn.lock web/system/tailwind/.yarnrc.yml web/system/tailwind/.yarn/releases/*.cjs web/system/tailwind/*.mjs web/system/tailwind/plugins/*.mjs
var embeddedWebTools embed.FS
