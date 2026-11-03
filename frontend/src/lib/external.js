export function openExternal(url) {
	if (!url || typeof window === 'undefined') return;
	if (window.runtime?.BrowserOpenURL) {
		window.runtime.BrowserOpenURL(url);
		return;
	}
	window.open(url, '_blank', 'noopener,noreferrer');
}

export function isExternalURL(href) {
	try {
		const url = new URL(href, window.location.href);
		return (url.protocol === 'http:' || url.protocol === 'https:') && url.origin !== window.location.origin;
	} catch {
		return false;
	}
}
