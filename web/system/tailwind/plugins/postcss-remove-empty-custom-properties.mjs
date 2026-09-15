const EMPTY_CUSTOM_PROPERTY = /^--[a-zA-Z0-9-_]+$/;

export default function removeEmptyCustomProperties() {
	return {
		postcssPlugin: "postcss-remove-empty-custom-properties",
		Declaration(decl) {
			if (
				EMPTY_CUSTOM_PROPERTY.test(decl.prop) &&
				decl.value.trim() === ""
			) {
				decl.remove();
			}
		},
	};
}

removeEmptyCustomProperties.postcss = true;
