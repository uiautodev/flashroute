<script>
	import { dismissToast, toasts } from '#lib/toast.js';
	import Icon from './Icon.svelte';
</script>

<div class="toast-stack" aria-live="polite">
	{#each $toasts as item (item.id)}
		<div class="toast-item {item.type}">
			<Icon name={item.type === 'error' ? 'alert' : item.type === 'info' ? 'spark' : 'check'} size={15} />
			<span>{item.message}</span>
			<button class="btn btn-ghost btn-icon" style="margin-left:auto;padding:2px" onclick={() => dismissToast(item.id)}>
				<Icon name="x" size={13} />
			</button>
		</div>
	{/each}
</div>
