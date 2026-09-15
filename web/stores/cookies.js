import { createStore } from 'zustand/vanilla';

const categories = ['necessary', 'analytics', 'marketing', 'functional'];
const pending = {
	necessary: true,
	analytics: false,
	marketing: false,
	functional: false,
	timestamp: 0,
	status: 'pending',
};
const draft = (preferences, enabledCategory = '') => ({
	...pending,
	...preferences,
	necessary: true,
	...(categories.includes(enabledCategory)
		? { [enabledCategory]: true }
		: {}),
});

function readConfig() {
	const node = document.querySelector('[data-cookie-config]');
	if (!node) return {};
	try {
		return JSON.parse(node.getAttribute('data-cookie-config') || '{}');
	} catch (_) {
		return {};
	}
}

function readCookie(name) {
	const prefix = encodeURIComponent(name) + '=';
	const item = document.cookie
		.split(';')
		.map((value) => value.trim())
		.find((value) => value.startsWith(prefix));
	if (!item) return null;
	try {
		return JSON.parse(decodeURIComponent(item.slice(prefix.length)));
	} catch (_) {
		return null;
	}
}

function writeCookie(name, value, days) {
	let cookie =
		encodeURIComponent(name) +
		'=' +
		encodeURIComponent(JSON.stringify(value)) +
		'; Path=/; Max-Age=' +
		Math.max(1, days) * 86400 +
		'; SameSite=Lax';
	if (location.protocol === 'https:') cookie += '; Secure';
	document.cookie = cookie;
}

const loadedScripts = new Set();
const initializedScripts = new Set();
const executedInitializers = new Set();
const appliedCategorySettings = new Set();

function scriptNonce() {
	const meta = document.querySelector('meta[name="gosh-csp-nonce"]');
	return meta ? meta.getAttribute('content') || '' : '';
}

function executeCode(code, script, allowed, suffix) {
	if (!code) return;
	const contextKey = 'cookie:' + script.id + ':' + suffix;
	window._gosh.cookieScriptContexts = window._gosh.cookieScriptContexts || {};
	window._gosh.cookieScriptContexts[contextKey] = {
		script,
		allowedCategories: Array.from(allowed),
		categories: Object.fromEntries(
			categories.map((category) => [category, allowed.has(category)]),
		),
		isAllowed(category) {
			return allowed.has(category);
		},
	};
	const node = document.createElement('script');
	const nonce = scriptNonce();
	if (nonce) node.nonce = nonce;
	node.dataset.cookieScriptHook = contextKey;
	node.textContent =
		'(function(context){\n' +
		code +
		'\n})(window._gosh.cookieScriptContexts[' +
		JSON.stringify(contextKey) +
		']);';
	document.head.append(node);
	delete window._gosh.cookieScriptContexts[contextKey];
}

function runInitializers(script, allowed, initializers) {
	(initializers || []).forEach((initializer) => {
		if (
			!initializer ||
			!initializer.key ||
			executedInitializers.has(initializer.key)
		)
			return;
		executeCode(
			initializer.code,
			script,
			allowed,
			'initializer:' + initializer.key,
		);
		executedInitializers.add(initializer.key);
	});
}

function applySettings(script, category, allowed) {
	runInitializers(script, allowed, script.initializers);
	if (!initializedScripts.has(script.id)) {
		executeCode(script.beforeLoad, script, allowed, 'before-load');
		initializedScripts.add(script.id);
	}
	const settings =
		script.categorySettings && script.categorySettings[category];
	const settingsKey =
		script.id +
		':' +
		category +
		':' +
		(script.src || script.code || script.loadKey || '');
	if (settings && !appliedCategorySettings.has(settingsKey)) {
		runInitializers(script, allowed, settings.initializers);
		executeCode(
			settings.beforeLoad,
			script,
			allowed,
			'category-before:' + category,
		);
		executeCode(
			settings.onConsentChange,
			script,
			allowed,
			'category-change:' + category,
		);
		appliedCategorySettings.add(settingsKey);
	}
	executeCode(
		script.onConsentChange,
		script,
		allowed,
		'consent-change:' + category + ':' + Date.now(),
	);
}

