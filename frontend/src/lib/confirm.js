import { writable } from 'svelte/store';

export const confirmState = writable(null);

let nextID = 1;

export function confirmDialog({ title = '', message, confirmLabel = '', cancelLabel = '', danger = true }) {
	return new Promise((resolve) => {
		confirmState.set({ id: nextID++, title, message, confirmLabel, cancelLabel, danger, resolve });
	});
}

export function resolveConfirm(value) {
	let resolve = null;
	confirmState.update((state) => {
		resolve = state?.resolve || null;
		return null;
	});
	resolve?.(value);
}
