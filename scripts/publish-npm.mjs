#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { copyFile, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const npmDir = path.join(root, 'npm');

let version = process.argv[2] || process.env.VERSION;
if (!version) {
	try {
		version = execFileSync('git', ['describe', '--tags', '--exact-match', 'HEAD'], {
			cwd: root,
			encoding: 'utf8'
		}).trim();
	} catch {
		throw new Error('pass a version or run this on an exact git tag');
	}
}
version = version.replace(/^v/, '');
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
	throw new Error(`invalid semver version: ${version}`);
}
if (process.env.GITHUB_REF_NAME && process.env.GITHUB_REF_NAME !== `v${version}`) {
	throw new Error(`version ${version} does not match tag ${process.env.GITHUB_REF_NAME}`);
}

const pkgPath = path.join(npmDir, 'package.json');
const pkg = JSON.parse(await readFile(pkgPath, 'utf8'));
pkg.version = version;
await writeFile(pkgPath, `${JSON.stringify(pkg, null, 2)}\n`);
await copyFile(path.join(root, 'LICENSE'), path.join(npmDir, 'LICENSE'));

const args = ['publish', '--access', 'public'];
if (process.env.GITHUB_ACTIONS) args.push('--provenance');
execFileSync(process.platform === 'win32' ? 'npm.cmd' : 'npm', args, {
	cwd: npmDir,
	stdio: 'inherit',
	env: process.env
});
console.log(`published flashroute@${version}`);
