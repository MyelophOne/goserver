function now() {
	return Math.round(performance.now());
}

function observe(type, callback, options) {
	if (typeof PerformanceObserver !== 'function') return null;
	try {
		var observer = new PerformanceObserver(function (list) {
			callback(list.getEntries());
		});
		observer.observe(
			Object.assign({ type: type, buffered: true }, options || {}),
		);
		return observer;
	} catch (_) {
		return null;
	}
}

export function start(report) {
	if (typeof report !== 'function' || !window.performance)
		return function () {};
	var stopped = false;
	var cleanup = [];
	function send(name, value, rating) {
		if (!stopped && Number.isFinite(value))
			report({
				name: name,
				value: Math.round(value),
				rating: rating || 'info',
				navigationType:
					performance.getEntriesByType &&
					performance.getEntriesByType('navigation')[0]
						? performance.getEntriesByType('navigation')[0].type
						: 'navigate',
			});
	}
	function onHidden(fn) {
		var done = false;
		function finish() {
			if (!done) {
				done = true;
				fn();
			}
		}
		addEventListener(
			'visibilitychange',
			function () {
				if (document.visibilityState === 'hidden') finish();
			},
			{ once: true },
		);
		addEventListener('pagehide', finish, { once: true });
	}

	var lcp = 0;
	var lcpObserver = observe('largest-contentful-paint', function (entries) {
		lcp = entries.length ? entries[entries.length - 1].startTime : lcp;
		send(
			'LCP',
			lcp,
			lcp <= 2500 ? 'good' : lcp <= 4000 ? 'needs-improvement' : 'poor',
		);
	});
	if (lcpObserver) {
		cleanup.push(function () {
			lcpObserver.disconnect();
		});
		onHidden(function () {
			send(
				'LCP',
				lcp,
				lcp <= 2500
					? 'good'
					: lcp <= 4000
						? 'needs-improvement'
						: 'poor',
			);
		});
	}

	var cls = 0;
	function clsRating() {
		return cls <= 0.1 ? 'good' : cls <= 0.25 ? 'needs-improvement' : 'poor';
	}
	var clsObserver = observe('layout-shift', function (entries) {
		entries.forEach(function (entry) {
			if (!entry.hadRecentInput) cls += entry.value;
		});
		send('CLS', cls * 1000, clsRating());
	});
	if (clsObserver) {
		var clsInitial = setTimeout(function () {
			send('CLS', cls * 1000, clsRating());
		}, 0);
		cleanup.push(function () {
			clearTimeout(clsInitial);
			clsObserver.disconnect();
		});
		onHidden(function () {
			send('CLS', cls * 1000, clsRating());
		});
	}

	var inp = 0;
	var inpObserver = observe(
		'event',
		function (entries) {
			entries.forEach(function (entry) {
				inp = Math.max(inp, entry.duration || 0);
			});
			send(
				'INP',
				inp,
				inp <= 200 ? 'good' : inp <= 500 ? 'needs-improvement' : 'poor',
			);
		},
		{ durationThreshold: 40 },
	);
	if (inpObserver) {
		cleanup.push(function () {
			inpObserver.disconnect();
		});
		onHidden(function () {
			send(
				'INP',
				inp,
				inp <= 200 ? 'good' : inp <= 500 ? 'needs-improvement' : 'poor',
			);
		});
	}

	var fcpObserver = observe('paint', function (entries) {
		entries.forEach(function (entry) {
			if (entry.name === 'first-contentful-paint')
				send(
					'FCP',
					entry.startTime,
					entry.startTime <= 1800
						? 'good'
						: entry.startTime <= 3000
							? 'needs-improvement'
							: 'poor',
				);
		});
	});
	if (fcpObserver)
		cleanup.push(function () {
			fcpObserver.disconnect();
		});

	var navigation =
		performance.getEntriesByType &&
		performance.getEntriesByType('navigation')[0];
	if (navigation)
		send(
			'TTFB',
			navigation.responseStart,
			navigation.responseStart <= 800
				? 'good'
				: navigation.responseStart <= 1800
					? 'needs-improvement'
					: 'poor',
		);
	return function () {
		stopped = true;
		cleanup.forEach(function (fn) {
			fn();
		});
	};
}
