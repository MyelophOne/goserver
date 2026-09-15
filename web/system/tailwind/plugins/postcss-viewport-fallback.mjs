const fallbackUnits = {
	dvh: "vh",
	svh: "vh",
	lvh: "vh",
	vhc: "vh",
	dvw: "vw",
	svw: "vw",
	lvw: "vw",
	vwc: "vw",
};

export default function viewportFallback() {
	return {
		postcssPlugin: "postcss-viewport-fallback",
		Declaration(decl) {
			const units = /(\d*\.?\d+)(dvh|svh|lvh|vhc|dvw|svw|lvw|vwc)\b/gi;
			if (!units.test(decl.value)) return;
			const fallback = decl.value.replace(
				units,
				(_, value, unit) =>
					`${value}${fallbackUnits[unit.toLowerCase()] ?? unit}`,
			);
			if (fallback === decl.value) return;

			let exists = false;
			decl.parent?.walkDecls(decl.prop, (candidate) => {
				if (candidate.value === fallback) exists = true;
			});
			if (!exists) decl.cloneBefore({ value: fallback });
		},
	};
}

viewportFallback.postcss = true;
