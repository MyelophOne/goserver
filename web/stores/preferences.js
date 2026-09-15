import { createStore } from 'zustand/vanilla';

const themes = new Set(['light', 'dark']);

function initialTheme() {
	try {
		const saved = localStorage.getItem('theme');
		if (themes.has(saved)) return saved;
	} catch (_) {}
	if (document.documentElement.getAttribute('data-theme') === 'dark')
		return 'dark';
	return window.matchMedia &&
		window.matchMedia('(prefers-color-scheme: dark)').matches
		? 'dark'
		: 'light';
}

function applyTheme(theme) {
	const root = document.documentElement;
	root.setAttribute('data-theme', theme);
}

export const store = createStore((set, get) => ({
	theme: initialTheme(),
	compactCards: false,
	localInspectorOpen: false,
	setTheme(theme) {
		if (themes.has(theme)) set({ theme });
	},
	toggleTheme() {
		get().setTheme(get().theme === 'dark' ? 'light' : 'dark');
	},
	setCompactCards(value) {
		set({ compactCards: !!value });
	},
	setLocalInspectorOpen(value) {
		set({ localInspectorOpen: !!value });
	},
}));

export function setup() {
	const unsubscribe = store.subscribe((state, previous) => {
		if (state.theme === previous.theme) return;
		applyTheme(state.theme);
		try {
			localStorage.setItem('theme', state.theme);
		} catch (_) {}
	});
	const onStorage = (event) => {
		if (event.key === 'theme' && themes.has(event.newValue))
			store.getState().setTheme(event.newValue);
	};
	window.addEventListener('storage', onStorage);
	applyTheme(store.getState().theme);
	return () => {
		unsubscribe();
		window.removeEventListener('storage', onStorage);
	};
}

export const sync = {
	channel: 'myelophone:preferences',
	fields: ['compactCards', 'theme'],
};
export const session = { enabled: false };
