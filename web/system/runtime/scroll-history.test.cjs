const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const test = require("node:test");

function fixture() {
	const source = fs.readFileSync(__dirname + "/runtime.js", "utf8");
	const start = source.indexOf("Runtime.prototype.updateScrollHistory =");
	const end = source.indexOf(
		"Runtime.prototype.restoreInitialScroll =",
		start,
	);
	const timers = new Map();
	let nextTimer = 0;
	let writes = 0;
	const context = {
		Runtime: function () {},
		STATE_KEY: "runtime",
		uid: () => "new-id",
		location: { href: "https://example.test/" },
		history: {
			state: {
				other: "preserved",
				runtime: {
					id: "first",
					url: "https://example.test/",
					x: 0,
					y: 0,
				},
			},
			replaceState(state) {
				this.state = state;
				writes++;
			},
		},
		setTimeout(callback, delay) {
			assert.equal(delay, 250);
			timers.set(++nextTimer, callback);
			return nextTimer;
		},
		clearTimeout(id) {
			timers.delete(id);
		},
	};
	vm.runInNewContext(source.slice(start, end), context);
	const runtime = new context.Runtime();
	runtime.scrollPosition = { x: 0, y: 0 };
	return {
		runtime,
		context,
		timers,
		writes: () => writes,
		flush() {
			const callbacks = [...timers.values()];
			timers.clear();
			callbacks.forEach((callback) => callback());
		},
	};
}

test("frame-by-frame scroll writes are coalesced and unchanged positions skipped", () => {
	const f = fixture();
	for (let y = 1; y <= 1000; y++) {
		f.runtime.scrollPosition.y = y;
		f.runtime.updateScrollHistory(true);
	}
	assert.equal(f.writes(), 0);
	assert.equal(f.timers.size, 1);
	f.flush();
	assert.equal(f.writes(), 1);
	assert.equal(f.context.history.state.runtime.y, 1000);
	assert.equal(f.context.history.state.other, "preserved");
	f.runtime.updateScrollHistory();
	assert.equal(f.writes(), 1);
});

test("immediate save flushes latest position and cancels pending write", () => {
	const f = fixture();
	f.runtime.scrollPosition.y = 42;
	f.runtime.updateScrollHistory(true);
	f.runtime.scrollPosition.y = 43;
	f.runtime.updateScrollHistory();
	assert.equal(f.context.history.state.runtime.y, 43);
	assert.equal(f.timers.size, 0);
	f.flush();
	assert.equal(f.writes(), 1);
});

test("pending scroll cannot overwrite a different history entry, even at the same URL", () => {
	const f = fixture();
	f.runtime.scrollPosition.y = 42;
	f.runtime.updateScrollHistory(true);
	f.context.history.state = { runtime: { id: "second", x: 0, y: 7 } };
	f.flush();
	assert.equal(f.writes(), 0);
	assert.equal(f.context.history.state.runtime.y, 7);
});

test("pending scroll cannot write after URL changes", () => {
	const f = fixture();
	f.runtime.scrollPosition.y = 42;
	f.runtime.updateScrollHistory(true);
	f.context.location.href += "next";
	f.flush();
	assert.equal(f.writes(), 0);
});