function loadAllowedScripts(config, preferences) {
	const allowed = new Set(['necessary']);
	if (
		preferences.status === 'accepted' ||
		preferences.status === 'declined'
	) {
		categories.forEach((category) => {
			if (preferences[category]) allowed.add(category);
		});
	}
	const groups = new Map();
	categories.forEach((category) => {
		if (!allowed.has(category)) return;
		((config.scripts && config.scripts[category]) || []).forEach(
			(script) => {
				const key =
					script.loadKey || script.src || 'inline:' + script.id;
				const entries = groups.get(key) || [];
				entries.push({ category, script });
				groups.set(key, entries);
			},
		);
	});
	groups.forEach((entries, key) => {
		entries.forEach(({ category, script }) =>
			applySettings(script, category, allowed),
		);
		if (loadedScripts.has(key)) return;
		const script = entries
			.map((entry) => entry.script)
			.find((item) => item.src || item.code);
		if (!script) return;
		const node = document.createElement('script'),
			nonce = scriptNonce();
		if (nonce) node.nonce = nonce;
		node.dataset.cookieScriptKey = key;
		node.dataset.cookieCategories = Array.from(allowed).join(',');
		Object.entries(script.attributes || {}).forEach(([name, value]) => {
			if (
				/^(?:async|defer|crossorigin|referrerpolicy|integrity|type)$/i.test(
					name,
				) ||
				name.startsWith('data-')
			)
				node.setAttribute(name, String(value));
		});
		if (script.src) node.src = script.src;
		else node.textContent = script.code;
		document.head.append(node);
		loadedScripts.add(key);
	});
}

export const store = createStore((set, get) => ({
	config: {},
	preferences: pending,
	draftPreferences: { ...pending },
	bannerVisible: false,
	settingsOpen: false,
	requestedCategory: '',
	openSettings() {
		const state = get();
		set({
			settingsOpen: true,
			requestedCategory: '',
			draftPreferences: draft(state.preferences),
		});
	},
	closeSettings() {
		const state = get(),
			control = state.config.control || {},
			banner = control.banner || {};
		set({
			settingsOpen: false,
			requestedCategory: '',
			draftPreferences: draft(state.preferences),
			bannerVisible:
				state.preferences.status === 'pending' &&
				control.enabled !== false &&
				banner.enabled !== false,
		});
	},
	requestConsent(category) {
		const requestedCategory = categories.includes(category) ? category : '';
		set({
			settingsOpen: true,
			bannerVisible: false,
			requestedCategory,
			draftPreferences: draft(get().preferences, requestedCategory),
		});
	},
	setDraftPreference(category, enabled) {
		if (!categories.includes(category) || category === 'necessary') return;
		set({
			draftPreferences: {
				...get().draftPreferences,
				[category]: !!enabled,
				necessary: true,
			},
		});
	},
	acceptAll() {
		const preferences = {
			necessary: true,
			analytics: true,
			marketing: true,
			functional: true,
			timestamp: Date.now(),
			status: 'accepted',
		};
		set({
			preferences,
			draftPreferences: draft(preferences),
			bannerVisible: false,
			settingsOpen: false,
			requestedCategory: '',
		});
	},
	declineAll() {
		const preferences = {
			necessary: true,
			analytics: false,
			marketing: false,
			functional: false,
			timestamp: Date.now(),
			status: 'declined',
		};
		set({
			preferences,
			draftPreferences: draft(preferences),
			bannerVisible: false,
			settingsOpen: false,
			requestedCategory: '',
		});
	},
	savePreferences(selection) {
		const preferences = {
			...pending,
			...get().draftPreferences,
			...selection,
			necessary: true,
			timestamp: Date.now(),
			status: 'accepted',
		};
		set({
			preferences,
			draftPreferences: draft(preferences),
			bannerVisible: false,
			settingsOpen: false,
			requestedCategory: '',
		});
	},
	hasConsent(category) {
		return category === 'necessary' || get().preferences[category] === true;
	},
}));

export function setup() {
	const config = readConfig();
	const control = config.control || {};
	const name = control.cookieName || 'privacy-preferences';
	let preferences = readCookie(name);
	if (!preferences || !preferences.status) preferences = { ...pending };
	categories.forEach((category) => {
		preferences[category] =
			category === 'necessary' || preferences[category] === true;
	});
	let bannerVisible = !!control.enabled && preferences.status === 'pending';
	if (preferences.status === 'declined') {
		const reask =
			Math.max(0, Number(control.declineReaskDays ?? 30)) * 86400000;
		bannerVisible =
			reask === 0 ||
			Date.now() - Number(preferences.timestamp || 0) > reask;
	}
	if (control.banner && control.banner.enabled === false)
		bannerVisible = false;
	store.setState({
		config,
		preferences,
		draftPreferences: draft(preferences),
		bannerVisible,
	});
	loadAllowedScripts(config, preferences);

	let previous = JSON.stringify(preferences);
	const unsubscribe = store.subscribe((state) => {
		const serialized = JSON.stringify(state.preferences);
		if (serialized === previous) return;
		previous = serialized;
		writeCookie(name, state.preferences, Number(control.maxAgeDays || 365));
		loadAllowedScripts(config, state.preferences);
		document.dispatchEvent(
			new CustomEvent('cookie:consent-change', {
				detail: state.preferences,
			}),
		);
	});
	const onClick = (event) => {
		if (event.target.closest('[data-cookie-open-settings]'))
			store.getState().openSettings();
	};
	document.addEventListener('click', onClick);
	return () => {
		unsubscribe();
		document.removeEventListener('click', onClick);
	};
}

export const sync = { channel: 'myelophone:cookies', fields: ['preferences'] };
export const session = { enabled: false };
