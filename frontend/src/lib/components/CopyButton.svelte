<script>
	import { copyText } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import Icon from './Icon.svelte';

	export let value = '';
	export let size = 13;
	export let label = '';
	export let buttonClass = 'btn btn-ghost btn-icon btn-sm';

	let copied = false;
	let timer;

	async function handleClick() {
		if (!value) return;
		await copyText(value);
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 1500);
	}
</script>

<button
	type="button"
	class={buttonClass}
	onclick={handleClick}
	title={label || $t('common.copy')}
	aria-label={label || $t('common.copy')}
	disabled={!value}
>
	{#if copied}
		<Icon name="check" {size} />
	{:else}
		<Icon name="copy" {size} />
	{/if}
</button>
