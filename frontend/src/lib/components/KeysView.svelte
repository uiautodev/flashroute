<script>
	import { onMount } from 'svelte';
	import { api, formatTime } from '#lib/api.js';
	import { t } from '#lib/i18n.js';
	import { toast } from '#lib/toast.js';
	import Icon from './Icon.svelte';
	import CopyButton from './CopyButton.svelte';
	import Modal from './Modal.svelte';

	let keys = [];
	let loading = true;
	let error = '';
	let showModal = false;
	let name = '';
	let creating = false;

	onMount(load);

	async function load() {
		loading = true;
		error = '';
		try {
			const data = await api.get('/api/keys');
			keys = data.keys || [];
		} catch (err) {
			error = err.message;
		} finally {
			loading = false;
		}
	}

	function openCreate() {
		name = '';
		showModal = true;
	}

	async function create() {
		creating = true;
		try {
			await api.post('/api/keys', { name });
			showModal = false;
			toast($t('common.save'), 'success');
			await load();
		} catch (err) {
			toast(err.message, 'error');
		} finally {
			creating = false;
		}
	}

	async function remove(key) {
		if (!confirm($t('keys.deleteConfirm', { name: key.name }))) return;
		try {
			await api.del('/api/keys/' + key.id);
			toast($t('common.delete'), 'success');
			await load();
		} catch (err) {
			toast(err.message, 'error');
		}
	}

	function maskKey(value) {
		const key = String(value || '');
		if (!key) return '';
		if (key.length <= 10) return key.slice(0, 2) + '****' + key.slice(-2);
		return key.slice(0, 6) + '****' + key.slice(-4);
	}

	$: endpoint = typeof location !== 'undefined' ? location.origin + '/v1' : '/v1';
	$: example = `curl ${endpoint}/chat/completions \\\n  -H "Authorization: Bearer $FLASHROUTE_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"grounder","messages":[{"role":"user","content":"hello"}]}'`;
	$: tsExample = `import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: '${endpoint}',
  apiKey: process.env.FLASHROUTE_API_KEY!
});

const res = await client.chat.completions.create({
  model: 'grounder',
  messages: [{ role: 'user', content: 'hello' }]
});

console.log(res.choices[0].message.content);`;
	$: pyExample = `import os
from openai import OpenAI

client = OpenAI(
    base_url="${endpoint}",
    api_key=os.environ["FLASHROUTE_API_KEY"],
)

res = client.chat.completions.create(
    model="grounder",
    messages=[{"role": "user", "content": "hello"}],
)

print(res.choices[0].message.content)`;
</script>

