<script>
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import Icon from './Icon.svelte';

	export let provider;

	let loading = true;
	let data = null;
	let error = '';

	$: supported = /deepseek\.com|openrouter\.ai/i.test(provider.base_url || '');
	$: symbol = data?.currency === 'CNY' ? '¥' : data?.currency === 'USD' ? '$' : '';
	$: label = data?.total ? `${symbol}${data.total}` : data?.usage ? `${$t('providers.used')} ${symbol}${data.usage}` : '';

	onMount(() => {
		if (supported) load();
		else loading = false;
	});

	async function load() {
		loading = true;
		error = '';
		try {
			data = await api.get(`/api/providers/${provider.id}/balance`);
		} catch (err) {
			error = err.message;
			data = null;
		} finally {
			loading = false;
		}
	}
</script>

{#if supported}
	<span class="balance">
		{#if loading}
			<span class="spinner"></span>
		{:else if label}
			<span class="balance-value">{label}</span>
		{:else}
			<span class="balance-error" title={error}>{error ? '—' : ''}</span>
		{/if}
		<button class="btn btn-ghost btn-icon btn-sm" onclick={load} title={$t('providers.refreshBalance')} aria-label={$t('providers.refreshBalance')}>
			<Icon name="refresh" size={12} />
		</button>
	</span>
{/if}

<style>
	.balance {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
	}
	.balance-value {
		font-size: 11px;
		color: var(--text-soft);
		white-space: nowrap;
	}
	.balance-error {
		font-size: 11px;
		color: var(--muted);
	}
</style>
