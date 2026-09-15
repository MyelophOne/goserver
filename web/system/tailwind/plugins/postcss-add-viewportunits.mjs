const viewportUnits = {
	"h-screen": { prop: "height", value: "100dvh" },
	"min-h-screen": { prop: "min-height", value: "100dvh" },
	"w-screen": { prop: "width", value: "100dvw" },
	"min-w-screen": { prop: "min-width", value: "100dvw" },
};

export default function addViewportUnits() {
	return {
		postcssPlugin: "postcss-add-dv-units-inline",
		Rule(rule) {
			const selector = rule.selector.trim();
			const className = selector.startsWith(".") ? selector.slice(1) : "";
			const unit = viewportUnits[className];
			if (!unit) return;

			let exists = false;
			rule.walkDecls(unit.prop, (decl) => {
				if (decl.value === unit.value) exists = true;
			});
			if (!exists) rule.append(unit);
		},
	};
}

addViewportUnits.postcss = true;