<div class="keys-layout">
	<div class="keys-main">
		<div class="page-toolbar">
			<button class="btn btn-primary" onclick={openCreate}><Icon name="plus" size={15} />{$t('keys.create')}</button>
		</div>
		{#if loading}
			<div class="card card-pad"><div class="skeleton" style="width:45%"></div></div>
			<div class="card card-pad"><div class="skeleton" style="width:60%"></div></div>
		{:else if error}
			<div class="empty"><span class="empty-text">{error}</span><button class="btn" onclick={load}>{$t('common.refresh')}</button></div>
		{:else if keys.length === 0}
			<div class="empty">
				<div class="empty-icon"><Icon name="key" size={23} /></div>
				<h2 class="empty-title">{$t('keys.emptyTitle')}</h2>
				<p class="empty-text">{$t('keys.emptyBody')}</p>
				<button class="btn btn-primary" onclick={openCreate}><Icon name="plus" size={15} />{$t('keys.create')}</button>
			</div>
		{:else}
			<div class="key-list">
				{#each keys as key (key.id)}
					<article class="card key-card">
						<div class="key-head">
							<div class="key-identity">
								<div class="key-avatar"><Icon name="key" size={16} /></div>
								<div>
									<div class="key-name">{key.name}</div>
									<div class="key-date">{$t('keys.created', { time: formatTime(key.created_at) })}</div>
								</div>
							</div>
							<div class="row">
								<button class="btn btn-ghost btn-icon" onclick={() => remove(key)} title={$t('common.delete')}>
									<Icon name="trash" size={15} />
								</button>
							</div>
						</div>
						<div class="key-row">
							<code class="key-line">{maskKey(key.key)}</code>
							<CopyButton value={key.key} />
						</div>
					</article>
				{/each}
			</div>
		{/if}
	</div>

	<aside class="card usage-card">
		<div class="usage-head">
			<div class="empty-icon" style="width:38px;height:38px;border-radius:10px"><Icon name="zap" size={18} /></div>
			<div>
				<h2>{$t('keys.usageTitle')}</h2>
				<p>{$t('keys.usageHint')}</p>
			</div>
		</div>
		<div class="field">
			<span class="field-label">{$t('keys.endpoint')}</span>
			<div class="endpoint">
				<code>{endpoint}</code>
				<CopyButton value={endpoint} />
			</div>
		</div>
		{#if example}
			<div class="field">
				<span class="field-label">cURL</span>
				<div class="code-block">{example}<CopyButton value={example} buttonClass="btn btn-ghost btn-icon btn-sm code-copy" /></div>
			</div>
			<div class="field">
				<span class="field-label">{$t('keys.openaiTs')}</span>
				<div class="code-block">{tsExample}<CopyButton value={tsExample} buttonClass="btn btn-ghost btn-icon btn-sm code-copy" /></div>
			</div>
			<div class="field">
				<span class="field-label">{$t('keys.openaiPy')}</span>
				<div class="code-block">{pyExample}<CopyButton value={pyExample} buttonClass="btn btn-ghost btn-icon btn-sm code-copy" /></div>
			</div>
		{/if}
		<div class="usage-note"><Icon name="shield" size={13} /><span>{$t('keys.visibility')}</span></div>
	</aside>
</div>

{#if showModal}
	<Modal title={$t('keys.create')} narrow onClose={() => (showModal = false)}>
		<div class="field">
			<label for="key-name">{$t('keys.name')}</label>
			<input
				id="key-name"
				class="input"
				bind:value={name}
				placeholder={$t('keys.namePlaceholder')}
				onkeydown={(e) => {
					if (e.key === 'Enter') create();
				}}
			/>
			<span class="hint">{$t('keys.subtitle')}</span>
		</div>
		<div slot="footer" class="modal-foot">
			<button class="btn" onclick={() => (showModal = false)}>{$t('common.cancel')}</button>
			<button class="btn btn-primary" onclick={create} disabled={creating}>
				{#if creating}<span class="spinner"></span>{/if}
				{$t('keys.create')}
			</button>
		</div>
	</Modal>
{/if}

<style>
	.keys-layout {
		display: grid;
		grid-template-columns: minmax(0, 55fr) minmax(0, 45fr);
		gap: 18px;
		align-items: start;
	}
	.keys-main {
		min-width: 0;
	}
	.key-list {
		display: flex;
		flex-direction: column;
		gap: 11px;
	}
	.key-card {
		padding: 14px 16px;
		display: flex;
		flex-direction: column;
		gap: 11px;
	}
	.key-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.key-identity {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.key-avatar {
		width: 32px;
		height: 32px;
		border-radius: 9px;
		display: grid;
		place-items: center;
		background: var(--accent-soft);
		color: var(--accent);
		border: 1px solid rgba(52, 211, 153, 0.14);
	}
	.key-name {
		font-size: 13px;
		font-weight: 650;
	}
	.key-date {
		font-size: 10px;
		color: var(--muted);
		margin-top: 2px;
	}
	.key-row {
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.key-row .key-line {
		flex: 1;
		min-width: 0;
	}
	.key-line {
		display: block;
		font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
		font-size: 11.5px;
		color: var(--key-text);
		background: var(--surface-code);
		border: 1px solid var(--line-soft);
		border-radius: 8px;
		padding: 9px 11px;
		overflow-wrap: anywhere;
		letter-spacing: 0.01em;
	}
	.usage-card {
		padding: 18px;
		display: flex;
		flex-direction: column;
		gap: 17px;
		position: sticky;
		top: 90px;
	}
	.usage-head {
		display: flex;
		gap: 11px;
		align-items: flex-start;
	}
	.usage-head h2 {
		font-size: 14px;
		font-weight: 680;
	}
	.usage-head p {
		font-size: 11px;
		color: var(--muted);
		line-height: 1.5;
		margin-top: 4px;
	}
	.endpoint {
		display: flex;
		align-items: center;
		gap: 4px;
		border: 1px solid var(--line-soft);
		border-radius: 8px;
		background: var(--surface-code);
		padding: 4px 4px 4px 10px;
	}
	.endpoint code {
		flex: 1;
		font-size: 10.5px;
		color: var(--text-soft);
		overflow-wrap: anywhere;
	}
	.usage-note {
		display: flex;
		gap: 7px;
		align-items: flex-start;
		font-size: 10.5px;
		color: var(--muted);
		line-height: 1.45;
		padding-top: 4px;
		border-top: 1px solid var(--line-soft);
	}
	@media (max-width: 1050px) {
		.keys-layout {
			grid-template-columns: 1fr;
		}
		.usage-card {
			position: static;
		}
	}
</style>
