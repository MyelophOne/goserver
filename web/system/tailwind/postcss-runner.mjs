import { readFile, writeFile } from "node:fs/promises";
import postcss from "postcss";
import config from "./postcss.config.mjs";

const [inputPath, outputPath] = process.argv.slice(2);
if (!inputPath || !outputPath) {
	throw new Error("usage: node postcss-runner.mjs <input.css> <output.css>");
}

const css = await readFile(inputPath, "utf8");
const result = await postcss(config.plugins).process(css, {
	from: inputPath,
	to: outputPath,
});
await writeFile(outputPath, result.css);
