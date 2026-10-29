<script>
	import { onMount } from 'svelte';
	import { api } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';

	let enabled = true;
	let rules = [];
	let loading = true;
	let saving = false;
	let testText = '';
	let testOutput = '';
	let testing = false;

	onMount(load);

	async function load() {
		loading = true;
		try {
			const data = await api.get('/api/redact');
			enabled = data.enabled !== false;
			rules = data.rules || [];
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			loading = false;
		}
	}

	function addRule() {
		rules = [...rules, { id: '', name: $t('security.redactNewName'), pattern: '', enabled: true }];
	}

	function removeRule(index) {
		rules = rules.filter((_, i) => i !== index);
	}

	async function save() {
		saving = true;
		try {
			const data = await api.post('/api/redact', { enabled, rules });
			enabled = data.enabled !== false;
			rules = data.rules || [];
			toast($t('security.saved'), 'success');
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			saving = false;
		}
	}

	async function testRules() {
		testing = true;
		try {
			const data = await api.post('/api/redact/test', { text: testText, rules });
			testOutput = data.output ?? '';
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			testing = false;
		}
	}
</script>

<section class="card card-pad">
	<div class="section-head">
		<div class="section-icon"><Icon name="shield" size={17} /></div>
		<div>
			<h2>{$t('security.redactTitle')}</h2>
			<p>{$t('security.redactHint')}</p>
		</div>
	</div>
	<div class="divider"></div>
	<div class="redact-switch">
		<button
			class="switch"
			class:on={enabled}
			onclick={() => (enabled = !enabled)}
			aria-label={$t('security.enabledLabel')}
			aria-pressed={enabled}
		></button>
		<div>
			<span class="switch-label">{$t('security.enabledLabel')}</span>
			<span class="hint">{$t('security.enabledHint')}</span>
		</div>
	</div>
	{#if loading}
		<div class="redact-list">
			<div class="skeleton" style="height:34px"></div>
			<div class="skeleton" style="height:34px"></div>
		</div>
	{:else}
		<div class="redact-head">
			<span>{$t('security.redactName')}</span>
			<span>{$t('security.redactPattern')}</span>
			<span>{$t('common.enabled')}</span>
			<span></span>
		</div>
		<div class="redact-list">
			{#each rules as rule, index (rule.id || index)}
				<div class="redact-row">
					<input class="input" bind:value={rule.name} placeholder={$t('security.redactName')} aria-label={$t('security.redactName')} />
					<input class="input mono" bind:value={rule.pattern} placeholder="sk-[A-Za-z0-9]+" aria-label={$t('security.redactPattern')} />
					<label class="redact-toggle">
						<input type="checkbox" bind:checked={rule.enabled} />
						<span>{$t('common.enabled')}</span>
					</label>
					<button class="btn btn-ghost btn-icon" onclick={() => removeRule(index)} title={$t('common.delete')} aria-label={$t('common.delete')}>
						<Icon name="trash" size={14} />
					</button>
				</div>
			{:else}
				<span class="hint">{$t('security.redactEmpty')}</span>
			{/each}
		</div>
		<div class="redact-actions">
			<button class="btn" onclick={addRule}><Icon name="plus" size={14} />{$t('security.redactAdd')}</button>
			<button class="btn btn-primary" onclick={save} disabled={saving}>
				{#if saving}<span class="spinner"></span>{/if}
				{$t('common.save')}
			</button>
		</div>
		<div class="divider"></div>
		<div class="test-block">
			<label for="redact-test" class="field-label">{$t('security.testLabel')}</label>
			<textarea
				id="redact-test"
				class="textarea mono"
				bind:value={testText}
				placeholder={$t('security.testPlaceholder')}
			></textarea>
			<div class="test-actions">
				<button class="btn" onclick={testRules} disabled={testing}>
					{#if testing}<span class="spinner"></span>{/if}
					{$t('security.testRun')}
				</button>
			</div>
			{#if testOutput}
				<div class="field">
					<span class="field-label">{$t('security.testResult')}</span>
					<div class="code-block test-output">{testOutput}</div>
				</div>
			{/if}
		</div>
	{/if}
</section>

<style>
	.section-head {
		display: flex;
		gap: 11px;
		align-items: flex-start;
	}
	.section-icon {
		width: 34px;
		height: 34px;
		border-radius: 10px;
		display: grid;
		place-items: center;
		background: var(--accent-soft);
		color: var(--accent);
		border: 1px solid rgba(52, 211, 153, 0.14);
		flex: none;
	}
	.section-head h2 {
		font-size: 14px;
		font-weight: 680;
	}
	.section-head p {
		font-size: 11.5px;
		color: var(--muted);
		line-height: 1.55;
		margin-top: 4px;
	}
	.redact-switch {
		display: flex;
		align-items: center;
		gap: 10px;
		margin-bottom: 14px;
	}
	.redact-switch > div {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.switch-label {
		font-size: 12px;
		font-weight: 650;
		color: var(--text-soft);
	}
	.redact-head,
	.redact-row {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 2.2fr) auto auto;
		gap: 8px;
		align-items: center;
	}
	.redact-head {
		font-size: 10px;
		font-weight: 650;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--muted);
		padding: 0 2px 6px;
	}
	.redact-list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.redact-toggle {
		display: flex;
		align-items: center;
		gap: 5px;
		font-size: 11px;
		color: var(--muted);
		white-space: nowrap;
	}
	.redact-actions {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		margin-top: 14px;
	}
	@media (max-width: 720px) {
		.redact-head {
			display: none;
		}
		.redact-row {
			grid-template-columns: 1fr auto;
			padding: 10px;
			border: 1px solid var(--line-soft);
			border-radius: 9px;
			background: var(--surface-inset);
		}
		.redact-row > input {
			grid-column: 1 / -1;
		}
		.redact-actions {
			flex-wrap: wrap;
		}
	}
</style>
