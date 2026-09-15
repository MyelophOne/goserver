import Fuse from 'fuse.js';

const databaseName = 'myelophone-site-search-v3';
const storeName = 'indexes';
const excludedExtensions =
	/\.(?:avif|bmp|css|csv|docx?|gif|ico|jpe?g|js|json|map|mp3|mp4|ogg|pdf|png|pptx?|svg|txt|webm|webp|woff2?|xlsx?|xml|zip)$/i;
let documents = [];
const indexes = new Map();
let serverSearchEnabled = false;
serverSearchEnabled =
	new URL(self.location.href).searchParams.get('serverSearch') === '1';

const decodeEntities = (value) =>
	value.replace(/&(#x?[\da-f]+|[a-z]+);/gi, (entity, code) => {
		const named = {
			amp: '&',
			apos: "'",
			gt: '>',
			lt: '<',
			nbsp: ' ',
			quot: '"',
		};
		if (code[0] !== '#') return named[code.toLowerCase()] ?? entity;
		const hex = code[1]?.toLowerCase() === 'x';
		const point = Number.parseInt(code.slice(hex ? 2 : 1), hex ? 16 : 10);
		return Number.isFinite(point) ? String.fromCodePoint(point) : entity;
	});

const textFromHTML = (value) =>
	decodeEntities(
		String(value || '')
			.replace(/<!--[\s\S]*?-->/g, ' ')
			.replace(
				/<(script|style|noscript|template|svg|header|footer|nav|form|dialog)\b[^>]*>[\s\S]*?<\/\1>/gi,
				' ',
			)
			.replace(
				/<([a-z][\w-]*)\b[^>]*(?:data-nosnippet|aria-hidden=["']?true)[^>]*>[\s\S]*?<\/\1>/gi,
				' ',
			)
			.replace(/<br\s*\/?\s*>/gi, ' ')
			.replace(/<[^>]+>/g, ' '),
	)
		.replace(/\s+/g, ' ')
		.trim();

const attribute = (tag, name) => {
	const match = String(tag || '').match(
		new RegExp(
			'\\b' + name + '\\s*=\\s*(?:["\']([^"\']*)["\']|([^\\s>]+))',
			'i',
		),
	);
	return match?.[1] ?? match?.[2] ?? '';
};

const metaContent = (html, name) => {
	for (const match of html.matchAll(/<meta\b[^>]*>/gi)) {
		const key =
			attribute(match[0], 'name') || attribute(match[0], 'property');
		if (key.toLowerCase() === name.toLowerCase())
			return decodeEntities(attribute(match[0], 'content')).trim();
	}
	return '';
};

const normalizeLocale = (value) =>
	String(value || 'en')
		.trim()
		.toLowerCase()
		.replace('_', '-')
		.split('-')[0] || 'en';

const inferLocale = (url, options) => {
	const first =
		url.pathname.split('/').filter(Boolean)[0]?.toLowerCase() || '';
	const configured = Array.isArray(options.locales)
		? options.locales.map(normalizeLocale)
		: [];
	if (configured.includes(normalizeLocale(first)))
		return normalizeLocale(first);
	if (!configured.length && /^[a-z]{2,3}(?:-[a-z]{2})?$/i.test(first))
		return normalizeLocale(first);
	return normalizeLocale(options.defaultLocale);
};

const is404Document = (html, status, url) => {
	if (status === 404 || /\/(?:404)(?:\.html?)?\/?$/i.test(url.pathname))
		return true;
	if (/\bdata-view-not-found\b/i.test(html)) return true;
	const h1 = textFromHTML(
		(html.match(/<h1\b[^>]*>([\s\S]*?)<\/h1>/i) || [])[1],
	);
	return (
		h1 === '404' &&
		/(?:not found|page not found|страница не найдена|nie znaleziono)/i.test(
			textFromHTML(html),
		)
	);
};

const extractDocument = (payload, sourceURL, options, status) => {
	const url = new URL(sourceURL);
	if (status === 404 || payload?.noIndex) return null;
	const title = String(payload?.title || url.pathname).trim();
	const description = String(payload?.description || '').trim();
	const content = String(payload?.content || '')
		.trim()
		.slice(0, options.maxContentLength);
	if (!title && !description && !content) return null;
	url.hash = '';
	return {
		id: url.href,
		url: url.href,
		path: url.pathname + url.search,
		title,
		description,
		headings: '',
		content,
		locale: inferLocale(url, options),
	};
};

const indexableURL = (value, origin) => {
	try {
		const url = new URL(value, origin);
		return (
			url.origin === origin &&
			/https?:/.test(url.protocol) &&
			!url.search &&
			!excludedExtensions.test(url.pathname) &&
			!url.pathname.startsWith('/api/') &&
			!url.pathname.startsWith('/_')
		);
	} catch (_) {
		return false;
	}
};

const linksFromHTML = (html, from, origin) =>
	[...html.matchAll(/<a\b[^>]*>/gi)]
		.filter(
			(match) => !/\brel\s*=\s*["'][^"']*\bnofollow\b/i.test(match[0]),
		)
		.map((match) => attribute(match[0], 'href'))
		.filter(Boolean)
		.map((href) => {
			try {
				const url = new URL(href, from);
				url.hash = '';
				return indexableURL(url.href, origin) ? url.href : '';
			} catch (_) {
				return '';
			}
		})
		.filter(Boolean);

const openDatabase = () =>
	new Promise((resolve, reject) => {
		const request = indexedDB.open(databaseName, 1);
		request.onupgradeneeded = () => {
			if (!request.result.objectStoreNames.contains(storeName))
				request.result.createObjectStore(storeName, { keyPath: 'key' });
		};
		request.onsuccess = () => resolve(request.result);
		request.onerror = () => reject(request.error);
	});

async function readCache(key) {
	try {
		const database = await openDatabase();
		return await new Promise((resolve) => {
			const request = database
				.transaction(storeName, 'readonly')
				.objectStore(storeName)
				.get(key);
			request.onsuccess = () => resolve(request.result);
			request.onerror = () => resolve();
		});
	} catch (_) {
		return undefined;
	}
}

async function writeCache(record) {
	try {
		const database = await openDatabase();
		await new Promise((resolve) => {
			const request = database
				.transaction(storeName, 'readwrite')
				.objectStore(storeName)
				.put(record);
			request.onsuccess = request.onerror = () => resolve();
		});
	} catch (_) {}
}

const snippetFor = (item, query, matches) => {
	const match = matches?.find((entry) =>
		['content', 'headings', 'description'].includes(entry.key),
	);
	const source = match ? String(item[match.key] || '') : item.content;
	const first =
		match?.indices?.[0]?.[0] ??
		source.toLocaleLowerCase().indexOf(query.toLocaleLowerCase());
	const start = Math.max(0, (first < 0 ? 0 : first) - 80);
	const end = Math.min(source.length, start + 260);
	return (
		(start ? '…' : '') +
		source.slice(start, end).trim() +
		(end < source.length ? '…' : '')
	);
};

async function initialize(requestID, options) {
	if (options.serverSearch || serverSearchEnabled) {
		documents = [];
		indexes.clear();
		postMessage({
			type: 'ready',
			requestId: requestID,
			count: 0,
			cached: false,
		});
		return;
	}
	const localeSignature = (options.locales || [])
		.map(normalizeLocale)
		.sort()
		.join(',');
	const key =
		options.origin + (options.basePath || '/') + ':v3:' + localeSignature;
	const cached = options.cacheTtl ? await readCache(key) : undefined;
	if (
		cached?.documents?.length &&
		Date.now() - cached.updatedAt < options.cacheTtl
	) {
		documents = cached.documents.filter(
			(item) => !/\/(?:404)(?:\.html?)?\/?$/i.test(item.path),
		);
	} else {
		const stream = await fetch('/_gosh/site-search', {
			credentials: 'same-origin',
			headers: { Accept: 'application/x-ndjson' },
		});
		if (stream.ok && stream.body) {
			documents = [];
			const reader = stream.body
				.pipeThrough(new TextDecoderStream())
				.getReader();
			let buffered = '',
				indexed = 0;
			for (;;) {
				const { value, done } = await reader.read();
				buffered += value || '';
				const lines = buffered.split('\n');
				buffered = lines.pop();
				for (const line of lines) {
					if (!line.trim()) continue;
					const payload = JSON.parse(line);
					const address = new URL(payload.path || '/', options.origin)
						.href;
					const item = extractDocument(
						payload,
						address,
						options,
						200,
					);
					if (item) documents.push(item);
					indexed++;
					postMessage({ type: 'progress', indexed, total: indexed });
				}
				if (done) break;
			}
			if (buffered.trim()) {
				const payload = JSON.parse(buffered);
				const item = extractDocument(
					payload,
					new URL(payload.path || '/', options.origin).href,
					options,
					200,
				);
				if (item) documents.push(item);
			}
			if (documents.length && options.cacheTtl)
				await writeCache({ key, updatedAt: Date.now(), documents });
			indexes.clear();
			postMessage({
				type: 'ready',
				requestId: requestID,
				count: documents.length,
				cached: false,
			});
			return;
		}
		const queue = [
			...new Set(
				options.seedUrls.map(
					(path) => new URL(path, options.origin).href,
				),
			),
		];
		const seen = new Set(queue);
		documents = [];
		for (
			let index = 0;
			index < queue.length && index < options.maxPages;
			index++
		) {
			try {
				const address = queue[index];
				const response = await fetch(address, {
					credentials: 'same-origin',
					headers: {
						Accept: 'application/json',
						'X-GOSH-Site-Search': '1',
					},
				});
				if (response.status === 404 || !response.ok) continue;
				const contentType = response.headers.get('content-type') || '';
				if (!contentType.includes('json')) continue;
				const payload = await response.json();
				const item = extractDocument(
					payload,
					address,
					options,
					response.status,
				);
				if (item) documents.push(item);
				for (const href of payload.links || []) {
					let next = '';
					try {
						const url = new URL(href, address);
						url.hash = '';
						next = indexableURL(url.href, options.origin)
							? url.href
							: '';
					} catch (_) {}
					if (!seen.has(next) && queue.length < options.maxPages) {
						seen.add(next);
						queue.push(next);
					}
				}
			} finally {
				if (index % 4 === 0 || index + 1 === queue.length)
					postMessage({
						type: 'progress',
						indexed: index + 1,
						total: queue.length,
					});
			}
		}
		if (documents.length && options.cacheTtl)
			await writeCache({ key, updatedAt: Date.now(), documents });
	}
	indexes.clear();
	postMessage({
		type: 'ready',
		requestId: requestID,
		count: documents.length,
		cached: Boolean(cached),
	});
}

function search(
	requestID,
	query,
	requestedLocale,
	allLocales,
	operator,
	limit,
) {
	const localeKey = allLocales ? '*' : normalizeLocale(requestedLocale);
	const key = localeKey + ':' + operator;
	let fuse = indexes.get(key);
	if (!fuse) {
		const canonical = (item) => {
			const parts = item.path.split('/').filter(Boolean);
			return (
				'/' +
				(normalizeLocale(parts[0]) === normalizeLocale(item.locale)
					? parts.slice(1)
					: parts
				).join('/')
			);
		};
		const source =
			localeKey === '*'
				? documents
				: documents.filter(
						(item) =>
							normalizeLocale(item.locale) === localeKey ||
							!documents.some(
								(other) =>
									normalizeLocale(other.locale) ===
										localeKey &&
									canonical(other) === canonical(item),
							),
					);
		fuse = new Fuse(source, {
			keys: [
				{ name: 'title', weight: 4 },
				{ name: 'headings', weight: 2.5 },
				{ name: 'description', weight: 2 },
				{ name: 'content', weight: 1 },
				{ name: 'path', weight: 0.35 },
			],
			includeMatches: true,
			includeScore: true,
			ignoreDiacritics: true,
			ignoreLocation: true,
			minMatchCharLength: 2,
			threshold: 0.34,
		});
		indexes.set(key, fuse);
	}
	const words =
		query.toLocaleLowerCase().match(/[\p{L}\p{M}\p{N}_]+/gu) || [];
	const results = fuse
		.search(query, { limit: Math.max(100, limit) })
		.filter((result) => {
			if (/\/(?:404)(?:\.html?)?\/?$/i.test(result.item.path))
				return false;
			const text = (
				result.item.title +
				' ' +
				result.item.headings +
				' ' +
				result.item.description +
				' ' +
				result.item.content
			).toLocaleLowerCase();
			const matched = words.filter((word) => text.includes(word)).length;
			return operator === 'and' ? matched === words.length : matched > 0;
		})
		.slice(0, Math.max(1, limit))
		.map((result) => ({
			id: result.item.id,
			url: result.item.url,
			path: result.item.path,
			title: result.item.title,
			description: result.item.description,
			snippet: snippetFor(result.item, query, result.matches),
			locale: result.item.locale,
			score: result.score ?? 1,
		}));
	postMessage({ type: 'results', requestId: requestID, results });
}

self.addEventListener('message', (event) => {
	const request = event.data;
	if (request.type === 'init')
		initialize(request.requestId, request.options).catch((error) =>
			postMessage({
				type: 'error',
				requestId: request.requestId,
				message: String(error.message || error),
			}),
		);
	else if (request.type === 'search') {
		if (serverSearchEnabled) searchServer(request);
		else
			try {
				search(
					request.requestId,
					request.query,
					request.locale,
					request.allLocales,
					request.operator,
					request.limit,
				);
			} catch (error) {
				postMessage({
					type: 'error',
					requestId: request.requestId,
					message: String(error.message || error),
				});
			}
	}
});

function searchServer(request) {
	const params = new URLSearchParams({
		q: request.query,
		locale: request.locale || '',
		operator: request.operator || 'or',
		limit: String(request.limit || 10),
	});
	fetch('/_gosh/site-search/query?' + params, {
		credentials: 'same-origin',
		headers: { Accept: 'application/json' },
	})
		.then((response) => (response.ok ? response.json() : null))
		.then((payload) => {
			if (payload?.results)
				postMessage({
					type: 'results',
					requestId: request.requestId,
					results: payload.results,
				});
		})
		.catch(() => {});
}
