import addViewportUnits from "./plugins/postcss-add-viewportunits.mjs";
import viewportFallback from "./plugins/postcss-viewport-fallback.mjs";
import removeEmptyCustomProperties from "./plugins/postcss-remove-empty-custom-properties.mjs";

export default {
	plugins: [
		addViewportUnits(),
		viewportFallback(),
		removeEmptyCustomProperties(),
	],
};
