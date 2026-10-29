import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const backend = process.env.FLASHROUTE_BACKEND_URL || 'http://127.0.0.1:8080';

export default defineConfig({
	plugins: [
		sveltekit({
			adapter: adapter({
				pages: 'build',
				assets: 'build',
				fallback: 'index.html',
				precompress: false,
				strict: true
			}),
			paths: {
				relative: false
			}
		})
	],
	server: {
		proxy: {
			'/api': backend,
			'/v1': backend
		}
	}
});
