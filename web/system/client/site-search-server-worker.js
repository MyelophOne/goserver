self.addEventListener('message', (event) => {
	const request = event.data;
	if (request.type === 'init') {
		postMessage({
			type: 'ready',
			requestId: request.requestId,
			count: 0,
			cached: false,
		});
		return;
	}
	if (request.type !== 'search') return;
	const params = new URLSearchParams({
		q: request.query || '',
		locale: request.locale || '',
		operator: request.operator || 'or',
		limit: String(request.limit || 10),
	});
	fetch('/_gosh/site-search/query?' + params, {
		credentials: 'same-origin',
		headers: { Accept: 'application/json' },
	})
		.then((response) =>
			response.ok
				? response.json()
				: Promise.reject(new Error('search unavailable')),
		)
		.then((payload) =>
			postMessage({
				type: 'results',
				requestId: request.requestId,
				results: payload.results || [],
			}),
		)
		.catch((error) =>
			postMessage({
				type: 'error',
				requestId: request.requestId,
				message: String(error.message || error),
			}),
		);
});
