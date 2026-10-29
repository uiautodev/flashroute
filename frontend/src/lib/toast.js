import { writable } from 'svelte/store';

export const toasts = writable([]);
let nextID = 1;

export function toast(message, type = 'success', timeout = 3200) {
	const id = nextID++;
	toasts.update((items) => [...items, { id, message, type }]);
	setTimeout(() => {
		toasts.update((items) => items.filter((item) => item.id !== id));
	}, timeout);
}

export function dismissToast(id) {
	toasts.update((items) => items.filter((item) => item.id !== id));
}
