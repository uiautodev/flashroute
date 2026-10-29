export class ApiError extends Error {
	constructor(message, status) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
}

async function request(path, options = {}) {
	const opts = { credentials: 'same-origin', ...options, headers: { ...(options.headers || {}) } };
	if (opts.body && typeof opts.body !== 'string') {
		opts.headers['Content-Type'] = 'application/json';
		opts.body = JSON.stringify(opts.body);
	}
	let res;
	try {
		res = await fetch(path, opts);
	} catch (err) {
		throw new ApiError(err.message || 'Network error', 0);
	}
	const contentType = res.headers.get('content-type') || '';
	const payload = contentType.includes('application/json') ? await res.json().catch(() => null) : await res.text();
	if (!res.ok) {
		const message = payload?.error || payload?.message || (typeof payload === 'string' ? payload : '') || `HTTP ${res.status}`;
		throw new ApiError(message, res.status);
	}
	return payload;
}

export const api = {
	get: (path) => request(path),
	post: (path, body) => request(path, { method: 'POST', body }),
	stream: (path, body, onEvent) => streamJSON(path, body, onEvent),
	patch: (path, body) => request(path, { method: 'PATCH', body }),
	del: (path) => request(path, { method: 'DELETE' })
};


async function streamJSON(path, body, onEvent) {
	let res;
	try {
		res = await fetch(path, {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(body)
		});
	} catch (err) {
		throw new ApiError(err.message || 'Network error', 0);
	}
	if (!res.ok) {
		const contentType = res.headers.get('content-type') || '';
		const payload = contentType.includes('application/json') ? await res.json().catch(() => null) : await res.text();
		const message = payload?.error || payload?.message || (typeof payload === 'string' ? payload : '') || `HTTP ${res.status}`;
		throw new ApiError(message, res.status);
	}
	if (!res.body) throw new ApiError('Streaming is not supported by this browser', 0);

	const reader = res.body.getReader();
	const decoder = new TextDecoder();
	let buffer = '';
	let done = false;

	const dispatch = (block) => {
		let event = 'message';
		const dataLines = [];
		for (const rawLine of block.split(/\r?\n/)) {
			const line = rawLine.trimEnd();
			if (line.startsWith('event:')) event = line.slice(6).trim();
			else if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart());
		}
		if (dataLines.length === 0) return;
		const raw = dataLines.join('\n');
		if (raw === '[DONE]') return;
		let payload = raw;
		try {
			payload = JSON.parse(raw);
		} catch {
			// Keep plain-text SSE payloads as-is.
		}
		if (event === 'error') {
			throw new ApiError(payload?.error || payload?.message || 'Stream request failed', 0);
		}
		if (event === 'done') done = true;
		onEvent(event, payload);
	};

	while (true) {
		const { value, done: finished } = await reader.read();
		if (finished) break;
		buffer += decoder.decode(value, { stream: true });
		let index;
		while ((index = buffer.indexOf('\n\n')) >= 0) {
			const block = buffer.slice(0, index);
			buffer = buffer.slice(index + 2);
			dispatch(block);
		}
	}
	buffer += decoder.decode();
	if (buffer.trim()) dispatch(buffer);
	if (!done) throw new ApiError('Stream ended unexpectedly', 0);
}

export async function copyText(text) {
	if (navigator.clipboard?.writeText) {
		await navigator.clipboard.writeText(text);
		return;
	}
	const textarea = document.createElement('textarea');
	textarea.value = text;
	textarea.style.position = 'fixed';
	textarea.style.opacity = '0';
	document.body.appendChild(textarea);
	textarea.select();
	document.execCommand('copy');
	textarea.remove();
}

export function formatTime(value) {
	if (!value) return '';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value;
	return date.toLocaleString();
}

export function relativeTime(value) {
	if (!value) return '';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value;
	const diff = Date.now() - date.getTime();
	const minutes = Math.floor(diff / 60000);
	if (minutes < 1) return 'just now';
	if (minutes < 60) return `${minutes}m ago`;
	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours}h ago`;
	const days = Math.floor(hours / 24);
	if (days < 7) return `${days}d ago`;
	return date.toLocaleDateString();
}
