(function () {
	'use strict';

	function runtimeIsSupported() {
		return (
			typeof Promise === 'function' &&
			typeof fetch === 'function' &&
			typeof URL === 'function' &&
			typeof AbortController === 'function' &&
			typeof Map === 'function' &&
			typeof Set === 'function'
		);
	}

	function showUnsupportedBrowserBanner() {
		if (!document.body || document.getElementById('gosh-browser-warning'))
			return;
		var banner = document.createElement('div');
		banner.id = 'gosh-browser-warning';
		banner.className = 'gosh-browser-warning';
		banner.setAttribute('role', 'alert');
		banner.textContent =
			'Your browser is outdated. This page is available, but interactive features are disabled. Please update your browser.';
		document.body.insertBefore(banner, document.body.firstChild);
	}

	if (!runtimeIsSupported()) {
		window._gosh = window._gosh || {};
		window._gosh.runtimeUnsupported = true;
		showUnsupportedBrowserBanner();
		return;
	}

	var V = 1;
	const author = 'Aliaksandr Ivanou (aleksivanov.me | @aleksivanou)';
	var STATE_KEY = '__universalRuntime';
	var DEFAULT_TTL = 20000;
	var DEFAULT_CACHE_LIMIT = 30;

	window._gosh = window._gosh || {};
	window._gosh._hooks = window._gosh._hooks || new Map();
	window._gosh.hook =
		window._gosh.hook ||
		function (name, fn) {
			if (typeof fn !== 'function')
				throw new Error('_gosh.hook requires a function');
			var list = window._gosh._hooks.get(name) || [];
			list.push(fn);
			window._gosh._hooks.set(name, list);
			return function () {
				var current = window._gosh._hooks.get(name) || [];
				var index = current.indexOf(fn);
				if (index >= 0) current.splice(index, 1);
			};
		};
	window._gosh.callHook =
		window._gosh.callHook ||
		async function (name, payload) {
			var list = (window._gosh._hooks.get(name) || []).slice();
			for (var i = 0; i < list.length; i++) await list[i](payload || {});
		};
	window._gosh._error = window._gosh._error || null;

	function initialStoreState() {
		var node = document.getElementById('gosh-store-state');
		if (!node) return {};
		try {
			var payload = JSON.parse(node.textContent || '{}');
			return payload &&
				payload.stores &&
				typeof payload.stores === 'object'
				? payload.stores
				: {};
		} catch (_) {
			return {};
		}
	}

	window._gosh._storeState = window._gosh._storeState || initialStoreState();
	window._gosh.useStoreState =
		window._gosh.useStoreState ||
		function (name, state) {
			if (
				!name ||
				!state ||
				typeof state !== 'object' ||
				Array.isArray(state)
			)
				return null;
			var snapshots = (window._gosh._storeState =
				window._gosh._storeState || {});
			snapshots[name] = Object.assign({}, snapshots[name] || {}, state);
			var store = window._gosh.stores && window._gosh.stores[name];
			if (store && typeof store.setState === 'function')
				store.setState(snapshots[name]);
			return store || snapshots[name];
		};
	window._gosh.createError =
		window._gosh.createError ||
		function (input) {
			input = input || {};
			var error = new Error(
				input.statusMessage || input.message || 'Application error',
			);
			error.statusCode = input.statusCode || 500;
			error.statusMessage = input.statusMessage || error.message;
			error.data = input.data;
			if (input.cause) error.cause = input.cause;
			return error;
		};
	window._gosh.useError =
		window._gosh.useError ||
		function () {
			return window._gosh._error;
		};
	window._gosh.clearError =
		window._gosh.clearError ||
		function () {
			window._gosh._error = null;
		};

	function readCSPNonce() {
		var meta = document.querySelector('meta[name="gosh-csp-nonce"]');
		if (meta && meta.content) return meta.content;
		if (document.currentScript && document.currentScript.nonce)
			return document.currentScript.nonce;
		var script = document.querySelector('script[nonce]');
		if (script && script.nonce) return script.nonce;
		var style = document.querySelector('style[nonce]');
		if (style && style.nonce) return style.nonce;
		return '';
	}

	function readRuntimeConfig() {
		var node = document.getElementById('gosh-runtime-config');
		if (!node) return {};
		try {
			return JSON.parse(node.textContent || '{}') || {};
		} catch (_) {
			return {};
		}
	}

	function readPageRuntime() {
		var node = document.getElementById('gosh-page-runtime');
		if (!node) return {};
		try {
			var payload = JSON.parse(node.textContent || '{}');
			return payload && typeof payload === 'object' ? payload : {};
		} catch (_) {
			return {};
		}
	}

	function supportsRuntime() {
		return runtimeIsSupported();
	}

	function Runtime(options) {
		var config = readRuntimeConfig();
		this.options = Object.assign(
			{
				cacheTTL: DEFAULT_TTL,
				cacheLimit: DEFAULT_CACHE_LIMIT,
				hardFallback: true,
				debug: false,
				prefetchDelay: 65,
				prefetchMaxConcurrent: 2,
				prefetchOnHover: false,
				viewTransitions: true,
				webVitals: false,
				webVitalsURL: '',
			},
			options || {},
		);
		[
			'prefetchDelay',
			'prefetchMaxConcurrent',
			'prefetchOnHover',
			'viewTransitions',
			'webVitals',
			'webVitalsURL',
		].forEach(function (key) {
			if (Object.prototype.hasOwnProperty.call(config, key))
				this.options[key] = config[key];
		}, this);

		this.cache = new Map();
		this.queryCache = new Map();
		this.queryFlights = new Map();
		this.queryControllers = new Map();
		this.active = null;
		this.seq = 0;
		this.loadingCount = 1;
		this.loadingListeners = new Set();

		this.loadedStyles = new Set();
		this.headMeta = new Map();
		this.headLinks = new Map();
		this.pageStyleNodes = new Map();
		this.fragmentStyleNodes = new Map();
		this.inlineStyleNodes = new Map();
		this.seoNodes = new Set();
		this.pageModules = [];
		this.looseModules = new Map();
		this.embeddedModules = new Map();
		this.chunkModules = new Map();
		this.scrollPosition = { x: 0, y: 0 };
		this.scrollSamplePending = false;
		this.indexHead();
		this.seoState = this.readSEOState();
		this.pageRuntime = readPageRuntime();
		this.nonce = readCSPNonce();
		this.componentStyleRules = new Map();
		this.componentStyleNode = null;
		this.tabId = uid();
		this.storeBindings = new Map();
		this.storeDOMSubscriptions = new Map();
		this.storeCleanups = [];
		window.addEventListener(
			'pagehide',
			function () {
				self.destroyStores();
			},
			{ once: true },
		);
		this.lazyNodes = new WeakSet();
		this.lazyObserver = null;
		this.lazyObservedRoots = new WeakMap();
		this.revealNodes = new WeakSet();
		this.revealObserver = null;
		this.revealQueue = [];
		this.revealQueued = new WeakSet();
		this.revealTimer = null;
		this.youtubeNodes = new WeakSet();
		this.youtubeObserver = null;
		this.iconNodes = new WeakSet();
		this.iconObserver = null;
		this.lazyIdle = new WeakMap();
		this.lazyRequests = new Set();
		this.prefetchTimers = new WeakMap();
		this.prefetchFlights = new Map();
		this.prefetchActive = 0;

		window._gosh = window._gosh || {};
		window._gosh.stores = window._gosh.stores || {};
		window._gosh.storeMeta = window._gosh.storeMeta || {};
		var self = this;
		window._gosh.runtime = this;
		window._gosh.loadLazy = function (node) {
			return self.loadLazy(node);
		};
		window._gosh.callHook('app:created', { runtime: this });
		window._gosh.import = function (src) {
			return import(absolute(src));
		};
		window._gosh.useSeo = function (input, data) {
			return window._gosh.runtime.useSeo(input, data);
		};
		window._gosh.useQuery = function (options) {
			return self.useQuery(options);
		};
		window._gosh.invalidateQuery = function (key, exact) {
			return self.invalidateQuery(key, exact);
		};
		window._gosh.cancelQuery = function (key, exact) {
			return self.cancelQuery(key, exact);
		};
		window._gosh.optimisticWrite = function (key, updater) {
			return self.optimisticWrite(key, updater);
		};
		window._gosh.runAction = function (action, source, options) {
			return self.invokeAction(action, source, options);
		};
		window._gosh.useForm = function (form, options) {
			return self.useForm(form, options);
		};
		window._gosh.registerStore = function (name, store, sync, session) {
			return self.registerStore(name, store, sync, session);
		};
		window._gosh.getStore = function (name) {
			return window._gosh.stores[name] || null;
		};
		window._gosh.setStoreSessionAdapter = function (adapter) {
			self.storeSessionAdapter = adapter || null;
		};
		window._gosh.useWebSocket = function (options) {
			return self.useWebSocket(options);
		};
		window._gosh.useLoading = function (listener) {
			return self.useLoading(listener);
		};
		window._gosh.setStyleProperty = function (element, property, value) {
			self.setStyleProperty(element, property, value);
		};
		window._gosh.withLoading = function (task) {
			return self.withLoading(task);
		};
		(window.__GOSH_PENDING_STORES__ || []).forEach(function (item) {
			self.registerStore(
				item.name,
				item.store,
				item.sync,
				item.session,
				item.setup,
			);
		});
		window.__GOSH_PENDING_STORES__ = [];

		this.installHistory();
		this.bind();
		this.restoreInitialScroll();
		this.mountInitialPageModules();
		this.drainBootstrapQueue();
		this.scanLazy(document);
		this.scanReveal(document);
		this.scanDeferredYouTube(document);
		this.scanDeferredIcons(document);
		this.bindStoreDOM(document);
		this.startWebVitals();
		(window._gosh.domReady || Promise.resolve()).then(function () {
			afterPaint(function () {
				self.endLoading();
			});
		});

		window._gosh = window._gosh || {};
		emit('ready', { runtime: this });
		window._gosh.callHook('app:mounted', { runtime: this });
	}

	Runtime.prototype.getStore = function (name) {
		return (window._gosh.stores && window._gosh.stores[name]) || null;
	};

	Runtime.prototype.createComponentAPI = function (ctx) {
		var runtime = this,
			root = ctx && ctx.element;
		var cleanups = (ctx.__goshCleanups = ctx.__goshCleanups || []);
		var addCleanup = function (fn) {
			if (typeof fn === 'function') cleanups.push(fn);
			return fn;
		};
		var resolve = function (value) {
			if (!value) return root;
			if (typeof value === 'string')
				return root && root.querySelector
					? root.querySelector(value)
					: null;
			return value;
		};
		return {
			useRoot: function () {
				return root;
			},
			useElement: function (selector) {
				return resolve(selector);
			},
			onMounted: function (fn) {
				if (typeof fn !== 'function') return fn;
				Promise.resolve().then(function () {
					if (!ctx.__goshDisposed) fn();
				});
				return fn;
			},
			onUnmounted: function (fn) {
				return addCleanup(fn);
			},
			useEvent: function (target, type, handler, options) {
				var element = resolve(target);
				if (
					!element ||
					!element.addEventListener ||
					typeof handler !== 'function'
				)
					return function () {};
				element.addEventListener(type, handler, options);
				return addCleanup(function () {
					element.removeEventListener(type, handler, options);
				});
			},
			useStore: function (name) {
				return runtime.getStore(name);
			},
			watchStore: function (name, selector, callback) {
				var store = runtime.getStore(name),
					select =
						typeof selector === 'function'
							? selector
							: function (state) {
									return selector
										? selector.split('.').reduce(function (
												value,
												key,
											) {
												return value == null
													? undefined
													: value[key];
											}, state)
										: state;
								};
				if (
					!store ||
					typeof store.subscribe !== 'function' ||
					typeof callback !== 'function'
				)
					return function () {};
				var previous = select(store.getState());
				var unsubscribe = store.subscribe(function (state) {
					var next = select(state);
					if (Object.is(next, previous)) return;
					var old = previous;
					previous = next;
					callback(next, old);
				});
				return addCleanup(unsubscribe);
			},
			emit: function (name, detail) {
				if (root && root.dispatchEvent)
					root.dispatchEvent(
						new CustomEvent(name, {
							bubbles: true,
							detail: detail,
						}),
					);
			},
		};
	};

	Runtime.prototype.registerStore = function (
		name,
		store,
		sync,
		session,
		setup,
	) {
		if (
			!name ||
			!store ||
			typeof store.getState !== 'function' ||
			typeof store.setState !== 'function' ||
			typeof store.subscribe !== 'function'
		) {
			throw new Error('Invalid Zustand vanilla store: ' + name);
		}
		if (this.storeBindings.has(name)) return store;

		var meta = { sync: sync || false, session: session || null };
		window._gosh.stores[name] = store;
		window._gosh.storeMeta[name] = meta;

		var snapshot =
			window._gosh._storeState && window._gosh._storeState[name];
		if (
			snapshot &&
			typeof snapshot === 'object' &&
			!Array.isArray(snapshot)
		)
			store.setState(snapshot);

		var binding = {
			store: store,
			unsubscribe: null,
			channel: null,
			applyingRemote: false,
			meta: meta,
		};
		this.storeBindings.set(name, binding);

		var fields = [];
		var all = false;
		var channelName = 'myelophone:store:' + name;
		if (sync === true) all = true;
		else if (Array.isArray(sync)) fields = sync.slice();
		else if (sync && typeof sync === 'object') {
			if (sync.fields === true || sync.fields === '*') all = true;
			else if (Array.isArray(sync.fields)) fields = sync.fields.slice();
			if (sync.channel) channelName = String(sync.channel);
		}

		if ((all || fields.length) && typeof BroadcastChannel === 'function') {
			var self = this;
			var channel = new BroadcastChannel(channelName);
			binding.channel = channel;

			function serializableState(state) {
				var selected = {};
				if (all) {
					Object.keys(state || {}).forEach(function (key) {
						if (typeof state[key] !== 'function')
							selected[key] = state[key];
					});
				} else {
					fields.forEach(function (key) {
						if (state && typeof state[key] !== 'function')
							selected[key] = state[key];
					});
				}
				try {
					return JSON.parse(JSON.stringify(selected));
				} catch (_) {
					return {};
				}
			}

			var lastSerialized = JSON.stringify(
				serializableState(store.getState()),
			);
			binding.unsubscribe = store.subscribe(function (state) {
				if (binding.applyingRemote) return;
				var selected = serializableState(state);
				var serialized = JSON.stringify(selected);
				if (serialized === lastSerialized) return;
				lastSerialized = serialized;
				channel.postMessage({
					v: 1,
					type: 'state',
					source: self.tabId,
					store: name,
					state: selected,
				});
			});

			channel.addEventListener('message', function (event) {
				var data = event.data || {};
				if (
					data.v !== 1 ||
					data.store !== name ||
					data.source === self.tabId
				)
					return;
				if (data.type === 'hello') {
					channel.postMessage({
						v: 1,
						type: 'state',
						source: self.tabId,
						store: name,
						state: serializableState(store.getState()),
					});
					return;
				}
				if (
					data.type !== 'state' ||
					!data.state ||
					typeof data.state !== 'object'
				)
					return;
				lastSerialized = JSON.stringify(data.state);
				binding.applyingRemote = true;
				try {
					store.setState(data.state);
				} finally {
					binding.applyingRemote = false;
				}
				emit('store-sync', { name: name, state: data.state });
			});
			channel.postMessage({
				v: 1,
				type: 'hello',
				source: self.tabId,
				store: name,
			});
		}

		if (
			this.storeSessionAdapter &&
			session &&
			session.enabled &&
			typeof this.storeSessionAdapter.attach === 'function'
		) {
			this.storeSessionAdapter.attach(name, store, session);
		}
		if (typeof setup === 'function') {
			var cleanup = setup({
				name: name,
				store: store,
				gosh: window._gosh,
			});
			if (typeof cleanup === 'function') this.storeCleanups.push(cleanup);
		}
		this.bindStoreDOM(document);
		emit('store-ready', {
			name: name,
			store: store,
			sync: sync || false,
			session: session || null,
		});
		return store;
	};

	Runtime.prototype.bindStoreDOM = function (root) {
		var self = this;
		var nodes = [];
		if (root instanceof Element && root.matches('[data-gosh-store-text]'))
			nodes.push(root);
		if (root && root.querySelectorAll)
			nodes = nodes.concat(
				Array.prototype.slice.call(
					root.querySelectorAll('[data-gosh-store-text]'),
				),
			);
		var names = {};
		nodes.forEach(function (node) {
			var path = node.getAttribute('data-gosh-store-text') || '';
			var dot = path.indexOf('.');
			if (dot <= 0) return;
			var name = path.slice(0, dot),
				key = path.slice(dot + 1);
			var store = window._gosh.stores[name];
			if (!store) return;
			var value = store.getState()[key];
			node.textContent = value == null ? '' : String(value);
			names[name] = true;
		});
		Object.keys(names).forEach(function (name) {
			if (self.storeDOMSubscriptions.has(name)) return;
			var store = window._gosh.stores[name];
			if (!store) return;
			self.storeDOMSubscriptions.set(
				name,
				store.subscribe(function () {
					self.bindStoreDOM(document);
				}),
			);
		});
	};

	Runtime.prototype.invokeStoreAction = function (source) {
		var action = source && source.getAttribute('data-gosh-store-action');
		if (!action) return false;
		var dot = action.indexOf('.');
		if (dot <= 0) return false;
		var store = window._gosh.stores[action.slice(0, dot)];
		var method = store && store.getState()[action.slice(dot + 1)];
		if (typeof method !== 'function') return false;
		var args = [];
		var raw = source.getAttribute('data-gosh-store-args');
		if (raw) {
			try {
				args = JSON.parse(raw);
			} catch (_) {
				args = [raw];
			}
			if (!Array.isArray(args)) args = [args];
		}
		method.apply(store.getState(), args);
		return true;
	};

	Runtime.prototype.destroyStores = function () {
		this.storeDOMSubscriptions.forEach(function (unsubscribe) {
			if (typeof unsubscribe === 'function') unsubscribe();
		});
		this.storeDOMSubscriptions.clear();
		while (this.storeCleanups.length) {
			try {
				this.storeCleanups.pop()();
			} catch (_) {}
		}
	};

	Runtime.prototype.log = function () {
		if (!this.options.debug) return;
		var args = Array.prototype.slice.call(arguments);
		args.unshift('[runtime]');
		console.log.apply(console, args);
	};

	function queryKeyID(key) {
		try {
			return JSON.stringify(key);
		} catch (_) {
			return String(key);
		}
	}

	Runtime.prototype.setStyleProperty = function (element, property, value) {
		if (
			!element ||
			!/^(?:--[a-zA-Z0-9_-]+|-?[a-zA-Z][a-zA-Z0-9-]*)$/.test(
				property || '',
			)
		)
			return;
		var stringValue = value == null ? '' : String(value);
		var lowered = stringValue.toLowerCase();
		if (
			/[{}<>]/.test(stringValue) ||
			lowered.indexOf('expression(') !== -1 ||
			lowered.indexOf('javascript:') !== -1 ||
			lowered.indexOf('-moz-binding') !== -1 ||
			lowered.indexOf('behavior:') !== -1
		)
			return;
		var id = element.getAttribute('data-gosh-component-style');
		if (!id) {
			id = uid();
			element.setAttribute('data-gosh-component-style', id);
		}
		if (!this.componentStyleNode) {
			this.componentStyleNode = document.createElement('style');
			if (this.nonce)
				this.componentStyleNode.setAttribute('nonce', this.nonce);
			this.componentStyleNode.setAttribute(
				'data-gosh-component-styles',
				'',
			);
			(document.head || document.documentElement).appendChild(
				this.componentStyleNode,
			);
		}
		var rule = this.componentStyleRules.get(id);
		if (!rule) {
			var ruleIndex = this.componentStyleNode.sheet.insertRule(
				'[data-gosh-component-style="' + id + '"]{}',
				this.componentStyleNode.sheet.cssRules.length,
			);
			rule = this.componentStyleNode.sheet.cssRules[ruleIndex];
			this.componentStyleRules.set(id, rule);
		}
		if (stringValue) rule.style.setProperty(property, stringValue);
		else rule.style.removeProperty(property);
	};

	Runtime.prototype.loadingSnapshot = function () {
		return { isLoading: this.loadingCount > 0, count: this.loadingCount };
	};

	Runtime.prototype.syncLoading = function () {
		var node = document.getElementById('gosh-preloader');
		var state = this.loadingSnapshot();
		if (node) {
			node.classList.toggle('is-loading', state.isLoading);
			node.setAttribute('aria-busy', state.isLoading ? 'true' : 'false');
		}
		this.loadingListeners.forEach(function (listener) {
			listener(state);
		});
		emit('loading', state);
		window._gosh.callHook('loading:change', {
			runtime: this,
			loading: state,
		});
	};

	Runtime.prototype.beginLoading = function () {
		this.loadingCount++;
		this.syncLoading();
	};

	Runtime.prototype.endLoading = function () {
		this.loadingCount = Math.max(0, this.loadingCount - 1);
		this.syncLoading();
	};

	Runtime.prototype.useLoading = function (listener) {
		if (typeof listener !== 'function') return this.loadingSnapshot();
		this.loadingListeners.add(listener);
		listener(this.loadingSnapshot());
		var self = this;
		return function () {
			self.loadingListeners.delete(listener);
		};
	};

	Runtime.prototype.withLoading = async function (task) {
		if (typeof task !== 'function')
			throw new Error('_gosh.withLoading requires a function');
		this.beginLoading();
		try {
			return await task();
		} finally {
			this.endLoading();
		}
	};

	Runtime.prototype.indexHead = function () {
		var self = this;
		Array.prototype.forEach.call(
			document.head.querySelectorAll('meta'),
			function (node) {
				self.headMeta.set(metaKey(attrs(node)), node);
			},
		);
		Array.prototype.forEach.call(
			document.head.querySelectorAll('link'),
			function (node) {
				var key = linkKey(attrs(node));
				if (key) {
					self.headLinks.set(key, node);
					self.loadedStyles.add(key);
				}
				if (node.hasAttribute('data-gosh-page-style') && node.href)
					self.pageStyleNodes.set(node.href, node);
				if (node.hasAttribute('data-gosh-fragment-style') && node.href)
					self.fragmentStyleNodes.set(node.href, node);
			},
		);
		Array.prototype.forEach.call(
			document.head.querySelectorAll('style[data-gosh-style]'),
			function (node) {
				self.inlineStyleNodes.set(
					node.getAttribute('data-gosh-style'),
					node,
				);
			},
		);
		Array.prototype.forEach.call(
			document.head.querySelectorAll('meta[data-gosh-seo]'),
			function (node) {
				self.seoNodes.add(node);
			},
		);
	};

	Runtime.prototype.startWebVitals = function () {
		if (
			!this.options.webVitals ||
			!this.options.webVitalsURL ||
			typeof window._gosh.import !== 'function'
		)
			return;
		var self = this;
		window._gosh
			.import(this.options.webVitalsURL)
			.then(function (module) {
				if (!module || typeof module.start !== 'function') return;
				var summary = {};
				var summaryLogged = false;
				function metricStyle(rating) {
					if (rating === 'good')
						return 'color:#16a34a;font-weight:700';
					if (rating === 'needs-improvement')
						return 'color:#d97706;font-weight:700';
					if (rating === 'poor')
						return 'color:#dc2626;font-weight:700';
					return 'color:#64748b;font-weight:600';
				}
				function logSummary() {
					if (summaryLogged) return;
					summaryLogged = true;
					var names = ['TTFB', 'FCP', 'LCP', 'CLS', 'INP'];
					var format = '%c[GOSH Web Vitals]';
					var args = [format, 'color:#6366f1;font-weight:800'];
					names.forEach(function (name) {
						var metric = summary[name] || {
							value: 'unavailable',
							rating: 'info',
						};
						format += ' | %c' + name + '=' + metric.value;
						args.push(metricStyle(metric.rating));
					});
					args[0] = format;
					console.info.apply(console, args);
				}
				self.stopWebVitals = module.start(function (metric) {
					var value =
						metric.name === 'CLS'
							? (metric.value / 1000).toFixed(3)
							: metric.value + 'ms';
					summary[metric.name] = {
						value: value + ' (' + metric.rating + ')',
						rating: metric.rating,
					};
					emit('web-vital', metric);
					window._gosh.callHook('web-vital', {
						runtime: self,
						metric: metric,
					});
				});
				setTimeout(logSummary, 3000);
				addEventListener('pagehide', logSummary, { once: true });
			})
			.catch(function (error) {
				self.log('Web Vitals module failed', error);
			});
	};

	Runtime.prototype.useWebSocket = function (options) {
		var self = this;
		var ready = window._gosh.domReady || Promise.resolve();
		return Promise.resolve(ready).then(function () {
			var url = self.options.webSocketChunkURL;
			if (!url)
				throw new Error('WebSocket composable chunk is unavailable');
			if (!self.webSocketComposable)
				self.webSocketComposable = window._gosh.import(url);
			return self.webSocketComposable.then(function (module) {
				if (!module || typeof module.useWebSocket !== 'function')
					throw new Error('Invalid WebSocket composable chunk');
				return module.useWebSocket(options);
			});
		});
	};

	function queryDelay(ms) {
		return new Promise(function (resolve) {
			setTimeout(resolve, Math.max(0, ms || 0));
		});
	}

	function queryError(response, url) {
		var error = new Error('HTTP ' + response.status + ' ' + url);
		error.status = response.status;
		error.response = response;
		return error;
	}

	Runtime.prototype.queryFetch = async function (options, signal) {
		var url = options.url;
		if (!url) throw new Error('useQuery requires either query() or url');
		var external = new URL(url, location.href).origin !== location.origin;
		var response = await fetch(url, {
			method: options.method || 'GET',
			headers: options.headers || undefined,
			body: options.body == null ? undefined : options.body,
			credentials:
				options.credentials || (external ? 'omit' : 'same-origin'),
			mode: options.mode || (external ? 'cors' : 'same-origin'),
			redirect: options.redirect || 'follow',
			signal: signal,
		});
		if (!response.ok) throw queryError(response, url);
		if (typeof options.parse === 'function') return options.parse(response);
		if (options.parse === 'response') return response;
		if (options.parse === 'text') return response.text();
		if (options.parse === 'blob') return response.blob();
		if (options.parse === 'arrayBuffer') return response.arrayBuffer();
		if (options.parse === 'stream') return response.body;
		if (response.status === 204) return undefined;
		var type = String(
			response.headers.get('content-type') || '',
		).toLowerCase();
		return type.indexOf('application/json') !== -1
			? response.json()
			: response.text();
	};

	Runtime.prototype.crossTabQuery = async function (key, signal, request) {
		if (typeof BroadcastChannel !== 'function') return request();
		var channel = new BroadcastChannel('myelophone:query-flight:' + key);
		var id = uid();
		var claims = [id];
		var settled = false;
		var resolveRemote;
		var rejectRemote;
		var remote = new Promise(function (resolve, reject) {
			resolveRemote = resolve;
			rejectRemote = reject;
		});
		channel.addEventListener('message', function (event) {
			var message = event.data || {};
			if (message.type === 'claim') claims.push(message.id);
			if (message.type === 'success' && !settled) {
				settled = true;
				resolveRemote(message.data);
			}
			if (message.type === 'error' && !settled) {
				settled = true;
				rejectRemote(
					new Error(message.message || 'Remote query failed'),
				);
			}
		});
		channel.postMessage({ v: V, type: 'claim', id: id });
		await queryDelay(25);
		if (signal.aborted) {
			channel.close();
			throw new DOMException('Query cancelled', 'AbortError');
		}
		claims.sort();
		if (claims[0] !== id) {
			try {
				return await Promise.race([
					remote,
					queryDelay(30000).then(request),
				]);
			} finally {
				channel.close();
			}
		}
		try {
			var data = await request();
			try {
				channel.postMessage({
					v: V,
					type: 'success',
					id: id,
					data: data,
				});
			} catch (_) {}
			return data;
		} catch (error) {
			channel.postMessage({
				v: V,
				type: 'error',
				id: id,
				message: error && error.message ? error.message : String(error),
			});
			throw error;
		} finally {
			channel.close();
		}
	};

	Runtime.prototype.useQuery = function (options) {
		options = options || {};
		if (options.key === undefined)
			throw new Error('useQuery requires a key');
		var self = this;
		var key =
			typeof options.key === 'function' ? options.key() : options.key;
		var id = queryKeyID(key);
		var listeners = new Set();
		var state = {
			key: key,
			data: undefined,
			error: null,
			status: 'idle',
			pending: false,
			isStale: true,
			isOutdated: false,
			promise: null,
		};
		function notify() {
			listeners.forEach(function (listener) {
				listener(state);
			});
		}
		function apply(entry) {
			state.data = entry.data;
			state.error = null;
			state.status = 'success';
			state.pending = false;
			state.isStale =
				Date.now() - entry.updatedAt >=
				(options.staleTime == null ? 30000 : options.staleTime);
			state.isOutdated = state.isStale && state.data !== undefined;
			notify();
		}
		state.subscribe = function (listener) {
			listeners.add(listener);
			return function () {
				listeners.delete(listener);
			};
		};
		state.execute = state.refresh = async function (force) {
			var cached = self.queryCache.get(id);
			var staleTime =
				options.staleTime == null ? 30000 : options.staleTime;
			if (!force && cached && Date.now() - cached.updatedAt < staleTime) {
				apply(cached);
				return cached.data;
			}
			state.pending = true;
			state.status = state.data === undefined ? 'pending' : 'success';
			state.isStale = state.data !== undefined;
			state.isOutdated = state.isStale;
			notify();
			var existing = self.queryFlights.get(id);
			if (existing) {
				state.promise = existing;
				try {
					var shared = await existing;
					apply(shared);
					return shared.data;
				} catch (error) {
					state.error = error;
					state.status = 'error';
					state.pending = false;
					notify();
					throw error;
				}
			}
			var controller = new AbortController();
			state.cancel = function () {
				controller.abort();
			};
			var request = async function () {
				var retries = options.retry == null ? 2 : options.retry;
				var attempt = 0;
				while (true) {
					try {
						if (options.debug) self.log('useQuery request', key);
						var value = options.query
							? await options.query({
									signal: controller.signal,
									queryKey: key,
								})
							: await self.queryFetch(options, controller.signal);
						if (typeof options.transform === 'function')
							value = await options.transform(value);
						return value;
					} catch (error) {
						if (controller.signal.aborted) throw error;
						var retry =
							typeof retries === 'function'
								? retries(attempt + 1, error)
								: attempt < retries;
						if (!retry) throw error;
						attempt++;
						var delay =
							typeof options.retryDelay === 'function'
								? options.retryDelay(attempt)
								: (options.retryDelay == null
										? 500
										: options.retryDelay) *
									Math.pow(2, attempt - 1);
						await queryDelay(delay);
					}
				}
			};
			var flight = (
				options.broadcast
					? self.crossTabQuery(id, controller.signal, request)
					: request()
			).then(function (data) {
				var entry = { data: data, updatedAt: Date.now() };
				self.queryCache.set(id, entry);
				var gcTime =
					options.gcTime === undefined ? 300000 : options.gcTime;
				if (gcTime !== false)
					setTimeout(function () {
						if (self.queryCache.get(id) === entry)
							self.queryCache.delete(id);
					}, gcTime);
				return entry;
			});
			self.queryFlights.set(id, flight);
			self.queryControllers.set(id, controller);
			state.promise = flight;
			try {
				var entry = await flight;
				apply(entry);
				return entry.data;
			} catch (error) {
				state.error = error;
				state.status = 'error';
				state.pending = false;
				notify();
				if (typeof options.onError === 'function')
					options.onError(error);
				throw error;
			} finally {
				if (self.queryFlights.get(id) === flight)
					self.queryFlights.delete(id);
				if (self.queryControllers.get(id) === controller)
					self.queryControllers.delete(id);
			}
		};
		state.clear = function () {
			self.queryCache.delete(id);
			state.data = undefined;
			state.error = null;
			state.status = 'idle';
			state.pending = false;
			state.isStale = true;
			state.isOutdated = false;
			notify();
		};
		var initial = self.queryCache.get(id);
		if (initial) apply(initial);
		if (options.immediate !== false)
			state.execute(false).catch(function () {});
		return state;
	};

	Runtime.prototype.invalidateQuery = function (key, exact) {
		var id = queryKeyID(key);
		this.queryCache.forEach(function (_, candidate) {
			if (
				exact === false ? candidate.indexOf(id) === 0 : candidate === id
			)
				this.queryCache.delete(candidate);
		}, this);
	};

	Runtime.prototype.cancelQuery = function (key, exact) {
		var id = queryKeyID(key);
		var controllers = this.queryControllers;
		controllers.forEach(function (controller, candidate) {
			if (
				exact === false ? candidate.indexOf(id) === 0 : candidate === id
			)
				controller.abort();
		});
		this.invalidateQuery(key, exact);
	};

	Runtime.prototype.optimisticWrite = function (key, updater) {
		var id = queryKeyID(key);
		var previous = this.queryCache.get(id);
		var next =
			typeof updater === 'function'
				? updater(previous && previous.data)
				: updater;
		this.queryCache.set(id, { data: next, updatedAt: Date.now() });
		return function () {
			if (previous) this.queryCache.set(id, previous);
			else this.queryCache.delete(id);
		}.bind(this);
	};

	Runtime.prototype.bind = function () {
		var self = this;

		document.addEventListener(
			'click',
			function (event) {
				self.handleClick(event);
			},
			true,
		);

		document.addEventListener(
			'submit',
			function (event) {
				self.handleSubmit(event);
			},
			true,
		);

		['change', 'input'].forEach(function (eventName) {
			document.addEventListener(
				eventName,
				function (event) {
					self.handleDelegatedAction(eventName, event);
				},
				true,
			);
		});

		window.addEventListener(
			'scroll',
			function () {
				if (self.scrollSamplePending) return;
				self.scrollSamplePending = true;
				requestAnimationFrame(function () {
					self.scrollPosition.x = window.scrollX || 0;
					self.scrollPosition.y = window.scrollY || 0;
					self.updateScrollHistory(true);
					self.scrollSamplePending = false;
				});
			},
			{ passive: true },
		);

		window.addEventListener('pagehide', function () {
			self.saveScroll();
		});

		window.addEventListener('pageshow', function (event) {
			if (!event.persisted) return;
			self.cache.clear();
			emit('bfcache-restore', { url: location.href });
			self.navigate(location.href, {
				history: 'none',
				saveScroll: false,
				scroll: 'restore',
				restoreScroll: {
					x: window.scrollX || 0,
					y: window.scrollY || 0,
				},
			});
		});

		window.addEventListener('popstate', function (event) {
			var s = event.state && event.state[STATE_KEY];

			self.navigate(location.href, {
				history: 'none',
				saveScroll: false,
				scroll: 'restore',
				restoreScroll: s
					? { x: s.x || 0, y: s.y || 0 }
					: { x: 0, y: 0 },
			});
		});

		document.addEventListener(
			'pointerover',
			function (event) {
				self.handlePrefetchIntent(event);
			},
			true,
		);

		document.addEventListener(
			'pointerout',
			function (event) {
				self.cancelPrefetchIntent(event);
			},
			true,
		);

		document.addEventListener(
			'focusin',
			function (event) {
				self.handlePrefetchIntent(event);
			},
			true,
		);
	};

	Runtime.prototype.installHistory = function () {
		if ('scrollRestoration' in history) {
			history.scrollRestoration = 'manual';
		}

		var state = Object.assign({}, history.state || {});
		state[STATE_KEY] = state[STATE_KEY] || {
			id: uid(),
			url: location.href,
			x: this.scrollPosition.x,
			y: this.scrollPosition.y,
		};

		history.replaceState(state, '', location.href);
	};

	Runtime.prototype.saveScroll = function () {
		this.scrollPosition.x = window.scrollX || 0;
		this.scrollPosition.y = window.scrollY || 0;
		this.updateScrollHistory();

		try {
			sessionStorage.setItem(
				'gosh:scroll:' + location.pathname + location.search,
				JSON.stringify({
					x: this.scrollPosition.x,
					y: this.scrollPosition.y,
				}),
			);
		} catch (_) {}
	};

	Runtime.prototype.updateScrollHistory = function (deferred) {
		clearTimeout(this.scrollHistoryTimer);
		this.scrollHistoryTimer = null;
		if (deferred) {
			var self = this;
			var url = location.href;
			var entry = history.state && history.state[STATE_KEY];
			var id = entry && entry.id;
			this.scrollHistoryTimer = setTimeout(function () {
				self.scrollHistoryTimer = null;
				var current = history.state && history.state[STATE_KEY];
				if (location.href !== url || (current && current.id) !== id)
					return;
				self.updateScrollHistory();
			}, 250);
			return;
		}

		var state = Object.assign({}, history.state || {});
		var s = Object.assign({}, state[STATE_KEY] || {});
		if (
			s.id &&
			s.url === location.href &&
			s.x === this.scrollPosition.x &&
			s.y === this.scrollPosition.y
		)
			return;

		s.id = s.id || uid();
		s.url = location.href;
		s.x = this.scrollPosition.x;
		s.y = this.scrollPosition.y;

		state[STATE_KEY] = s;
		try {
			history.replaceState(state, '', location.href);
		} catch (_) {}
	};

	Runtime.prototype.restoreInitialScroll = function () {
		var navigation =
			performance.getEntriesByType &&
			performance.getEntriesByType('navigation')[0];
		var isReload =
			(navigation && navigation.type === 'reload') ||
			(performance.navigation && performance.navigation.type === 1);
		if (!isReload) return;

		var saved = history.state && history.state[STATE_KEY];
		if (!saved) {
			try {
				saved = JSON.parse(
					sessionStorage.getItem(
						'gosh:scroll:' + location.pathname + location.search,
					) || 'null',
				);
			} catch (_) {}
		}
		if (!saved) return;
		var x = Number(saved.x) || 0;
		var y = Number(saved.y) || 0;
		if (x === 0 && y === 0) return;

		requestAnimationFrame(function () {
			requestAnimationFrame(function () {
				window.scrollTo(x, y);
			});
		});
	};

	Runtime.prototype.handleClick = function (event) {
		if (event.defaultPrevented || event.button !== 0) return;
		if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
			return;

		var retry =
			event.target instanceof Element
				? event.target.closest('[data-gosh-retry]')
				: null;
		if (retry) {
			event.preventDefault();
			var retryFn = retry.__goshRetry;
			if (typeof retryFn === 'function') retryFn();
			return;
		}

		var storeAction =
			event.target instanceof Element
				? event.target.closest('[data-gosh-store-action]')
				: null;
		if (storeAction && this.invokeStoreAction(storeAction)) {
			event.preventDefault();
			return;
		}

		var actionEl =
			event.target instanceof Element
				? event.target.closest('[data-gosh-on-click]')
				: null;
		if (actionEl) {
			event.preventDefault();
			this.invokeAction(
				actionEl.getAttribute('data-gosh-on-click'),
				actionEl,
			);
			return;
		}

		var link =
			event.target instanceof Element
				? event.target.closest('a[href]')
				: null;
		if (!link) return;

		if (
			link.hasAttribute('download') ||
			link.hasAttribute('data-runtime-off')
		)
			return;
		if (link.target && link.target !== '_self') return;

		var url;
		try {
			url = new URL(link.href, document.baseURI);
		} catch (_) {
			return;
		}

		if (url.origin !== location.origin) return;
		if (url.protocol !== 'http:' && url.protocol !== 'https:') return;

		if (
			url.pathname === location.pathname &&
			url.search === location.search &&
			url.hash &&
			url.hash !== location.hash
		) {
			return;
		}

		event.preventDefault();

		var linkTarget = link.getAttribute('data-runtime-target');
		var requestedHistory = link.getAttribute('data-runtime-history');
		this.navigate(url.href, {
			history:
				requestedHistory ||
				(linkTarget
					? 'none'
					: link.hasAttribute('data-runtime-replace')
						? 'replace'
						: 'push'),
			target: linkTarget,
			mode: link.getAttribute('data-runtime-mode') || 'inner',
			scroll: linkTarget ? 'preserve' : 'top',
		});
	};

	Runtime.prototype.handleSubmit = function (event) {
		if (event.defaultPrevented) return;

		var form = event.target;
		if (!(form instanceof HTMLFormElement)) return;
		if (form.hasAttribute('data-runtime-off')) return;

		var goshAction =
			form.getAttribute('data-gosh-form') ||
			form.getAttribute('data-gosh-on-submit');
		if (goshAction) {
			event.preventDefault();
			this.submitActionForm(form, goshAction);
			return;
		}

		var submitter = event.submitter || null;
		var method = (
			(submitter && submitter.getAttribute('formmethod')) ||
			form.getAttribute('method') ||
			'GET'
		).toUpperCase();

		var action =
			(submitter && submitter.getAttribute('formaction')) ||
			form.getAttribute('action') ||
			location.href;

		var url = new URL(action, document.baseURI);

		if (url.origin !== location.origin) return;

		event.preventDefault();

		var body = new FormData(form);

		if (method === 'GET') {
			var params = new URLSearchParams(url.search);
			body.forEach(function (value, key) {
				if (typeof value === 'string') params.append(key, value);
			});
			url.search = params.toString();

			this.navigate(url.href, {
				method: 'GET',
				history: form.hasAttribute('data-runtime-target')
					? 'none'
					: 'push',
				target: form.getAttribute('data-runtime-target'),
				mode: form.getAttribute('data-runtime-mode') || 'inner',
				scroll: 'preserve',
			});
			return;
		}

		this.navigate(url.href, {
			method: method,
			body: body,

			history: 'none',

			target: form.getAttribute('data-runtime-target'),
			mode: form.getAttribute('data-runtime-mode') || 'inner',
			scroll: 'preserve',
		});
	};

	Runtime.prototype.handleDelegatedAction = function (eventName, event) {
		if (event.defaultPrevented) return;
		var selector = '[data-gosh-on-' + eventName + ']';
		var source =
			event.target instanceof Element
				? event.target.closest(selector)
				: null;
		if (!source) return;
		var action = source.getAttribute('data-gosh-on-' + eventName);
		if (!action) return;
		this.invokeAction(action, source);
	};

	Runtime.prototype.handlePrefetchIntent = function (event) {
		var link =
			event.target instanceof Element
				? event.target.closest('a[href]')
				: null;
		if (
			!link ||
			(link.getAttribute('data-prefetch') !== 'hover' &&
				!this.options.prefetchOnHover)
		)
			return;
		if (!sameOrigin(link.href)) return;
		if (link.target && link.target !== '_self') return;
		if (
			link.hasAttribute('download') ||
			link.getAttribute('rel') === 'external'
		)
			return;
		if (link.href.split('#')[0] === location.href.split('#')[0]) return;
		if (link.contains(event.relatedTarget)) return;
		if (this.shouldAvoidPrefetch()) return;
		this.cancelPrefetchIntentFor(link);
		var self = this;
		var delay = Math.max(0, Number(this.options.prefetchDelay) || 0);
		this.prefetchTimers.set(
			link,
			setTimeout(function () {
				self.prefetchTimers.delete(link);
				self.prefetch(link.href);
			}, delay),
		);
	};

	Runtime.prototype.useForm = function (form, input) {
		var element =
			typeof form === 'string' ? document.querySelector(form) : form;
		if (!(element instanceof HTMLFormElement))
			throw new Error(
				'_gosh.useForm requires a form element or selector',
			);
		var options = Object.assign(
			{ event: '', validate: null, resetOnSuccess: false },
			input || {},
		);
		var eventName =
			options.event ||
			element.getAttribute('data-gosh-form') ||
			element.getAttribute('data-gosh-on-submit');
		if (!eventName)
			throw new Error(
				'_gosh.useForm requires options.event or data-gosh-form',
			);
		element.setAttribute('data-gosh-form', eventName);
		element.removeAttribute('action');
		var state = {
			element: element,
			event: eventName,
			pending: false,
			errors: {},
			values: {},
		};
		var subscribers = new Set();
		function notify() {
			subscribers.forEach(function (listener) {
				listener(state);
			});
		}
		state.subscribe = function (listener) {
			subscribers.add(listener);
			listener(state);
			return function () {
				subscribers.delete(listener);
			};
		};
		state.setErrors = function (errors) {
			state.errors = errors || {};
			notify();
		};
		state.submit = function () {
			return this.submitActionForm(element, eventName, state, options);
		}.bind(this);
		state.reset = function () {
			element.reset();
			state.values = {};
			state.errors = {};
			notify();
		};
		state._notify = notify;
		element.__goshForm = { state: state, options: options };
		return state;
	};

	Runtime.prototype.submitActionForm = async function (
		form,
		action,
		state,
		options,
	) {
		var binding = form.__goshForm || {};
		state = state || binding.state || null;
		options = options || binding.options || {};
		if (state && state.pending) return false;
		if (typeof form.checkValidity === 'function' && !form.checkValidity()) {
			if (typeof form.reportValidity === 'function')
				form.reportValidity();
			return false;
		}
		var fields = formFields(form);
		if (typeof options.validate === 'function') {
			var validation = await options.validate(fields, form);
			if (
				validation === false ||
				(validation &&
					typeof validation === 'object' &&
					Object.keys(validation).length)
			) {
				if (state) {
					state.errors =
						validation === false
							? { form: 'Invalid form' }
							: validation;
					state._notify();
				}
				return false;
			}
		}
		if (state) {
			state.pending = true;
			state.errors = {};
			state.values = fields;
			state._notify();
		}
		form.setAttribute('aria-busy', 'true');
		try {
			var ok = await this.invokeAction(
				action,
				form,
				Object.assign({}, options, { fields: fields }),
			);
			if (ok && options.resetOnSuccess) form.reset();
			return ok;
		} finally {
			form.removeAttribute('aria-busy');
			if (state) {
				state.pending = false;
				state._notify();
			}
		}
	};

	Runtime.prototype.cancelPrefetchIntent = function (event) {
		var link =
			event.target instanceof Element
				? event.target.closest('a[href]')
				: null;
		if (!link || link.contains(event.relatedTarget)) return;
		this.cancelPrefetchIntentFor(link);
	};

	Runtime.prototype.cancelPrefetchIntentFor = function (link) {
		var timer = this.prefetchTimers.get(link);
		if (timer) clearTimeout(timer);
		this.prefetchTimers.delete(link);
	};

	Runtime.prototype.shouldAvoidPrefetch = function () {
		var connection =
			navigator.connection ||
			navigator.mozConnection ||
			navigator.webkitConnection;
		return !!(
			connection &&
			(connection.saveData ||
				/(^|-)2g$/.test(connection.effectiveType || ''))
		);
	};

	Runtime.prototype.navigate = async function (url, options) {
		var opts = Object.assign(
			{
				method: 'GET',
				body: null,
				history: 'push',
				target: null,
				mode: 'inner',
				scroll: 'top',
				restoreScroll: null,
			},
			options || {},
		);

		var targetURL = new URL(url, document.baseURI);
		var method = String(opts.method || 'GET').toUpperCase();

		if (targetURL.origin !== location.origin) {
			location.assign(targetURL.href);
			return;
		}

		if (targetURL.pathname.length > 1 && /\/$/.test(targetURL.pathname)) {
			targetURL.pathname = targetURL.pathname.replace(/\/+$/, '');
		}

		if (!opts.target) {
			this.lazyRequests.forEach(function (controller) {
				controller.abort();
			});
			this.lazyRequests.clear();
		}
		await window._gosh.callHook('page:beforeNavigate', {
			runtime: this,
			url: targetURL.href,
			options: opts,
		});

		if (this.active && this.active.controller) {
			this.active.controller.abort();
		}

		if (opts.saveScroll !== false) this.saveScroll();

		var sequence = ++this.seq;
		var controller = new AbortController();

		this.active = {
			sequence: sequence,
			controller: controller,
			url: targetURL.href,
		};

		this.beginLoading();
		document.documentElement.classList.add('runtime-loading');
		var loadingTarget = opts.target
			? document.querySelector(opts.target)
			: document.querySelector('#app');
		if (loadingTarget) loadingTarget.setAttribute('aria-busy', 'true');
		emit('navigation-start', { url: targetURL.href, options: opts });

		var key = this.cacheKey(targetURL.href, opts.target);

		try {
			window._gosh._error = null;
			if (method === 'GET') {
				var cached = this.cacheGet(key);
				if (cached) {
					await this.renderNavigation(cached, sequence, opts, method);
					if (!this.isCurrent(sequence)) return;

					this.commitHistory(
						cached.finalURL || targetURL.href,
						opts.history,
					);
					this.finish(opts, cached.finalURL || targetURL.href);
					return;
				}
			} else {
				this.cache.clear();
			}

			var payload = await this.fetchPayload(targetURL.href, {
				method: method,
				body: opts.body,
				target: opts.target,
				mode: opts.mode,
				signal: controller.signal,
				sequence: sequence,
				render: !this.shouldUseViewTransition(opts, method),
			});

			if (!this.isCurrent(sequence)) return;

			if (method === 'GET' && payload.cacheable) {
				this.cacheSet(key, payload.cacheValue, payload.ttl);
			}

			if (this.shouldUseViewTransition(opts, method)) {
				await this.renderNavigation(
					payload.cacheValue,
					sequence,
					opts,
					method,
				);
				if (!this.isCurrent(sequence)) return;
			}

			this.commitHistory(
				payload.finalURL || targetURL.href,
				opts.history,
			);
			this.finish(opts, payload.finalURL || targetURL.href);
		} catch (error) {
			if (isAbort(error)) {
				emit('navigation-abort', { url: targetURL.href });
				return;
			}

			window._gosh._error = error;
			console.error('[runtime]', error);
			this.reportError(
				opts.target
					? document.querySelector(opts.target)
					: document.querySelector('#app'),
				error,
				'navigation',
				function () {
					this.navigate(targetURL.href, opts);
				}.bind(this),
			);
			await window._gosh.callHook('app:error', {
				runtime: this,
				url: targetURL.href,
				error: error,
			});
			emit('navigation-error', { url: targetURL.href, error: error });

			if (
				this.options.hardFallback &&
				(method === 'GET' || method === 'HEAD')
			) {
				location.assign(targetURL.href);
			}
		} finally {
			this.endLoading();
			if (this.isCurrent(sequence)) {
				this.active = null;
				this.scanLazy(document);
				document.documentElement.classList.remove('runtime-loading');
				if (loadingTarget) loadingTarget.removeAttribute('aria-busy');
			}
		}
	};

	Runtime.prototype.shouldUseViewTransition = function (options, method) {
		return (
			this.options.viewTransitions !== false &&
			options.transition !== false &&
			method === 'GET' &&
			!options.target &&
			typeof document.startViewTransition === 'function'
		);
	};

	Runtime.prototype.renderNavigation = async function (
		payload,
		sequence,
		options,
		method,
	) {
		var self = this;
		var render = function () {
			return self.applyCached(payload, sequence);
		};
		var canTransition = this.shouldUseViewTransition(options, method);

		if (!canTransition) return render();

		var transition;
		try {
			transition = document.startViewTransition(render);
		} catch (_) {
			return render();
		}

		if (transition && transition.finished)
			transition.finished.catch(function () {});
		if (transition && transition.updateCallbackDone) {
			await transition.updateCallbackDone;
		}
	};

	Runtime.prototype.fetchPayload = async function (url, options) {
		var headers = new Headers();
		headers.set('X-Runtime', '1');
		headers.set('X-GOSH-Runtime', 'navigate');
		if (options.prefetch) headers.set('X-GOSH-Prefetch', '1');
		headers.set(
			'Accept',
			'application/x-ndjson, application/json;q=0.9, text/html;q=0.8',
		);

		if (options.target) {
			headers.set('X-Runtime-Target', options.target);
			headers.set('X-Runtime-Mode', options.mode || 'inner');
		}

		await window._gosh.callHook('data:before', {
			runtime: this,
			url: url,
			options: options,
		});
		var response = await fetch(url, {
			method: options.method,
			body: options.body || null,
			headers: headers,
			credentials: 'same-origin',
			redirect: 'follow',
			signal: options.signal,
		});

		if (!response.ok) {
			var fetchError = new Error('HTTP ' + response.status + ' ' + url);
			await window._gosh.callHook('data:error', {
				runtime: this,
				url: url,
				options: options,
				error: fetchError,
			});
			throw fetchError;
		}
		await window._gosh.callHook('data:after', {
			runtime: this,
			url: url,
			options: options,
			response: response,
		});

		var type = String(
			response.headers.get('content-type') || '',
		).toLowerCase();
		var finalURL = response.url || url;
		var ttl = intOr(response.headers.get('x-runtime-cache-ttl'), 0);
		var noStore =
			String(response.headers.get('cache-control') || '')
				.toLowerCase()
				.indexOf('no-store') !== -1;

		if (type.indexOf('application/x-ndjson') !== -1) {
			var frames = await this.consumeNDJSON(response, options);
			return {
				finalURL: finalURL,
				ttl: ttl,
				cacheable: options.method === 'GET' && ttl > 0 && !noStore,
				cacheValue: {
					kind: 'ndjson',
					finalURL: finalURL,
					frames: frames,
				},
			};
		}

		if (type.indexOf('application/json') !== -1) {
			var frame = await response.json();

			if (options.render) {
				await this.applyFrame(frame, options.sequence);
			}

			return {
				finalURL: finalURL,
				ttl: ttl,
				cacheable: options.method === 'GET' && ttl > 0 && !noStore,
				cacheValue: { kind: 'json', finalURL: finalURL, frame: frame },
			};
		}

		var html = await response.text();

		if (options.render) {
			await this.applyFullHTML(html, options.sequence);
		}

		return {
			finalURL: finalURL,
			ttl: ttl,
			cacheable: options.method === 'GET' && ttl > 0 && !noStore,
			cacheValue: { kind: 'html', finalURL: finalURL, html: html },
		};
	};

	Runtime.prototype.consumeNDJSON = async function (response, options) {
		var frames = [];
		var pendingModules = [];
		var self = this;

		async function applyStreamFrame(frame) {
			frames.push(frame);
			if (!options.render) return;

			if (frame.type === 'modules' || frame.type === 'module') {
				pendingModules.push(frame);
				return;
			}

			if (frame.type === 'end') {
				for (var m = 0; m < pendingModules.length; m++) {
					await self.applyFrame(
						pendingModules[m],
						options.sequence,
						options.generation,
					);
				}
				pendingModules = [];
				await self.applyFrame(
					frame,
					options.sequence,
					options.generation,
				);
				return;
			}

			await self.applyFrame(frame, options.sequence, options.generation);
		}

		if (!response.body || typeof response.body.getReader !== 'function') {
			var all = await response.text();
			var lines = all.split(/\r?\n/);
			for (var i = 0; i < lines.length; i++) {
				if (!lines[i].trim()) continue;
				await applyStreamFrame(JSON.parse(lines[i]));
			}
			for (var pf = 0; pf < pendingModules.length; pf++) {
				await self.applyFrame(
					pendingModules[pf],
					options.sequence,
					options.generation,
				);
			}
			return frames;
		}

		var reader = response.body.getReader();
		var decoder = new TextDecoder('utf-8');
		var buffer = '';

		try {
			while (true) {
				var part = await reader.read();
				if (part.done) break;
				buffer += decoder.decode(part.value, { stream: true });

				while (true) {
					var nl = buffer.indexOf('\n');
					if (nl < 0) break;
					var line = buffer.slice(0, nl);
					buffer = buffer.slice(nl + 1);
					if (line.endsWith('\r')) line = line.slice(0, -1);
					if (!line.trim()) continue;
					await applyStreamFrame(JSON.parse(line));
				}
			}

			buffer += decoder.decode();
			if (buffer.trim()) await applyStreamFrame(JSON.parse(buffer));

			for (var m = 0; m < pendingModules.length; m++) {
				await self.applyFrame(
					pendingModules[m],
					options.sequence,
					options.generation,
				);
			}
			return frames;
		} finally {
			try {
				reader.releaseLock();
			} catch (_) {}
		}
	};

	Runtime.prototype.applyFrame = async function (
		frame,
		sequence,
		generation,
	) {
		if (generation !== undefined && generation !== this.seq) return;
		if (sequence && !this.isCurrent(sequence)) return;

		if (!frame || frame.v !== V) {
			throw new Error('Unsupported frame version');
		}

		emit('frame', { frame: frame });

		switch (frame.type) {
			case 'begin':
				if (frame.scope === 'page') {
					await this.unmountPageModule();
					await this.unmountLooseWithin('#app');
				} else if (frame.target) {
					await this.unmountLooseWithin(frame.target);
				}

				if (frame.clear && frame.target) {
					var clearTarget = document.querySelector(frame.target);
					if (!clearTarget)
						throw new Error('Target not found: ' + frame.target);
					clearTarget.replaceChildren();
				}

				emit('stream-begin', { frame: frame });
				if (frame.target) {
					var beginTarget = document.querySelector(frame.target);
					if (beginTarget)
						beginTarget.setAttribute('aria-busy', 'true');
				}
				return;

			case 'head':
				await this.applyHead(frame);
				return;

			case 'seo':
				this.applySEO(frame);
				return;

			case 'store-state':
				this.applyStoreState(frame);
				return;

			case 'page-runtime':
				this.pageRuntime = {
					page: typeof frame.page === 'string' ? frame.page : '',
					state: typeof frame.state === 'string' ? frame.state : '',
					modules: frame.modules || { bindings: [] },
				};
				return;

			case 'cache-tags':
				emit('cache-tags', { tags: frame.tags || [] });
				return;

			case 'cache-invalidate':
				this.invalidateCacheTags(frame.tags || []);
				return;

			case 'html':
				this.applyHTML(frame);
				return;

			case 'style':
				this.applyStyle(frame);
				return;

			case 'style-link':
				await this.applyStyleLink(frame);
				return;

			case 'page-style':
				await this.applyPageStyles({
					hrefs: frame.href ? [frame.href] : [],
				});
				return;

			case 'page-styles':
				await this.applyPageStyles(frame);
				return;

			case 'module':
				if (frame.scope === 'page') await this.mountPageModule(frame);
				else await this.mountLooseModule(frame);
				return;

			case 'modules':
				if (frame.scope === 'page') {
					await this.mountPageModules(frame);
				} else {
					await this.mountLooseModules(frame);
				}
				return;

			case 'patch':
				await this.applyPatch(frame);
				return;

			case 'redirect':
				await this.navigate(frame.url, {
					history: frame.history || 'replace',
				});
				return;

			case 'reload':
				location.assign(frame.url || location.href);
				return;

			case 'event':
				emit(frame.name || 'server-event', frame.detail || {});
				return;

			case 'end':
				if (frame.target) {
					var endTarget = document.querySelector(frame.target);
					if (endTarget) endTarget.removeAttribute('aria-busy');
				}
				emit('stream-end', { frame: frame });
				return;

			case 'error':
				throw new Error(frame.message || 'Server stream error');

			default:
				throw new Error('Unknown frame type: ' + frame.type);
		}
	};

	Runtime.prototype.applyStoreState = function (frame) {
		var stores = frame && frame.stores;
		if (!stores || typeof stores !== 'object') return;
		Object.keys(stores).forEach(function (name) {
			window._gosh.useStoreState(name, stores[name]);
		});
		emit('store-state', { stores: stores });
	};

	Runtime.prototype.applyHTML = function (frame) {
		if (!frame.target) throw new Error('html frame requires target');

		var target = document.querySelector(frame.target);
		if (!target) throw new Error('HTML target not found: ' + frame.target);

		var frag = fragment(frame.html || '');
		this.applyCSPNonce(frag);

		switch (frame.mode || 'inner') {
			case 'inner':
				target.replaceChildren(frag);
				break;
			case 'append':
				target.appendChild(frag);
				break;
			case 'prepend':
				target.insertBefore(frag, target.firstChild);
				break;
			case 'before':
				target.parentNode.insertBefore(frag, target);
				break;
			case 'after':
				target.parentNode.insertBefore(frag, target.nextSibling);
				break;
			case 'replace':
				target.replaceWith(frag);
				break;
			default:
				throw new Error('Unknown html mode: ' + frame.mode);
		}

		emit('dom-updated', {
			target: frame.target,
			mode: frame.mode || 'inner',
		});
		if (!this.domBatch) {
			this.scanLazy(target);
			this.scanReveal(target);
			this.scanDeferredYouTube(target);
			this.scanDeferredIcons(target);
			this.bindStoreDOM(target);
		}
	};

	Runtime.prototype.applyPatch = async function (frame) {
		var ops = Array.isArray(frame.operations) ? frame.operations : [];
		var lazyRoots = [];
		this.domBatch = (this.domBatch || 0) + 1;
		try {
			for (var i = 0; i < ops.length; i++) {
				var op = ops[i];
				var el;

				switch (op.op) {
					case 'html':
					case 'append':
					case 'prepend':
					case 'before':
					case 'after':
					case 'replace':
						this.applyHTML({
							target: op.target,
							mode: op.op === 'html' ? 'inner' : op.op,
							html: op.html || '',
						});
						lazyRoots.push(document.querySelector(op.target));
						break;

					case 'text':
						el = document.querySelector(op.target);
						if (el)
							el.textContent =
								op.value == null ? '' : String(op.value);
						break;

					case 'remove':
						el = document.querySelector(op.target);
						if (el) el.remove();
						break;

					case 'attr':
					case 'attrs':
						el = document.querySelector(op.target);
						if (el) patchAttrs(el, op.attrs || {});
						break;

					case 'title':
						document.title = op.value || '';
						break;

					case 'head':
						await this.applyHead(op);
						break;

					case 'cache-clear':
						this.cache.clear();
						break;

					default:
						throw new Error('Unknown patch op: ' + op.op);
				}
			}
		} finally {
			this.domBatch--;
		}

		lazyRoots.forEach(function (root) {
			if (!root) return;
			this.scanLazy(root);
			this.scanReveal(root);
			this.scanDeferredYouTube(root);
			this.scanDeferredIcons(root);
			this.bindStoreDOM(root);
		}, this);

		emit('patch-applied', { operations: ops });
	};

	Runtime.prototype.applySEO = function (frame) {
		this.seoState = {
			title: typeof frame.title === 'string' ? frame.title : '',
			description:
				typeof frame.description === 'string' ? frame.description : '',
			image: frame.image || '',
			noIndex: frame.noIndex === true,
		};
		if (typeof frame.title === 'string') {
			document.title = frame.title;
		}
		if (typeof frame.lang === 'string' && frame.lang.trim() !== '') {
			document.documentElement.lang = frame.lang;
		}

		this.seoNodes.forEach(function (node) {
			node.remove();
		});
		this.seoNodes.clear();

		var add = function (attr, name, content) {
			if (content == null || String(content).trim() === '') return;
			var node = document.createElement('meta');
			node.setAttribute('data-gosh-seo', '');
			node.setAttribute(attr, name);
			node.setAttribute('content', String(content));
			document.head.appendChild(node);
			this.seoNodes.add(node);
		}.bind(this);

		add('property', 'og:type', 'website');
		add('property', 'og:title', frame.title);
		add('name', 'twitter:title', frame.title);
		add('property', 'og:description', frame.description);
		add('name', 'twitter:description', frame.description);
		if (frame.image) add('name', 'twitter:card', 'summary_large_image');
		add('property', 'og:image', frame.image);
		add('name', 'twitter:image', frame.image);
		if (frame.noIndex) add('name', 'robots', 'noindex,nofollow');

		var description = this.headMeta.get('name:description');
		if (!description) {
			description = document.createElement('meta');
			description.setAttribute('name', 'description');
			document.head.appendChild(description);
			this.headMeta.set('name:description', description);
		}
		description.setAttribute('content', frame.description || '');
	};

	Runtime.prototype.readSEOState = function () {
		var description = this.headMeta.get('name:description');
		var image = this.headMeta.get('property:og:image');
		var robots = this.headMeta.get('name:robots');
		return {
			title: document.title || '',
			description: description
				? description.getAttribute('content') || ''
				: '',
			image: image ? image.getAttribute('content') || '' : '',
			noIndex: !!(
				robots &&
				/(?:^|,)\s*noindex(?:,|$)/i.test(
					robots.getAttribute('content') || '',
				)
			),
		};
	};

	Runtime.prototype.useSeo = function (input, data) {
		input = input || {};
		data = data || {};

		function lookup(obj, key) {
			if (!key) return undefined;
			return String(key)
				.split('.')
				.reduce(function (cur, part) {
					return cur == null ? undefined : cur[part];
				}, obj);
		}
		function interpolate(value, params) {
			var text = value == null ? '' : String(value);
			if (!params || typeof params !== 'object') return text;
			Object.keys(params).forEach(function (key) {
				var v = params[key] == null ? '' : String(params[key]);
				text = text.split('{' + key + '}').join(v);
				text = text.split(':' + key).join(v);
			});
			return text;
		}

		var current = this.seoState || this.readSEOState();
		var hasTitle =
			Object.prototype.hasOwnProperty.call(input, 'title') ||
			!!input.titleKey;
		var hasDescription =
			Object.prototype.hasOwnProperty.call(input, 'description') ||
			!!input.descriptionKey;
		var title = input.title;
		if ((title == null || title === '') && input.titleKey)
			title = lookup(data, input.titleKey);
		var description = input.description;
		if ((description == null || description === '') && input.descriptionKey)
			description = lookup(data, input.descriptionKey);

		var frame = {
			v: 1,
			type: 'seo',
			title: hasTitle ? interpolate(title, input.params) : current.title,
			description: hasDescription
				? interpolate(description, input.params)
				: current.description,
			image: Object.prototype.hasOwnProperty.call(input, 'image')
				? input.image || ''
				: current.image,
			noIndex: Object.prototype.hasOwnProperty.call(input, 'noIndex')
				? input.noIndex === true
				: current.noIndex,
		};
		this.applySEO(frame);
		emit('seo', { seo: frame });
		return frame;
	};

	Runtime.prototype.applyHead = async function (frame) {
		if (typeof frame.title === 'string') {
			document.title = frame.title;
		}

		if (frame.htmlAttrs) {
			syncAttrs(document.documentElement, frame.htmlAttrs);
		}

		if (frame.bodyAttrs) {
			syncAttrs(document.body, frame.bodyAttrs);
		}

		var incomingMeta = new Set();
		var incomingLinks = new Set();

		(frame.meta || []).forEach(function (d) {
			incomingMeta.add(metaKey(d));
		});

		(frame.links || []).forEach(function (d) {
			incomingLinks.add(linkKey(d));
		});

		if (frame.sync) {
			this.seoNodes.forEach(function (node) {
				node.remove();
			});
			this.seoNodes.clear();
			this.headMeta.forEach(function (node, key) {
				if (node.hasAttribute('data-runtime-persistent')) return;
				if (!incomingMeta.has(key)) {
					node.remove();
					this.headMeta.delete(key);
				}
			}, this);

			this.headLinks.forEach(function (node, key) {
				if (node.hasAttribute('data-runtime-persistent')) return;
				if (!incomingLinks.has(key)) {
					node.remove();
					this.headLinks.delete(key);
					this.loadedStyles.delete(key);
				}
			}, this);
		}

		(frame.meta || []).forEach(function (d) {
			var key = metaKey(d);
			var existing = this.headMeta.get(key);

			if (!existing) {
				existing = document.createElement('meta');
				document.head.appendChild(existing);
				this.headMeta.set(key, existing);
			}

			setDescriptor(existing, d);
			existing.setAttribute('data-runtime-meta-key', key);
		}, this);

		for (var i = 0; i < (frame.links || []).length; i++) {
			var link = frame.links[i];
			var key = linkKey(link);

			if (!key) continue;
			if (this.headLinks.has(key)) continue;

			var node = document.createElement('link');
			setDescriptor(node, link);

			if (
				String(link.rel || '')
					.split(/\s+/)
					.indexOf('stylesheet') !== -1
			) {
				await this.loadStylesheet(node, key);
			} else {
				document.head.appendChild(node);
				this.headLinks.set(key, node);
			}
		}
	};

	Runtime.prototype.applyPageStyles = async function (frame) {
		var hrefs = Array.isArray(frame.hrefs)
			? frame.hrefs.filter(Boolean)
			: [];
		var wanted = hrefs.map(absolute);
		var existing = Array.from(this.pageStyleNodes.values());

		if (
			existing.length === wanted.length &&
			existing.every(function (node, i) {
				return !node.disabled && node.href === wanted[i];
			})
		)
			return;

		var byHref = new Map();
		existing.forEach(function (node) {
			if (node.href) byHref.set(node.href, node);
		});
		var next = [];
		var created = [];

		for (var i = 0; i < hrefs.length; i++) {
			var abs = wanted[i];
			var reused = byHref.get(abs);
			if (reused && !reused.disabled) {
				next.push(reused);
				continue;
			}

			var node = document.createElement('link');
			node.rel = 'stylesheet';
			node.setAttribute('data-gosh-page-style', String(i));
			node.href = hrefs[i];
			var load = new Promise(function (resolve, reject) {
				var n = node;
				n.onload = function () {
					resolve();
				};
				n.onerror = function () {
					reject(new Error('Page stylesheet failed: ' + n.href));
				};
			});
			document.head.appendChild(node);
			this.pageStyleNodes.set(abs, node);
			next.push(node);
			created.push(load);
		}

		if (created.length) await Promise.all(created);

		var keep = new Set(next);
		existing.forEach(function (node) {
			if (!keep.has(node)) {
				this.pageStyleNodes.delete(node.href);
				node.remove();
			}
		}, this);
		next.forEach(function (node, i) {
			node.disabled = false;
			node.setAttribute('data-gosh-page-style', String(i));
		});
	};

	Runtime.prototype.scanStyles = function () {
		this.indexHead();
	};

	Runtime.prototype.loadStylesheet = function (node, key) {
		var self = this;

		if (this.loadedStyles.has(key)) {
			return Promise.resolve();
		}

		return new Promise(function (resolve, reject) {
			node.onload = function () {
				self.loadedStyles.add(key);
				self.headLinks.set(key, node);
				resolve();
			};

			node.onerror = function () {
				reject(new Error('Stylesheet failed: ' + node.href));
			};

			document.head.appendChild(node);
		});
	};

	Runtime.prototype.parseModulePlan = function (value) {
		if (!value) return { bindings: [] };
		try {
			var parsed = typeof value === 'string' ? JSON.parse(value) : value;
			if (parsed && Array.isArray(parsed.bindings)) return parsed;
			if (Array.isArray(parsed)) {
				return {
					bindings: parsed.filter(Boolean).map(function (src) {
						return { src: src, target: '#app' };
					}),
				};
			}
		} catch (_) {
			return {
				bindings: String(value)
					.split(',')
					.map(function (x) {
						return x.trim();
					})
					.filter(Boolean)
					.map(function (src) {
						return { src: src, target: '#app' };
					}),
			};
		}
		return { bindings: [] };
	};

	Runtime.prototype.loadPageRuntimePlan = async function () {
		var plan = this.pageRuntime && this.pageRuntime.modules;
		if (plan && Array.isArray(plan.bindings)) return plan;
		if (!this.pageRuntime || !this.pageRuntime.state) return null;
		try {
			var bootstrap = await import(
				'/_gosh/bootstrap/' +
					encodeURIComponent(this.pageRuntime.state) +
					'.js'
			);
			this.pageRuntime.modules = (bootstrap.default &&
				bootstrap.default.modules) || { bindings: [] };
			return this.pageRuntime.modules;
		} catch (_) {
			return null;
		}
	};

	Runtime.prototype.mountInitialPageModules = async function () {
		var app = document.querySelector('#app');
		if (!app) return;

		var plan = await this.loadPageRuntimePlan();
		if (plan && Array.isArray(plan.bindings) && plan.bindings.length) {
			await this.mountPageModules({
				plan: plan,
				data: { initial: true, path: location.pathname },
			});
			return;
		}

		var encodedPlan = app.getAttribute('data-gosh-modules');
		if (encodedPlan) {
			await this.mountPageModules({
				plan: this.parseModulePlan(encodedPlan),
				data: { initial: true, path: location.pathname },
			});
			return;
		}

		var legacy =
			app.getAttribute('data-page-modules') ||
			app.getAttribute('data-page-module');
		if (!legacy) return;
		await this.mountPageModules({
			plan: this.parseModulePlan(legacy),
			data: { initial: true, path: location.pathname },
		});
	};

	Runtime.prototype.loadChunk = function (src) {
		if (!src) return Promise.resolve({ sources: {} });
		var key = absolute(src);
		if (!this.chunkModules.has(key))
			this.chunkModules.set(key, import(key));
		return this.chunkModules.get(key);
	};

	Runtime.prototype.loadEmbeddedModule = async function (chunkSrc, id) {
		var cacheKey = absolute(chunkSrc) + '#' + id;
		if (this.embeddedModules.has(cacheKey))
			return this.embeddedModules.get(cacheKey);

		var promise = async function () {
			var chunk = await this.loadChunk(chunkSrc);
			var module = chunk && chunk.modules ? chunk.modules[id] : null;
			if (!module) throw new Error('Route module not found: ' + id);
			return module;
		}.call(this);

		this.embeddedModules.set(cacheKey, promise);
		return promise;
	};

	Runtime.prototype.announceRoute = function () {
		var announcer = document.getElementById('gosh-route-announcer');
		if (!announcer) return;
		var title = String(document.title || '').trim();
		announcer.textContent = '';
		setTimeout(function () {
			announcer.textContent = title;
		}, 0);
	};

	Runtime.prototype.applyCSPNonce = function (root) {
		if (!this.nonce || !root || !root.querySelectorAll) return;
		Array.prototype.forEach.call(
			root.querySelectorAll('style,script'),
			function (node) {
				if (!node.nonce && !node.getAttribute('nonce'))
					node.setAttribute('nonce', this.nonce);
			},
			this,
		);
	};

	Runtime.prototype.loadEntryModule = async function (id) {
		var cacheKey = 'entry#' + id;
		if (this.embeddedModules.has(cacheKey))
			return this.embeddedModules.get(cacheKey);

		var promise = async function () {
			var modules = window.__GOSH_ENTRY_MODULES__ || {};
			var module = modules[id];
			if (!module) throw new Error('Entry module not found: ' + id);
			return module;
		}.call(this);

		this.embeddedModules.set(cacheKey, promise);
		return promise;
	};

	Runtime.prototype.loadBindingModule = async function (binding, plan) {
		if (binding.src) return import(absolute(binding.src));
		if (
			binding.id &&
			window.__GOSH_ENTRY_MODULES__ &&
			window.__GOSH_ENTRY_MODULES__[binding.id]
		)
			return this.loadEntryModule(binding.id);
		if (binding.id && (binding.chunk || (plan && plan.chunk)))
			return this.loadEmbeddedModule(
				binding.chunk || plan.chunk,
				binding.id,
			);
		throw new Error('Invalid module binding');
	};

	Runtime.prototype.mountBinding = async function (binding, plan, data) {
		var generation = this.seq;
		var roots = [];
		if (binding.selector) {
			roots = Array.prototype.slice.call(
				document.querySelectorAll(binding.selector),
			);
		} else if (binding.target) {
			var single = document.querySelector(binding.target);
			if (single) roots = [single];
		}

		if (!roots.length) {
			if (binding.selector) return [];
			throw new Error('Module target not found: ' + binding.target);
		}

		var mod = await this.loadBindingModule(binding, plan);
		if (generation !== this.seq) return [];
		var chunk = binding.chunk || (plan && plan.chunk) || '';
		var label =
			binding.src || (chunk ? chunk + '#' + binding.id : binding.id);
		var mounted = [];

		for (var i = 0; i < roots.length; i++) {
			var root = roots[i];
			if (!root.isConnected || generation !== this.seq) continue;
			var ctx = {
				runtime: this,
				element: root,
				data: data || {},
				props: {},
				url: location.href,
				owner: binding.owner || '',
				instance: root.getAttribute
					? root.getAttribute('data-gosh-instance') || ''
					: '',
			};
			var cleanup = null;
			if (typeof mod.mount === 'function') {
				var result = await mod.mount(ctx);
				if (typeof result === 'function') cleanup = result;
			}
			var targetLabel = binding.selector || binding.target || '';
			emit('module-mounted', {
				src: label,
				target: targetLabel,
				instance: ctx.instance,
				owner: binding.owner || '',
			});
			await window._gosh.callHook('component:mounted', {
				runtime: this,
				src: label,
				target: targetLabel,
				instance: ctx.instance,
				owner: binding.owner || '',
				context: ctx,
			});
			mounted.push({
				src: label,
				target: targetLabel,
				element: root,
				mod: mod,
				ctx: ctx,
				cleanup: cleanup,
			});
		}
		return mounted;
	};

	Runtime.prototype.cleanupModuleEntry = async function (entry, label) {
		if (!entry) return;
		if (entry.ctx) entry.ctx.__goshDisposed = true;
		try {
			if (entry.cleanup) await entry.cleanup();
			else if (entry.mod && typeof entry.mod.unmount === 'function')
				await entry.mod.unmount(entry.ctx);
			var cleanups =
				entry.ctx && entry.ctx.__goshCleanups
					? entry.ctx.__goshCleanups.splice(0)
					: [];
			for (var i = cleanups.length - 1; i >= 0; i--) {
				try {
					await cleanups[i]();
				} catch (error) {
					console.error(
						'[runtime] ' + label + ' helper cleanup failed',
						error,
					);
				}
			}
		} catch (error) {
			console.error('[runtime] ' + label + ' cleanup failed', error);
		}
		emit('module-unmounted', { src: entry.src, target: entry.target });
		await window._gosh.callHook('component:unmounted', {
			runtime: this,
			src: entry.src,
			target: entry.target,
			context: entry.ctx,
		});
	};

	Runtime.prototype.mountPageModules = async function (frame) {
		await this.unmountPageModule();
		var plan = frame.plan || this.parseModulePlan(frame.srcs || []);
		var bindings = Array.isArray(plan.bindings) ? plan.bindings : [];
		var mounted = [];
		try {
			for (var i = 0; i < bindings.length; i++) {
				var entries = await this.mountBinding(
					bindings[i],
					plan,
					frame.data,
				);
				for (var e = 0; e < entries.length; e++)
					mounted.push(entries[e]);
			}
			this.pageModules = mounted;
		} catch (error) {
			for (var j = mounted.length - 1; j >= 0; j--)
				await this.cleanupModuleEntry(mounted[j], 'page module');
			throw error;
		}
	};

	Runtime.prototype.unmountPageModule = async function () {
		var entries = this.pageModules || [];
		this.pageModules = [];
		var app = document.querySelector('#app');
		for (var i = entries.length - 1; i >= 0; i--) {
			var entry = entries[i];
			var element = entry && entry.element;
			if (app && element && element !== app && !app.contains(element)) {
				var key = entry.target || 'document';
				var persistent = this.looseModules.get(key) || [];
				if (!Array.isArray(persistent)) persistent = [persistent];
				if (persistent.indexOf(entry) < 0) persistent.push(entry);
				this.looseModules.set(key, persistent);
				continue;
			}
			await this.cleanupModuleEntry(entry, 'page module');
		}
	};

	Runtime.prototype.unmountLooseTarget = async function (targetKey) {
		var entries = this.looseModules.get(targetKey);
		if (!entries) return;
		this.looseModules.delete(targetKey);
		if (!Array.isArray(entries)) entries = [entries];
		for (var i = entries.length - 1; i >= 0; i--)
			await this.cleanupModuleEntry(entries[i], 'component module');
	};

	Runtime.prototype.unmountLooseWithin = async function (targetSelector) {
		var root = document.querySelector(targetSelector);
		var keys = Array.from(this.looseModules.keys());
		for (var i = 0; i < keys.length; i++) {
			var entries = this.looseModules.get(keys[i]);
			if (!Array.isArray(entries)) entries = entries ? [entries] : [];
			var intersects = !root;
			for (var j = 0; j < entries.length && !intersects; j++) {
				var el = entries[j].element;
				if (el && (el === root || root.contains(el))) intersects = true;
			}
			if (intersects) await this.unmountLooseTarget(keys[i]);
		}
	};

	Runtime.prototype.unmountAllLooseModules = async function () {
		var keys = Array.from(this.looseModules.keys());
		for (var i = 0; i < keys.length; i++)
			await this.unmountLooseTarget(keys[i]);
	};

	Runtime.prototype.mountLooseModules = async function (frame) {
		var plan = frame.plan || this.parseModulePlan(frame.srcs || []);
		var bindings = Array.isArray(plan.bindings) ? plan.bindings : [];
		var grouped = new Map();
		for (var i = 0; i < bindings.length; i++) {
			var key = bindings[i].target || bindings[i].selector || 'document';
			if (!grouped.has(key)) grouped.set(key, []);
			grouped.get(key).push(bindings[i]);
		}

		var keys = Array.from(grouped.keys());
		for (var k = 0; k < keys.length; k++) {
			var targetKey = keys[k];
			await this.unmountLooseTarget(targetKey);
			var list = grouped.get(targetKey);
			var mounted = [];
			try {
				for (var j = 0; j < list.length; j++) {
					var entries = await this.mountBinding(
						list[j],
						plan,
						frame.data,
					);
					for (var e = 0; e < entries.length; e++)
						mounted.push(entries[e]);
				}
				this.looseModules.set(targetKey, mounted);
			} catch (error) {
				for (var x = mounted.length - 1; x >= 0; x--)
					await this.cleanupModuleEntry(
						mounted[x],
						'component module',
					);
				throw error;
			}
		}
	};

	Runtime.prototype.mountPageModule = async function (frame) {
		return this.mountPageModules({
			plan: {
				bindings: frame.src
					? [{ src: frame.src, target: frame.target || '#app' }]
					: [],
			},
			data: frame.data,
		});
	};

	Runtime.prototype.mountLooseModule = async function (frame) {
		return this.mountLooseModules({
			plan: {
				bindings: frame.src
					? [{ src: frame.src, target: frame.target || 'document' }]
					: [],
			},
			data: frame.data,
		});
	};

	Runtime.prototype.applyFullHTML = async function (html, sequence) {
		if (sequence && !this.isCurrent(sequence)) return;

		await this.unmountPageModule();

		var doc = new DOMParser().parseFromString(html, 'text/html');

		await this.applyHead({
			v: 1,
			type: 'head',
			sync: true,
			title: doc.title || '',
			meta: Array.prototype.map.call(
				doc.head.querySelectorAll('meta'),
				attrs,
			),
			links: Array.prototype.map.call(
				doc.head.querySelectorAll('link'),
				attrs,
			),
			htmlAttrs: attrs(doc.documentElement),
			bodyAttrs: attrs(doc.body),
		});

		var current = document.querySelector('#app');
		var incoming = doc.querySelector('#app');

		if (!current || !incoming) {
			throw new Error('Full HTML fallback requires #app');
		}

		var imported = document.importNode(incoming, true);
		current.replaceChildren();
		while (imported.firstChild) current.appendChild(imported.firstChild);

		var metadata = doc.getElementById('gosh-page-runtime');
		var modulePlan = null;
		if (metadata) {
			try {
				this.pageRuntime =
					JSON.parse(metadata.textContent || '{}') || {};
				modulePlan = this.pageRuntime.modules;
			} catch (_) {}
		}
		if (!modulePlan)
			modulePlan = incoming.getAttribute('data-gosh-modules');
		if (!modulePlan) modulePlan = await this.loadPageRuntimePlan();
		if (modulePlan) {
			await this.mountPageModules({
				plan: this.parseModulePlan(modulePlan),
				data: { path: location.pathname },
			});
		}

		emit('dom-updated', { target: '#app', mode: 'inner' });
	};

	Runtime.prototype.applyStyleLink = function (frame) {
		var href = frame && frame.href ? String(frame.href) : '';
		if (!href) return Promise.resolve();

		var absoluteHref = absolute(href);
		if (this.fragmentStyleNodes.has(absoluteHref)) return Promise.resolve();
		var self = this;

		return new Promise(function (resolve, reject) {
			var node = document.createElement('link');
			node.rel = 'stylesheet';
			node.href = href;
			node.setAttribute('data-gosh-fragment-style', '');
			node.onload = function () {
				self.fragmentStyleNodes.set(absoluteHref, node);
				resolve();
			};
			node.onerror = function () {
				reject(new Error('Failed to load stylesheet ' + href));
			};
			document.head.appendChild(node);
		});
	};
	Runtime.prototype.applyStyle = function (frame) {
		if (!frame.id) throw new Error('style frame requires id');
		var node = this.inlineStyleNodes.get(frame.id);
		if (!node) {
			node = document.createElement('style');
			node.setAttribute('data-gosh-style', frame.id);
			if (this.nonce) node.nonce = this.nonce;
			document.head.appendChild(node);
			this.inlineStyleNodes.set(frame.id, node);
		}
		node.textContent = frame.css || '';
	};

	Runtime.prototype.postRuntimeJSON = async function (
		url,
		payload,
		requestOptions,
	) {
		var requestOptions = requestOptions || {};
		this.beginLoading();
		try {
			await window._gosh.callHook('data:before', {
				runtime: this,
				url: url,
				payload: payload,
				method: 'POST',
			});
			var response = await fetch(url, {
				method: 'POST',
				signal: requestOptions.signal,
				headers: {
					'X-Runtime': '1',
					'X-GOSH-Runtime': 'action',
					Accept: 'application/x-ndjson, application/json;q=0.9',
					'Content-Type': 'application/json',
				},
				credentials: 'same-origin',
				body: JSON.stringify(payload),
			});
			if (!response.ok) {
				var postError = new Error(
					'HTTP ' + response.status + ' ' + url,
				);
				postError.goshStateExpired =
					response.status === 409 &&
					response.headers.get('X-GOSH-State') === 'expired';
				await window._gosh.callHook('data:error', {
					runtime: this,
					url: url,
					payload: payload,
					method: 'POST',
					error: postError,
				});
				throw postError;
			}
			await window._gosh.callHook('data:after', {
				runtime: this,
				url: url,
				payload: payload,
				method: 'POST',
				response: response,
			});
			var type = String(
				response.headers.get('content-type') || '',
			).toLowerCase();
			if (requestOptions.signal && requestOptions.signal.aborted) return;
			if (
				requestOptions.sequence !== undefined &&
				requestOptions.sequence !== this.seq
			)
				return;
			if (type.indexOf('application/x-ndjson') !== -1) {
				await this.consumeNDJSON(response, {
					render: true,
					sequence: 0,
					generation: requestOptions.sequence,
				});
				return;
			}
			var frame = await response.json();
			if (requestOptions.signal && requestOptions.signal.aborted) return;
			if (
				requestOptions.sequence !== undefined &&
				requestOptions.sequence !== this.seq
			)
				return;
			await this.applyFrame(frame, 0, requestOptions.sequence);
		} finally {
			this.endLoading();
		}
	};

	Runtime.prototype.invokeAction = async function (action, source, options) {
		if (!action) return;
		var component = source.closest('[data-gosh-component]');
		var app = document.querySelector('#app');
		var payload;
		var fields = options && options.fields;
		if (!fields && source instanceof HTMLFormElement)
			fields = formFields(source);
		if (component) {
			payload = {
				kind: 'component',
				name: component.getAttribute('data-gosh-component') || '',
				instance: component.getAttribute('data-gosh-instance') || '',
				action: action,
				state: component.getAttribute('data-gosh-state') || '',
				fields: fields || undefined,
				url: location.pathname + location.search,
			};
		} else if (app) {
			payload = {
				kind: 'page',
				name:
					(this.pageRuntime && this.pageRuntime.page) ||
					app.getAttribute('data-gosh-page') ||
					'',
				instance: 'app',
				action: action,
				state:
					(this.pageRuntime && this.pageRuntime.state) ||
					app.getAttribute('data-gosh-state') ||
					'',
				fields: fields || undefined,
				url: location.pathname + location.search,
			};
		} else return;

		var self = this;
		var optimistic = options && options.optimistic;
		var rollback =
			optimistic && typeof optimistic.rollback === 'function'
				? optimistic.rollback
				: null;
		try {
			if (optimistic && typeof optimistic.apply === 'function')
				optimistic.apply();
			await this.postRuntimeJSON('/_gosh/action', payload);
			emit('action-success', { action: action, source: source });
			return true;
		} catch (error) {
			if (rollback) rollback(error);
			if (error && error.goshStateExpired) {
				location.reload();
				return false;
			}
			self.reportError(
				component || app || source,
				error,
				'action',
				function () {
					self.invokeAction(action, source, options);
				},
			);
			emit('action-error', { action: action, error: error });
			return false;
		}
	};

	Runtime.prototype.scanLazy = function (root) {
		var self = this;
		var nodes = [];
		if (root instanceof Element && root.matches('[data-gosh-lazy="1"]'))
			nodes.push(root);
		if (root && root.querySelectorAll)
			nodes = nodes.concat(
				Array.prototype.slice.call(
					root.querySelectorAll('[data-gosh-lazy="1"]'),
				),
			);
		nodes.forEach(function (node) {
			if (
				self.lazyNodes.has(node) ||
				node.getAttribute('data-gosh-lazy-loading') === '1'
			)
				return;
			var priority = node.getAttribute('data-gosh-island') || 'visible';
			if (priority === 'manual') return;
			if (priority === 'immediate') {
				self.loadLazy(node);
				return;
			}
			if (priority === 'idle') {
				var idle =
					typeof requestIdleCallback === 'function'
						? requestIdleCallback(
								function () {
									self.loadLazy(node);
								},
								{ timeout: 1200 },
							)
						: setTimeout(function () {
								self.loadLazy(node);
							}, 32);
				self.lazyIdle.set(node, idle);
				return;
			}
			if (priority === 'interaction') {
				var activate = function () {
					node.removeEventListener('pointerdown', activate);
					node.removeEventListener('focusin', activate);
					self.loadLazy(node);
				};
				node.addEventListener('pointerdown', activate, {
					once: true,
					passive: true,
				});
				node.addEventListener('focusin', activate, { once: true });
				return;
			}
			if (typeof IntersectionObserver !== 'function') {
				self.loadLazy(node);
				return;
			}
			if (!self.lazyObserver)
				self.lazyObserver = new IntersectionObserver(
					function (entries) {
						entries.forEach(function (entry) {
							if (
								!entry.isIntersecting &&
								entry.intersectionRatio <= 0
							)
								return;
							self.lazyObserver.unobserve(entry.target);
							var lazyRoot =
								self.lazyObservedRoots.get(entry.target) ||
								entry.target;
							self.lazyObservedRoots.delete(entry.target);
							self.loadLazy(lazyRoot);
						});
					},
					{ rootMargin: '240px 0px' },
				);
			var observed = node.firstElementChild || node;
			if (observed === node && node.getClientRects().length === 0) {
				self.loadLazy(node);
				return;
			}
			self.lazyObservedRoots.set(observed, node);
			self.lazyObserver.observe(observed);
		});
	};

	Runtime.prototype.shouldSkipReveal = function () {
		if (
			typeof matchMedia === 'function' &&
			matchMedia('(prefers-reduced-motion: reduce)').matches
		)
			return true;
		var connection =
			navigator.connection ||
			navigator.mozConnection ||
			navigator.webkitConnection;
		return !!(
			connection &&
			(connection.saveData ||
				/(^|-)2g$/.test(connection.effectiveType || ''))
		);
	};

	Runtime.prototype.flushRevealQueue = function () {
		var self = this;
		var queue = this.revealQueue;
		this.revealQueue = [];
		this.revealTimer = null;
		queue.forEach(function (node, index) {
			self.revealQueued.delete(node);
			if (!node.isConnected || node.classList.contains('is-visible'))
				return;
			var step = Number(node.getAttribute('data-gosh-reveal-step'));
			if (!Number.isFinite(step) || step < 0) step = 100;
			self.setStyleProperty(
				node,
				'--reveal-delay',
				String(index * step) + 'ms',
			);
			var show = function () {
				node.classList.add('is-visible');
			};
			if (typeof requestAnimationFrame === 'function')
				requestAnimationFrame(show);
			else setTimeout(show, 0);
		});
	};

	Runtime.prototype.queueReveal = function (node) {
		if (
			this.revealQueued.has(node) ||
			node.classList.contains('is-visible')
		)
			return;
		this.revealQueued.add(node);
		this.revealQueue.push(node);
		if (this.revealTimer) clearTimeout(this.revealTimer);
		var self = this;
		this.revealTimer = setTimeout(function () {
			self.flushRevealQueue();
		}, 50);
	};

	Runtime.prototype.scanReveal = function (root) {
		var self = this;
		var nodes = [];
		if (root instanceof Element && root.matches('[data-gosh-reveal]'))
			nodes.push(root);
		if (root && root.querySelectorAll)
			nodes = nodes.concat(
				Array.prototype.slice.call(
					root.querySelectorAll('[data-gosh-reveal]'),
				),
			);
		nodes.forEach(function (node) {
			if (self.revealNodes.has(node)) return;
			self.revealNodes.add(node);
			if (
				self.shouldSkipReveal() ||
				typeof IntersectionObserver !== 'function'
			) {
				node.classList.add('is-visible');
				return;
			}
			var type = (node.getAttribute('data-gosh-reveal') || 'slide')
				.trim()
				.toLowerCase();
			if (!/^[a-z0-9-]+$/.test(type)) type = 'slide';
			node.classList.add('reveal-active', 'reveal-' + type);
			var speed = (
				node.getAttribute('data-gosh-reveal-speed') || ''
			).toLowerCase();
			if (speed === 'fast' || speed === 'slow')
				node.classList.add('reveal-' + speed);
			if (!self.revealObserver)
				self.revealObserver = new IntersectionObserver(
					function (entries) {
						entries.forEach(function (entry) {
							var element = entry.target;
							var repeat = element.hasAttribute(
								'data-gosh-reveal-repeat',
							);
							if (
								entry.isIntersecting ||
								entry.intersectionRatio > 0
							) {
								self.queueReveal(element);
								if (!repeat)
									self.revealObserver.unobserve(element);
							} else if (
								repeat &&
								element.classList.contains('is-visible')
							) {
								element.classList.remove('is-visible');
								self.setStyleProperty(
									element,
									'--reveal-delay',
									'',
								);
							}
						});
					},
					{ rootMargin: '20px 0px 20px 0px', threshold: [0, 0.15] },
				);
			self.revealObserver.observe(node);
		});
	};

	Runtime.prototype.scanDeferredYouTube = function (root) {
		var self = this;
		var nodes = [];
		if (root instanceof Element && root.matches('iframe[data-youtube-src]'))
			nodes.push(root);
		if (root && root.querySelectorAll)
			nodes = nodes.concat(
				Array.prototype.slice.call(
					root.querySelectorAll('iframe[data-youtube-src]'),
				),
			);
		nodes.forEach(function (node) {
			if (
				self.youtubeNodes.has(node) ||
				!node.getAttribute('data-youtube-src')
			)
				return;
			self.youtubeNodes.add(node);
			var load = function (iframe) {
				var src = iframe.getAttribute('data-youtube-src');
				if (!src) return;
				iframe.setAttribute('src', src);
				iframe.removeAttribute('data-youtube-src');
			};
			if (typeof IntersectionObserver !== 'function') {
				load(node);
				return;
			}
			if (!self.youtubeObserver)
				self.youtubeObserver = new IntersectionObserver(
					function (entries) {
						entries.forEach(function (entry) {
							if (
								!entry.isIntersecting &&
								entry.intersectionRatio <= 0
							)
								return;
							self.youtubeObserver.unobserve(entry.target);
							load(entry.target);
						});
					},
					{ rootMargin: '240px 0px' },
				);
			self.youtubeObserver.observe(node);
		});
	};

	Runtime.prototype.scanDeferredIcons = function (root) {
		var self = this;
		var nodes = [];
		if (root instanceof Element && root.matches('[data-ui-icon-src]'))
			nodes.push(root);
		if (root && root.querySelectorAll)
			nodes = nodes.concat(
				Array.prototype.slice.call(
					root.querySelectorAll('[data-ui-icon-src]'),
				),
			);
		nodes.forEach(function (node) {
			if (
				self.iconNodes.has(node) ||
				!node.getAttribute('data-ui-icon-src')
			)
				return;
			self.iconNodes.add(node);
			var load = function (icon) {
				var src = icon.getAttribute('data-ui-icon-src');
				var key = icon.getAttribute('data-ui-icon-key');
				var size = icon.getAttribute('data-ui-icon-size') || '';
				if (!src || !key) return;
				try {
					var url = new URL(src, document.baseURI);
					if (url.protocol !== 'https:') return;
					var css =
						'[data-ui-icon-key="' +
						cssEscape(key) +
						'"]{display:inline-block;width:1.25rem;height:1.25rem;flex-shrink:0;vertical-align:middle;background-color:currentColor;mask-repeat:no-repeat;mask-size:100% 100%;mask-image:url("' +
						url.href +
						'");-webkit-mask-repeat:no-repeat;-webkit-mask-size:100% 100%;-webkit-mask-image:url("' +
						url.href +
						'")';
					if (
						/^\d+(?:\.\d+)?(?:px|rem|em|vh|vw|%|pt|pc|in|cm|mm)$/.test(
							size,
						)
					)
						css += ';width:' + size + ';height:' + size;
					css += '}';
					var style = document.createElement('style');
					if (self.nonce) style.setAttribute('nonce', self.nonce);
					style.textContent = css;
					document.head.appendChild(style);
					icon.removeAttribute('data-ui-icon-src');
				} catch (_) {}
			};
			if (typeof IntersectionObserver !== 'function') {
				load(node);
				return;
			}
			if (!self.iconObserver)
				self.iconObserver = new IntersectionObserver(
					function (entries) {
						entries.forEach(function (entry) {
							if (
								!entry.isIntersecting &&
								entry.intersectionRatio <= 0
							)
								return;
							self.iconObserver.unobserve(entry.target);
							load(entry.target);
						});
					},
					{ rootMargin: '240px 0px' },
				);
			self.iconObserver.observe(node);
		});
	};

	Runtime.prototype.loadLazy = function (node) {
		var self = this;
		if (
			!node ||
			!node.isConnected ||
			(this.active &&
				this.active.controller &&
				!this.active.controller.signal.aborted)
		)
			return;
		if (
			!node ||
			this.lazyNodes.has(node) ||
			node.getAttribute('data-gosh-lazy-loading') === '1'
		)
			return;
		this.lazyNodes.add(node);
		node.setAttribute('data-gosh-lazy-loading', '1');
		var controller = new AbortController();
		var sequence = this.seq;
		this.lazyRequests.add(controller);
		var request =
			node.getAttribute('data-gosh-lazy-cache') === '1'
				? this.loadCachedTeleport(node, controller.signal, sequence)
				: this.postRuntimeJSON(
						'/_gosh/lazy',
						{
							kind: 'component',
							name:
								node.getAttribute('data-gosh-component') || '',
							instance:
								node.getAttribute('data-gosh-instance') || '',
							state: node.getAttribute('data-gosh-state') || '',
							url: location.pathname + location.search,
						},
						{ signal: controller.signal, sequence: sequence },
					);
		request
			.then(function () {
				if (node.isConnected) emit('lazy-loaded', { element: node });
			})
			.catch(function (error) {
				self.lazyNodes.delete(node);
				node.removeAttribute('data-gosh-lazy-loading');
				if (
					isAbort(error) ||
					controller.signal.aborted ||
					sequence !== self.seq ||
					!node.isConnected
				)
					return;
				if (error && error.goshStateExpired) {
					location.reload();
					return;
				}
				self.reportError(node, error, 'lazy', function () {
					self.loadLazy(node);
				});
				emit('lazy-error', { element: node, error: error });
			})
			.finally(function () {
				self.lazyRequests.delete(controller);
			});
	};

	Runtime.prototype.loadCachedTeleport = async function (
		node,
		signal,
		sequence,
	) {
		var name = node.getAttribute('data-gosh-component') || '';
		var instance = node.getAttribute('data-gosh-instance') || '';
		if (!name || !instance) throw new Error('Invalid cached teleport');
		var source = node.getAttribute('data-gosh-lazy-src') || '';
		if (!source) throw new Error('Cached teleport source is missing');
		var response = await fetch(source, {
			signal: signal,
			method: 'GET',
			credentials: 'same-origin',
			cache: 'force-cache',
			headers: { Accept: 'application/json' },
		});
		if (!response.ok)
			throw new Error('HTTP ' + response.status + ' /_gosh/teleport');
		var payload = await response.json();
		if (
			(signal && signal.aborted) ||
			(sequence !== undefined && sequence !== this.seq) ||
			!node.isConnected
		)
			return;
		var target = '[data-gosh-instance="' + cssEscape(instance) + '"]';
		this.applyHTML({
			target: target,
			mode: 'inner',
			html: payload.html || '',
		});
		patchAttrs(document.querySelector(target), {
			'data-gosh-lazy': null,
			'data-gosh-lazy-cache': null,
			'data-gosh-lazy-src': null,
			'data-gosh-state': null,
		});
		var plan = payload.modules || { bindings: [] };
		var bindings = Array.isArray(plan.bindings) ? plan.bindings : [];
		for (var i = 0; i < bindings.length; i++)
			bindings[i] = Object.assign({}, bindings[i], {
				target: target,
				selector: undefined,
			});
		await this.mountLooseModules({ plan: { bindings: bindings } });
	};

	Runtime.prototype.prefetch = async function (url) {
		var key = this.cacheKey(url, null);
		if (this.cacheGet(key)) return;
		if (this.prefetchFlights.has(key)) return this.prefetchFlights.get(key);
		if (
			this.prefetchActive >=
			Math.max(1, Number(this.options.prefetchMaxConcurrent) || 1)
		)
			return;
		var self = this;
		var flight = async function () {
			self.prefetchActive++;
			try {
				var payload = await this.fetchPayload(url, {
					method: 'GET',
					body: null,
					target: null,
					mode: 'inner',
					signal: undefined,
					render: false,
					prefetch: true,
				});

				if (payload.cacheable) {
					self.cacheSet(key, payload.cacheValue, payload.ttl);
				}
			} catch (_) {
			} finally {
				self.prefetchActive--;
				self.prefetchFlights.delete(key);
			}
		}.call(this);
		this.prefetchFlights.set(key, flight);
		return flight;
	};

	Runtime.prototype.reportError = function (target, error, kind, retry) {
		window._gosh._error = error;
		if (!target) return;
		target.setAttribute('data-gosh-error', kind || 'runtime');
		var existing = null;
		Array.prototype.forEach.call(target.children || [], function (child) {
			if (
				child.classList &&
				child.classList.contains('gosh-runtime-error')
			)
				existing = child;
		});
		if (!existing) {
			existing = document.createElement('div');
			existing.className = 'gosh-runtime-error';
			existing.setAttribute('role', 'alert');
			existing.setAttribute('data-runtime-persistent', '');
			target.appendChild(existing);
		}
		existing.textContent = 'Unable to update this section. ';
		if (typeof retry === 'function') {
			var button = document.createElement('button');
			button.type = 'button';
			button.textContent = 'Retry';
			button.setAttribute('data-gosh-retry', '');
			button.__goshRetry = retry;
			existing.appendChild(button);
		}
		emit('runtime-error', {
			target: target,
			error: error,
			kind: kind || 'runtime',
		});
	};

	Runtime.prototype.cacheKey = function (url, target) {
		return absolute(url) + '|target:' + (target || '__page__');
	};

	Runtime.prototype.cacheGet = function (key) {
		var item = this.cache.get(key);
		if (!item) return null;

		if (item.expires <= Date.now()) {
			this.cache.delete(key);
			return null;
		}

		this.cache.delete(key);
		this.cache.set(key, item);

		return clone(item.value);
	};

	Runtime.prototype.cacheSet = function (key, value, ttl) {
		this.cache.delete(key);

		this.cache.set(key, {
			expires:
				Date.now() +
				Math.max(0, ttl == null ? this.options.cacheTTL : ttl),
			value: clone(value),
			tags: this.cacheTagsFor(value),
		});

		while (this.cache.size > this.options.cacheLimit) {
			this.cache.delete(this.cache.keys().next().value);
		}
	};

	Runtime.prototype.cacheTagsFor = function (value) {
		var frames = value && value.kind === 'ndjson' ? value.frames : [];
		for (var i = 0; i < frames.length; i++) {
			if (
				frames[i] &&
				frames[i].type === 'cache-tags' &&
				Array.isArray(frames[i].tags)
			)
				return frames[i].tags.slice();
		}
		return [];
	};

	Runtime.prototype.invalidateCacheTags = function (tags) {
		if (!Array.isArray(tags) || !tags.length) return;
		this.cache.forEach(function (entry, key) {
			if (
				(entry.tags || []).some(function (tag) {
					return tags.indexOf(tag) >= 0;
				})
			)
				this.cache.delete(key);
		}, this);
		emit('cache-invalidated', { tags: tags });
	};

	Runtime.prototype.applyCached = async function (cached, sequence) {
		if (cached.kind === 'ndjson') {
			for (var i = 0; i < cached.frames.length; i++) {
				await this.applyFrame(cached.frames[i], sequence);
			}
			return;
		}

		if (cached.kind === 'json') {
			await this.applyFrame(cached.frame, sequence);
			return;
		}

		if (cached.kind === 'html') {
			await this.applyFullHTML(cached.html, sequence);
			return;
		}

		throw new Error('Unknown cache entry');
	};

	Runtime.prototype.commitHistory = function (url, mode) {
		if (mode === 'none') return;

		var state = {};
		state[STATE_KEY] = {
			id: uid(),
			url: url,
			x: 0,
			y: 0,
		};

		if (mode === 'replace') {
			history.replaceState(state, '', url);
		} else {
			history.pushState(state, '', url);
		}
	};

	Runtime.prototype.finish = function (options, url) {
		var restore = options.restoreScroll;

		afterPaint(function () {
			if (options.scroll === 'restore' && restore) {
				window.scrollTo(restore.x || 0, restore.y || 0);
			} else if (options.scroll === 'top') {
				window.scrollTo(0, 0);
			}

			if (!options.target) {
				var app = document.querySelector('#app');
				if (app && typeof app.focus === 'function') {
					try {
						app.focus({ preventScroll: true });
					} catch (_) {
						app.focus();
					}
				}
			}
		});

		emit('navigation-end', { url: url, options: options });
		window._gosh.callHook('page:afterNavigate', {
			runtime: this,
			url: url,
			options: options,
		});
		this.announceRoute();
	};

	Runtime.prototype.drainBootstrapQueue = function () {
		var self = this;
		var queue = window.__RUNTIME_BOOT_QUEUE__ || [];

		window.__RUNTIME_BOOT_QUEUE__ = [];

		queue.forEach(function (item) {
			if (item.kind === 'navigate') {
				self.navigate(item.url, {
					history: item.history || 'push',
					target: item.target || null,
					mode: item.mode || 'inner',
					scroll: item.target ? 'preserve' : 'top',
				});
				return;
			}

			if (item.kind === 'submit') {
				var method = String(item.method || 'GET').toUpperCase();

				self.navigate(item.url, {
					method: method,
					body: item.body || null,
					history: method === 'GET' && !item.target ? 'push' : 'none',
					target: item.target || null,
					mode: item.mode || 'inner',
					scroll: 'preserve',
				});
			}
		});
	};

	Runtime.prototype.isCurrent = function (sequence) {
		return !!this.active && this.active.sequence === sequence;
	};

	Runtime.prototype.load = function (url, target, options) {
		return this.navigate(
			url,
			Object.assign(
				{
					target: target,
					history: 'none',
					scroll: 'preserve',
				},
				options || {},
			),
		);
	};

	function cssEscape(value) {
		if (window.CSS && typeof window.CSS.escape === 'function')
			return window.CSS.escape(String(value));
		return String(value).replace(/[^a-zA-Z0-9_-]/g, '\\$&');
	}

	function fragment(html) {
		var t = document.createElement('template');
		t.innerHTML = html;
		return t.content;
	}

	function patchAttrs(el, values) {
		Object.keys(values).forEach(function (name) {
			var value = values[name];

			if (value === null || value === false) {
				el.removeAttribute(name);
			} else if (value === true) {
				el.setAttribute(name, '');
			} else {
				el.setAttribute(name, String(value));
			}
		});
	}

	function syncAttrs(el, values) {
		var preserve = new Set(['data-runtime-persistent']);

		Array.prototype.slice.call(el.attributes).forEach(function (attr) {
			if (preserve.has(attr.name)) return;

			if (!Object.prototype.hasOwnProperty.call(values, attr.name)) {
				el.removeAttribute(attr.name);
			}
		});

		patchAttrs(el, values);
	}

	function attrs(el) {
		var out = {};
		Array.prototype.forEach.call(el.attributes, function (attr) {
			out[attr.name] = attr.value === '' ? true : attr.value;
		});
		return out;
	}

	function setDescriptor(el, d) {
		Array.prototype.slice.call(el.attributes).forEach(function (attr) {
			if (attr.name !== 'data-runtime-persistent')
				el.removeAttribute(attr.name);
		});

		patchAttrs(el, d);
	}

	function metaKey(d) {
		if (d.charset) return 'charset';
		if (d.name) return 'name:' + d.name;
		if (d.property) return 'property:' + d.property;
		if (d['http-equiv']) return 'http-equiv:' + d['http-equiv'];
		if (d.itemprop) return 'itemprop:' + d.itemprop;
		return 'meta:' + JSON.stringify(d);
	}

	function findMeta(d) {
		if (d.charset) return document.head.querySelector('meta[charset]');
		if (d.name)
			return document.head.querySelector(
				'meta[name="' + esc(d.name) + '"]',
			);
		if (d.property)
			return document.head.querySelector(
				'meta[property="' + esc(d.property) + '"]',
			);
		if (d['http-equiv'])
			return document.head.querySelector(
				'meta[http-equiv="' + esc(d['http-equiv']) + '"]',
			);
		if (d.itemprop)
			return document.head.querySelector(
				'meta[itemprop="' + esc(d.itemprop) + '"]',
			);
		return null;
	}

	function linkKey(d) {
		if (!d.rel || !d.href) return null;
		return [
			d.rel,
			absolute(d.href),
			d.as || '',
			d.media || '',
			d.type || '',
		].join('|');
	}

	function findLink(key) {
		var links = document.head.querySelectorAll('link');

		for (var i = 0; i < links.length; i++) {
			if (linkKey(attrs(links[i])) === key) return links[i];
		}

		return null;
	}

	function absolute(value) {
		return new URL(value, document.baseURI).href;
	}

	function sameOrigin(value) {
		return new URL(value, document.baseURI).origin === location.origin;
	}

	function esc(value) {
		if (window.CSS && CSS.escape) return CSS.escape(value);
		return String(value).replace(/["\\]/g, '\\$&');
	}

	function emit(name, detail) {
		document.dispatchEvent(
			new CustomEvent('runtime:' + name, {
				detail: detail || {},
			}),
		);
	}

	function isAbort(error) {
		return error && (error.name === 'AbortError' || error.code === 20);
	}

	function uid() {
		return (
			'r' + Date.now().toString(36) + Math.random().toString(36).slice(2)
		);
	}

	function clone(value) {
		return JSON.parse(JSON.stringify(value));
	}

	function intOr(value, fallback) {
		var n = parseInt(value, 10);
		return Number.isFinite(n) ? n : fallback;
	}

	function afterPaint(fn) {
		if (typeof requestAnimationFrame === 'function') {
			requestAnimationFrame(function () {
				requestAnimationFrame(fn);
			});
		} else {
			setTimeout(fn, 0);
		}
	}

	function formFields(form) {
		var output = {};
		if (!form || typeof FormData !== 'function') return output;
		new FormData(form).forEach(function (value, key) {
			if (typeof File !== 'undefined' && value instanceof File) return;
			if (Object.prototype.hasOwnProperty.call(output, key)) {
				output[key] = Array.isArray(output[key])
					? output[key]
					: [output[key]];
				output[key].push(value);
			} else {
				output[key] = value;
			}
		});
		return output;
	}

	function removeSPALoader() {
		var loader = document.getElementById('spa-loader');
		if (!loader) return;
		loader.setAttribute('aria-hidden', 'true');
		if (window._gosh && window._gosh.setStyleProperty)
			window._gosh.setStyleProperty(loader, 'opacity', '0');
		setTimeout(function () {
			loader.remove();
		}, 150);
	}

	window.UniversalRuntime = Runtime;
	window._gosh = window._gosh || {};
	window._gosh.start = function () {
		if (window._gosh.runtime) return window._gosh.runtime;
		if (!supportsRuntime()) {
			window._gosh.runtimeUnsupported = true;
			document.documentElement.setAttribute(
				'data-gosh-runtime',
				'unsupported',
			);
			emit('runtime-unsupported', {});
			return null;
		}
		window.runtime = new Runtime();
		removeSPALoader();
		return window.runtime;
	};
})();
