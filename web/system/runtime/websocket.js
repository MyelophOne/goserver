const connections = new Map();

function endpoint(value) {
	if (/^wss?:\/\//i.test(value)) return value;
	const url = new URL(value || '/ws', location.href);
	url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
	return url.href;
}

function keyFor(options) {
	return endpoint(options.url) + '|' + options.channel;
}

function create(options) {
	const listeners = new Set();
	let socket = null;
	let timer = 0;
	let attempts = 0;
	let closed = false;
	let status = 'idle';

	const state = {
		get status() {
			return status;
		},
		get connected() {
			return status === 'open';
		},
		get socket() {
			return socket;
		},
		subscribe(listener) {
			listeners.add(listener);
			listener(state);
			return () => listeners.delete(listener);
		},
		send(value) {
			if (!socket || socket.readyState !== WebSocket.OPEN) return false;
			socket.send(options.encode(value));
			return true;
		},
		reconnect() {
			closeSocket();
			closed = false;
			connect();
		},
		close() {
			closed = true;
			clearTimeout(timer);
			closeSocket();
			update('closed');
		},
		release() {
			state.close();
			connections.delete(keyFor(options));
		},
	};

	function update(next, detail) {
		status = next;
		listeners.forEach((listener) => listener(state, detail));
		if (typeof options.onStatus === 'function')
			options.onStatus(next, state, detail);
	}
	function closeSocket() {
		if (socket) {
			socket.onopen =
				socket.onmessage =
				socket.onerror =
				socket.onclose =
					null;
			try {
				socket.close();
			} catch (_) {}
			socket = null;
		}
	}
	function schedule(event) {
		if (closed || options.reconnect === false) return;
		const base = Math.max(0, Number(options.reconnectDelay) || 500);
		const cap = Math.max(base, Number(options.maxReconnectDelay) || 10000);
		const delay = Math.min(cap, base * Math.pow(2, attempts++));
		update('reconnecting', event);
		timer = setTimeout(connect, delay);
	}
	function connect() {
		if (closed || typeof WebSocket !== 'function') {
			update('unsupported');
			return;
		}
		clearTimeout(timer);
		update('connecting');
		try {
			socket = new WebSocket(endpoint(options.url), options.protocols);
		} catch (error) {
			update('error', error);
			schedule(error);
			return;
		}
		socket.onopen = (event) => {
			attempts = 0;
			socket.send(options.channel);
			update('open', event);
			if (typeof options.onOpen === 'function')
				options.onOpen(state, event);
		};
		socket.onmessage = (event) => {
			let data;
			try {
				data = options.decode(event.data);
			} catch (error) {
				if (typeof options.onError === 'function')
					options.onError(error, state);
				return;
			}
			if (typeof options.onMessage === 'function')
				options.onMessage(data, state, event);
		};
		socket.onerror = (event) => {
			update('error', event);
			if (typeof options.onError === 'function')
				options.onError(event, state);
		};
		socket.onclose = (event) => {
			socket = null;
			if (!closed) {
				if (typeof options.onClose === 'function')
					options.onClose(event, state);
				schedule(event);
			}
		};
	}
	connect();
	return state;
}

export function useWebSocket(input) {
	const options = Object.assign(
		{
			url: '/ws',
			channel: '',
			reconnect: true,
			reconnectDelay: 500,
			maxReconnectDelay: 10000,
			json: false,
		},
		input || {},
	);
	if (!options.channel || typeof options.channel !== 'string')
		throw new Error('_gosh.useWebSocket requires a channel');
	options.encode =
		typeof options.encode === 'function'
			? options.encode
			: (value) => (options.json ? JSON.stringify(value) : String(value));
	options.decode =
		typeof options.decode === 'function'
			? options.decode
			: (value) => (options.json ? JSON.parse(value) : value);
	const key = keyFor(options);
	if (!connections.has(key)) connections.set(key, create(options));
	return connections.get(key);
}
