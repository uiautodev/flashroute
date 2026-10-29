#!/usr/bin/env node
'use strict';

const { createHash } = require('node:crypto');
const { createWriteStream } = require('node:fs');
const { chmod, mkdir, readFile, rename, rm } = require('node:fs/promises');
const { spawn } = require('node:child_process');
const os = require('node:os');
const path = require('node:path');
const { pipeline } = require('node:stream/promises');
const { Readable } = require('node:stream');

const pkg = require('../package.json');
const version = pkg.version;

const platforms = {
	darwin: { x64: 'darwin-x86_64', arm64: 'darwin-arm64' },
	linux: { x64: 'linux-x86_64', arm64: 'linux-arm64' },
	win32: { x64: 'windows-amd64' }
};

const arch = platforms[process.platform]?.[process.arch];
if (!arch) {
	console.error(`flashroute: unsupported platform ${process.platform}/${process.arch}`);
	process.exit(1);
}
if (!/^\d+\.\d+\.\d+/.test(version)) {
	console.error(`flashroute: invalid package version ${version}`);
	process.exit(1);
}

const ext = process.platform === 'win32' ? '.exe' : '';
const asset = `flashroute-server-${arch}-${version}${ext}`;
const tag = `v${version}`;
const baseURL =
	process.env.FLASHROUTE_RELEASE_BASE_URL || `https://github.com/uiautodev/flashroute/releases/download/${tag}`;
const cacheRoot =
	process.env.FLASHROUTE_CACHE_DIR ||
	(process.platform === 'win32'
		? path.join(process.env.LOCALAPPDATA || os.homedir(), 'flashroute')
		: path.join(os.homedir(), '.cache', 'flashroute'));
const binaryPath = path.join(cacheRoot, 'bin', version, asset);

async function download(url, destination, checksum) {
	const response = await fetch(url);
	if (!response.ok) {
		throw new Error(`download failed (${response.status}) for ${url}`);
	}
	await mkdir(path.dirname(destination), { recursive: true });
	const temporary = `${destination}.tmp-${process.pid}`;
	await pipeline(Readable.fromWeb(response.body), createWriteStream(temporary));
	if (checksum) {
		const actual = createHash('sha256').update(await readFile(temporary)).digest('hex');
		if (actual !== checksum) {
			await rm(temporary, { force: true });
			throw new Error(`checksum mismatch for ${asset}`);
		}
	}
	await rename(temporary, destination);
	await chmod(destination, 0o755);
}

async function ensureBinary() {
	try {
		await readFile(binaryPath);
		return binaryPath;
	} catch {
		// Download below.
	}
	const sumsResponse = await fetch(`${baseURL}/SHA256SUMS`);
	if (!sumsResponse.ok) {
		throw new Error(`could not fetch checksums (${sumsResponse.status}) for ${tag}`);
	}
	const sums = await sumsResponse.text();
	const line = sums.split('\n').find((item) => item.trim().split(/\s+/).at(-1) === asset);
	if (!line) {
		throw new Error(`no checksum found for ${asset} in ${tag}`);
	}
	const checksum = line.trim().split(/\s+/)[0];
	console.error(`flashroute: downloading ${asset}...`);
	await download(`${baseURL}/${asset}`, binaryPath, checksum);
	return binaryPath;
}

async function main() {
	let binary;
	try {
		binary = await ensureBinary();
	} catch (error) {
		console.error(`flashroute: ${error.message}`);
		process.exit(1);
	}
	const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });
	for (const signal of ['SIGINT', 'SIGTERM']) {
		process.on(signal, () => child.kill(signal));
	}
	child.on('exit', (code, signal) => {
		if (signal) process.kill(process.pid, signal);
		else process.exit(code ?? 1);
	});
}

main();
