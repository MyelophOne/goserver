const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const source = fs.readFileSync("web/system/runtime/runtime.js", "utf8");
function runtime(methods, extras = {}) {
	const context = vm.createContext({
		Runtime: function () {},
		URL,
		AbortController,
		console,
		window: { _gosh: { callHook: async () => {} } },
		location: { origin: "http://test", href: "http://test/test" },
		document: {
			baseURI: "http://test/",
			documentElement: { classList: { add() {}, remove() {} } },
			querySelector() {
				return null;
			},
		},
		emit() {},
		isAbort: (e) => e.name === "AbortError",
		...extras,
	});
	for (const method of methods) {
		const start = source.indexOf("    Runtime.prototype." + method + " =");
		const end = source.indexOf("\n    };", start) + "\n    };".length;
		assert.ok(start >= 0 && end > start);
		vm.runInContext(source.slice(start, end), context);
	}
	return new context.Runtime();
}

for (const type of ["application/json", "application/x-ndjson"]) {
	test(`new page lazy ${type} applies HTML and modules after navigation finishes`, async () => {
		const frames = [
			{ v: 1, type: "html", html: "loaded" },
			{ v: 1, type: "modules", plan: { bindings: [] } },
			{ v: 1, type: "end" },
		];
		const applied = [];
		const r = runtime(
			["postRuntimeJSON", "consumeNDJSON", "applyFrame", "isCurrent"],
			{
				V: 1,
				fetch: async () => ({
					ok: true,
					headers: {
						get() {
							return type;
						},
					},
					json: async () => frames[0],
					text: async () => frames.map(JSON.stringify).join("\n"),
				}),
			},
		);
		Object.assign(r, {
			seq: 3,
			active: null,
			beginLoading() {},
			endLoading() {},
			applyHTML(frame) {
				applied.push(frame.html);
			},
			async mountLooseModules() {
				applied.push("modules");
			},
			scanLazy() {},
			scanReveal() {},
			scanDeferredYouTube() {},
			scanDeferredIcons() {},
			bindStoreDOM() {},
		});
		await r.postRuntimeJSON(
			"/_gosh/lazy",
			{},
			{ sequence: 3, signal: new AbortController().signal },
		);
		assert.equal(applied[0], "loaded");
		if (type.includes("ndjson")) assert.ok(applied.includes("modules"));
	});
}

test("initial-page frames stop when a new navigation starts", async () => {
	const r = runtime(["applyFrame", "isCurrent"], { V: 1 });
	let applied = false;
	Object.assign(r, {
		seq: 1,
		active: { sequence: 1 },
		applyHTML() {
			applied = true;
		},
	});
	await r.applyFrame({ v: 1, type: "html" }, 0, 0);
	assert.equal(applied, false);
});
test("navigation aborts old lazy fetch before starting page fetch", async () => {
	const controller = new AbortController();
	const r = runtime(["navigate"]);
	Object.assign(r, {
		seq: 0,
		lazyRequests: new Set([controller]),
		active: null,
		saveScroll() {},
		beginLoading() {},
		endLoading() {},
		cacheKey() {
			return "/";
		},
		cacheGet() {
			return null;
		},
		shouldUseViewTransition() {
			return false;
		},
		isCurrent(seq) {
			return seq === this.seq;
		},
		scanLazy() {},
		async fetchPayload() {
			assert.equal(controller.signal.aborted, true);
			const error = new Error("cancel");
			error.name = "AbortError";
			throw error;
		},
	});
	await r.navigate("http://test/");
	assert.equal(r.active, null);
});
test("late lazy JSON cannot apply after navigation changes generation", async () => {
	let r,
		applied = false;
	r = runtime(["postRuntimeJSON"], {
		fetch: async () => ({
			ok: true,
			headers: {
				get() {
					return "application/json";
				},
			},
			async json() {
				r.seq = 2;
				return { v: 1 };
			},
		}),
	});
	Object.assign(r, {
		seq: 1,
		beginLoading() {},
		endLoading() {},
		async applyFrame() {
			applied = true;
		},
	});
	await r.postRuntimeJSON(
		"/_gosh/lazy",
		{},
		{ sequence: 1, signal: new AbortController().signal },
	);
	assert.equal(applied, false);
});
