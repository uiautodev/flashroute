<script>
	import Icon from './Icon.svelte';

	export let title = '';
	export let narrow = false;
	export let onClose = () => {};
	export let closeOnBackdrop = false;

	function close() {
		onClose();
	}

	function onKeydown(event) {
		if (event.key === 'Escape') close();
	}
</script>

<svelte:window onkeydown={onKeydown} />

<div
	class="modal-backdrop"
	role="presentation"
	onclick={(event) => {
		if (closeOnBackdrop && event.target === event.currentTarget) close();
	}}
>
	<div class="modal" class:narrow role="dialog" aria-modal="true" aria-label={title}>
		<div class="modal-head">
			<h2 class="modal-title">{title}</h2>
			<button class="btn btn-ghost btn-icon" onclick={close} aria-label="Close">
				<Icon name="x" size={17} />
			</button>
		</div>
		<div class="modal-body">
			<slot />
		</div>
		<slot name="footer" />
	</div>
</div>
