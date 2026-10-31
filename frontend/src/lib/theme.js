import { get, writable } from 'svelte/store';

const STORAGE_KEY = 'flashroute.theme';
const ACCENT_KEY = 'flashroute.accent';
const MODES = ['system', 'dark', 'light'];
const ACCENTS = ['green', 'blue', 'violet', 'amber', 'rose'];

function initialMode() {
	if (typeof localStorage === 'undefined') return 'system';
	const stored = localStorage.getItem(STORAGE_KEY);
	return MODES.includes(stored) ? stored : 'system';
}

function initialAccent() {
	if (typeof localStorage === 'undefined') return 'green';
	const stored = localStorage.getItem(ACCENT_KEY);
	return ACCENTS.includes(stored) ? stored : 'green';
}

export const themeMode = writable(initialMode());
export const accentTheme = writable(initialAccent());

function systemTheme() {
	if (typeof window === 'undefined' || !window.matchMedia) return 'dark';
	return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function resolvedTheme(mode) {
	return mode === 'system' ? systemTheme() : mode;
}

function apply(mode) {
	if (typeof document === 'undefined') return;
	const resolved = resolvedTheme(mode);
	document.documentElement.dataset.theme = resolved;
	document.documentElement.style.colorScheme = resolved;
	const meta = document.querySelector('meta[name="theme-color"]');
	if (meta) meta.setAttribute('content', resolved === 'dark' ? '#070b10' : '#f4f7fb');
}

themeMode.subscribe((mode) => {
	if (typeof localStorage !== 'undefined') localStorage.setItem(STORAGE_KEY, mode);
	apply(mode);
});

accentTheme.subscribe((accent) => {
	if (typeof localStorage !== 'undefined') localStorage.setItem(ACCENT_KEY, accent);
	if (typeof document !== 'undefined') document.documentElement.dataset.accent = accent;
});

if (typeof window !== 'undefined' && window.matchMedia) {
	const query = window.matchMedia('(prefers-color-scheme: dark)');
	query.addEventListener?.('change', () => {
		if (get(themeMode) === 'system') apply('system');
	});
}

export function setTheme(mode) {
	if (MODES.includes(mode)) themeMode.set(mode);
}

export function cycleTheme() {
	themeMode.update((mode) => {
		const index = MODES.indexOf(mode);
		return MODES[(index + 1) % MODES.length];
	});
}

export const themeChoices = [
	{ id: 'system', icon: 'monitor' },
	{ id: 'dark', icon: 'moon' },
	{ id: 'light', icon: 'sun' }
];

export const accentChoices = [
	{ id: 'green', color: '#34d399', label: 'theme.accentGreen' },
	{ id: 'blue', color: '#60a5fa', label: 'theme.accentBlue' },
	{ id: 'violet', color: '#a78bfa', label: 'theme.accentViolet' },
	{ id: 'amber', color: '#fbbf24', label: 'theme.accentAmber' },
	{ id: 'rose', color: '#fb7185', label: 'theme.accentRose' }
];
